package image_test

import (
	"archive/tar"
	"bytes"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/kairos-io/kairos-sdk/utils/image"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// currentUserImage builds a single-layer image whose one file is owned by the
// current uid/gid, so ExtractOCIImage's chown succeeds when the suite runs
// rootless (v1random.Image writes uid/gid 0, which a non-root lchown rejects).
func currentUserImage() (v1.Image, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("hello from the insecure registry")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "hello.txt",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(content)),
		Uid:      os.Getuid(),
		Gid:      os.Getgid(),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
	if err != nil {
		return nil, err
	}
	return mutate.AppendLayers(empty.Image, layer)
}

var _ = Describe("OCIImageExtractor", func() {
	Describe("against a registry with an untrusted (self-signed) TLS certificate", func() {
		var (
			server   *httptest.Server
			imageRef string
			destDir  string
		)

		BeforeEach(func() {
			server = httptest.NewTLSServer(ggcrregistry.New())

			u, err := url.Parse(server.URL)
			Expect(err).ToNot(HaveOccurred())
			imageRef = u.Host + "/test/extract:latest"

			img, err := currentUserImage()
			Expect(err).ToNot(HaveOccurred())
			ref, err := name.ParseReference(imageRef, name.Insecure)
			Expect(err).ToNot(HaveOccurred())
			Expect(remote.Write(ref, img, remote.WithTransport(insecureTransport()))).To(Succeed())

			destDir, err = os.MkdirTemp("", "sdk-extractor-*")
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			server.Close()
			Expect(os.RemoveAll(destDir)).To(Succeed())
		})

		It("ExtractImage fails when Insecure is false", func() {
			err := image.OCIImageExtractor{}.ExtractImage(imageRef, destDir, "linux/amd64")
			Expect(err).To(HaveOccurred())
			// Must fail on certificate verification, not because the image is missing.
			Expect(strings.ToLower(err.Error())).To(Or(
				ContainSubstring("certificate"),
				ContainSubstring("tls"),
				ContainSubstring("x509"),
			))
		})

		It("ExtractImage succeeds and unpacks files when Insecure is true", func() {
			err := image.OCIImageExtractor{Insecure: true}.ExtractImage(imageRef, destDir, "linux/amd64")
			Expect(err).ToNot(HaveOccurred())

			// Assert the known file from currentUserImage was extracted verbatim,
			// not merely that something landed in the directory.
			content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("hello from the insecure registry"))
		})

		It("GetOCIImageSize requires Insecure for this registry", func() {
			_, err := image.OCIImageExtractor{}.GetOCIImageSize(imageRef, "linux/amd64")
			Expect(err).To(HaveOccurred())

			size, err := image.OCIImageExtractor{Insecure: true}.GetOCIImageSize(imageRef, "linux/amd64")
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(BeNumerically(">", 0))
		})
	})
})
