// internal/debugbundle/bundle_test.go
package debugbundle_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-installer/internal/debugbundle"
)

var _ = Describe("OutputPath", func() {
	It("produces a timestamped .tar.gz name", func() {
		ts := time.Date(2026, 6, 23, 13, 14, 15, 0, time.UTC)
		p := debugbundle.OutputPath(ts)
		Expect(filepath.Base(p)).To(Equal("kairos-logs-20260623-131415.tar.gz"))
	})
})

var _ = Describe("Generate", func() {
	It("falls back to a stdlib tarball when no agent binary is given", func() {
		dir := GinkgoT().TempDir()
		a := filepath.Join(dir, "a.log")
		Expect(os.WriteFile(a, []byte("hello-a"), 0o644)).To(Succeed())
		out := filepath.Join(dir, "bundle.tar.gz")

		Expect(debugbundle.Generate("", out, []string{a})).To(Succeed())

		// Open the gzip+tar and assert the file is present with its content.
		f, err := os.Open(out)
		Expect(err).ToNot(HaveOccurred())
		defer f.Close()
		gz, err := gzip.NewReader(f)
		Expect(err).ToNot(HaveOccurred())
		tr := tar.NewReader(gz)
		hdr, err := tr.Next()
		Expect(err).ToNot(HaveOccurred())
		Expect(hdr.Name).To(Equal("a.log"))
		buf := make([]byte, hdr.Size)
		_, _ = tr.Read(buf)
		Expect(string(buf)).To(Equal("hello-a"))
	})
})
