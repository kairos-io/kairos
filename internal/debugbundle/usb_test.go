package debugbundle_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-installer/internal/debugbundle"
)

var _ = Describe("CopyTo", func() {
	It("copies the file into the destination directory", func() {
		dir := GinkgoT().TempDir()
		src := filepath.Join(dir, "bundle.tar.gz")
		Expect(os.WriteFile(src, []byte("data"), 0o644)).To(Succeed())
		destDir := GinkgoT().TempDir()

		dest, err := debugbundle.CopyTo(src, destDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(dest).To(Equal(filepath.Join(destDir, "bundle.tar.gz")))

		got, _ := os.ReadFile(dest)
		Expect(string(got)).To(Equal("data"))
	})
})
