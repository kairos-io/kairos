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
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/labstack/echo/v4"
)

var _ = Describe("SettingsHandler image-source", func() {
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

	// newHandler builds a settings handler wired with the fake settings store and,
	// optionally, a launch-configured (non-nil) iso-serve.
	newHandler := func(withServe bool) *handlers.SettingsHandler {
		var serve *isoserve.Server
		if withServe {
			serve = isoserve.New(isoserve.Config{BaseURL: "http://10.0.0.5:8090"})
		}
		return handlers.NewSettingsHandler(token, "").
			WithImageSource(settings, serve, "http://10.0.0.5:8090")
	}

	doGet := func(h *handlers.SettingsHandler) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/image-source", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(h.GetImageSource(c)).To(Succeed())
		return rec
	}

	doPut := func(h *handlers.SettingsHandler, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/image-source", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(h.UpdateImageSource(c)).To(Succeed())
		return rec
	}

	Describe("GET", func() {
		It("reports configured=false and seeded advertised URL with no listener", func() {
			rec := doGet(newHandler(false))
			Expect(rec.Code).To(Equal(http.StatusOK))

			var resp struct {
				DefaultImageURL string `json:"defaultImageURL"`
				LocalServe      struct {
					Configured    bool   `json:"configured"`
					Enabled       bool   `json:"enabled"`
					AdvertisedURL string `json:"advertisedURL"`
					UsesTLS       bool   `json:"usesTLS"`
				} `json:"localServe"`
			}
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.LocalServe.Configured).To(BeFalse())
			Expect(resp.LocalServe.Enabled).To(BeFalse())
			Expect(resp.LocalServe.AdvertisedURL).To(Equal("http://10.0.0.5:8090"))
		})

		It("reports configured=true when a listener is wired", func() {
			rec := doGet(newHandler(true))
			var resp struct {
				LocalServe struct {
					Configured bool `json:"configured"`
				} `json:"localServe"`
			}
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.LocalServe.Configured).To(BeTrue())
		})

		It("returns the persisted default image URL", func() {
			settings.values[handlers.SettingDefaultImageURL] = "https://10.0.0.5/os.iso"
			rec := doGet(newHandler(false))
			var resp struct {
				DefaultImageURL string `json:"defaultImageURL"`
			}
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.DefaultImageURL).To(Equal("https://10.0.0.5/os.iso"))
		})
	})

	Describe("PUT", func() {
		It("persists a valid default image URL", func() {
			rec := doPut(newHandler(false), `{"defaultImageURL":"https://10.0.0.5/os.iso"}`)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(settings.values[handlers.SettingDefaultImageURL]).To(Equal("https://10.0.0.5/os.iso"))
		})

		It("rejects an SSRF-blocked default image URL with 400", func() {
			rec := doPut(newHandler(false), `{"defaultImageURL":"http://169.254.169.254/x.iso"}`)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("invalid defaultImageURL"))
			Expect(settings.values).NotTo(HaveKey(handlers.SettingDefaultImageURL))
		})

		It("allows clearing the default image URL with an empty string", func() {
			settings.values[handlers.SettingDefaultImageURL] = "https://10.0.0.5/os.iso"
			rec := doPut(newHandler(false), `{"defaultImageURL":""}`)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(settings.values[handlers.SettingDefaultImageURL]).To(BeEmpty())
		})

		It("rejects enabling local serve with 409 when no listener is configured", func() {
			rec := doPut(newHandler(false), `{"localServeEnabled":true}`)
			Expect(rec.Code).To(Equal(http.StatusConflict))
			Expect(rec.Body.String()).To(ContainSubstring("no --redfish-serve-addr"))
			Expect(settings.values).NotTo(HaveKey(handlers.SettingLocalServeEnabled))
		})

		It("enables local serve when a listener is configured", func() {
			rec := doPut(newHandler(true), `{"localServeEnabled":true}`)
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(settings.values[handlers.SettingLocalServeEnabled]).To(Equal("true"))
		})

		It("rejects an SSRF-blocked advertised URL with 400", func() {
			rec := doPut(newHandler(true), `{"localServeAdvertisedURL":"http://169.254.169.254/"}`)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("invalid localServeAdvertisedURL"))
		})
	})
})
