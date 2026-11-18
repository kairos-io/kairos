package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/metrics"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

var _ = Describe("MetricsHandler", func() {
	var (
		e       *echo.Echo
		buf     *metrics.Buffer
		handler *handlers.MetricsHandler
	)

	BeforeEach(func() {
		e = echo.New()
		buf = metrics.NewBuffer(metrics.DefaultCapacity)
		handler = handlers.NewMetricsHandler(buf)
	})

	getNode := func(nodeID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID+"/metrics", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("nodeID")
		c.SetParamValues(nodeID)
		Expect(handler.GetNode(c)).To(Succeed())
		return rec
	}

	It("returns the latest sample and all samples of a node", func() {
		buf.Record("node-1", store.NodeMetrics{CPU: &store.CPUMetrics{UsedPercent: 10}})
		buf.Record("node-1", store.NodeMetrics{CPU: &store.CPUMetrics{UsedPercent: 20}})

		rec := getNode("node-1")
		Expect(rec.Code).To(Equal(http.StatusOK))
		var body handlers.APINodeMetricsResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.Latest).NotTo(BeNil())
		Expect(body.Latest.CPU.UsedPercent).To(Equal(20.0))
		Expect(body.Samples).To(HaveLen(2))
		Expect(body.Samples[0].CPU.UsedPercent).To(Equal(10.0))
	})

	It("returns a null latest and an empty samples list for an unknown node", func() {
		rec := getNode("nope")
		Expect(rec.Code).To(Equal(http.StatusOK))
		var raw map[string]json.RawMessage
		Expect(json.Unmarshal(rec.Body.Bytes(), &raw)).To(Succeed())
		Expect(string(raw["latest"])).To(Equal("null"))
		Expect(string(raw["samples"])).To(Equal("[]"))
	})

	It("returns the latest sample of every node", func() {
		buf.Record("node-1", store.NodeMetrics{CPU: &store.CPUMetrics{UsedPercent: 10}})
		buf.Record("node-2", store.NodeMetrics{CPU: &store.CPUMetrics{UsedPercent: 30}})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/latest", nil)
		rec := httptest.NewRecorder()
		Expect(handler.GetLatest(e.NewContext(req, rec))).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))
		var body map[string]store.NodeMetrics
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(HaveLen(2))
		Expect(body["node-2"].CPU.UsedPercent).To(Equal(30.0))
	})

	It("returns an empty object when no node has metrics", func() {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/latest", nil)
		rec := httptest.NewRecorder()
		Expect(handler.GetLatest(e.NewContext(req, rec))).To(Succeed())
		Expect(rec.Body.String()).To(MatchJSON(`{}`))
	})
})
