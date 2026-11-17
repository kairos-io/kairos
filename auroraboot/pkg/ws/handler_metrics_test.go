package ws_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"time"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/metrics"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// failingHeartbeatNodes wraps a node store and fails UpdateHeartbeat when fail
// is set, as it would for a node deleted while its socket is still open.
type failingHeartbeatNodes struct {
	*gormstore.NodeStoreAdapter
	fail atomic.Bool
}

func (f *failingHeartbeatNodes) UpdateHeartbeat(ctx context.Context, id string, agentVersion string, osRelease map[string]string, addresses []store.NodeAddress, bootState string, hostname string, remoteIP string) error {
	if f.fail.Load() {
		return errors.New("node not found")
	}
	return f.NodeStoreAdapter.UpdateHeartbeat(ctx, id, agentVersion, osRelease, addresses, bootState, hostname, remoteIP)
}

var _ = Describe("WebSocket heartbeat metrics", func() {
	var (
		nodes  *failingHeartbeatNodes
		hub    *ws.Hub
		buf    *metrics.Buffer
		server *httptest.Server
		nodeID string
		apiKey string
	)

	BeforeEach(func() {
		n := testCounter.Add(1)
		db, err := gormstore.New(filepath.Join(GinkgoT().TempDir(), fmt.Sprintf("ws_metrics_%d.db", n)))
		Expect(err).NotTo(HaveOccurred())
		nodes = &failingHeartbeatNodes{NodeStoreAdapter: &gormstore.NodeStoreAdapter{S: db}}
		node := &store.ManagedNode{MachineID: fmt.Sprintf("metrics-machine-%d", n), Labels: map[string]string{}}
		Expect(nodes.Register(GinkgoT().Context(), node)).To(Succeed())
		nodeID, apiKey = node.ID, node.APIKey

		hub = ws.NewHub()
		buf = metrics.NewBuffer(metrics.DefaultCapacity)
		h := &ws.AgentHandler{Hub: hub, Nodes: nodes, Commands: &gormstore.CommandStoreAdapter{S: db}, Metrics: buf}
		e := echo.New()
		e.GET("/api/v1/ws", h.HandleAgentWS)
		server = httptest.NewServer(e)
		DeferCleanup(func() {
			server.Close()
			_ = db.Close()
		})
	})

	It("records the metrics sent with a heartbeat", func() {
		conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()
		Eventually(func() bool { return hub.IsOnline(nodeID) }, 10*time.Second, 50*time.Millisecond).Should(BeTrue())

		Expect(sendMsg(conn, "heartbeat", map[string]any{
			"agentVersion": "2.0.0",
			"metrics":      json.RawMessage(`{"cpu":{"usedPercent":87.2}}`),
		})).To(Succeed())

		Eventually(func() float64 {
			m, ok := buf.Latest(nodeID)
			if !ok || m.CPU == nil {
				return 0
			}
			return m.CPU.UsedPercent
		}, 10*time.Second, 50*time.Millisecond).Should(Equal(87.2))
	})

	It("records nothing when the heartbeat has no metrics", func() {
		conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()
		Eventually(func() bool { return hub.IsOnline(nodeID) }, 10*time.Second, 50*time.Millisecond).Should(BeTrue())

		Expect(sendMsg(conn, "heartbeat", heartbeatData{AgentVersion: "2.0.1"})).To(Succeed())
		Consistently(func() bool {
			_, ok := buf.Latest(nodeID)
			return ok
		}, 500*time.Millisecond, 50*time.Millisecond).Should(BeFalse())
	})

	It("records nothing when the heartbeat update fails", func() {
		conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()
		Eventually(func() bool { return hub.IsOnline(nodeID) }, 10*time.Second, 50*time.Millisecond).Should(BeTrue())

		nodes.fail.Store(true)
		Expect(sendMsg(conn, "heartbeat", map[string]any{
			"agentVersion": "2.0.2",
			"metrics":      json.RawMessage(`{"cpu":{"usedPercent":55}}`),
		})).To(Succeed())
		Consistently(func() bool {
			_, ok := buf.Latest(nodeID)
			return ok
		}, 500*time.Millisecond, 50*time.Millisecond).Should(BeFalse())
	})
})
