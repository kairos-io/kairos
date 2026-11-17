package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ExtensionHandler.Create", func() {
	var (
		e       *echo.Echo
		fb      *fakeExtensionBuilder
		es      *fakeExtensionStore
		bs      *fakeBundleStore
		handler *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeExtensionBuilder{}
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		handler = handlers.NewExtensionHandler(fb, es, bs, nil, nil, "")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	It("creates a sysext build and returns 201 with a Pending status", func() {
		rec := post(`{"name":"tailscale-agent","type":"sysext","arch":"amd64","version":"v1.74.0",
			"source":{"mode":"image","baseImage":"ubuntu:24.04"}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))

		var status builder.ExtensionBuildStatus
		Expect(json.Unmarshal(rec.Body.Bytes(), &status)).To(Succeed())
		Expect(status.Phase).To(Equal(builder.BuildPending))
		Expect(status.ID).ToNot(BeEmpty())
		Expect(fb.lastOpts.Type).To(Equal("sysext"))
		Expect(fb.lastOpts.Source.Mode).To(Equal("image"))
		Expect(fb.lastOpts.Source.BaseImage).To(Equal("ubuntu:24.04"))
	})
})

var _ = Describe("ExtensionHandler.Create — image reference validation", func() {
	var (
		e       *echo.Echo
		fb      *fakeExtensionBuilder
		handler *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeExtensionBuilder{}
		handler = handlers.NewExtensionHandler(fb, newFakeExtensionStore(), newFakeBundleStore(), nil, nil, "")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	It("rejects a base image that would add lines to the generated Dockerfile", func() {
		rec := post(`{"name":"x","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04\nRUN id"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("source.baseImage"))
		Expect(fb.lastOpts.Name).To(BeEmpty())
	})
})

var _ = Describe("ExtensionHandler.Create — hierarchies validation", func() {
	var (
		e       *echo.Echo
		fb      *fakeExtensionBuilder
		handler *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeExtensionBuilder{}
		handler = handlers.NewExtensionHandler(fb, newFakeExtensionStore(), newFakeBundleStore(), nil, nil, "")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	base := `"name":"x","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04"}`

	It("rejects a path without a leading slash", func() {
		rec := post(`{` + base + `,"hierarchies":["opt"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("hierarchies[0]"))
	})

	It("rejects a path containing ..", func() {
		rec := post(`{` + base + `,"hierarchies":["/opt/../etc"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring(".."))
	})

	It("rejects exactly /usr", func() {
		rec := post(`{` + base + `,"hierarchies":["/usr"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("/usr"))
	})

	It("rejects exactly /", func() {
		rec := post(`{` + base + `,"hierarchies":["/"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("rejects a path longer than 256 chars", func() {
		long := "/" + strings.Repeat("a", 256)
		rec := post(`{` + base + `,"hierarchies":["` + long + `"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("256"))
	})

	It("normalizes: trims trailing slashes, dedupes, sorts alphabetically", func() {
		rec := post(`{` + base + `,"hierarchies":["/srv/","/opt","/srv","/opt/"]}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.Hierarchies).To(Equal([]string{"/opt", "/srv"}))
	})

	It("accepts nil hierarchies for a confext", func() {
		rec := post(`{"name":"fb","type":"confext","arch":"amd64","source":{"mode":"image","baseImage":"alpine:3"}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.Hierarchies).To(BeNil())
	})

	// `auroraboot confext` declares neither --include-path nor
	// --service-reload, so forwarding either ended the build in phase Error
	// with "flag provided but not defined" rather than telling the caller.
	It("rejects hierarchies on a confext", func() {
		rec := post(`{"name":"fb","type":"confext","arch":"amd64","source":{"mode":"image","baseImage":"alpine:3"},"hierarchies":["/srv"]}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("sysext-only"))
		Expect(fb.lastOpts.Name).To(BeEmpty(), "must not reach the builder")
	})

	It("rejects serviceReload on a confext", func() {
		rec := post(`{"name":"fb","type":"confext","arch":"amd64","source":{"mode":"image","baseImage":"alpine:3"},"serviceReload":true}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("sysext-only"))
		Expect(fb.lastOpts.Name).To(BeEmpty(), "must not reach the builder")
	})

	It("still accepts both on a sysext", func() {
		rec := post(`{` + base + `,"hierarchies":["/srv"],"serviceReload":true}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.Hierarchies).To(Equal([]string{"/srv"}))
		Expect(fb.lastOpts.ServiceReload).To(BeTrue())
	})
})

var _ = Describe("ExtensionHandler.Create — signing key set linkage", func() {
	var (
		e       *echo.Echo
		fb      *fakeExtensionBuilder
		es      *fakeExtensionStore
		sb      *fakeSecureBootKeySetStore
		handler *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeExtensionBuilder{}
		es = newFakeExtensionStore()
		sb = &fakeSecureBootKeySetStore{}
		Expect(sb.Create(context.Background(), &store.SecureBootKeySet{
			Name: "prod", KeysDir: "/keys/prod",
		})).To(Succeed())
		handler = handlers.NewExtensionHandler(fb, es, newFakeBundleStore(), sb, nil, "")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	// The keyset used to be attached by re-reading and re-saving the row after
	// Build returned. Build's goroutine is already running by then and its
	// first act is a full-row upsert of the phase, so that second write raced
	// it and could blank signing_key_set_id for good. The ID now travels in
	// the build options, which the builder writes into the initial record
	// before starting the goroutine.
	It("passes the key set id to the builder instead of re-saving the row", func() {
		rec := post(`{"name":"signed","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04"},"signingKeySetId":"ks-1"}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.SigningKeySetID).To(Equal("ks-1"))
		Expect(fb.lastOpts.Signing.PrivateKey).To(Equal(filepath.Join("/keys/prod", "db.key")))
		Expect(fb.lastOpts.Signing.Certificate).To(Equal(filepath.Join("/keys/prod", "db.pem")))
	})

	It("400s on an unknown key set", func() {
		rec := post(`{"name":"signed","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04"},"signingKeySetId":"nope"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("leaves the options empty when no key set was asked for", func() {
		rec := post(`{"name":"plain","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04"}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.SigningKeySetID).To(BeEmpty())
	})

	// Every build gets its own download bearer, so the install command's
	// source URL can name the extension without carrying the admin password.
	// It travels in the build options for the same reason the keyset does:
	// the builder must write it into the record its synchronous Create
	// persists, not a second read-modify-write racing the build goroutine.
	It("mints a download token and passes it to the builder", func() {
		rec := post(`{"name":"plain","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04"}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.DownloadToken).To(HaveLen(64),
			"32 bytes of crypto/rand, hex encoded")
		Expect(fb.lastOpts.DownloadToken).To(MatchRegexp(`^[0-9a-f]{64}$`))
	})

	It("mints a different download token for every build", func() {
		Expect(post(`{"name":"a","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04"}}`).Code).
			To(Equal(http.StatusCreated))
		first := fb.lastOpts.DownloadToken
		Expect(post(`{"name":"b","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04"}}`).Code).
			To(Equal(http.StatusCreated))
		Expect(fb.lastOpts.DownloadToken).ToNot(Equal(first))
	})

	// The response the UI gets back on Create must not be a place the token
	// leaks by accident, and neither must the build status type in general.
	It("does not put the download token in the Create response", func() {
		rec := post(`{"name":"plain","type":"sysext","arch":"amd64","source":{"mode":"image","baseImage":"ubuntu:24.04"}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(rec.Body.String()).ToNot(ContainSubstring(fb.lastOpts.DownloadToken))
	})
})

var _ = Describe("ExtensionHandler.Create — source/mode validation", func() {
	var (
		e       *echo.Echo
		handler *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		handler = handlers.NewExtensionHandler(&fakeExtensionBuilder{}, newFakeExtensionStore(), newFakeBundleStore(), nil, nil, "")
	})

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.Create(c)).To(Succeed())
		return rec
	}

	common := `"name":"x","type":"sysext","arch":"amd64"`

	It("rejects an unsupported source.mode", func() {
		rec := post(`{` + common + `,"source":{"mode":"voodoo"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("mode"))
	})

	It("requires source.baseImage for mode=image", func() {
		rec := post(`{` + common + `,"source":{"mode":"image"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("baseImage"))
	})

	It("requires source.artifactId for mode=artifact", func() {
		rec := post(`{` + common + `,"source":{"mode":"artifact"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("artifactId"))
	})

	It("requires source.dockerfile for mode=dockerfile", func() {
		rec := post(`{` + common + `,"source":{"mode":"dockerfile"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("dockerfile"))
	})

	It("rejects extraSteps with a FROM line", func() {
		rec := post(`{` + common + `,"source":{"mode":"artifact","artifactId":"a-1","extraSteps":"FROM ubuntu:24.04\nRUN ls"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("FROM"))
	})

	It("rejects extraSteps with a FROM line preceded by whitespace and case-insensitive", func() {
		rec := post(`{` + common + `,"source":{"mode":"artifact","artifactId":"a-1","extraSteps":"  from ubuntu:24.04"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("accepts extraSteps without any FROM line", func() {
		rec := post(`{` + common + `,"source":{"mode":"artifact","artifactId":"a-1","extraSteps":"RUN curl -fsSL https://tailscale.com/install.sh | sh"}}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
	})

	It("rejects unsupported arch", func() {
		rec := post(`{"name":"x","type":"sysext","arch":"i386","source":{"mode":"image","baseImage":"ubuntu:24.04"}}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("arch"))
	})
})

var _ = Describe("ExtensionHandler — Get / List / PATCH / GetLogs / Cancel", func() {
	var (
		e       *echo.Echo
		fb      *fakeExtensionBuilder
		es      *fakeExtensionStore
		handler *handlers.ExtensionHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeExtensionBuilder{}
		es = newFakeExtensionStore()
		handler = handlers.NewExtensionHandler(fb, es, newFakeBundleStore(), nil, nil, "")
		_ = es.Create(ctx, &store.ExtensionRecord{ID: "e-1", Name: "ts", Type: "sysext", Phase: "Ready", Arch: "amd64", Logs: "step 1\nstep 2"})
	})

	withParam := func(method, path, id, body string) (echo.Context, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		return c, rec
	}

	It("Get returns the extension record", func() {
		c, rr := withParam(http.MethodGet, "/api/v1/extensions/e-1", "e-1", "")
		Expect(handler.Get(c)).To(Succeed())
		Expect(rr.Code).To(Equal(http.StatusOK))
		Expect(rr.Body.String()).To(ContainSubstring(`"name":"ts"`))
	})

	It("Get returns 404 when the extension does not exist", func() {
		c, rr := withParam(http.MethodGet, "/api/v1/extensions/missing", "missing", "")
		Expect(handler.Get(c)).To(Succeed())
		Expect(rr.Code).To(Equal(http.StatusNotFound))
	})

	It("List returns every record", func() {
		_ = es.Create(ctx, &store.ExtensionRecord{ID: "e-2", Name: "x", Type: "confext", Phase: "Ready"})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/extensions", nil)
		rr := httptest.NewRecorder()
		c := e.NewContext(req, rr)
		Expect(handler.List(c)).To(Succeed())
		Expect(rr.Code).To(Equal(http.StatusOK))
		Expect(rr.Body.String()).To(ContainSubstring("e-1"))
		Expect(rr.Body.String()).To(ContainSubstring("e-2"))
	})

	It("PATCH renames the extension", func() {
		c, rr := withParam(http.MethodPatch, "/api/v1/extensions/e-1", "e-1", `{"name":"tailscale-renamed"}`)
		Expect(handler.Update(c)).To(Succeed())
		Expect(rr.Code).To(Equal(http.StatusOK))
		got, _ := es.GetByID(ctx, "e-1")
		Expect(got.Name).To(Equal("tailscale-renamed"))
	})

	It("PATCH rejects an empty body", func() {
		c, rr := withParam(http.MethodPatch, "/api/v1/extensions/e-1", "e-1", `{}`)
		Expect(handler.Update(c)).To(Succeed())
		Expect(rr.Code).To(Equal(http.StatusBadRequest))
	})

	It("GetLogs returns the appended log buffer as text/plain", func() {
		c, rr := withParam(http.MethodGet, "/api/v1/extensions/e-1/logs", "e-1", "")
		Expect(handler.GetLogs(c)).To(Succeed())
		Expect(rr.Code).To(Equal(http.StatusOK))
		Expect(rr.Body.String()).To(Equal("step 1\nstep 2"))
	})

	It("Cancel delegates to the builder", func() {
		c, rr := withParam(http.MethodPost, "/api/v1/extensions/e-1/cancel", "e-1", "")
		Expect(handler.Cancel(c)).To(Succeed())
		Expect(rr.Code).To(Equal(http.StatusNoContent))
		Expect(fb.cancels).To(Equal([]string{"e-1"}))
	})
})

var _ = Describe("ExtensionHandler.Delete — bundle-blocks-delete", func() {
	var (
		e       *echo.Echo
		es      *fakeExtensionStore
		bs      *fakeBundleStore
		handler *handlers.ExtensionHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		handler = handlers.NewExtensionHandler(&fakeExtensionBuilder{}, es, bs, nil, nil, "")
		_ = es.Create(ctx, &store.ExtensionRecord{ID: "e-1", Name: "tailscale-agent", Type: "sysext", Phase: "Ready"})
	})

	doDelete := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/extensions/"+id, nil)
		rr := httptest.NewRecorder()
		c := e.NewContext(req, rr)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(handler.Delete(c)).To(Succeed())
		return rr
	}

	It("allows deletion when no bundle references the name", func() {
		rec := doDelete("e-1")
		Expect(rec.Code).To(Equal(http.StatusNoContent))
		_, err := es.GetByID(ctx, "e-1")
		Expect(err).To(HaveOccurred())
	})

	It("returns 409 when a bundle references the extension by name", func() {
		_ = bs.ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "tailscale-agent", ExtensionType: "sysext"},
		})
		rec := doDelete("e-1")
		Expect(rec.Code).To(Equal(http.StatusConflict))
		Expect(rec.Body.String()).To(ContainSubstring("a-1"))
		// Record must NOT be deleted.
		_, err := es.GetByID(ctx, "e-1")
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns 404 for a missing extension", func() {
		rec := doDelete("nope")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})
})

var _ = Describe("ExtensionHandler.Update: bundle-blocks-rename", func() {
	var (
		e       *echo.Echo
		es      *fakeExtensionStore
		bs      *fakeBundleStore
		handler *handlers.ExtensionHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		handler = handlers.NewExtensionHandler(&fakeExtensionBuilder{}, es, bs, nil, nil, "")
		_ = es.Create(ctx, &store.ExtensionRecord{ID: "e-1", Name: "tailscale-agent", Type: "sysext", Phase: "Ready"})
	})

	doPatch := func(id, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/extensions/"+id, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rr := httptest.NewRecorder()
		c := e.NewContext(req, rr)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(handler.Update(c)).To(Succeed())
		return rr
	}

	It("allows renaming when no bundle references the name", func() {
		rec := doPatch("e-1", `{"name":"tailscale-renamed"}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		got, _ := es.GetByID(ctx, "e-1")
		Expect(got.Name).To(Equal("tailscale-renamed"))
	})

	It("returns 409 when a bundle references the old name", func() {
		_ = bs.ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "tailscale-agent", ExtensionType: "sysext"},
		})
		rec := doPatch("e-1", `{"name":"tailscale-renamed"}`)
		Expect(rec.Code).To(Equal(http.StatusConflict))
		Expect(rec.Body.String()).To(ContainSubstring("a-1"))
		got, _ := es.GetByID(ctx, "e-1")
		Expect(got.Name).To(Equal("tailscale-agent"))
	})

	It("accepts a referenced extension keeping its own name", func() {
		_ = bs.ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
			{ArtifactID: "a-1", ExtensionName: "tailscale-agent", ExtensionType: "sysext"},
		})
		rec := doPatch("e-1", `{"name":"tailscale-agent"}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
	})
})

var _ = Describe("ExtensionHandler.Download", func() {
	var (
		e       *echo.Echo
		tmp     string
		es      *fakeExtensionStore
		handler *handlers.ExtensionHandler
	)

	BeforeEach(func() {
		e = echo.New()
		tmp = GinkgoT().TempDir()
		extDir := filepath.Join(tmp, "extensions", "e-1")
		Expect(os.MkdirAll(extDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(extDir, "tailscale-agent.sysext.raw"), []byte("RAW-BYTES"), 0o644)).To(Succeed())
		es = newFakeExtensionStore()
		Expect(es.Create(context.Background(), &store.ExtensionRecord{ID: "e-1", Name: "tailscale-agent", Type: "sysext", Phase: "Ready"})).To(Succeed())
		handler = handlers.NewExtensionHandler(&fakeExtensionBuilder{}, es, newFakeBundleStore(), nil, nil, tmp)
	})

	do := func(id, filename string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/extensions/"+id+"/download/"+filename, nil)
		rr := httptest.NewRecorder()
		c := e.NewContext(req, rr)
		c.SetParamNames("id", "filename")
		c.SetParamValues(id, filename)
		Expect(handler.Download(c)).To(Succeed())
		return rr
	}

	It("serves the .raw file", func() {
		rec := do("e-1", "tailscale-agent.sysext.raw")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("RAW-BYTES"))
	})

	It("returns 404 for a missing file", func() {
		rec := do("e-1", "missing.raw")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	It("rejects path traversal in filename", func() {
		rec := do("e-1", "../../etc/passwd")
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("rejects path traversal in id", func() {
		rec := do("../e-1", "foo.raw")
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("rejects a NUL byte in the filename", func() {
		// An escaped "%00" in the URL decodes to a NUL byte in the param.
		req := httptest.NewRequest(http.MethodGet, "/api/v1/extensions/e-1/download/tailscale-agent.sysext.raw%00", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id", "filename")
		c.SetParamValues("e-1", "tailscale-agent.sysext.raw\x00")
		Expect(handler.Download(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 404 when the file is on disk but the extension record is gone", func() {
		Expect(es.Delete(context.Background(), "e-1")).To(Succeed())
		rec := do("e-1", "tailscale-agent.sysext.raw")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	It("returns 404 after the extension is deleted through the handler", func() {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/extensions/e-1", nil)
		rr := httptest.NewRecorder()
		c := e.NewContext(req, rr)
		c.SetParamNames("id")
		c.SetParamValues("e-1")
		Expect(handler.Delete(c)).To(Succeed())
		Expect(rr.Code).To(Equal(http.StatusNoContent))

		_, err := os.Stat(filepath.Join(tmp, "extensions", "e-1"))
		Expect(os.IsNotExist(err)).To(BeTrue(), "the extension output directory should be removed")
		Expect(filepath.Join(tmp, "extensions")).To(BeADirectory())

		rec := do("e-1", "tailscale-agent.sysext.raw")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})
})
