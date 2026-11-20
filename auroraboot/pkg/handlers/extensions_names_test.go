package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/kairos-io/kairos/auroraboot/pkg/handlers"
	"github.com/kairos-io/kairos/auroraboot/pkg/store"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ArtifactHandler.ResolveBundle: download URL of the extension", func() {
	var (
		e       *echo.Echo
		es      *fakeExtensionStore
		bs      *fakeBundleStore
		handler *handlers.ArtifactHandler
		ctx     = context.Background()
	)

	BeforeEach(func() {
		e = echo.New()
		es = newFakeExtensionStore()
		bs = newFakeBundleStore()
		as := &fakeArtifactStore{}
		Expect(as.Create(ctx, &store.ArtifactRecord{ID: "a-1"})).To(Succeed())
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, as, nil, nil, es, bs, "", "reg-token", "http://auroraboot:8080/")
	})

	resolve := func() []handlers.ResolvedBundleEntry {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/a-1/bundle-resolve", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("a-1")
		Expect(handler.ResolveBundle(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		var out []handlers.ResolvedBundleEntry
		Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed())
		return out
	}

	// Extensions built before names were validated can still carry any of
	// these characters, so the URL has to survive them.
	DescribeTable("keeps the whole file name in the path and the token in the query",
		func(name string) {
			Expect(es.Create(ctx, &store.ExtensionRecord{
				ID: "e-1", Name: name, Type: "sysext", Phase: "Ready",
				RawFilename: name + ".sysext.raw", DownloadToken: "tok&123",
			})).To(Succeed())
			Expect(bs.ReplaceForArtifact(ctx, "a-1", []store.ArtifactExtensionBundle{
				{ArtifactID: "a-1", ExtensionName: name, ExtensionType: "sysext"},
			})).To(Succeed())

			out := resolve()
			Expect(out).To(HaveLen(1))
			u, err := url.Parse(out[0].Source)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Host).To(Equal("auroraboot:8080"))
			Expect(u.Fragment).To(BeEmpty())
			Expect(u.Path).To(Equal("/api/v1/extensions/e-1/download/" + name + ".sysext.raw"))
			Expect(u.Query()).To(Equal(url.Values{"token": {"tok&123"}}))
		},
		Entry("a plain name", "tools"),
		Entry("a name with '#'", "tools#2"),
		Entry("a name with '?'", "tools?beta"),
		Entry("a name with '%'", "tools%zz"),
		Entry("a name with a space", "my tools"),
	)
})

var _ = Describe("ExtensionHandler: name validation", func() {
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
		Expect(es.Create(ctx, &store.ExtensionRecord{ID: "e-1", Name: "tools", Type: "sysext", Phase: "Ready"})).To(Succeed())
	})

	create := func(name string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{
			"name": name, "type": "sysext", "arch": "amd64",
			"source": map[string]string{"mode": "image", "baseImage": "ubuntu:24.04"},
		})
		Expect(err).ToNot(HaveOccurred())
		req := httptest.NewRequest(http.MethodPost, "/api/v1/extensions", strings.NewReader(string(body)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		Expect(handler.Create(e.NewContext(req, rec))).To(Succeed())
		return rec
	}

	rename := func(name string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]string{"name": name})
		Expect(err).ToNot(HaveOccurred())
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/extensions/e-1", strings.NewReader(string(body)))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("e-1")
		Expect(handler.Update(c)).To(Succeed())
		return rec
	}

	DescribeTable("refuses a name that cannot be the extension's file name",
		func(name string) {
			rec := create(name)
			Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
			Expect(rec.Body.String()).To(ContainSubstring("name must not"))
			Expect(fb.lastOpts.Name).To(BeEmpty())

			rec = rename(name)
			Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
			Expect(rec.Body.String()).To(ContainSubstring("name must not"))
			got, err := es.GetByID(ctx, "e-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(got.Name).To(Equal("tools"))
		},
		Entry("a slash", "tools/extra"),
		Entry("a backslash", `tools\extra`),
		Entry("a parent directory", ".."),
		Entry("two dots inside", "tools..extra"),
		Entry("a leading dash, read as a flag of the build", "-tools"),
		Entry("a leading dot, a hidden image", ".tools"),
		Entry("a newline", "tools\nextra"),
		Entry("a NUL byte", "tools\x00"),
	)

	It("accepts ordinary names, and a name the URL has to escape", func() {
		for _, name := range []string{"tailscale-agent", "tools_2", "k9s.v0.32", "tools#2"} {
			rec := create(name)
			Expect(rec.Code).To(Equal(http.StatusCreated), name+": "+rec.Body.String())
			Expect(fb.lastOpts.Name).To(Equal(name))

			rec = rename(name)
			Expect(rec.Code).To(Equal(http.StatusOK), name+": "+rec.Body.String())
		}
	})
})
