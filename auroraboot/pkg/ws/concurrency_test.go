package ws_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/ws"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These specs exercise the per-connection write lock added to defend against
// gorilla/websocket's "concurrent write to websocket connection" panic. They are
// meaningful under `go test -race`: without serialized writes the race detector
// fires (or gorilla panics) when several goroutines write to one conn.
var _ = Describe("WebSocket concurrent writers", func() {
	var (
		hub      *ws.Hub
		gormDB   *gormstore.Store
		nodes    store.NodeStore
		commands store.CommandStore
		server   *httptest.Server
		nodeID   string
		apiKey   string
	)

	BeforeEach(func() {
		var err error
		machineNum := testCounter.Add(1)
		dbName := fmt.Sprintf("%s/ws_conc_%d.db", GinkgoT().TempDir(), machineNum)
		gormDB, err = gormstore.New(dbName)
		Expect(err).NotTo(HaveOccurred())

		nodes = &gormstore.NodeStoreAdapter{S: gormDB}
		commands = &gormstore.CommandStoreAdapter{S: gormDB}
		hub = ws.NewHub()

		testNode := &store.ManagedNode{
			MachineID: fmt.Sprintf("machine-conc-%d", machineNum),
			Hostname:  "conc-host",
			Labels:    map[string]string{},
		}
		Expect(nodes.Register(context.Background(), testNode)).To(Succeed())
		nodeID = testNode.ID
		apiKey = testNode.APIKey

		agentHandler := &ws.AgentHandler{Hub: hub, Nodes: nodes, Commands: commands}
		uiHandler := &ws.UIHandler{Hub: hub}

		e := echo.New()
		e.GET("/api/v1/ws", agentHandler.HandleAgentWS)
		e.GET("/api/v1/ws/ui", uiHandler.HandleUIWS)
		server = httptest.NewServer(e)

		DeferCleanup(func() {
			server.Close()
			_ = gormDB.Close()
		})
	})

	It("serializes many concurrent SendCommand to one node without racing", func() {
		conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		Eventually(func() bool { return hub.IsOnline(nodeID) },
			10*time.Second, 50*time.Millisecond).Should(BeTrue())

		const n = 200

		// Drain the reader so writes don't block on a full buffer; count the
		// command frames we receive back.
		received := make(chan struct{}, n)
		done := make(chan struct{})
		go func() {
			defer close(done)
			conn.SetReadDeadline(time.Now().Add(15 * time.Second))
			for i := 0; i < n; i++ {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
				received <- struct{}{}
			}
		}()

		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				Expect(hub.SendCommand(nodeID, commandData{
					ID:      fmt.Sprintf("cmd-%d", i),
					Command: "exec",
				})).To(Succeed())
			}(i)
		}
		wg.Wait()

		got := 0
		for got < n {
			select {
			case <-received:
				got++
			case <-time.After(10 * time.Second):
				Fail(fmt.Sprintf("only received %d/%d command frames", got, n))
			}
		}
		Expect(got).To(Equal(n))
	})

	It("serializes concurrent UI Broadcasts to one connection without racing", func() {
		uiURL := "ws" + server.URL[len("http"):] + "/api/v1/ws/ui"
		conn, _, err := websocket.DefaultDialer.Dial(uiURL, nil)
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		Eventually(func() int { return hub.UI.Count() },
			5*time.Second, 50*time.Millisecond).Should(Equal(1))

		const n = 200
		received := make(chan struct{}, n)
		go func() {
			conn.SetReadDeadline(time.Now().Add(15 * time.Second))
			for {
				mt, _, err := conn.ReadMessage()
				if err != nil {
					return
				}
				// Ignore control frames; count text broadcasts only.
				if mt == websocket.TextMessage {
					received <- struct{}{}
				}
			}
		}()

		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				hub.UI.Broadcast(map[string]any{"type": "test", "seq": i})
			}(i)
		}
		wg.Wait()

		got := 0
		for got < n {
			select {
			case <-received:
				got++
			case <-time.After(10 * time.Second):
				Fail(fmt.Sprintf("only received %d/%d broadcast frames", got, n))
			}
		}
		Expect(got).To(Equal(n))
	})

	It("drops a UI client that stops reading instead of blocking every broadcast", func() {
		DeferCleanup(ws.SetWriteWait(200 * time.Millisecond))

		good, _, err := dialWS(server, "/api/v1/ws/ui")
		Expect(err).NotTo(HaveOccurred())
		defer good.Close()
		Eventually(hub.UI.Count, 5*time.Second, 20*time.Millisecond).Should(Equal(1))

		// Never read from this one, so its socket buffers fill up.
		stuck, _, err := dialWS(server, "/api/v1/ws/ui")
		Expect(err).NotTo(HaveOccurred())
		defer stuck.Close()
		Eventually(hub.UI.Count, 5*time.Second, 20*time.Millisecond).Should(Equal(2))

		var goodGot atomic.Int64
		go func() {
			for {
				mt, _, err := good.ReadMessage()
				if err != nil {
					return
				}
				if mt == websocket.TextMessage {
					goodGot.Add(1)
				}
			}
		}()

		pad := strings.Repeat("x", 1<<20)
		var sent atomic.Int64
		var slowest atomic.Int64
		broadcastDone := make(chan struct{})
		go func() {
			defer close(broadcastDone)
			for i := 0; i < 128 && hub.UI.Count() > 1; i++ {
				start := time.Now()
				hub.UI.Broadcast(map[string]any{"type": "test", "seq": i, "pad": pad})
				if d := int64(time.Since(start)); d > slowest.Load() {
					slowest.Store(d)
				}
				sent.Add(1)
			}
		}()

		Eventually(broadcastDone, 20*time.Second).Should(BeClosed(), "Broadcast blocked on the client that stopped reading")
		Expect(time.Duration(slowest.Load())).To(BeNumerically("<", 2*time.Second))
		Eventually(hub.UI.Count, 5*time.Second, 20*time.Millisecond).Should(Equal(1))

		hub.UI.Broadcast(map[string]any{"type": "test", "seq": "after"})
		Eventually(goodGot.Load, 10*time.Second, 20*time.Millisecond).Should(Equal(sent.Load() + 1))
	})
})
