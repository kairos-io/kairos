package state

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
)

var _ = Describe("verifyImageFile", func() {
	var img string
	var banners []string
	var haltErrs []error
	content := []byte("pretend this is an ext4 image")

	// settings builds the resolved settings the way ResolveVerifySettings would
	settings := func(a imagesum.Action, require bool) internalUtils.VerifySettings {
		return internalUtils.VerifySettings{Action: a, Require: require, ActionSource: internalUtils.SourceCmdline, RequireSource: internalUtils.SourceCmdline}
	}
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

	BeforeEach(func() {
		img = filepath.Join(GinkgoT().TempDir(), "active.img")
		Expect(os.WriteFile(img, content, 0o644)).To(Succeed())
		banners, haltErrs = nil, nil
		orig := haltWithBannerFn
		haltWithBannerFn = func(banner, _ string, err error) {
			banners = append(banners, ansi.ReplaceAllString(banner, ""))
			haltErrs = append(haltErrs, err)
		}
		DeferCleanup(func() { haltWithBannerFn = orig })
	})

	good := func() string {
		sum, err := imagesum.Generate(vfs.OSFS, img)
		Expect(err).ToNot(HaveOccurred())
		return sum
	}
	// rot changes the content but keeps the size and mtime, like bit rot
	rot := func() {
		info, err := os.Stat(img)
		Expect(err).ToNot(HaveOccurred())
		rotten := append([]byte{}, content...)
		rotten[3] ^= 1
		Expect(os.WriteFile(img, rotten, 0o644)).To(Succeed())
		Expect(os.Chtimes(img, info.ModTime(), info.ModTime())).To(Succeed())
	}
	replaced := func() {
		Expect(os.WriteFile(img, []byte("a different, longer image from an older agent"), 0o644)).To(Succeed())
		future := time.Now().Add(time.Hour)
		Expect(os.Chtimes(img, future, future)).To(Succeed())
	}
	malformed := func() {
		Expect(os.WriteFile(img+".sha256", []byte("garbage"), 0o644)).To(Succeed())
	}

	DescribeTable("a healthy image passes in every action",
		func(a imagesum.Action) {
			good()
			Expect(verifyImageFile(settings(a, true), img, false)).To(Succeed())
			Expect(banners).To(BeEmpty())
		},
		Entry("none", imagesum.None),
		Entry("warn", imagesum.Warn),
		Entry("fail", imagesum.Fail),
	)

	Describe("none", func() {
		It("does not look at anything", func() {
			good()
			rot()
			Expect(verifyImageFile(settings(imagesum.None, true), img, false)).To(Succeed())
			malformed()
			Expect(verifyImageFile(settings(imagesum.None, true), img, false)).To(Succeed())
			Expect(banners).To(BeEmpty())
		})
		It("is what an empty action means", func() {
			good()
			rot()
			Expect(verifyImageFile(settings("", false), img, false)).To(Succeed())
		})
	})

	Describe("warn", func() {
		DescribeTable("logs the problem and lets the boot continue",
			func(break_ func(), require bool) {
				good()
				break_()
				Expect(verifyImageFile(settings(imagesum.Warn, require), img, false)).To(Succeed())
				Expect(banners).To(BeEmpty(), "warn must not paint the failure screen")
			},
			Entry("mismatch", func() { rot() }, false),
			Entry("stale with require", func() { replaced() }, true),
			Entry("malformed", func() { malformed() }, false),
			Entry("missing with require", func() { Expect(imagesum.Remove(vfs.OSFS, img)).To(Succeed()) }, true),
		)
	})

	Describe("fail", func() {
		It("stops on a mismatch and shows the image, the stored and the computed sha256", func() {
			stored := good()
			rot()
			actual, err := imagesum.Compute(vfs.OSFS, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).ToNot(Equal(stored))

			err = verifyImageFile(settings(imagesum.Fail, false), img, false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(img))
			Expect(err.Error()).To(ContainSubstring(stored))
			Expect(err.Error()).To(ContainSubstring(actual))

			Expect(banners).To(HaveLen(1))
			screen := banners[0]
			GinkgoWriter.Println("\n" + screen)
			Expect(screen).To(ContainSubstring("KAIROS BOOT FAILED"))
			Expect(screen).To(ContainSubstring(img))
			Expect(screen).To(ContainSubstring("Expected sha256 (stored in active.img.sha256):\n    " + stored))
			Expect(screen).To(ContainSubstring("Actual sha256 (computed from the image just now):\n    " + actual))
			Expect(haltErrs[0]).To(Equal(err))
		})

		It("stops on a checksum file it cannot read", func() {
			good()
			malformed()
			Expect(verifyImageFile(settings(imagesum.Fail, false), img, false)).To(HaveOccurred())
			Expect(banners).To(HaveLen(1))
			Expect(banners[0]).To(ContainSubstring("malformed checksum file"))
		})

		It("boots an image without a checksum, the old installs rule", func() {
			Expect(verifyImageFile(settings(imagesum.Fail, false), img, false)).To(Succeed())
			Expect(banners).To(BeEmpty())
		})
		It("boots an image with a stale checksum, an older agent replaced it", func() {
			good()
			replaced()
			Expect(verifyImageFile(settings(imagesum.Fail, false), img, false)).To(Succeed())
			Expect(banners).To(BeEmpty())
		})

		It("stops on a missing checksum when require is set", func() {
			err := verifyImageFile(settings(imagesum.Fail, true), img, false)
			Expect(err).To(HaveOccurred())
			Expect(banners).To(HaveLen(1))
			GinkgoWriter.Println("\n" + banners[0])
			Expect(banners[0]).To(ContainSubstring("no checksum recorded for this image"))
			Expect(banners[0]).To(ContainSubstring("require is on"))
		})
		It("stops on a stale checksum when require is set", func() {
			good()
			replaced()
			Expect(verifyImageFile(settings(imagesum.Fail, true), img, false)).To(HaveOccurred())
			Expect(banners[0]).To(ContainSubstring("stale checksum"))
		})
	})

	DescribeTable("when the state is mounted rw it skips the check and drops the checksum, which would go stale",
		func(a imagesum.Action) {
			good()
			rot()
			Expect(verifyImageFile(settings(a, true), img, true)).To(Succeed())
			_, err := os.Stat(img + ".sha256")
			Expect(os.IsNotExist(err)).To(BeTrue())
			Expect(banners).To(BeEmpty())
		},
		Entry("none", imagesum.None),
		Entry("warn", imagesum.Warn),
		Entry("fail", imagesum.Fail),
	)
	It("rw without a checksum file is fine", func() {
		Expect(verifyImageFile(settings(imagesum.Fail, true), img, true)).To(Succeed())
	})

	It("defaults to not verifying anything", func() {
		Expect(internalUtils.DefaultVerifyAction).To(Equal(imagesum.None))
	})
})

// The agent writes the checksum through sdk/imagesum and immucore checks it
// through the same package. This runs that chain on a real ext4 image.
var _ = Describe("verifyImageFile on a real ext4 image", Label("ext4"), func() {
	var img string
	var banners []string
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

	BeforeEach(func() {
		for _, tool := range []string{"mke2fs", "sha256sum"} {
			if _, err := exec.LookPath(tool); err != nil {
				Skip(tool + " not available")
			}
		}
		dir := GinkgoT().TempDir()
		root := filepath.Join(dir, "root")
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "hello"), []byte("hello kairos"), 0o644)).To(Succeed())
		img = filepath.Join(dir, "active.img")
		out, err := exec.Command("mke2fs", "-q", "-t", "ext4", "-L", "COS_ACTIVE", "-d", root, img, "16M").CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))

		banners = nil
		orig := haltWithBannerFn
		haltWithBannerFn = func(banner, _ string, _ error) { banners = append(banners, ansi.ReplaceAllString(banner, "")) }
		DeferCleanup(func() { haltWithBannerFn = orig })
	})

	It("boots a freshly written image and stops on one that rotted afterwards", func() {
		stored, err := imagesum.Generate(vfs.OSFS, img)
		Expect(err).ToNot(HaveOccurred())

		fail := internalUtils.VerifySettings{Action: imagesum.Fail, ActionSource: internalUtils.SourcePolicy}
		Expect(verifyImageFile(fail, img, false)).To(Succeed())
		Expect(banners).To(BeEmpty())

		info, err := os.Stat(img)
		Expect(err).ToNot(HaveOccurred())
		f, err := os.OpenFile(img, os.O_RDWR, 0)
		Expect(err).ToNot(HaveOccurred())
		// flip every bit of a byte: a constant could equal what is already there
		// (with 4096 byte blocks that offset is a bitmap and already 0xff)
		orig := make([]byte, 1)
		_, err = f.ReadAt(orig, 4096*3)
		Expect(err).ToNot(HaveOccurred())
		_, err = f.WriteAt([]byte{^orig[0]}, 4096*3)
		Expect(err).ToNot(HaveOccurred())
		Expect(f.Close()).To(Succeed())
		Expect(os.Chtimes(img, info.ModTime(), info.ModTime())).To(Succeed())

		actual, err := imagesum.Compute(vfs.OSFS, img)
		Expect(err).ToNot(HaveOccurred())
		err = verifyImageFile(fail, img, false)
		Expect(err).To(HaveOccurred())
		Expect(banners).To(HaveLen(1))
		GinkgoWriter.Println("\n" + banners[0])
		Expect(banners[0]).To(ContainSubstring(stored))
		Expect(banners[0]).To(ContainSubstring(actual))
		Expect(strings.Count(banners[0], "\n")).To(BeNumerically(">", 10))

		// warn lets the very same image boot
		banners = nil
		Expect(verifyImageFile(internalUtils.VerifySettings{Action: imagesum.Warn}, img, false)).To(Succeed())
		Expect(banners).To(BeEmpty())
	})
})

// State.verifyImage ties the cmdline/policy settings, the image path under the
// state mount and the rw mode to verifyImageFile.
var _ = Describe("State.verifyImage", func() {
	var root, img string
	var banners []string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		stateDir := filepath.Join(root, "run/initramfs/cos-state/cOS")
		Expect(os.MkdirAll(stateDir, 0o755)).To(Succeed())
		img = filepath.Join(stateDir, "active.img")
		Expect(os.WriteFile(img, []byte("image"), 0o644)).To(Succeed())
		_, err := imagesum.Generate(vfs.OSFS, img)
		Expect(err).ToNot(HaveOccurred())
		// rot it, keeping size and mtime
		info, _ := os.Stat(img)
		Expect(os.WriteFile(img, []byte("imagf"), 0o644)).To(Succeed())
		Expect(os.Chtimes(img, info.ModTime(), info.ModTime())).To(Succeed())

		cmdline := filepath.Join(root, "cmdline")
		Expect(os.WriteFile(cmdline, []byte("root=LABEL=COS_ACTIVE"), 0o644)).To(Succeed())
		Expect(os.Setenv("HOST_PROC_CMDLINE", cmdline)).To(Succeed())
		DeferCleanup(func() { os.Unsetenv("HOST_PROC_CMDLINE") })

		banners = nil
		orig := haltWithBannerFn
		haltWithBannerFn = func(banner, _ string, _ error) { banners = append(banners, banner) }
		DeferCleanup(func() { haltWithBannerFn = orig })
	})

	setCmdline := func(c string) {
		Expect(os.WriteFile(os.Getenv("HOST_PROC_CMDLINE"), []byte(c), 0o644)).To(Succeed())
	}
	st := func(mode string) *State {
		return &State{Rootdir: root, TargetImage: "/cOS/active.img", RootMountMode: mode}
	}

	It("does nothing by default, even for a rotten image", func() {
		Expect(st("ro").verifyImage(img)).To(Succeed())
		Expect(banners).To(BeEmpty())
	})
	It("fails from the cmdline", func() {
		setCmdline("image-checksum.verify=fail")
		Expect(st("ro").verifyImage(img)).To(HaveOccurred())
		Expect(banners).To(HaveLen(1))
	})
	It("fails from the policy file the agent wrote, and the cmdline can turn it off", func() {
		Expect(imagesum.WritePolicy(vfs.OSFS, filepath.Dir(img), imagesum.Policy{Verify: imagesum.Fail})).To(Succeed())
		Expect(st("ro").verifyImage(img)).To(HaveOccurred())
		setCmdline("image-checksum.verify=none")
		Expect(st("ro").verifyImage(img)).To(Succeed())
	})
	It("drops the checksum instead of checking when mounted rw", func() {
		setCmdline("image-checksum.verify=fail")
		Expect(st("rw").verifyImage(img)).To(Succeed())
		_, err := os.Stat(img + ".sha256")
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
})
