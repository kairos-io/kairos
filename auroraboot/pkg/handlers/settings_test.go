package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/labstack/echo/v4"
)

var _ = Describe("SettingsHandler", func() {
	var (
		e       *echo.Echo
		token   *auth.RegistrationToken
		handler *handlers.SettingsHandler
	)

	BeforeEach(func() {
		e = echo.New()
		token = auth.NewRegistrationToken("initial-token")
		handler = handlers.NewSettingsHandler(token, "")
	})

	Describe("GetRegistrationToken", func() {
		It("should return the current token", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/registration-token", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.GetRegistrationToken(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var resp map[string]string
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["registrationToken"]).To(Equal("initial-token"))
		})
	})

	Describe("RotateRegistrationToken", func() {
		It("should rotate the token", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/registration-token/rotate", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handler.RotateRegistrationToken(c)
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Code).To(Equal(http.StatusOK))

			var resp map[string]string
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["registrationToken"]).NotTo(Equal("initial-token"))
			Expect(resp["registrationToken"]).NotTo(BeEmpty())
			// The shared holder the middleware reads must see the new token
			Expect(token.Get()).To(Equal(resp["registrationToken"]))
		})
	})

	Describe("rotation racing registration", func() {
		It("keeps the registration middleware consistent with concurrent rotations", func() {
			register := auth.RegistrationTokenAuth(token)(func(c echo.Context) error {
				return c.String(http.StatusOK, "ok")
			})

			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(2)
				go func() {
					defer wg.Done()
					defer GinkgoRecover()
					for j := 0; j < 50; j++ {
						req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/registration-token/rotate", nil)
						Expect(handler.RotateRegistrationToken(e.NewContext(req, httptest.NewRecorder()))).To(Succeed())
					}
				}()
				go func() {
					defer wg.Done()
					defer GinkgoRecover()
					for j := 0; j < 50; j++ {
						body := `{"registrationToken":"stale-token","hostname":"node"}`
						req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/register", strings.NewReader(body))
						req.Header.Set("Content-Type", "application/json")
						rec := httptest.NewRecorder()
						Expect(register(e.NewContext(req, rec))).To(Succeed())
						Expect(rec.Code).To(Equal(http.StatusUnauthorized))
					}
				}()
			}
			wg.Wait()
		})
	})
})
