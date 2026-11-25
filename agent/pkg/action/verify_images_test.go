package action_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
)

var _ = Describe("VerifyImages", Label("imagesum", "verify-images"), func() {
	var dir string
	var out *bytes.Buffer
	var cfg *sdkConfig.Config

	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		Expect(os.WriteFile(p, []byte(content), 0o644)).To(Succeed())
		return p
	}
	// rot changes the content of an image the way bit rot does: size and mtime stay
	rot := func(p string) {
		info, err := os.Stat(p)
		Expect(err).ToNot(HaveOccurred())
		data, err := os.ReadFile(p)
		Expect(err).ToNot(HaveOccurred())
		data[0] ^= 1
		Expect(os.WriteFile(p, data, 0o644)).To(Succeed())
		Expect(os.Chtimes(p, info.ModTime(), info.ModTime())).To(Succeed())
	}
	verify := func() error { return action.VerifyImages(cfg, out, []string{dir}) }

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		out = &bytes.Buffer{}
		cfg = &sdkConfig.Config{Fs: vfs.OSFS}
	})

	It("passes healthy images and prints the expected and the actual sha256", func() {
		for _, n := range []string{"active.img", "passive.img", "recovery.img"} {
			p := write(n, "image "+n)
			_, err := imagesum.Generate(vfs.OSFS, p)
			Expect(err).ToNot(HaveOccurred())
		}
		Expect(verify()).To(Succeed())
		Expect(out.String()).To(ContainSubstring("3 image(s) checked, 0 failed"))
		Expect(strings.Count(out.String(), "result:   OK")).To(Equal(3))
		sum, _ := imagesum.Compute(vfs.OSFS, filepath.Join(dir, "active.img"))
		Expect(out.String()).To(ContainSubstring("expected: " + sum))
		Expect(out.String()).To(ContainSubstring("actual:   " + sum))
	})

	It("fails on a mismatch and shows both digests, still reporting the other images", func() {
		active := write("active.img", "active content")
		passive := write("passive.img", "passive content")
		stored, _ := imagesum.Generate(vfs.OSFS, active)
		_, _ = imagesum.Generate(vfs.OSFS, passive)
		rot(active)
		actual, _ := imagesum.Compute(vfs.OSFS, active)

		err := verify()
		Expect(err).To(MatchError(ContainSubstring("1 of 2 OS images failed")))
		Expect(out.String()).To(ContainSubstring("MISMATCH"))
		Expect(out.String()).To(ContainSubstring("expected: " + stored))
		Expect(out.String()).To(ContainSubstring("actual:   " + actual))
		Expect(out.String()).To(ContainSubstring("result:   OK"))
	})

	It("skips an image without a checksum unless require is on", func() {
		write("active.img", "no sum")
		Expect(verify()).To(Succeed())
		Expect(out.String()).To(ContainSubstring("NO CHECKSUM, skipped"))

		out.Reset()
		yes := true
		cfg.ImageChecksum.Require = &yes
		Expect(verify()).To(MatchError(ContainSubstring("1 of 1 OS images failed")))
		Expect(out.String()).To(ContainSubstring("FAILED because image-checksum.require is on"))
	})

	It("treats an image replaced after its checksum as stale: skipped, or failed with require", func() {
		p := write("active.img", "first image")
		_, err := imagesum.Generate(vfs.OSFS, p)
		Expect(err).ToNot(HaveOccurred())
		write("active.img", "another, longer image")
		future := time.Now().Add(time.Hour)
		Expect(os.Chtimes(p, future, future)).To(Succeed())

		Expect(verify()).To(Succeed())
		Expect(out.String()).To(ContainSubstring("STALE"))
		yes := true
		cfg.ImageChecksum.Require = &yes
		Expect(verify()).To(HaveOccurred())
	})

	It("fails on a checksum file it cannot parse", func() {
		p := write("active.img", "x")
		Expect(os.WriteFile(p+".sha256", []byte("garbage"), 0o644)).To(Succeed())
		Expect(verify()).To(HaveOccurred())
		Expect(out.String()).To(ContainSubstring("the check could not be done"))
	})

	It("changes nothing on disk", func() {
		active := write("active.img", "content")
		_, _ = imagesum.Generate(vfs.OSFS, active)
		rot(active)
		before := snapshot(dir)
		_ = verify()
		Expect(snapshot(dir)).To(Equal(before))
	})

	It("errors when there are no images at all", func() {
		Expect(verify()).To(MatchError(ContainSubstring("no OS images found")))
	})

	Describe("on a real ext4 image", Label("ext4"), func() {
		BeforeEach(func() {
			for _, tool := range []string{"mke2fs"} {
				if _, err := exec.LookPath(tool); err != nil {
					Skip(tool + " not available")
				}
			}
			root := filepath.Join(dir, "root")
			Expect(os.MkdirAll(root, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(root, "hello"), []byte("hello kairos"), 0o644)).To(Succeed())
			o, err := exec.Command("mke2fs", "-q", "-t", "ext4", "-L", "COS_ACTIVE", "-d", root, filepath.Join(dir, "active.img"), "16M").CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(o))
			Expect(os.RemoveAll(root)).To(Succeed())
		})

		It("passes, then fails once a block rots", func() {
			img := filepath.Join(dir, "active.img")
			_, err := imagesum.Generate(vfs.OSFS, img)
			Expect(err).ToNot(HaveOccurred())
			Expect(verify()).To(Succeed())
			GinkgoWriter.Println("\n" + out.String())

			info, _ := os.Stat(img)
			f, err := os.OpenFile(img, os.O_RDWR, 0)
			Expect(err).ToNot(HaveOccurred())
			_, err = f.WriteAt([]byte{0xff}, 4096*3)
			Expect(err).ToNot(HaveOccurred())
			Expect(f.Close()).To(Succeed())
			Expect(os.Chtimes(img, info.ModTime(), info.ModTime())).To(Succeed())

			out.Reset()
			Expect(verify()).To(HaveOccurred())
			GinkgoWriter.Println("\n" + out.String())
			Expect(out.String()).To(ContainSubstring("MISMATCH"))
		})
	})
})

// snapshot lists every file under dir with its size and content, to compare before and after.
func snapshot(dir string) map[string]string {
	res := map[string]string{}
	entries, err := os.ReadDir(dir)
	Expect(err).ToNot(HaveOccurred())
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		Expect(err).ToNot(HaveOccurred())
		res[e.Name()] = string(data)
	}
	return res
}
