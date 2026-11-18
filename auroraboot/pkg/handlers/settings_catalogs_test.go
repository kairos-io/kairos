package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/labstack/echo/v4"
)

var _ = Describe("SettingsHandler extension catalogs", func() {
	type response struct {
		Launch []string `json:"launch"`
		Saved  []string `json:"saved"`
	}

	var (
		e        *echo.Echo
		token    *auth.RegistrationToken
		settings *fakeSettingsStore
	)

	BeforeEach(func() {
		e = echo.New()
		token = auth.NewRegistrationToken("tok")
		settings = newFakeSettingsStore()
	})

	newHandler := func(launch ...string) *handlers.SettingsHandler {
		return handlers.NewSettingsHandler(token, "").
			WithImageSource(settings, nil, "").
			WithExtensionCatalogs(launch)
	}

	doGet := func(h *handlers.SettingsHandler) (*httptest.ResponseRecorder, response) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/extension-catalogs", nil)
		rec := httptest.NewRecorder()
		Expect(h.GetExtensionCatalogs(e.NewContext(req, rec))).To(Succeed())
		var resp response
		if rec.Code == http.StatusOK {
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		}
		return rec, resp
	}

	doPut := func(h *handlers.SettingsHandler, body string) (*httptest.ResponseRecorder, response) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/extension-catalogs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		Expect(h.UpdateExtensionCatalogs(e.NewContext(req, rec))).To(Succeed())
		var resp response
		if rec.Code == http.StatusOK {
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		}
		return rec, resp
	}

	It("reports empty lists, not null, when nothing is configured", func() {
		rec, _ := doGet(newHandler())
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(MatchJSON(`{"launch":[],"saved":[]}`))
	})

	It("reports the launch catalogs", func() {
		_, resp := doGet(newHandler("https://example.com/a.json", "https://example.com/b.json"))
		Expect(resp.Launch).To(Equal([]string{"https://example.com/a.json", "https://example.com/b.json"}))
		Expect(resp.Saved).To(BeEmpty())
	})

	It("saves catalogs trimmed, de-duplicated and in order, and reads them back", func() {
		h := newHandler("https://launch.example.com/c.json")
		rec, resp := doPut(h, `{"saved":[" https://b.example.com/c.json ","https://a.example.com/c.json","","https://b.example.com/c.json"]}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(resp.Saved).To(Equal([]string{"https://b.example.com/c.json", "https://a.example.com/c.json"}))
		Expect(resp.Launch).To(Equal([]string{"https://launch.example.com/c.json"}))

		_, resp = doGet(h)
		Expect(resp.Saved).To(Equal([]string{"https://b.example.com/c.json", "https://a.example.com/c.json"}))
	})

	It("clears the saved list with an empty array", func() {
		h := newHandler()
		doPut(h, `{"saved":["https://a.example.com/c.json"]}`)
		rec, resp := doPut(h, `{"saved":[]}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(resp.Saved).To(BeEmpty())
	})

	DescribeTable("rejects a catalog the browser cannot read",
		func(value string) {
			body, _ := json.Marshal(map[string][]string{"saved": {value}})
			rec, _ := doPut(newHandler(), string(body))
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(settings.values).ToNot(HaveKey(handlers.SettingExtensionCatalogs))
		},
		Entry("a local path", "/srv/catalog.json"),
		Entry("a file URL", "file:///srv/catalog.json"),
		Entry("no host", "https:///catalog.json"),
		Entry("another scheme", "ftp://example.com/catalog.json"),
	)

	It("rejects writes when no settings store is wired", func() {
		h := handlers.NewSettingsHandler(token, "")
		rec, _ := doPut(h, `{"saved":["https://a.example.com/c.json"]}`)
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
	})
})
