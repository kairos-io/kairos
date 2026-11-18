package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

var _ = Describe("ArtifactHandler: file names in responses", func() {
	var (
		e       *echo.Echo
		as      *fakeArtifactStore
		handler *handlers.ArtifactHandler
	)
	stored := []string{"/srv/auroraboot/data/artifacts/a1/kairos.iso", "data/artifacts/a1/kairos.raw"}

	BeforeEach(func() {
		e = echo.New()
		as = &fakeArtifactStore{}
		Expect(as.Create(context.Background(), &store.ArtifactRecord{ID: "a1", Phase: store.ArtifactReady, ArtifactFiles: append([]string(nil), stored...)})).To(Succeed())
		Expect(as.Create(context.Background(), &store.ArtifactRecord{ID: "a2", Phase: store.ArtifactPending})).To(Succeed())
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, as, nil, nil, nil, nil, GinkgoT().TempDir(), "reg-token", "http://localhost:8080")
	})

	serve := func(method, target, id, body string, h echo.HandlerFunc) map[string]any {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(h(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).NotTo(ContainSubstring("data/artifacts/"))
		var out map[string]any
		Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed())
		return out
	}

	It("lists base names and leaves the stored paths alone", func() {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts", nil)
		rec := httptest.NewRecorder()
		Expect(handler.List(e.NewContext(req, rec))).To(Succeed())
		Expect(rec.Body.String()).NotTo(ContainSubstring("data/artifacts/"))
		var out []map[string]any
		Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed())
		byID := map[string]map[string]any{}
		for _, o := range out {
			byID[o["id"].(string)] = o
		}
		Expect(byID["a1"]["artifacts"]).To(Equal([]any{"kairos.iso", "kairos.raw"}))
		Expect(byID["a2"]["artifacts"]).To(BeNil())

		rec2, err := as.GetByID(context.Background(), "a1")
		Expect(err).NotTo(HaveOccurred())
		Expect(rec2.ArtifactFiles).To(Equal(stored))
	})

	It("returns base names from Get, Update and Cancel", func() {
		Expect(serve(http.MethodGet, "/api/v1/artifacts/a1", "a1", "", handler.Get)["artifacts"]).To(Equal([]any{"kairos.iso", "kairos.raw"}))
		Expect(serve(http.MethodPatch, "/api/v1/artifacts/a1", "a1", `{"name":"n"}`, handler.Update)["artifacts"]).To(Equal([]any{"kairos.iso", "kairos.raw"}))
		Expect(serve(http.MethodPost, "/api/v1/artifacts/a1/cancel", "a1", "", handler.Cancel)["artifacts"]).To(Equal([]any{"kairos.iso", "kairos.raw"}))
		rec, err := as.GetByID(context.Background(), "a1")
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.ArtifactFiles).To(Equal(stored))
	})

	It("returns base names from the builder when there is no store", func() {
		fb := &fakeBuilder{builds: []*builder.BuildStatus{{ID: "b1", Phase: builder.BuildReady, Artifacts: append([]string(nil), stored...)}}}
		h := handlers.NewArtifactHandler(fb, nil, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
		Expect(serve(http.MethodGet, "/api/v1/artifacts/b1", "b1", "", h.Get)["artifacts"]).To(Equal([]any{"kairos.iso", "kairos.raw"}))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts", nil)
		rec := httptest.NewRecorder()
		Expect(h.List(e.NewContext(req, rec))).To(Succeed())
		Expect(rec.Body.String()).NotTo(ContainSubstring("data/artifacts/"))
		Expect(fb.builds[0].Artifacts).To(Equal(stored))
	})
})
