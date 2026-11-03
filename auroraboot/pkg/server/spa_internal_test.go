package server

import (
	"net/http"
	"net/http/httptest"
	"testing/fstest"

	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("serving the web UI", func() {
	get := func(assets fstest.MapFS, path string) *httptest.ResponseRecorder {
		e := echo.New()
		e.GET("/healthz", func(c echo.Context) error {
			return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
		})
		setupSPA(e, assets)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	It("serves index.html when the UI is built in", func() {
		rec := get(fstest.MapFS{"dist/index.html": {Data: []byte("<html>ui</html>")}}, "/")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring("<html>ui</html>"))
	})

	It("serves index.html for HTML requests to unknown SPA routes", func() {
		rec := get(fstest.MapFS{"dist/index.html": {Data: []byte("<html>ui</html>")}}, "/some/spa/route")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring("<html>ui</html>"))
	})

	It("says the binary has no web UI when it was built without one", func() {
		rec := get(fstest.MapFS{}, "/")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
		Expect(rec.Body.String()).To(ContainSubstring("built without the web UI"))
		Expect(rec.Body.String()).To(ContainSubstring("release binaries"))
		Expect(rec.Body.String()).ToNot(ContainSubstring("make"))
	})

	It("does not answer unknown API paths with the UI", func() {
		rec := get(fstest.MapFS{"dist/index.html": {Data: []byte("<html>ui</html>")}}, "/api/v1/nope")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
		Expect(rec.Body.String()).ToNot(ContainSubstring("<html>ui</html>"))
	})

	It("returns 404 for an unknown path when the request does not accept HTML", func() {
		e := echo.New()
		setupSPA(e, fstest.MapFS{"dist/index.html": {Data: []byte("<html>ui</html>")}})
		req := httptest.NewRequest(http.MethodGet, "/some/spa/route", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	// Browsers and some probes send Accept: text/html to every URL. A route
	// the server registered must answer itself, with or without the UI.
	DescribeTable("leaves registered routes to their handlers",
		func(assets fstest.MapFS) {
			rec := get(assets, "/healthz")
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Body.String()).To(ContainSubstring(`"status":"ok"`))
		},
		Entry("with the UI built in", fstest.MapFS{"dist/index.html": {Data: []byte("<html>ui</html>")}}),
		Entry("without the UI", fstest.MapFS{}),
	)
})
