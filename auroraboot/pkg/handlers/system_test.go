package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/labstack/echo/v4"
)

var _ = Describe("SystemHandler", func() {
	var e *echo.Echo

	BeforeEach(func() {
		e = echo.New()
	})

	// decodeBody unmarshals the response body into a map, which is enough to
	// assert both the field values and the omitempty behaviour in one shot:
	// gomega's Equal on maps requires an exact key set, so absent fields are
	// proven by their absence from the expected map.
	decodeBody := func(rec *httptest.ResponseRecorder) map[string]any {
		var raw map[string]any
		Expect(json.Unmarshal(rec.Body.Bytes(), &raw)).To(Succeed())
		return raw
	}

	Describe("GetBuilder", func() {
		It("reports the local backend without cluster or namespace", func() {
			handler := handlers.NewSystemHandler(handlers.APISystemBuilder{
				Backend:           "local",
				DownloadSupported: true,
			})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/system/builder", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.GetBuilder(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(decodeBody(rec)).To(Equal(map[string]any{
				"backend":           "local",
				"downloadSupported": true,
			}))
		})

		It("reports the operator backend with cluster and namespace", func() {
			handler := handlers.NewSystemHandler(handlers.APISystemBuilder{
				Backend:           "operator",
				Cluster:           "https://kind.example",
				Namespace:         "kairos-builds",
				DownloadSupported: false,
			})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/system/builder", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			Expect(handler.GetBuilder(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(decodeBody(rec)).To(Equal(map[string]any{
				"backend":           "operator",
				"cluster":           "https://kind.example",
				"namespace":         "kairos-builds",
				"downloadSupported": false,
			}))
		})
	})
})
