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

	"github.com/kairos-io/kairos/v4/agent/internal/phonehome"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// keyServer issues apiKey on registration and accepts only that key on
// /api/v1/ws, refusing any other with 401 the way AuroraBoot does for a node
// it does not know.
type keyServer struct {
	server *httptest.Server
	apiKey string

	mu        sync.Mutex
	registers int
	accepted  int
}

func newKeyServer(apiKey string) *keyServer {
	ks := &keyServer{apiKey: apiKey}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/nodes/register", func(w http.ResponseWriter, _ *http.Request) {
		ks.mu.Lock()
		ks.registers++
		ks.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(phonehome.Credentials{NodeID: "node", APIKey: ks.apiKey})
	})
	mux.HandleFunc("/api/v1/ws", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != ks.apiKey {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ks.mu.Lock()
		ks.accepted++
		ks.mu.Unlock()
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	ks.server = httptest.NewServer(mux)
	return ks
}

func (ks *keyServer) counts() (int, int) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.registers, ks.accepted
}

var _ = Describe("PhoneHome credentials from another server", func() {
	var (
		tmpDir    string
		credsPath string
		server    *keyServer
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "phonehome-reregister")
		Expect(err).ToNot(HaveOccurred())
		credsPath = filepath.Join(tmpDir, "creds.yaml")
		server = newKeyServer("new-key")
	})

	AfterEach(func() {
		server.server.Close()
		os.RemoveAll(tmpDir)
	})

	writeCreds := func(creds phonehome.Credentials) {
		data, err := yaml.Marshal(creds)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(credsPath, data, 0600)).To(Succeed())
	}

	readCreds := func() phonehome.Credentials {
		data, err := os.ReadFile(credsPath)
		Expect(err).ToNot(HaveOccurred())
		var creds phonehome.Credentials
		Expect(yaml.Unmarshal(data, &creds)).To(Succeed())
		return creds
	}

	newClient := func() *phonehome.Client {
		cfg := &phonehome.Config{
			URL:               server.server.URL + "/",
			RegistrationToken: "token",
			HeartbeatInterval: time.Second,
			ReconnectBackoff:  10 * time.Millisecond,
		}
		return phonehome.NewClient(cfg,
			phonehome.WithCredentialsPath(credsPath),
			phonehome.WithMachineIDFunc(func() string { return "machine" }),
			phonehome.WithLogger(sdkLogger.NewNullLogger()),
		)
	}

	It("records the server url with the credentials it saves", func() {
		Expect(newClient().Register(context.Background())).To(Succeed())

		Expect(readCreds()).To(Equal(phonehome.Credentials{
			NodeID: "node", APIKey: "new-key", ServerURL: server.server.URL,
		}))
	})

	It("keeps credentials the configured server issued", func() {
		writeCreds(phonehome.Credentials{NodeID: "node", APIKey: "new-key", ServerURL: server.server.URL})

		client := newClient()
		Expect(client.Register(context.Background())).To(Succeed())

		registers, _ := server.counts()
		Expect(registers).To(Equal(0))
		Expect(client.APIKey()).To(Equal("new-key"))
	})

	It("registers again when the saved credentials came from another server", func() {
		writeCreds(phonehome.Credentials{NodeID: "old", APIKey: "old-key", ServerURL: "https://old.example.com"})

		client := newClient()
		Expect(client.Register(context.Background())).To(Succeed())

		registers, _ := server.counts()
		Expect(registers).To(Equal(1))
		Expect(client.APIKey()).To(Equal("new-key"))
		Expect(readCreds().ServerURL).To(Equal(server.server.URL))
	})

	It("registers again when the server refuses the saved key", func() {
		// Written before the server url was recorded, so only the 401 says
		// the key is stale: the node was deleted on the server.
		writeCreds(phonehome.Credentials{NodeID: "old", APIKey: "old-key"})

		client := newClient()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- client.Run(ctx) }()

		Eventually(func() int {
			_, accepted := server.counts()
			return accepted
		}, 5*time.Second, 10*time.Millisecond).Should(Equal(1))

		registers, _ := server.counts()
		Expect(registers).To(Equal(1))
		Expect(readCreds()).To(Equal(phonehome.Credentials{
			NodeID: "node", APIKey: "new-key", ServerURL: server.server.URL,
		}))

		cancel()
		Eventually(done, 5*time.Second).Should(Receive(BeNil()))
	})
})
