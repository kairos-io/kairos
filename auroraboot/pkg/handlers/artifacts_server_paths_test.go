package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// The HTTP API never takes a filesystem path on the server. Overlays are
// referenced by the ID upload-overlay returns, signing goes through a key
// set, extension catalogs are remote URLs, and a path sent under a field the
// API does not have is dropped by decoding before it can reach the builder.
var _ = Describe("ArtifactHandler: no server paths in the API", func() {
	var (
		e            *echo.Echo
		fb           *fakeBuilder
		as           *fakeArtifactStore
		handler      *handlers.ArtifactHandler
		artifactsDir string
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeBuilder{}
		as = &fakeArtifactStore{}
		artifactsDir = GinkgoT().TempDir()
		handler = handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, artifactsDir, "reg-token", "http://localhost:8080")
	})

	create := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		Expect(handler.Create(e.NewContext(req, rec))).To(Succeed())
		return rec
	}

	errorOf := func(rec *httptest.ResponseRecorder) string {
		var resp map[string]string
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		return resp["error"]
	}

	makeOverlay := func() string {
		id := uuid.New().String()
		Expect(os.MkdirAll(filepath.Join(artifactsDir, "overlays", id), 0o755)).To(Succeed())
		return id
	}

	Describe("Create takes no server paths", func() {
		DescribeTable("a path sent in a field outside the API never reaches the builder",
			func(fragment string) {
				rec := create(`{"baseImage":"ubuntu:24.04","outputs":{"iso":true},` + fragment + `}`)
				Expect(rec.Code).To(Equal(http.StatusCreated))
				Expect(fb.lastOpts.OverlayRootfs).To(BeEmpty())
				Expect(fb.lastOpts.BuildContextDir).To(BeEmpty())
				Expect(fb.lastOpts.OutputDir).To(BeEmpty())
				Expect(fb.lastOpts.Signing.UKISecureBootKey).To(BeEmpty())
				Expect(fb.lastOpts.Signing.UKISecureBootCert).To(BeEmpty())
				Expect(fb.lastOpts.Signing.UKITPMPCRKey).To(BeEmpty())
				Expect(fb.lastOpts.Signing.UKIPublicKeysDir).To(BeEmpty())
			},
			Entry("overlayRootfs", `"overlayRootfs":"/etc"`),
			Entry("buildContextDir", `"buildContextDir":"/etc"`),
			Entry("outputDir", `"outputDir":"/tmp/out"`),
			Entry("signing.ukiSecureBootKey", `"signing":{"ukiSecureBootKey":"/k/db.key"}`),
			Entry("signing.ukiSecureBootCert", `"signing":{"ukiSecureBootCert":"/k/db.pem"}`),
			Entry("signing.ukiTpmPcrKey", `"signing":{"ukiTpmPcrKey":"/k/tpm.pem"}`),
			Entry("signing.ukiPublicKeysDir", `"signing":{"ukiPublicKeysDir":"/k"}`),
		)

		It("keeps accepting ukiSecureBootEnroll without a key set", func() {
			rec := create(`{"baseImage":"ubuntu:24.04","outputs":{"iso":true},"signing":{"ukiSecureBootEnroll":"if-safe"}}`)
			Expect(rec.Code).To(Equal(http.StatusCreated))
			Expect(fb.lastOpts.Signing.UKISecureBootEnroll).To(Equal("if-safe"))
			Expect(fb.lastOpts.Signing.UKISecureBootKey).To(BeEmpty())
		})

		DescribeTable("returns 400 for an extension catalog that is not an http(s) URL",
			func(catalog string) {
				rec := create(`{"baseImage":"ubuntu:24.04","outputs":{"iso":true},"extensions":["nvidia"],` +
					`"extensionsCatalogs":["https://example.test/releases.json",` + strconv.Quote(catalog) + `]}`)
				Expect(rec.Code).To(Equal(http.StatusBadRequest))
				Expect(errorOf(rec)).To(ContainSubstring("extensionsCatalogs"))
				Expect(fb.builds).To(BeEmpty())
			},
			Entry("absolute path", "/etc/passwd"),
			Entry("relative path", "../secrets/catalog.json"),
			Entry("file URL", "file:///etc/passwd"),
			Entry("URL without a host", "https:///releases.json"),
		)

		It("passes http(s) extension catalogs to the builder", func() {
			rec := create(`{"baseImage":"ubuntu:24.04","outputs":{"iso":true},"extensions":["nvidia"],` +
				`"extensionsCatalogs":["https://example.test/releases.json","http://mirror.test/releases.json"]}`)
			Expect(rec.Code).To(Equal(http.StatusCreated))
			Expect(fb.lastOpts.ExtensionsCatalogs).To(Equal([]string{"https://example.test/releases.json", "http://mirror.test/releases.json"}))
		})
	})

	Describe("Create resolves overlayId", func() {
		DescribeTable("returns 400 for an overlayId that is not an uploaded overlay",
			func(id string) {
				Expect(os.MkdirAll(filepath.Join(artifactsDir, "x"), 0o755)).To(Succeed())
				body, err := json.Marshal(map[string]any{
					"baseImage": "ubuntu:24.04",
					"outputs":   map[string]bool{"iso": true},
					"overlayId": id,
				})
				Expect(err).NotTo(HaveOccurred())
				rec := create(string(body))
				Expect(rec.Code).To(Equal(http.StatusBadRequest))
				Expect(errorOf(rec)).To(ContainSubstring("overlayId"))
				Expect(fb.builds).To(BeEmpty())
			},
			Entry("parent traversal", "../x"),
			Entry("absolute path", "/etc"),
			Entry("arbitrary string", "not-a-uuid"),
			Entry("UUID in braces", "{"+uuid.New().String()+"}"),
			Entry("URN-form UUID", "urn:uuid:"+uuid.New().String()),
			Entry("well-formed UUID with no uploaded overlay", uuid.New().String()),
		)

		It("returns 400 when the overlay ID names a file, not a directory", func() {
			id := uuid.New().String()
			Expect(os.MkdirAll(filepath.Join(artifactsDir, "overlays"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(artifactsDir, "overlays", id), []byte("x"), 0o644)).To(Succeed())
			rec := create(`{"baseImage":"ubuntu:24.04","outputs":{"iso":true},"overlayId":"` + id + `"}`)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(fb.builds).To(BeEmpty())
		})

		It("passes the resolved directory to the builder and records the ID", func() {
			id := makeOverlay()
			rec := create(`{"baseImage":"ubuntu:24.04","outputs":{"iso":true},"overlayId":"` + id + `"}`)
			Expect(rec.Code).To(Equal(http.StatusCreated))

			Expect(fb.lastOpts.OverlayRootfs).To(Equal(filepath.Join(artifactsDir, "overlays", id)))
			Expect(fb.lastOpts.OverlayID).To(Equal(id))
			Expect(as.records).To(HaveLen(1))
			Expect(as.records[0].OverlayID).To(Equal(id))
		})

		It("leaves the overlay unset when no overlayId is sent", func() {
			rec := create(`{"baseImage":"ubuntu:24.04","outputs":{"iso":true}}`)
			Expect(rec.Code).To(Equal(http.StatusCreated))
			Expect(fb.lastOpts.OverlayRootfs).To(BeEmpty())
			Expect(fb.lastOpts.OverlayID).To(BeEmpty())
		})
	})

	It("returns the stored record from Create", func() {
		overlayID := makeOverlay()
		rec := create(`{"name":"edge","baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true},"overlayId":"` + overlayID + `"}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))

		var out store.ArtifactRecord
		Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed())
		Expect(out.ID).NotTo(BeEmpty())
		Expect(out.Name).To(Equal("edge"))
		Expect(out.OverlayID).To(Equal(overlayID))
		Expect(out.Phase).To(Equal(store.ArtifactPending))
		Expect(out.BaseImage).To(Equal("quay.io/kairos/ubuntu:24.04"))
	})

	It("returns the row the builder wrote when it persists the record itself", func() {
		h := handlers.NewArtifactHandler(&persistingBuilder{fakeBuilder: fb, store: as}, as, nil, nil, nil, nil, artifactsDir, "reg-token", "http://localhost:8080")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(`{"name":"edge","baseImage":"quay.io/kairos/ubuntu:24.04","outputs":{"iso":true}}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		Expect(h.Create(e.NewContext(req, rec))).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusCreated))

		var out store.ArtifactRecord
		Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed())
		Expect(out.Name).To(Equal("written-by-builder"))
	})

	Describe("UploadOverlay", func() {
		It("returns the overlay ID and no server path", func() {
			buf, contentType := uploadOverlayMultipart([]byte("hello"), "motd")
			req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/upload-overlay", buf)
			req.Header.Set(echo.HeaderContentType, contentType)
			rec := httptest.NewRecorder()
			Expect(handler.UploadOverlay(e.NewContext(req, rec))).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var resp map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp).To(HaveLen(1))
			Expect(resp).To(HaveKey("id"))
			id, ok := resp["id"].(string)
			Expect(ok).To(BeTrue())
			Expect(uuid.Parse(id)).Error().NotTo(HaveOccurred())
			Expect(rec.Body.String()).NotTo(ContainSubstring(artifactsDir))

			data, err := os.ReadFile(filepath.Join(artifactsDir, "overlays", id, "motd"))
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(Equal([]byte("hello")))
		})
	})

	Describe("overlay cleanup", func() {
		var victim string

		BeforeEach(func() {
			// A directory outside the overlays dir that a crafted stored
			// value would point at.
			victim = filepath.Join(artifactsDir, "victim")
			Expect(os.MkdirAll(victim, 0o755)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(artifactsDir, "overlays"), 0o755)).To(Succeed())
		})

		deleteRecord := func(id string) {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/"+id, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(id)
			Expect(handler.Delete(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNoContent))
		}

		clearFailed := func() {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/failed", nil)
			rec := httptest.NewRecorder()
			Expect(handler.ClearFailed(e.NewContext(req, rec))).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNoContent))
		}

		It("Delete removes the overlay named by a stored UUID", func() {
			id := makeOverlay()
			as.records = []*store.ArtifactRecord{{ID: "art-1", Phase: store.ArtifactReady, OverlayID: id}}
			deleteRecord("art-1")
			Expect(filepath.Join(artifactsDir, "overlays", id)).NotTo(BeADirectory())
		})

		It("ClearFailed removes the overlay named by a stored UUID", func() {
			id := makeOverlay()
			as.records = []*store.ArtifactRecord{{ID: "art-err", Phase: store.ArtifactError, OverlayID: id}}
			clearFailed()
			Expect(filepath.Join(artifactsDir, "overlays", id)).NotTo(BeADirectory())
		})

		// A cloned build carries the overlayId of the build it was cloned
		// from, so several records can reference one overlay directory.
		It("Delete keeps an overlay another record still references, and removes it with the last one", func() {
			id := makeOverlay()
			overlay := filepath.Join(artifactsDir, "overlays", id)
			as.records = []*store.ArtifactRecord{
				{ID: "art-1", Phase: store.ArtifactReady, OverlayID: id},
				{ID: "art-2", Phase: store.ArtifactReady, OverlayID: id},
			}

			deleteRecord("art-1")
			Expect(overlay).To(BeADirectory())

			deleteRecord("art-2")
			Expect(overlay).NotTo(BeADirectory())
		})

		It("ClearFailed keeps an overlay a record that is not failed still references", func() {
			id := makeOverlay()
			overlay := filepath.Join(artifactsDir, "overlays", id)
			as.records = []*store.ArtifactRecord{
				{ID: "art-err", Phase: store.ArtifactError, OverlayID: id},
				{ID: "art-ok", Phase: store.ArtifactReady, OverlayID: id},
			}

			clearFailed()
			Expect(overlay).To(BeADirectory())
		})

		It("ClearFailed removes an overlay shared only by failed records", func() {
			id := makeOverlay()
			overlay := filepath.Join(artifactsDir, "overlays", id)
			as.records = []*store.ArtifactRecord{
				{ID: "art-err1", Phase: store.ArtifactError, OverlayID: id},
				{ID: "art-err2", Phase: store.ArtifactError, OverlayID: id},
			}

			clearFailed()
			Expect(overlay).NotTo(BeADirectory())
		})

		DescribeTable("Delete refuses a stored overlay ID that is not a UUID",
			func(stored string) {
				stored = strings.ReplaceAll(stored, "{dir}", artifactsDir)
				as.records = []*store.ArtifactRecord{{ID: "art-1", Phase: store.ArtifactReady, OverlayID: stored}}
				deleteRecord("art-1")
				Expect(victim).To(BeADirectory())
				Expect(filepath.Join(artifactsDir, "overlays")).To(BeADirectory())
				Expect(artifactsDir).To(BeADirectory())
			},
			refusedOverlayIDEntries(),
		)

		DescribeTable("ClearFailed refuses a stored overlay ID that is not a UUID",
			func(stored string) {
				stored = strings.ReplaceAll(stored, "{dir}", artifactsDir)
				as.records = []*store.ArtifactRecord{{ID: "art-err", Phase: store.ArtifactError, OverlayID: stored}}
				clearFailed()
				Expect(victim).To(BeADirectory())
				Expect(filepath.Join(artifactsDir, "overlays")).To(BeADirectory())
				Expect(artifactsDir).To(BeADirectory())
			},
			refusedOverlayIDEntries(),
		)
	})
})

var _ = Describe("ExtensionHandler.Create: no server paths in the API", func() {
	It("never hands a build context path to the builder", func() {
		e := echo.New()
		fb := &fakeExtensionBuilder{}
		handler := handlers.NewExtensionHandler(fb, newFakeExtensionStore(), newFakeBundleStore(), nil, nil, "")

		body := `{"name":"x","type":"sysext","arch":"amd64","source":{"mode":"dockerfile","dockerfile":"FROM scratch","buildContextDir":"/etc"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		Expect(handler.Create(e.NewContext(req, rec))).To(Succeed())

		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.Source.BuildContextDir).To(BeEmpty())
	})
})

// refusedOverlayIDEntries lists stored overlay IDs that cleanup must refuse.
// "{dir}" stands for the test's artifacts dir, which is only known once a
// spec runs, so the table body substitutes it.
func refusedOverlayIDEntries() []TableEntry {
	return []TableEntry{
		Entry("parent traversal", "../victim"),
		Entry("empty-looking dot", "."),
		Entry("parent dir", ".."),
		Entry("absolute path", "{dir}/victim"),
		Entry("path under the artifacts dir that climbs out of it", "{dir}/overlays/../victim"),
		Entry("artifacts dir itself", "{dir}"),
	}
}

// readBackFailingStore fails GetByID once Create has stored a record, so the
// read-back after a build has started fails while the existence check before
// it does not.
type readBackFailingStore struct {
	*fakeArtifactStore
	created bool
}

func (s *readBackFailingStore) Create(ctx context.Context, rec *store.ArtifactRecord) error {
	if err := s.fakeArtifactStore.Create(ctx, rec); err != nil {
		return err
	}
	s.created = true
	return nil
}

func (s *readBackFailingStore) GetByID(ctx context.Context, id string) (*store.ArtifactRecord, error) {
	if s.created {
		return nil, errors.New("read-back failed")
	}
	return s.fakeArtifactStore.GetByID(ctx, id)
}

var _ = Describe("ArtifactHandler.Create when the stored build cannot be read back", func() {
	It("answers 201 with the build status, because the build has started", func() {
		fb := &fakeBuilder{}
		failing := &readBackFailingStore{fakeArtifactStore: &fakeArtifactStore{}}
		handler := handlers.NewArtifactHandler(fb, failing, nil, nil, nil, nil, GinkgoT().TempDir(), "reg-token", "http://localhost:8080")

		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(`{"baseImage":"ubuntu:24.04","outputs":{"iso":true}}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		Expect(handler.Create(echo.New().NewContext(req, rec))).To(Succeed())

		Expect(rec.Code).To(Equal(http.StatusCreated))
		var resp map[string]any
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp).To(HaveKeyWithValue("id", fb.builds[0].ID))
		Expect(resp).To(HaveKeyWithValue("phase", "Pending"))
	})
})

// persistingBuilder writes the artifact row inside Build, as the local
// builder does, so Create finds an existing row instead of writing its own.
type persistingBuilder struct {
	*fakeBuilder
	store *fakeArtifactStore
}

func (b *persistingBuilder) Build(ctx context.Context, opts builder.BuildOptions) (*builder.BuildStatus, error) {
	st, err := b.fakeBuilder.Build(ctx, opts)
	if err != nil {
		return nil, err
	}
	Expect(b.store.Create(ctx, &store.ArtifactRecord{ID: st.ID, Name: "written-by-builder", Phase: store.ArtifactPending})).To(Succeed())
	return st, nil
}
