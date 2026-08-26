// internal/debugbundle/serve_test.go
package debugbundle_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-installer/internal/debugbundle"
)

var _ = Describe("Serve", func() {
	It("serves the bundle at the token path and 404s everything else", func() {
		dir := GinkgoT().TempDir()
		bundle := filepath.Join(dir, "bundle.tar.gz")
		Expect(os.WriteFile(bundle, []byte("payload"), 0o644)).To(Succeed())

		srv, err := debugbundle.Serve(bundle)
		Expect(err).ToNot(HaveOccurred())
		defer srv.Close()

		good := fmt.Sprintf("http://127.0.0.1:%d/%s/%s", srv.Port(), srv.Token(), srv.Base())
		resp, err := http.Get(good)
		Expect(err).ToNot(HaveOccurred())
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(string(body)).To(Equal("payload"))

		bad := fmt.Sprintf("http://127.0.0.1:%d/%s", srv.Port(), srv.Base())
		resp2, err := http.Get(bad)
		Expect(err).ToNot(HaveOccurred())
		resp2.Body.Close()
		Expect(resp2.StatusCode).To(Equal(http.StatusNotFound))
	})
})
