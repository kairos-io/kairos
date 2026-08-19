package image_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1random "github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/kairos-io/kairos-sdk/utils/image"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// insecureTransport returns an http transport that skips TLS verification, used
// to talk to the self-signed test registry when seeding it with an image. The
// type assertion on http.DefaultTransport is guarded so a replaced default
// transport in the test process does not turn into a runtime panic.
func insecureTransport() *http.Transport {
	tr := &http.Transport{}
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = dt.Clone()
	}
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	return tr
}

var _ = Describe("GetImage", func() {
	Describe("against a registry with an untrusted (self-signed) TLS certificate", func() {
		var (
			server   *httptest.Server
			imageRef string
		)

		BeforeEach(func() {
			// httptest.NewTLSServer serves HTTPS with a self-signed certificate
			// that the default system roots do not trust. Running a real
			// go-containerregistry registry handler behind it lets us push and
			// then pull a real image.
			server = httptest.NewTLSServer(ggcrregistry.New())

			u, err := url.Parse(server.URL)
			Expect(err).ToNot(HaveOccurred())
			imageRef = u.Host + "/test/img:latest"

			// Seed the registry with a small image, using an insecure transport
			// since the certificate is self-signed.
			img, err := v1random.Image(1024, 1)
			Expect(err).ToNot(HaveOccurred())

			ref, err := name.ParseReference(imageRef, name.Insecure)
			Expect(err).ToNot(HaveOccurred())
			Expect(remote.Write(ref, img, remote.WithTransport(insecureTransport()))).To(Succeed())
		})

		AfterEach(func() {
			server.Close()
		})

		It("fails without the insecure option", func() {
			_, err := image.GetImage(imageRef, "linux/amd64", nil, nil)
			Expect(err).To(HaveOccurred())
			// Should fail because of certificate verification, not because the
			// image is missing.
			Expect(strings.ToLower(err.Error())).To(Or(
				ContainSubstring("certificate"),
				ContainSubstring("tls"),
				ContainSubstring("x509"),
			))
		})

		It("succeeds with the insecure option", func() {
			img, err := image.GetImage(imageRef, "linux/amd64", nil, nil, image.WithInsecureRegistry())
			Expect(err).ToNot(HaveOccurred())
			Expect(img).ToNot(BeNil())

			// The returned image must be usable (digest resolvable).
			_, err = img.Digest()
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
