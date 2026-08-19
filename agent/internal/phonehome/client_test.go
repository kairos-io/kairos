package phonehome_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type mockServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	regCalls int
	lastReg  phonehome.RegisterRequest
	wsToken  string
	// WS tracking
	wsConnected    chan struct{}
	heartbeats     []phonehome.HeartbeatData
	cmdStatuses    []phonehome.CommandStatusData
	commandsToSend []phonehome.CommandData
}

func (ms *mockServer) setWSToken(t string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.wsToken = t
}

func (ms *mockServer) getWSToken() string {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.wsToken
}

func newMockServer(validToken string) *mockServer {
	ms := &mockServer{
		wsConnected: make(chan struct{}, 1),
		wsToken:     "test-api-key",
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/nodes/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req phonehome.RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		ms.mu.Lock()
		ms.regCalls++
		ms.lastReg = req
		ms.mu.Unlock()

		if req.RegistrationToken != validToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(phonehome.Credentials{
			NodeID: "test-node-id",
			APIKey: "test-api-key",
		})
	})

	mux.HandleFunc("/api/v1/ws", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token != ms.getWSToken() {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Signal connected
		select {
		case ms.wsConnected <- struct{}{}:
		default:
		}

		// Send any queued commands
		ms.mu.Lock()
		cmds := ms.commandsToSend
		ms.commandsToSend = nil
		ms.mu.Unlock()

		for _, cmd := range cmds {
			data, _ := json.Marshal(cmd)
			msg := phonehome.WSMessage{Type: "command", Data: data}
			msgBytes, _ := json.Marshal(msg)
			conn.WriteMessage(websocket.TextMessage, msgBytes)
		}

		// Read messages until close
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg phonehome.WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			ms.mu.Lock()
			switch msg.Type {
			case "heartbeat":
				var hb phonehome.HeartbeatData
				json.Unmarshal(msg.Data, &hb)
				ms.heartbeats = append(ms.heartbeats, hb)
			case "command_status":
				var cs phonehome.CommandStatusData
				json.Unmarshal(msg.Data, &cs)
				ms.cmdStatuses = append(ms.cmdStatuses, cs)
			}
			ms.mu.Unlock()
		}
	})

	ms.server = httptest.NewServer(mux)
	return ms
}

func (ms *mockServer) close() {
	ms.server.Close()
}

var _ = Describe("PhoneHome Client", func() {
	var (
		ms     *mockServer
		tmpDir string
		logger sdkLogger.KairosLogger
	)

	BeforeEach(func() {
		ms = newMockServer("test-token")
		var err error
		tmpDir, err = os.MkdirTemp("", "phonehome-test")
		Expect(err).ToNot(HaveOccurred())
		// Null logger keeps the client quiet under ginkgo — the test body
		// asserts on channels and HTTP side-effects, not on log output.
		logger = sdkLogger.NewNullLogger()
	})

	AfterEach(func() {
		ms.close()
		os.RemoveAll(tmpDir)
	})

	newTestClient := func(token string) *phonehome.Client {
		cfg := &phonehome.Config{
			URL:               ms.server.URL,
			RegistrationToken: token,
			Group:             "test-group",
			Labels:            map[string]string{"env": "test"},
			HeartbeatInterval: 100 * time.Millisecond,
			ReconnectBackoff:  50 * time.Millisecond,
		}
		return phonehome.NewClient(cfg,
			phonehome.WithCredentialsPath(filepath.Join(tmpDir, "creds.yaml")),
			phonehome.WithMachineIDFunc(func() string { return "test-machine-id" }),
			phonehome.WithLogger(logger),
		)
	}

	Describe("Register", func() {
		It("should register successfully with valid token", func() {
			client := newTestClient("test-token")
			err := client.Register(context.Background())
			Expect(err).ToNot(HaveOccurred())

			ms.mu.Lock()
			defer ms.mu.Unlock()
			Expect(ms.regCalls).To(Equal(1))
			Expect(ms.lastReg.MachineID).To(Equal("test-machine-id"))
			Expect(ms.lastReg.RegistrationToken).To(Equal("test-token"))
			Expect(ms.lastReg.Group).To(Equal("test-group"))
			Expect(ms.lastReg.Labels).To(HaveKeyWithValue("env", "test"))
		})

		It("should store credentials to file", func() {
			client := newTestClient("test-token")
			err := client.Register(context.Background())
			Expect(err).ToNot(HaveOccurred())

			credFile := filepath.Join(tmpDir, "creds.yaml")
			Expect(credFile).To(BeAnExistingFile())
		})

		It("should skip registration when credentials file exists", func() {
			// Write credentials file first
			credFile := filepath.Join(tmpDir, "creds.yaml")
			err := os.WriteFile(credFile, []byte("node_id: existing-id\napi_key: existing-key\n"), 0600)
			Expect(err).ToNot(HaveOccurred())

			client := newTestClient("test-token")
			err = client.Register(context.Background())
			Expect(err).ToNot(HaveOccurred())

			ms.mu.Lock()
			defer ms.mu.Unlock()
			Expect(ms.regCalls).To(Equal(0)) // no registration call made
		})

		It("should return error on invalid token", func() {
			client := newTestClient("wrong-token")
			err := client.Register(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid registration token"))
		})
	})

	Describe("Connect", func() {
		It("should establish WebSocket connection", func() {
			client := newTestClient("test-token")
			err := client.Register(context.Background())
			Expect(err).ToNot(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())

			go func() {
				defer GinkgoRecover()
				// Wait for connection then cancel
				<-ms.wsConnected
				time.Sleep(50 * time.Millisecond)
				cancel()
			}()

			client.Connect(ctx)
			// Connected successfully if we get here
		})

		It("should send heartbeat messages", func() {
			client := newTestClient("test-token")
			err := client.Register(context.Background())
			Expect(err).ToNot(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())

			go func() {
				defer GinkgoRecover()
				<-ms.wsConnected
				// Wait for at least 2 heartbeats
				time.Sleep(300 * time.Millisecond)
				cancel()
			}()

			client.Connect(ctx)

			ms.mu.Lock()
			defer ms.mu.Unlock()
			Expect(len(ms.heartbeats)).To(BeNumerically(">=", 2))
		})

		It("should fail when the stored API key is rejected by the server", func() {
			// Register with a valid registration token so credentials are stored,
			// then make the server reject the resulting API key on the WS handshake.
			// This covers the case where the registration token is correct but the
			// node's API key is invalid/revoked at connection time.
			client := newTestClient("test-token")
			err := client.Register(context.Background())
			Expect(err).ToNot(HaveOccurred())

			ms.setWSToken("some-other-key")

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			err = client.Connect(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("websocket"))
		})

		It("should receive and handle command messages", func() {
			ms.mu.Lock()
			ms.commandsToSend = []phonehome.CommandData{
				{ID: "cmd-1", Command: "exec", Args: map[string]string{"command": "echo hi"}},
			}
			ms.mu.Unlock()

			cfg := &phonehome.Config{
				URL:               ms.server.URL,
				RegistrationToken: "test-token",
				HeartbeatInterval: 100 * time.Millisecond,
				ReconnectBackoff:  50 * time.Millisecond,
			}
			client := phonehome.NewClient(cfg,
				phonehome.WithCredentialsPath(filepath.Join(tmpDir, "creds.yaml")),
				phonehome.WithMachineIDFunc(func() string { return "test-machine-id" }),
				phonehome.WithLogger(logger),
				phonehome.WithCommandHandler(func(cmd phonehome.CommandData) (string, error) {
					return "ok", nil
				}),
			)
			err := client.Register(context.Background())
			Expect(err).ToNot(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())

			go func() {
				defer GinkgoRecover()
				<-ms.wsConnected
				time.Sleep(300 * time.Millisecond)
				cancel()
			}()

			client.Connect(ctx)

			ms.mu.Lock()
			defer ms.mu.Unlock()
			Expect(len(ms.cmdStatuses)).To(BeNumerically(">=", 1))
			// Should have Running then Completed
			var hasCompleted bool
			for _, s := range ms.cmdStatuses {
				if s.ID == "cmd-1" && s.Phase == "Completed" {
					hasCompleted = true
				}
			}
			Expect(hasCompleted).To(BeTrue())
		})
	})

	Describe("Run", func() {
		It("should register then connect", func() {
			client := newTestClient("test-token")

			ctx, cancel := context.WithCancel(context.Background())

			go func() {
				defer GinkgoRecover()
				<-ms.wsConnected
				time.Sleep(50 * time.Millisecond)
				cancel()
			}()

			err := client.Run(ctx)
			Expect(err).ToNot(HaveOccurred())

			ms.mu.Lock()
			defer ms.mu.Unlock()
			Expect(ms.regCalls).To(Equal(1))
		})

		It("should stop on context cancel", func() {
			client := newTestClient("test-token")

			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()

			err := client.Run(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		// Stop() is the decommission escape hatch: once the unregister
		// command handler has torn down the phone-home install on disk,
		// the Run loop has nothing to reconnect to and should exit cleanly
		// even though the caller's context is still alive.
		It("should exit Run cleanly when Stop() is called", func() {
			client := newTestClient("test-token")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Kick Stop() shortly after the WS connection is established —
			// Run must return without waiting for ctx to fire.
			go func() {
				defer GinkgoRecover()
				<-ms.wsConnected
				time.Sleep(50 * time.Millisecond)
				client.Stop()
			}()

			done := make(chan error, 1)
			go func() { done <- client.Run(ctx) }()

			Eventually(done, "2s", "50ms").Should(Receive(BeNil()))
			Expect(ctx.Err()).To(BeNil(), "caller's context must not have been cancelled")
		})
	})
})
