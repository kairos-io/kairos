package image_test

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/kairos-io/kairos/v4/sdk/utils/image"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// excludeTreeImage builds a one layer image holding paths, owned by the
// current user so the suite can extract it without being root. A path ending
// in "/" is a directory, anything else a file.
func excludeTreeImage(paths ...string) v1.Image {
	GinkgoHelper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, p := range paths {
		hdr := &tar.Header{Name: p, Uid: os.Getuid(), Gid: os.Getgid()}
		var body []byte
		if strings.HasSuffix(p, "/") {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
		} else {
			body = []byte(p)
			hdr.Typeflag = tar.TypeReg
			hdr.Mode = 0o644
			hdr.Size = int64(len(body))
		}
		Expect(tw.WriteHeader(hdr)).To(Succeed())
		_, err := tw.Write(body)
		Expect(err).ToNot(HaveOccurred())
	}
	Expect(tw.Close()).To(Succeed())

	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
	Expect(err).ToNot(HaveOccurred())
	img, err := mutate.AppendLayers(empty.Image, layer)
	Expect(err).ToNot(HaveOccurred())
	return img
}

var _ = Describe("Excluded", func() {
	DescribeTable("matches entries against exclude patterns",
		func(name, pattern string, want bool) {
			got, err := image.Excluded(name, pattern)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("absolute pattern, the directory itself", "var/lib/foo/", "/var/lib/foo", true),
		Entry("absolute pattern, a file inside the directory", "var/lib/foo/data", "/var/lib/foo", true),
		Entry("relative pattern, a file inside the directory", "var/lib/foo/data", "var/lib/foo", true),
		Entry("trailing slash on the pattern", "var/lib/foo/data", "/var/lib/foo/", true),
		Entry("entry name with a ./ prefix", "./var/lib/foo/data", "/var/lib/foo", true),
		Entry("a sibling sharing the prefix is kept", "var/lib/foobar", "/var/lib/foo", false),
		Entry("the parent directory is kept", "var/lib/", "/var/lib/foo", false),
		Entry("glob, a direct child", "etc/a.conf", "etc/*", true),
		Entry("glob with a leading slash", "etc/a.conf", "/etc/*", true),
		Entry("glob, the contents of a matched child directory", "etc/ssh/sshd_config", "etc/*", true),
		Entry("glob does not match the directory it names", "etc/", "etc/*", false),
		Entry("glob on a file name", "etc/b.conf", "/etc/*.conf", true),
		Entry("exact file", "etc/a.conf", "/etc/a.conf", true),
		Entry("exact file leaves its neighbour alone", "etc/b.conf", "/etc/a.conf", false),
		Entry("a lone slash excludes nothing", "etc/a.conf", "/", false),
	)

	It("returns an error for a malformed pattern", func() {
		_, err := image.Excluded("etc/a.conf", "etc/[")
		Expect(err).To(HaveOccurred())
	})

	It("reports nothing excluded with no patterns", func() {
		Expect(image.Excluded("etc/a.conf")).To(BeFalse())
	})
})

var _ = Describe("ExtractOCIImage with excludes", func() {
	tree := []string{
		"etc/", "etc/a.conf", "etc/b.conf",
		"var/", "var/lib/", "var/lib/foo/", "var/lib/foo/data", "var/lib/bar",
	}

	DescribeTable("drops the same entries whatever form the pattern takes",
		func(exclude string, gone, kept []string) {
			dest := GinkgoT().TempDir()
			Expect(image.ExtractOCIImage(excludeTreeImage(tree...), dest, exclude)).To(Succeed())
			for _, p := range gone {
				Expect(filepath.Join(dest, p)).ToNot(BeAnExistingFile(), "%s should be excluded by %q", p, exclude)
			}
			for _, p := range kept {
				Expect(filepath.Join(dest, p)).To(BeAnExistingFile(), "%s should survive %q", p, exclude)
			}
		},
		Entry("/var/lib/foo", "/var/lib/foo",
			[]string{"var/lib/foo", "var/lib/foo/data"}, []string{"var/lib/bar", "etc/a.conf"}),
		Entry("var/lib/foo", "var/lib/foo",
			[]string{"var/lib/foo", "var/lib/foo/data"}, []string{"var/lib/bar", "etc/a.conf"}),
		Entry("etc/*", "etc/*",
			[]string{"etc/a.conf", "etc/b.conf"}, []string{"etc", "var/lib/foo/data"}),
		Entry("/etc/a.conf", "/etc/a.conf",
			[]string{"etc/a.conf"}, []string{"etc/b.conf", "var/lib/foo/data"}),
	)
})
