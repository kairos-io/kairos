package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/auth"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/labstack/echo/v4"
)

var _ = Describe("SettingsHandler.RotateRegistrationToken persistence", func() {
	rotate := func(handler *handlers.SettingsHandler) *httptest.ResponseRecorder {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/registration-token/rotate", nil)
		rec := httptest.NewRecorder()
		Expect(handler.RotateRegistrationToken(e.NewContext(req, rec))).To(Succeed())
		return rec
	}

	It("returns 500 and keeps the old token when the file cannot be written", func() {
		token := auth.NewRegistrationToken("initial-token")
		missing := filepath.Join(GinkgoT().TempDir(), "no-such-dir", "token")

		rec := rotate(handlers.NewSettingsHandler(token, missing))

		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		var resp map[string]string
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp).To(HaveKey("error"))
		Expect(resp).NotTo(HaveKey("registrationToken"))
		Expect(token.Get()).To(Equal("initial-token"))
	})

	It("writes the new token to the file and swaps it in memory", func() {
		token := auth.NewRegistrationToken("initial-token")
		path := filepath.Join(GinkgoT().TempDir(), "token")

		rec := rotate(handlers.NewSettingsHandler(token, path))

		Expect(rec.Code).To(Equal(http.StatusOK))
		var resp map[string]string
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp["registrationToken"]).NotTo(BeEmpty())
		Expect(resp["registrationToken"]).NotTo(Equal("initial-token"))
		Expect(token.Get()).To(Equal(resp["registrationToken"]))
		onDisk, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(onDisk)).To(Equal(resp["registrationToken"]))
	})

	It("keeps the file and the in-memory token equal under concurrent rotations", func() {
		token := auth.NewRegistrationToken("initial-token")
		path := filepath.Join(GinkgoT().TempDir(), "token")
		handler := handlers.NewSettingsHandler(token, path)

		for round := 0; round < 200; round++ {
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wg.Done()
					rotate(handler)
				}()
			}
			wg.Wait()

			onDisk, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(onDisk)).To(Equal(token.Get()), "round %d", round)
		}
	})
})
