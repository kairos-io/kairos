package handlers_test

import (
	"errors"
	"fmt"
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

// wrappedNotSupported produces the shape the operator scaffold returns:
// fmt.Errorf("%w: ...", builder.ErrNotSupported). Using this helper (rather
// than passing builder.ErrNotSupported straight through) guards the handler
// mapping against a future switch from errors.Is to errors.Equal.
func wrappedNotSupported() error {
	return fmt.Errorf("%w: fake backend", builder.ErrNotSupported)
}

var _ = Describe("ArtifactHandler ErrNotSupported mapping", func() {
	var (
		e       *echo.Echo
		fb      *fakeBuilder
		handler *handlers.ArtifactHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeBuilder{}
		handler = handlers.NewArtifactHandler(fb, nil, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
	})

	Describe("Create", func() {
		It("returns 501 when Build wraps ErrNotSupported", func() {
			fb.buildErr = wrappedNotSupported()

			body := `{"baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true}}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.Create(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNotImplemented))
		})
	})

	Describe("List (no store, builder fallback)", func() {
		It("returns 501 when List wraps ErrNotSupported", func() {
			fb.listErr = wrappedNotSupported()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.List(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNotImplemented))
		})
	})

	Describe("Get (no store, builder fallback)", func() {
		It("returns 501 when Status wraps ErrNotSupported", func() {
			fb.statusErr = wrappedNotSupported()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/some-id", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("some-id")

			Expect(handler.Get(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNotImplemented))
		})
	})

	Describe("Cancel (store-backed)", func() {
		var handlerWithStore *handlers.ArtifactHandler

		BeforeEach(func() {
			as := &fakeArtifactStore{
				records: []*store.ArtifactRecord{
					{ID: "art-1", Phase: store.ArtifactBuilding, BaseImage: "img"},
				},
			}
			handlerWithStore = handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
		})

		// Backends that structurally do not implement Cancel surface as 501
		// regardless of whether the id happens to match a store row. The old
		// scaffold-era short-circuit ("missing + ErrNotSupported -> 404")
		// went away when Cancel became idempotent: a real backend now
		// distinguishes "not mine" from "cannot cancel" itself, so we no
		// longer have to override the builder's error taxonomy.
		It("returns 501 when Cancel wraps ErrNotSupported, missing id included", func() {
			fb.cancelErr = wrappedNotSupported()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/does-not-exist/cancel", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("does-not-exist")

			Expect(handlerWithStore.Cancel(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNotImplemented))
		})

		It("returns 501 when the artifact exists and Cancel wraps ErrNotSupported", func() {
			fb.cancelErr = wrappedNotSupported()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/art-1/cancel", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("art-1")

			Expect(handlerWithStore.Cancel(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNotImplemented))
		})

		It("returns 500 when the artifact exists and Cancel fails for a non-ErrNotSupported reason", func() {
			fb.cancelErr = errors.New("cancel exploded")

			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/art-1/cancel", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("art-1")

			Expect(handlerWithStore.Cancel(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		})

		// Idempotent Cancel: the local backend now returns nil when its
		// in-memory map has no record of the id. That's the "post-restart
		// orphan" case where a store row survives but the builder does not.
		// The handler must still hand back the store row and a 200 so the
		// UI reads the cancelled state.
		It("returns 200 and the store record when Cancel succeeds against an idempotent backend", func() {
			fb.cancelErr = nil

			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/art-1/cancel", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("art-1")

			Expect(handlerWithStore.Cancel(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(fb.cancelledIDs).To(ContainElement("art-1"))
		})

		It("still calls builder.Cancel when the store lookup returns an error", func() {
			// A transient store failure must not skip the side-effect of
			// reclaiming backend state; the handler drops back to the
			// builder's error taxonomy for the response.
			as := &fakeArtifactStore{getErr: errors.New("db temporarily unavailable")}
			handlerFlaky := handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
			fb.cancelErr = nil

			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/orphaned/cancel", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("orphaned")

			Expect(handlerFlaky.Cancel(c)).To(Succeed())
			Expect(fb.cancelledIDs).To(ContainElement("orphaned"))
			// Cancel succeeded but the store cannot show a record; the
			// handler falls back to a generic cancelled envelope.
			Expect(rec.Code).To(Equal(http.StatusOK))
		})
	})

	Describe("Delete (store-backed)", func() {
		var (
			as               *fakeArtifactStore
			handlerWithStore *handlers.ArtifactHandler
		)

		BeforeEach(func() {
			as = &fakeArtifactStore{
				records: []*store.ArtifactRecord{
					{ID: "art-pending", Phase: store.ArtifactPending, BaseImage: "img"},
				},
			}
			handlerWithStore = handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, "", "reg-token", "http://localhost:8080")
		})

		It("completes cleanup for a Pending record even when Cancel wraps ErrNotSupported", func() {
			fb.cancelErr = wrappedNotSupported()

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/art-pending", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("art-pending")

			Expect(handlerWithStore.Delete(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNoContent))

			_, lookupErr := as.GetByID(nil, "art-pending")
			Expect(lookupErr).To(HaveOccurred())
		})
	})
})
