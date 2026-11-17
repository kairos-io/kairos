package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/labstack/echo/v4"
)

var _ = Describe("SecureBootHandler key-set name traversal", func() {
	var (
		e       *echo.Echo
		handler *handlers.SecureBootHandler
		fakeSB  *fakeSecureBootKeySetStore
		baseDir string
		keysDir string
		ctx     context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		_ = ctx
		e = echo.New()
		var err error
		// keysDir lives under baseDir so we can assert nothing was written to a
		// sibling path outside keysDir via traversal.
		baseDir, err = os.MkdirTemp("", "sb-traversal-")
		Expect(err).NotTo(HaveOccurred())
		keysDir = filepath.Join(baseDir, "keys")
		Expect(os.MkdirAll(keysDir, 0o700)).To(Succeed())
		fakeSB = &fakeSecureBootKeySetStore{}
		handler = handlers.NewSecureBootHandler(fakeSB, keysDir)
	})

	AfterEach(func() {
		os.RemoveAll(baseDir)
	})

	Describe("GenerateKeys", func() {
		It("returns 400 for a traversal name and writes nothing outside keysDir", func() {
			body := `{"name":"../escape"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/secureboot-keys/generate", strings.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.GenerateKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))

			// The traversal target (baseDir/escape) must not exist.
			_, err := os.Stat(filepath.Join(baseDir, "escape"))
			Expect(os.IsNotExist(err)).To(BeTrue())
			// keysDir must remain empty.
			entries, err := os.ReadDir(keysDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})

		It("returns 400 for an absolute name", func() {
			body := `{"name":"/etc/evil"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/secureboot-keys/generate", strings.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.GenerateKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("ImportKeys", func() {
		It("returns 400 for a traversal ?name= override and writes nothing outside keysDir", func() {
			// A well-formed archive whose only problem is the override name.
			manifest := keySetManifestJSON("prod")
			body := makeCustomTarGz([]tarEntry{
				{name: "manifest.json", data: manifest, mode: 0o644},
				{name: "keys/db.key", data: []byte("DBKEY"), mode: 0o600},
			})
			buf, contentType := buildMultipart(body, "keyset.tar.gz")

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secureboot-keys/import?name=../escape", buf)
			req.Header.Set(echo.HeaderContentType, contentType)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.ImportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))

			_, err := os.Stat(filepath.Join(baseDir, "escape"))
			Expect(os.IsNotExist(err)).To(BeTrue())
			entries, err := os.ReadDir(keysDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})

		It("returns 400 when the manifest name itself is a traversal", func() {
			manifest := keySetManifestJSON("../escape")
			body := makeCustomTarGz([]tarEntry{
				{name: "manifest.json", data: manifest, mode: 0o644},
				{name: "keys/db.key", data: []byte("DBKEY"), mode: 0o600},
			})
			buf, contentType := buildMultipart(body, "keyset.tar.gz")

			req := httptest.NewRequest(http.MethodPost, "/api/v1/secureboot-keys/import", buf)
			req.Header.Set(echo.HeaderContentType, contentType)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.ImportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))

			_, err := os.Stat(filepath.Join(baseDir, "escape"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})
})

// keySetManifestJSON builds a valid v1 manifest with the given name.
func keySetManifestJSON(name string) []byte {
	m := map[string]interface{}{
		"version":          1,
		"kind":             "auroraboot.secureboot-keyset",
		"name":             name,
		"secureBootEnroll": "if-safe",
	}
	b, err := json.Marshal(m)
	Expect(err).NotTo(HaveOccurred())
	return b
}
