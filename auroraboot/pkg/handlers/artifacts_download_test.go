package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// The build folder also holds the deployer's working files, such as the
// cloud-config it wrote as config.yaml. Download serves only the files the
// record lists as build outputs.
var _ = Describe("ArtifactHandler: Download", func() {
	var (
		e            *echo.Echo
		handler      *handlers.ArtifactHandler
		artifactsDir string
	)

	BeforeEach(func() {
		e = echo.New()
		artifactsDir = GinkgoT().TempDir()
		dir := filepath.Join(artifactsDir, "a1")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "kairos.iso"), []byte("iso"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("token: secret"), 0o600)).To(Succeed())

		as := &fakeArtifactStore{}
		Expect(as.Create(context.Background(), &store.ArtifactRecord{
			ID:            "a1",
			Phase:         store.ArtifactReady,
			ArtifactFiles: []string{filepath.Join(dir, "kairos.iso")},
		})).To(Succeed())
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, as, nil, nil, nil, nil, artifactsDir, "reg-token", "http://localhost:8080")
	})

	download := func(filename string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/a1/download/"+filename, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id", "*")
		c.SetParamValues("a1", filename)
		Expect(handler.Download(c)).To(Succeed())
		return rec
	}

	It("serves a file the record lists", func() {
		rec := download("kairos.iso")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("iso"))
	})

	It("refuses a file in the build folder that the record does not list", func() {
		rec := download("config.yaml")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
		Expect(rec.Body.String()).NotTo(ContainSubstring("secret"))
	})

	It("refuses a name that is not in the folder", func() {
		Expect(download("missing.iso").Code).To(Equal(http.StatusNotFound))
	})

	Context("without a store", func() {
		BeforeEach(func() {
			fb := &fakeBuilder{builds: []*builder.BuildStatus{{
				ID:        "a1",
				Phase:     builder.BuildReady,
				Artifacts: []string{filepath.Join(artifactsDir, "a1", "kairos.iso")},
			}}}
			handler = handlers.NewArtifactHandler(fb, nil, nil, nil, nil, nil, artifactsDir, "reg-token", "http://localhost:8080")
		})

		It("serves a file the builder lists", func() {
			Expect(download("kairos.iso").Code).To(Equal(http.StatusOK))
		})

		It("refuses a file in the build folder that the builder does not list", func() {
			Expect(download("config.yaml").Code).To(Equal(http.StatusNotFound))
		})

		It("refuses a build the builder does not know", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/other/download/kairos.iso", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id", "*")
			c.SetParamValues("other", "kairos.iso")
			Expect(handler.Download(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})
	})
})
