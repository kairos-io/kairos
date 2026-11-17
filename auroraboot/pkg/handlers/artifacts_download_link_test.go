package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

var _ = Describe("ArtifactHandler.CreateDownloadLink", func() {
	var (
		e      *echo.Echo
		signer *auth.LinkSigner
	)

	BeforeEach(func() {
		as := &fakeArtifactStore{
			records: []*store.ArtifactRecord{
				{ID: "art-1", ArtifactFiles: []string{"data/artifacts/art-1/kairos.iso"}},
			},
		}
		signer = auth.NewLinkSigner()
		h := handlers.NewArtifactHandler(&fakeBuilder{}, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080").WithLinkSigner(signer)
		e = echo.New()
		e.POST("/api/v1/artifacts/:id/download-link", h.CreateDownloadLink)
	})

	post := func(body, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/"+id+"/download-link", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	It("returns a signed path for one of the artifact's files", func() {
		rec := post(`{"filename":"kairos.iso"}`, "art-1")
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		var got handlers.DownloadLinkResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &got)).To(Succeed())
		u, err := url.Parse(got.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(u.Path).To(Equal("/api/v1/artifacts/art-1/download/kairos.iso"))
		Expect(signer.Verify("art-1", "kairos.iso", u.Query())).To(BeTrue())
		Expect(got.ExpiresAt).To(BeTemporally("~", time.Now().Add(time.Hour), 5*time.Second))
		Expect(u.Query().Has("token")).To(BeFalse())
	})

	It("answers 404 for a file the artifact does not have", func() {
		Expect(post(`{"filename":"other.iso"}`, "art-1").Code).To(Equal(http.StatusNotFound))
	})

	It("answers 404 for an unknown artifact", func() {
		Expect(post(`{"filename":"kairos.iso"}`, "nope").Code).To(Equal(http.StatusNotFound))
	})

	It("answers 400 for a filename with a path in it", func() {
		Expect(post(`{"filename":"../../etc/passwd"}`, "art-1").Code).To(Equal(http.StatusBadRequest))
	})
})
