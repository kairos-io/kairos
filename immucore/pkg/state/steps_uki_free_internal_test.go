package state

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("freeInitramfsCopies", func() {
	It("removes each copied directory with everything below it", func() {
		root := GinkgoT().TempDir()
		usr := filepath.Join(root, "usr")
		Expect(os.MkdirAll(filepath.Join(usr, "lib", "modules"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(usr, "lib", "modules", "a.ko"), []byte("x"), 0o644)).To(Succeed())
		Expect(os.Symlink("lib", filepath.Join(usr, "lib64"))).To(Succeed())
		keep := filepath.Join(root, "keep")
		Expect(os.MkdirAll(keep, 0o755)).To(Succeed())

		freeInitramfsCopies([]string{usr})

		_, err := os.Lstat(usr)
		Expect(os.IsNotExist(err)).To(BeTrue(), "usr is still there")
		Expect(keep).To(BeADirectory(), "a directory that was not copied was removed")
	})

	It("keeps going when a directory is already gone", func() {
		root := GinkgoT().TempDir()
		etc := filepath.Join(root, "etc")
		Expect(os.MkdirAll(etc, 0o755)).To(Succeed())

		freeInitramfsCopies([]string{filepath.Join(root, "missing"), etc})

		_, err := os.Lstat(etc)
		Expect(os.IsNotExist(err)).To(BeTrue(), "etc is still there")
	})

	It("does not delete a symlink target outside the directory", func() {
		root := GinkgoT().TempDir()
		outside := filepath.Join(root, "outside")
		Expect(os.MkdirAll(outside, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(outside, "f"), []byte("x"), 0o644)).To(Succeed())
		var_ := filepath.Join(root, "var")
		Expect(os.MkdirAll(var_, 0o755)).To(Succeed())
		Expect(os.Symlink(outside, filepath.Join(var_, "link"))).To(Succeed())

		freeInitramfsCopies([]string{var_})

		Expect(filepath.Join(outside, "f")).To(BeARegularFile())
	})
})
