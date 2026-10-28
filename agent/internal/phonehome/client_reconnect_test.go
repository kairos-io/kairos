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
	"github.com/kairos-io/kairos/v4/agent/internal/phonehome"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PhoneHome command status across a reconnect", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "phonehome-reconnect")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	It("should send the final status of a command on the session it finishes in", func() {
		secondSession := make(chan struct{})
		var (
			mu       sync.Mutex
			sessions int
			// statuses holds every command status the server read, by session.
			statuses = map[int][]phonehome.CommandStatusData{}
		)

		readStatus := func(conn *websocket.Conn) (phonehome.CommandStatusData, bool, error) {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return phonehome.CommandStatusData{}, false, err
			}
			var msg phonehome.WSMessage
			if json.Unmarshal(message, &msg) != nil || msg.Type != "command_status" {
				return phonehome.CommandStatusData{}, false, nil
			}
			var cs phonehome.CommandStatusData
			_ = json.Unmarshal(msg.Data, &cs)
			return cs, true, nil
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/nodes/register", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(phonehome.Credentials{NodeID: "node", APIKey: "key"})
		})
		mux.HandleFunc("/api/v1/ws", func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			mu.Lock()
			sessions++
			session := sessions
			mu.Unlock()

			if session == 1 {
				data, _ := json.Marshal(phonehome.CommandData{ID: "cmd-1", Command: "upgrade"})
				msgBytes, _ := json.Marshal(phonehome.WSMessage{Type: "command", Data: data})
				if conn.WriteMessage(websocket.TextMessage, msgBytes) != nil {
					return
				}
			}
			if session == 2 {
				close(secondSession)
			}

			for {
				cs, ok, err := readStatus(conn)
				if err != nil {
					return
				}
				if !ok {
					continue
				}
				mu.Lock()
				statuses[session] = append(statuses[session], cs)
				mu.Unlock()
				// The first session drops as soon as the command is running,
				// the way a server restart or a network blip would.
				if session == 1 && cs.Phase == "Running" {
					return
				}
			}
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		cfg := &phonehome.Config{
			URL:               server.URL,
			RegistrationToken: "token",
			HeartbeatInterval: time.Hour,
			ReconnectBackoff:  20 * time.Millisecond,
		}
		client := phonehome.NewClient(cfg,
			phonehome.WithCredentialsPath(filepath.Join(tmpDir, "creds.yaml")),
			phonehome.WithMachineIDFunc(func() string { return "machine" }),
			phonehome.WithLogger(sdkLogger.NewNullLogger()),
			phonehome.WithCommandHandler(func(cmd phonehome.CommandData) (string, error) {
				<-secondSession
				return "upgraded", nil
			}),
		)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(done)
			_ = client.Run(ctx)
		}()
		defer func() {
			cancel()
			Eventually(done, 5*time.Second).Should(BeClosed())
		}()

		Eventually(func() []phonehome.CommandStatusData {
			mu.Lock()
			defer mu.Unlock()
			return append([]phonehome.CommandStatusData(nil), statuses[2]...)
		}, 5*time.Second, 20*time.Millisecond).Should(ContainElement(phonehome.CommandStatusData{
			ID: "cmd-1", Phase: "Completed", Result: "upgraded",
		}))

		mu.Lock()
		defer mu.Unlock()
		Expect(statuses[1]).To(ConsistOf(phonehome.CommandStatusData{ID: "cmd-1", Phase: "Running"}))
	})
})
