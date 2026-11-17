package ws_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

var testCounter atomic.Int64

// wsMessage mirrors the envelope used by the handler.
type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type heartbeatData struct {
	AgentVersion string            `json:"agentVersion"`
	OSRelease    map[string]string `json:"osRelease,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Hostname     string            `json:"hostname,omitempty"`
}

type commandData struct {
	ID      string            `json:"id"`
	Command string            `json:"command"`
	Args    map[string]string `json:"args,omitempty"`
}

// bootReport is one call of AgentHandler.OnNodeBooted.
type bootReport struct {
	nodeID, bootState string
	bootedAt          *time.Time
}

type commandStatusData struct {
	ID     string `json:"id"`
	Phase  string `json:"phase"`
	Result string `json:"result,omitempty"`
}

func dialWS(server *httptest.Server, path string) (*websocket.Conn, *http.Response, error) {
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + path
	return websocket.DefaultDialer.Dial(wsURL, nil)
}

func sendMsg(conn *websocket.Conn, msgType string, data interface{}) error {
	raw, _ := json.Marshal(data)
	msg := wsMessage{Type: msgType, Data: raw}
	msgBytes, _ := json.Marshal(msg)
	return conn.WriteMessage(websocket.TextMessage, msgBytes)
}

func readMsg(conn *websocket.Conn) (*wsMessage, error) {
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var msg wsMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

var _ = Describe("WebSocket Handler", func() {
	var (
		hub       *ws.Hub
		gormDB    *gormstore.Store
		nodes     store.NodeStore
		commands  store.CommandStore
		server    *httptest.Server
		nodeID    string
		apiKey    string
		finalized chan string
		phases    chan string
		booted    chan bootReport
	)

	bg := context.Background()

	BeforeEach(func() {
		var err error
		// Every spec gets its own private, on-disk SQLite database in a
		// per-spec temp dir. We deliberately do NOT use a shared-cache
		// in-memory DSN (`mode=memory&cache=shared`): SQLite's shared cache
		// is process-wide, so every spec's in-memory DB would contend on a
		// single global write lock. A prior spec's still-draining read-loop
		// goroutine could then hold that lock long enough (up to the 5s
		// busy_timeout, retried) to starve the current spec's command_status
		// write past its Eventually deadline — the source of the flake.
		// A file-per-spec DB has no cross-spec lock contention.
		machineNum := testCounter.Add(1)
		dbName := filepath.Join(GinkgoT().TempDir(), fmt.Sprintf("ws_test_%d.db", machineNum))
		gormDB, err = gormstore.New(dbName)
		Expect(err).NotTo(HaveOccurred())

		nodes = &gormstore.NodeStoreAdapter{S: gormDB}
		commands = &gormstore.CommandStoreAdapter{S: gormDB}
		hub = ws.NewHub()

		// Create a test node. Register overwrites ID and APIKey.
		testNode := &store.ManagedNode{
			MachineID: fmt.Sprintf("machine-%d", machineNum),
			Hostname:  "test-host",
			Labels:    map[string]string{},
		}
		Expect(nodes.Register(bg, testNode)).To(Succeed())
		nodeID = testNode.ID
		apiKey = testNode.APIKey
		Expect(nodeID).NotTo(BeEmpty())
		Expect(apiKey).NotTo(BeEmpty())

		// Record auto eject-on-phone-home invocations: a WS heartbeat must fire
		// the same finalize hook as the REST register/heartbeat path.
		//
		// The hook closes over a per-spec local, NOT over the shared `finalized`
		// variable. triggerFinalize dispatches on its own goroutine with a 2
		// minute budget, so one can still be in flight when the next spec's
		// BeforeEach runs; closing over the shared variable made that goroutine's
		// read race the next assignment (caught by -race roughly one run in
		// three). A fresh local per spec has no writer.
		finalized = make(chan string, 8)
		specFinalized := finalized
		phases = make(chan string, 8)
		specPhases := phases
		booted = make(chan bootReport, 8)
		specBooted := booted
		agentHandler := &ws.AgentHandler{
			Hub:            hub,
			Nodes:          nodes,
			Commands:       commands,
			Finalize:       func(_ context.Context, id string) { specFinalized <- id },
			OnCommandPhase: func(_ context.Context, id string) { specPhases <- id },
			OnNodeBooted: func(_ context.Context, id, bootState string, bootedAt *time.Time) {
				specBooted <- bootReport{nodeID: id, bootState: bootState, bootedAt: bootedAt}
			},
		}
		uiHandler := &ws.UIHandler{Hub: hub}

		e := echo.New()
		e.GET("/api/v1/ws", agentHandler.HandleAgentWS)
		e.GET("/api/v1/ws/ui", uiHandler.HandleUIWS)
		server = httptest.NewServer(e)

		// Tear down in reverse: close the server first so it blocks until
		// every in-flight WS handler goroutine (and its read-loop) has
		// returned, THEN close the DB. Closing the DB while a read-loop is
		// still writing would race. DeferCleanup runs LIFO, so this is the
		// only teardown hook — no separate AfterEach.
		DeferCleanup(func() {
			server.Close()
			_ = gormDB.Close()
		})
	})

	Describe("Agent connection", func() {
		It("should reject connection without token", func() {
			_, resp, err := dialWS(server, "/api/v1/ws")
			Expect(err).To(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("should reject connection with invalid token", func() {
			_, resp, err := dialWS(server, "/api/v1/ws?token=bad-token")
			Expect(err).To(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("should accept connection with valid token and mark node online", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				return node.Phase
			}, 5*time.Second, 100*time.Millisecond).Should(Equal(store.PhaseOnline))
		})

		It("should mark node offline when agent disconnects", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeFalse())

			Eventually(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				if node == nil {
					return ""
				}
				return node.Phase
			}, 10*time.Second, 100*time.Millisecond).Should(Equal(store.PhaseOffline))
		})

		// kairos-io/kairos#5784: an agent that reconnects before its old socket
		// times out must stay registered and Online when the old socket closes.
		It("keeps a reconnected agent online when its old socket closes", func() {
			oldConn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			newConn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer newConn.Close()
			// The new socket's handler registers before its read loop starts, so
			// once it has applied a heartbeat it owns the hub entry.
			Expect(sendMsg(newConn, "heartbeat", heartbeatData{AgentVersion: "reconnected"})).To(Succeed())
			Eventually(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				if node == nil {
					return ""
				}
				return node.AgentVersion
			}, 10*time.Second, 100*time.Millisecond).Should(Equal("reconnected"))

			oldConn.Close()

			Consistently(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				if node == nil || !hub.IsOnline(nodeID) {
					return "unregistered"
				}
				return node.Phase
			}, 2*time.Second, 100*time.Millisecond).Should(Equal(store.PhaseOnline))

			Expect(hub.SendCommand(nodeID, commandData{ID: "cmd-after", Command: "exec"})).To(Succeed())
			msg, err := readMsg(newConn)
			Expect(err).NotTo(HaveOccurred())
			var cd commandData
			Expect(json.Unmarshal(msg.Data, &cd)).To(Succeed())
			Expect(cd.ID).To(Equal("cmd-after"))

			newConn.Close()
			Eventually(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				if node == nil {
					return ""
				}
				return node.Phase
			}, 10*time.Second, 100*time.Millisecond).Should(Equal(store.PhaseOffline))
			Expect(hub.IsOnline(nodeID)).To(BeFalse())
		})

		It("should handle heartbeat and update node info", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			hb := heartbeatData{
				AgentVersion: "2.0.0",
				OSRelease:    map[string]string{"ID": "kairos"},
			}
			Expect(sendMsg(conn, "heartbeat", hb)).To(Succeed())

			Eventually(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				if node == nil {
					return ""
				}
				return node.AgentVersion
			}, 10*time.Second, 100*time.Millisecond).Should(Equal("2.0.0"))
		})

		// The node in BeforeEach is registered as "test-host", standing in for a
		// node that phoned home before cloud-init applied its real hostname. The
		// WS heartbeat is the only path that can correct that, because the agent
		// stops sending registration payloads once its credentials are on disk
		// (kairos-io/kairos#4196).
		It("should apply a hostname the heartbeat reports", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(sendMsg(conn, "heartbeat", heartbeatData{
				AgentVersion: "2.0.0",
				Hostname:     "kairos-a1b2",
			})).To(Succeed())

			Eventually(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				if node == nil {
					return ""
				}
				return node.Hostname
			}, 10*time.Second, 100*time.Millisecond).Should(Equal("kairos-a1b2"))
		})

		// An agent older than the field sends no hostname at all. That must read
		// as "nothing to report", not as "my hostname is empty".
		It("should keep the stored hostname when the heartbeat omits it", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(sendMsg(conn, "heartbeat", heartbeatData{AgentVersion: "2.0.0"})).To(Succeed())

			// Wait for the heartbeat to have been applied, then assert on the
			// hostname: waiting on agentVersion is what makes this a real check
			// rather than a race the empty-hostname case would also pass.
			Eventually(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				if node == nil {
					return ""
				}
				return node.AgentVersion
			}, 10*time.Second, 100*time.Millisecond).Should(Equal("2.0.0"))

			node, err := nodes.GetByID(bg, nodeID)
			Expect(err).NotTo(HaveOccurred())
			Expect(node.Hostname).To(Equal("test-host"))
		})

		It("should trigger the auto eject-on-phone-home finalizer on heartbeat", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(sendMsg(conn, "heartbeat", heartbeatData{AgentVersion: "2.0.0"})).To(Succeed())

			// The hook fires off the read loop with the node this connection
			// authenticated as — never an agent-chosen id.
			Eventually(finalized, 10*time.Second).Should(Receive(Equal(nodeID)))
		})

		It("should handle command_status and update command", func() {
			cmd := &store.NodeCommand{
				ManagedNodeID: nodeID,
				Command:       "upgrade",
			}
			Expect(commands.Create(bg, cmd)).To(Succeed())
			// Create sets Phase to Pending; mark as Delivered so it won't be sent on connect.
			Expect(commands.UpdateStatus(bg, cmd.ID, store.CommandDelivered, "")).To(Succeed())

			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			status := commandStatusData{
				ID:     cmd.ID,
				Phase:  store.CommandCompleted,
				Result: "success",
			}
			Expect(sendMsg(conn, "command_status", status)).To(Succeed())

			Eventually(func() string {
				c, _ := commands.GetByID(bg, cmd.ID)
				if c == nil {
					return ""
				}
				return c.Phase
			}, 10*time.Second, 100*time.Millisecond).Should(Equal(store.CommandCompleted))
		})

		// A batch waits on its nodes' command reports, and a real agent sends
		// them over this frame, so every applied report reaches the hook.
		It("hands every applied status report to OnCommandPhase", func() {
			cmd := &store.NodeCommand{ManagedNodeID: nodeID, Command: "exec"}
			Expect(commands.Create(bg, cmd)).To(Succeed())
			Expect(commands.UpdateStatus(bg, cmd.ID, store.CommandDelivered, "")).To(Succeed())

			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Expect(sendMsg(conn, "command_status", commandStatusData{ID: cmd.ID, Phase: store.CommandRunning})).To(Succeed())
			Eventually(phases, 10*time.Second).Should(Receive(Equal(cmd.ID)))
			Expect(sendMsg(conn, "command_status", commandStatusData{ID: cmd.ID, Phase: store.CommandFailed, Result: "boom"})).To(Succeed())
			Eventually(phases, 10*time.Second).Should(Receive(Equal(cmd.ID)))
		})

		It("does not hand a rejected status report to OnCommandPhase", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Expect(sendMsg(conn, "command_status", commandStatusData{ID: "not-a-command", Phase: store.CommandFailed})).To(Succeed())
			Consistently(phases, time.Second).ShouldNot(Receive())
		})

		It("drops a status report with an unknown phase and does not hand it to OnCommandPhase", func() {
			cmd := &store.NodeCommand{ManagedNodeID: nodeID, Command: "exec"}
			Expect(commands.Create(bg, cmd)).To(Succeed())
			Expect(commands.UpdateStatus(bg, cmd.ID, store.CommandDelivered, "")).To(Succeed())

			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Expect(sendMsg(conn, "command_status", commandStatusData{ID: cmd.ID, Phase: "Bogus", Result: "x"})).To(Succeed())
			Consistently(phases, time.Second).ShouldNot(Receive())
			c, err := commands.GetByID(bg, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Phase).To(Equal(store.CommandDelivered))
		})

		It("drops a report that moves a finished command back to Running", func() {
			cmd := &store.NodeCommand{ManagedNodeID: nodeID, Command: "exec"}
			Expect(commands.Create(bg, cmd)).To(Succeed())
			Expect(commands.UpdateStatus(bg, cmd.ID, store.CommandDelivered, "")).To(Succeed())

			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Expect(sendMsg(conn, "command_status", commandStatusData{ID: cmd.ID, Phase: store.CommandCompleted, Result: "first"})).To(Succeed())
			Eventually(phases, 10*time.Second).Should(Receive(Equal(cmd.ID)))

			Expect(sendMsg(conn, "command_status", commandStatusData{ID: cmd.ID, Phase: store.CommandRunning, Result: "later"})).To(Succeed())
			Consistently(phases, time.Second).ShouldNot(Receive())
			c, err := commands.GetByID(bg, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Phase).To(Equal(store.CommandCompleted))
			Expect(c.Result).To(Equal("first"))
		})

		It("hands a heartbeat that carries an uptime to OnNodeBooted with its boot time", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			before := time.Now()
			Expect(sendMsg(conn, "heartbeat", map[string]any{
				"agentVersion": "2.0.0",
				"bootState":    "active",
				"metrics":      map[string]any{"uptimeSeconds": 3600},
			})).To(Succeed())

			var got bootReport
			Eventually(booted, 10*time.Second).Should(Receive(&got))
			Expect(got.nodeID).To(Equal(nodeID))
			Expect(got.bootState).To(Equal("active"))
			Expect(got.bootedAt).NotTo(BeNil())
			Expect(*got.bootedAt).To(BeTemporally("~", before.Add(-time.Hour), 5*time.Second))
		})

		It("does not hand a heartbeat without an uptime to OnNodeBooted", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Expect(sendMsg(conn, "heartbeat", map[string]any{"agentVersion": "2.0.0", "bootState": "active"})).To(Succeed())
			Consistently(booted, time.Second).ShouldNot(Receive())
		})

		It("should reject a command_status for a command owned by another node", func() {
			// Register a second node and queue a command for IT.
			otherNum := testCounter.Add(1000)
			other := &store.ManagedNode{
				MachineID: fmt.Sprintf("machine-other-%d", otherNum),
				Hostname:  "other-host",
				Labels:    map[string]string{},
			}
			Expect(nodes.Register(bg, other)).To(Succeed())

			foreign := &store.NodeCommand{
				ManagedNodeID: other.ID,
				Command:       "upgrade",
			}
			Expect(commands.Create(bg, foreign)).To(Succeed())
			Expect(commands.UpdateStatus(bg, foreign.ID, store.CommandDelivered, "")).To(Succeed())

			// Connect as the FIRST node and try to move the other node's command.
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			status := commandStatusData{
				ID:     foreign.ID,
				Phase:  store.CommandCompleted,
				Result: "pwned",
			}
			Expect(sendMsg(conn, "command_status", status)).To(Succeed())

			// The foreign command must stay Delivered — the impersonating
			// node's update is dropped. Consistently holds over the window.
			Consistently(func() string {
				c, _ := commands.GetByID(bg, foreign.ID)
				if c == nil {
					return ""
				}
				return c.Phase
			}, 1*time.Second, 100*time.Millisecond).Should(Equal(store.CommandDelivered))
		})

		It("should send pending commands on connect", func() {
			cmd := &store.NodeCommand{
				ManagedNodeID: nodeID,
				Command:       "reset",
				Args:          map[string]string{"force": "true"},
			}
			Expect(commands.Create(bg, cmd)).To(Succeed())
			// Create sets Phase to Pending, which is what we want.
			cmdID := cmd.ID

			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			msg, err := readMsg(conn)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Type).To(Equal("command"))

			var cd commandData
			Expect(json.Unmarshal(msg.Data, &cd)).To(Succeed())
			Expect(cd.ID).To(Equal(cmdID))
			Expect(cd.Command).To(Equal("reset"))
			Expect(cd.Args["force"]).To(Equal("true"))

			Eventually(func() string {
				c, _ := commands.GetByID(bg, cmdID)
				if c == nil {
					return ""
				}
				return c.Phase
			}, 10*time.Second, 100*time.Millisecond).Should(Equal(store.CommandDelivered))
		})
	})

	Describe("Message size limit", func() {
		oversized := strings.Repeat("x", 4*1024*1024+1)

		// expectClosedByServer reads until the connection fails and checks the
		// server closed it for the message size.
		expectClosedByServer := func(conn *websocket.Conn) {
			GinkgoHelper()
			Expect(conn.SetReadDeadline(time.Now().Add(10 * time.Second))).To(Succeed())
			var err error
			for err == nil {
				_, _, err = conn.ReadMessage()
			}
			Expect(websocket.IsCloseError(err, websocket.CloseMessageTooBig)).To(BeTrue(), "got %v", err)
		}

		It("closes an agent connection that sends a message over 4 MiB and marks the node offline", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(conn.WriteMessage(websocket.TextMessage, []byte(oversized))).To(Succeed())
			expectClosedByServer(conn)

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeFalse())
			Eventually(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				if node == nil {
					return ""
				}
				return node.Phase
			}, 10*time.Second, 100*time.Millisecond).Should(Equal(store.PhaseOffline))
		})

		It("closes a UI connection that sends a message over 4 MiB and unregisters it", func() {
			conn, _, err := dialWS(server, "/api/v1/ws/ui")
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(hub.UI.Count, 10*time.Second, 100*time.Millisecond).Should(Equal(1))

			Expect(conn.WriteMessage(websocket.TextMessage, []byte(oversized))).To(Succeed())
			expectClosedByServer(conn)

			Eventually(hub.UI.Count, 10*time.Second, 100*time.Millisecond).Should(Equal(0))
		})
	})

	// A node that vanishes without closing its socket (power cut, network
	// partition, a NAT or load balancer dropping the flow) leaves the server
	// blocked in ReadMessage forever. Nothing else in AuroraBoot ever writes
	// PhaseOffline — the deferred write in HandleAgentWS is the only one — so
	// without a read deadline such a node reports Online for the rest of the
	// process' life, and the hub keeps pushing commands into a dead socket.
	// These two specs pin both halves: silent peers get reaped, live ones do not.
	Describe("Agent keepalive", func() {
		var (
			kaServer *httptest.Server
			// Short enough to keep the suite fast, and far enough apart that a
			// loaded CI runner cannot reorder them: the server pings 5 times
			// within one read timeout.
			readTimeout  = 1500 * time.Millisecond
			pingInterval = 300 * time.Millisecond
		)

		BeforeEach(func() {
			e := echo.New()
			h := &ws.AgentHandler{
				Hub:          hub,
				Nodes:        nodes,
				Commands:     commands,
				ReadTimeout:  readTimeout,
				PingInterval: pingInterval,
			}
			e.GET("/api/v1/ws", h.HandleAgentWS)
			kaServer = httptest.NewServer(e)
			DeferCleanup(kaServer.Close)
		})

		It("reaps a peer that stops answering pings but keeps the socket open", func() {
			conn, _, err := dialWS(kaServer, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			// Swallow pings instead of ponging them. gorilla's default ping
			// handler replies automatically, so overriding it is what turns a
			// healthy client into a wedged one. Control frames are only
			// processed inside ReadMessage, hence the read loop below: without
			// it the pings would sit unread in the socket buffer and the test
			// would pass for the wrong reason.
			conn.SetPingHandler(func(string) error { return nil })
			go func() {
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			// The client never closes, so this can only come from the server's
			// own read deadline expiring.
			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeFalse())

			Eventually(func() string {
				node, _ := nodes.GetByID(bg, nodeID)
				if node == nil {
					return ""
				}
				return node.Phase
			}, 10*time.Second, 100*time.Millisecond).Should(Equal(store.PhaseOffline))
		})

		It("keeps a peer that answers pings online well past the read timeout", func() {
			conn, _, err := dialWS(kaServer, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			// No SetPingHandler override: gorilla pongs for us, which is exactly
			// what the real agent does (pkg/client/ws.go and the agent's
			// phonehome client both leave the default handler in place).
			readErr := make(chan error, 1)
			go func() {
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						readErr <- err
						return
					}
				}
			}()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			// Guards the regression this change could introduce: a read deadline
			// that is not refreshed on pong would drop healthy agents.
			Consistently(func() bool {
				return hub.IsOnline(nodeID)
			}, 3*readTimeout, 100*time.Millisecond).Should(BeTrue())

			Expect(readErr).NotTo(Receive())
		})
	})

	Describe("Hub", func() {
		It("should send command to online node", func() {
			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			cmdPayload := commandData{
				ID:      "cmd-3",
				Command: "exec",
				Args:    map[string]string{"script": "echo hello"},
			}
			Expect(hub.SendCommand(nodeID, cmdPayload)).To(Succeed())

			msg, err := readMsg(conn)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Type).To(Equal("command"))

			var cd commandData
			Expect(json.Unmarshal(msg.Data, &cd)).To(Succeed())
			Expect(cd.ID).To(Equal("cmd-3"))
			Expect(cd.Command).To(Equal("exec"))
		})

		It("should return error when sending command to offline node", func() {
			err := hub.SendCommand("nonexistent-node", map[string]string{"cmd": "test"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not connected"))
		})

		It("should track online count correctly", func() {
			Expect(hub.OnlineCount()).To(Equal(0))

			conn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() int {
				return hub.OnlineCount()
			}, 10*time.Second, 100*time.Millisecond).Should(Equal(1))

			conn.Close()

			Eventually(func() int {
				return hub.OnlineCount()
			}, 10*time.Second, 100*time.Millisecond).Should(Equal(0))
		})
	})

	Describe("UI WebSocket", func() {
		It("should accept UI WebSocket connections", func() {
			conn, _, err := dialWS(server, "/api/v1/ws/ui")
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close()
			Expect(conn).NotTo(BeNil())
		})
	})

	Describe("UIHub", func() {
		It("should broadcast command_update to connected UI clients", func() {
			// Connect a UI WS client.
			uiConn, _, err := dialWS(server, "/api/v1/ws/ui")
			Expect(err).NotTo(HaveOccurred())
			defer uiConn.Close()

			// Give the UI connection time to register.
			time.Sleep(100 * time.Millisecond)

			// Create a command and connect agent.
			cmd := &store.NodeCommand{
				ManagedNodeID: nodeID,
				Command:       "upgrade",
			}
			Expect(commands.Create(bg, cmd)).To(Succeed())
			Expect(commands.UpdateStatus(bg, cmd.ID, store.CommandDelivered, "")).To(Succeed())

			agentConn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer agentConn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			// Agent sends command_status.
			status := commandStatusData{
				ID:     cmd.ID,
				Phase:  store.CommandCompleted,
				Result: "upgrade done",
			}
			Expect(sendMsg(agentConn, "command_status", status)).To(Succeed())

			// UI client should receive a command_update broadcast.
			msg, err := readMsg(uiConn)
			Expect(err).NotTo(HaveOccurred())
			Expect(msg.Type).To(Equal("command_update"))

			var update commandStatusData
			Expect(json.Unmarshal(msg.Data, &update)).To(Succeed())
			Expect(update.ID).To(Equal(cmd.ID))
			Expect(update.Phase).To(Equal(store.CommandCompleted))
		})

		It("should not crash when no UI clients connected", func() {
			// Create a command and connect agent only (no UI client).
			cmd := &store.NodeCommand{
				ManagedNodeID: nodeID,
				Command:       "exec",
			}
			Expect(commands.Create(bg, cmd)).To(Succeed())
			Expect(commands.UpdateStatus(bg, cmd.ID, store.CommandDelivered, "")).To(Succeed())

			agentConn, _, err := dialWS(server, "/api/v1/ws?token="+apiKey)
			Expect(err).NotTo(HaveOccurred())
			defer agentConn.Close()

			Eventually(func() bool {
				return hub.IsOnline(nodeID)
			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

			// Agent sends command_status — should not panic.
			status := commandStatusData{
				ID:     cmd.ID,
				Phase:  store.CommandRunning,
				Result: "in progress",
			}
			Expect(sendMsg(agentConn, "command_status", status)).To(Succeed())

			// Verify the command was updated in the DB (handler didn't crash).
			Eventually(func() string {
				c, _ := commands.GetByID(bg, cmd.ID)
				if c == nil {
					return ""
				}
				return c.Phase
			}, 10*time.Second, 100*time.Millisecond).Should(Equal(store.CommandRunning))
		})
	})
})
