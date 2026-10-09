package phonehome_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/kairos-io/kairos/v4/agent/internal/phonehome"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// nodeAPIKey is distinctive so a spec can assert it is absent from an error.
const nodeAPIKey = "s3cret-node-api-key"

// rejectingServer registers any node and then refuses every WebSocket upgrade
// with status and body, the way AuroraBoot's /api/v1/ws refuses a key it does
// not recognise.
func rejectingServer(status int, body map[string]string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/nodes/register", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(phonehome.Credentials{NodeID: "node", APIKey: nodeAPIKey})
	})
	mux.HandleFunc("/api/v1/ws", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	})
	return httptest.NewServer(mux)
}

var _ = Describe("PhoneHome rejected WebSocket handshake", func() {
	var (
		tmpDir string
		server *httptest.Server
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "phonehome-handshake")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
			server = nil
		}
		os.RemoveAll(tmpDir)
	})

	connectErr := func() string {
		cfg := &phonehome.Config{
			URL:               server.URL,
			RegistrationToken: "token",
			HeartbeatInterval: time.Second,
			ReconnectBackoff:  10 * time.Millisecond,
		}
		client := phonehome.NewClient(cfg,
			phonehome.WithCredentialsPath(filepath.Join(tmpDir, "creds.yaml")),
			phonehome.WithMachineIDFunc(func() string { return "machine" }),
			phonehome.WithLogger(sdkLogger.NewNullLogger()),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		Expect(client.Register(ctx)).To(Succeed())

		err := client.Connect(ctx)
		Expect(err).To(HaveOccurred())

		return err.Error()
	}

	// Register, in the same file, reports the status and the server's body when
	// it is refused. The reconnect loop logs the connect error once per attempt
	// for the life of the process, so it has to say the same things: a revoked
	// key and an unreachable server must not read alike.
	It("reports the status and the server's message when the key is refused", func() {
		server = rejectingServer(http.StatusUnauthorized, map[string]string{"error": "invalid token"})

		Expect(connectErr()).To(Equal(
			`websocket dial: websocket: bad handshake (401 Unauthorized: {"error":"invalid token"})`))
	})

	It("reports the status when the server has no body to add", func() {
		server = rejectingServer(http.StatusServiceUnavailable, nil)

		Expect(connectErr()).To(Equal(
			"websocket dial: websocket: bad handshake (503 Service Unavailable)"))
	})

	// The node's API key is in the query of the URL that was dialled. Whatever
	// the error says about the refusal, it must not carry the credential.
	It("keeps the node API key out of the error", func() {
		server = rejectingServer(http.StatusUnauthorized, map[string]string{"error": "invalid token"})

		Expect(connectErr()).ToNot(ContainSubstring(nodeAPIKey))
	})

	// A dial that never reached a server has no response to describe, and the
	// error has to stay the plain network failure rather than grow an empty
	// pair of brackets.
	It("adds nothing when the dial never got a response", func() {
		server = rejectingServer(http.StatusUnauthorized, nil)

		cfg := &phonehome.Config{
			URL:               server.URL,
			RegistrationToken: "token",
			HeartbeatInterval: time.Second,
			ReconnectBackoff:  10 * time.Millisecond,
		}
		client := phonehome.NewClient(cfg,
			phonehome.WithCredentialsPath(filepath.Join(tmpDir, "creds.yaml")),
			phonehome.WithMachineIDFunc(func() string { return "machine" }),
			phonehome.WithLogger(sdkLogger.NewNullLogger()),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		Expect(client.Register(ctx)).To(Succeed())

		// Registered, then the server goes away: the dial is refused by the
		// kernel and there is no response at all.
		server.Close()
		server = nil

		err := client.Connect(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("("))
	})
})
