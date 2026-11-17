package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// racingArtifactStore hands out copies from GetByID, as a real database
// does, and publishes a builder phase transition right after the read so the
// handler's write always lands on a row that changed under it.
type racingArtifactStore struct {
	*fakeArtifactStore
}

func (s *racingArtifactStore) GetByID(ctx context.Context, id string) (*store.ArtifactRecord, error) {
	rec, err := s.fakeArtifactStore.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	snapshot := *rec
	if err := s.fakeArtifactStore.UpdatePhaseMessage(ctx, id, store.ArtifactReady, "done"); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

var _ = Describe("ArtifactHandler: PATCH /artifacts/:id", func() {
	var (
		e  *echo.Echo
		fs *fakeArtifactStore
		h  *handlers.ArtifactHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fs = &fakeArtifactStore{}
		Expect(fs.Create(context.Background(), &store.ArtifactRecord{
			ID: "a1", Name: "old", Phase: store.ArtifactBuilding, Message: "building",
			UploadToken: "tok",
		})).To(Succeed())
		h = handlers.NewArtifactHandler(&fakeBuilder{}, &racingArtifactStore{fs}, nil, nil, nil, nil, GinkgoT().TempDir(), "reg-token", "http://localhost:8080")
	})

	patch := func(body string) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/a1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("a1")
		Expect(h.Update(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))
	}

	It("keeps a phase the builder wrote between the handler's read and write", func() {
		patch(`{"name":"renamed","saved":true}`)

		got, err := fs.GetByID(context.Background(), "a1")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Name).To(Equal("renamed"))
		Expect(got.Saved).To(BeTrue())
		Expect(got.Phase).To(Equal(store.ArtifactReady))
		Expect(got.Message).To(Equal("done"))
		Expect(got.UploadToken).To(Equal("tok"))
	})

	It("writes only the fields present in the body", func() {
		patch(`{"saved":true}`)

		got, err := fs.GetByID(context.Background(), "a1")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Name).To(Equal("old"))
		Expect(got.Saved).To(BeTrue())
		Expect(got.Phase).To(Equal(store.ArtifactReady))
	})
})
