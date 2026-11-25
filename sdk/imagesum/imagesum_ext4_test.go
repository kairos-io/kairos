package imagesum_test

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
)

// Runs against a real ext4 image on the real filesystem, no mount or root
// needed. Skipped where e2fsprogs/coreutils are missing.
var _ = Describe("sidecar on a real ext4 image", Label("ext4"), func() {
	var img, dir string
	var fs sdkFs.KairosFS

	BeforeEach(func() {
		for _, tool := range []string{"mke2fs", "tune2fs", "sha256sum"} {
			if _, err := exec.LookPath(tool); err != nil {
				Skip(tool + " not available")
			}
		}
		dir = GinkgoT().TempDir()
		root := filepath.Join(dir, "root")
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "hello"), []byte("hello kairos"), 0o644)).To(Succeed())
		img = filepath.Join(dir, "active.img")
		out, err := exec.Command("mke2fs", "-q", "-t", "ext4", "-L", "COS_ACTIVE", "-d", root, img, "16M").CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))
		fs = vfs.OSFS
	})

	sha256sumCheck := func() (string, error) {
		cmd := exec.Command("sha256sum", "-c", "active.img.sha256")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	It("writes a sidecar that sha256sum -c accepts, and rejects after corruption", func() {
		_, err := imagesum.Generate(fs, img)
		Expect(err).ToNot(HaveOccurred())

		out, err := sha256sumCheck()
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("active.img: OK"))

		info, err := os.Stat(img)
		Expect(err).ToNot(HaveOccurred())
		f, err := os.OpenFile(img, os.O_RDWR, 0)
		Expect(err).ToNot(HaveOccurred())
		_, err = f.WriteAt([]byte{0xff}, 4096*3)
		Expect(err).ToNot(HaveOccurred())
		Expect(f.Close()).To(Succeed())
		// bit rot does not touch the mtime
		Expect(os.Chtimes(img, info.ModTime(), info.ModTime())).To(Succeed())

		out, err = sha256sumCheck()
		Expect(err).To(HaveOccurred())
		Expect(out).To(ContainSubstring("FAILED"))

		r, err := imagesum.Check(fs, img)
		Expect(err).ToNot(HaveOccurred())
		Expect(r.Result).To(Equal(imagesum.Mismatch))
		Expect(r.Actual).ToNot(Equal(r.Expected))
	})

	It("reads a sidecar produced by the real sha256sum", func() {
		out, err := func() ([]byte, error) {
			cmd := exec.Command("sha256sum", "active.img")
			cmd.Dir = dir
			return cmd.Output()
		}()
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(img+".sha256", out, 0o644)).To(Succeed())
		r, err := imagesum.Check(fs, img)
		Expect(err).ToNot(HaveOccurred())
		Expect(r.Result).To(Equal(imagesum.OK))
	})

	It("is invalidated by a relabel, which is why upgrade re-hashes passive", func() {
		_, err := imagesum.Generate(fs, img)
		Expect(err).ToNot(HaveOccurred())
		out, err := exec.Command("tune2fs", "-L", "COS_PASSIVE", img).CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))
		r, err := imagesum.Check(fs, img)
		Expect(err).ToNot(HaveOccurred())
		// tune2fs rewrites the superblock and so changes the mtime: stale, not rot
		Expect(r.Result).To(Or(Equal(imagesum.Stale), Equal(imagesum.Mismatch)))
	})

	It("read-only fsck leaves the image, and so its checksum, alone", func() {
		if _, err := exec.LookPath("e2fsck"); err != nil {
			Skip("e2fsck not available")
		}
		_, err := imagesum.Generate(fs, img)
		Expect(err).ToNot(HaveOccurred())
		out, err := exec.Command("e2fsck", "-fn", img).CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))
		r, err := imagesum.Check(fs, img)
		Expect(err).ToNot(HaveOccurred())
		Expect(r.Result).To(Equal(imagesum.OK))
	})
})
