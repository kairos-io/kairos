package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/metrics"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
)

var _ = Describe("NodeHandler metrics", func() {
	var (
		e       *echo.Echo
		ns      *fakeNodeStore
		buf     *metrics.Buffer
		handler *handlers.NodeHandler
	)

	BeforeEach(func() {
		e = echo.New()
		ns = &fakeNodeStore{nodes: []*store.ManagedNode{{ID: "node-1", Phase: store.PhaseRegistered}}}
		buf = metrics.NewBuffer(metrics.DefaultCapacity)
		handler = handlers.NewNodeHandler(ns, &fakeCommandStore{}, &fakeGroupStore{}, ws.NewHub(), "reg-token", "http://localhost:8080")
		handler.SetMetrics(buf)
	})

	postHeartbeat := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/node-1/heartbeat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("nodeID")
		c.SetParamValues("node-1")
		Expect(handler.Heartbeat(c)).To(Succeed())
		return rec
	}

	It("records the metrics sent with a heartbeat", func() {
		rec := postHeartbeat(`{"agentVersion":"1.1","metrics":{"cpu":{"usedPercent":87.2},"load":[1,2,3]}}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		m, ok := buf.Latest("node-1")
		Expect(ok).To(BeTrue())
		Expect(m.CPU).NotTo(BeNil())
		Expect(m.CPU.UsedPercent).To(Equal(87.2))
		Expect(m.Load).To(Equal([]float64{1, 2, 3}))
		Expect(m.SampledAt.IsZero()).To(BeFalse())
	})

	It("records nothing when the heartbeat has no metrics", func() {
		rec := postHeartbeat(`{"agentVersion":"1.1"}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		_, ok := buf.Latest("node-1")
		Expect(ok).To(BeFalse())
	})

	It("still accepts the heartbeat when the metrics do not decode", func() {
		rec := postHeartbeat(`{"agentVersion":"1.2","metrics":{"cpu":"lots"}}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(ns.nodes[0].Phase).To(Equal(store.PhaseOnline))
		Expect(ns.nodes[0].AgentVersion).To(Equal("1.2"))
		_, ok := buf.Latest("node-1")
		Expect(ok).To(BeFalse())
	})

	It("ignores metrics when no buffer is set", func() {
		handler = handlers.NewNodeHandler(ns, &fakeCommandStore{}, &fakeGroupStore{}, ws.NewHub(), "reg-token", "http://localhost:8080")
		handler.SetMetrics(nil)
		rec := postHeartbeat(`{"agentVersion":"1.1","metrics":{"cpu":{"usedPercent":87.2}}}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
	})

	It("forgets the metrics of a deleted node", func() {
		buf.Record("node-1", store.NodeMetrics{CPU: &store.CPUMetrics{UsedPercent: 10}})
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/node-1", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("nodeID")
		c.SetParamValues("node-1")
		Expect(handler.Delete(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusNoContent))
		_, ok := buf.Latest("node-1")
		Expect(ok).To(BeFalse())
	})
})
