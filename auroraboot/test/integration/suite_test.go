package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/batches"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/client"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/server"
	"github.com/kairos-io/AuroraBoot/pkg/ws"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testAdminPassword = "test-admin-password"
)

var (
	nodeGormStore *gormstore.Store

	// testRegToken starts at a fixed seed value and is kept in sync with
	// the server's active token by the Settings rotation specs — rotating
	// the token invalidates the previous value (RegistrationTokenAuth reads
	// it through a pointer), so any spec that registers after a rotation
	// must use the refreshed value.
	testRegToken = "test-reg-token"

	testServer    *httptest.Server
	testServerURL string

	// adminClient is a pkg/client.Client authenticated with the test
	// admin password. Every admin test hits the server through this
	// rather than open-coding http.NewRequest — which keeps pkg/client
	// exercised end-to-end on every CI run.
	adminClient *client.Client

	// testArtifactsDir is the server's artifacts directory, where uploaded
	// overlays live under overlays/<id>.
	testArtifactsDir string

	// artifactBuilder is the builder the server hands build requests to, so
	// specs can check the options a request turned into.
	artifactBuilder *mockArtifactBuilder
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

// mockArtifactBuilder implements builder.ArtifactBuilder for testing.
type mockArtifactBuilder struct {
	builds map[string]*builder.BuildStatus

	mu   sync.Mutex
	opts map[string]builder.BuildOptions
}

func newMockArtifactBuilder() *mockArtifactBuilder {
	return &mockArtifactBuilder{
		builds: make(map[string]*builder.BuildStatus),
		opts:   make(map[string]builder.BuildOptions),
	}
}

func (m *mockArtifactBuilder) Build(_ context.Context, opts builder.BuildOptions) (*builder.BuildStatus, error) {
	status := &builder.BuildStatus{
		ID:    opts.ID,
		Phase: builder.BuildPending,
	}
	m.builds[opts.ID] = status
	m.mu.Lock()
	m.opts[opts.ID] = opts
	m.mu.Unlock()
	return status, nil
}

// optsFor returns the options the build with the given ID was started with.
func (m *mockArtifactBuilder) optsFor(id string) (builder.BuildOptions, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	opts, ok := m.opts[id]
	return opts, ok
}

func (m *mockArtifactBuilder) Status(_ context.Context, id string) (*builder.BuildStatus, error) {
	s, ok := m.builds[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return s, nil
}

func (m *mockArtifactBuilder) List(_ context.Context) ([]*builder.BuildStatus, error) {
	var result []*builder.BuildStatus
	for _, s := range m.builds {
		result = append(result, s)
	}
	return result, nil
}

func (m *mockArtifactBuilder) Cancel(_ context.Context, id string) error {
	delete(m.builds, id)
	return nil
}

var _ = BeforeSuite(func() {
	// File-backed SQLite store, NOT :memory: — WAL mode and busy_timeout do not
	// apply to in-memory databases, so concurrent writers (e.g. the WebSocket
	// heartbeat path) can hit "database is locked" even though the store sets
	// both. Matches the pattern in internal/store/gorm/concurrency_test.go.
	dbPath := filepath.Join(GinkgoT().TempDir(), "integration.db")
	store, err := gormstore.New(dbPath)
	Expect(err).NotTo(HaveOccurred())
	nodeGormStore = store

	nodeStore := &gormstore.NodeStoreAdapter{S: store}
	commandStore := &gormstore.CommandStoreAdapter{S: store}
	groupStore := &gormstore.GroupStoreAdapter{S: store}
	artifactStore := &gormstore.ArtifactStoreAdapter{S: store}

	hub := ws.NewHub()
	batchStore := &gormstore.BatchStoreAdapter{S: store}
	sched := batches.New(batchStore, handlers.NewCommandHandler(commandStore, nodeStore, hub, nil, nil).PushCommand)
	testArtifactsDir = GinkgoT().TempDir()
	artifactBuilder = newMockArtifactBuilder()

	cfg := server.Config{
		NodeStore:     nodeStore,
		CommandStore:  commandStore,
		BatchStore:    batchStore,
		Batches:       sched,
		GroupStore:    groupStore,
		ArtifactStore: artifactStore,
		Builder:       artifactBuilder,
		ArtifactsDir:  testArtifactsDir,
		AdminPassword: testAdminPassword,
		RegToken:      testRegToken,
		AuroraBootURL: "http://localhost",
		Hub:           hub,
		// The whole suite drives one shared server from 127.0.0.1 and registers
		// dozens of nodes across its specs. With per-IP registration rate limiting
		// on (the default), those registrations share one bucket and exhaust its
		// burst, so later specs' registrations flakily 429. This suite exercises
		// fleet behaviour, not the limiter (which has its own coverage in
		// pkg/auth/ratelimit_test.go and pkg/server), so turn it off here.
		DisableRateLimit: true,
	}

	e := server.New(cfg)
	testServer = httptest.NewServer(e)
	testServerURL = testServer.URL

	adminClient = client.New(testServerURL, client.WithAdminPassword(testAdminPassword))
})

var _ = AfterSuite(func() {
	if testServer != nil {
		testServer.Close()
	}
	if nodeGormStore != nil {
		Expect(nodeGormStore.Close()).To(Succeed())
	}
})

// --- Helper functions ---
//
// These are thin wrappers that either:
// (a) call pkg/client when the new typed interface exists, or
// (b) fall back to raw http when the test needs a specific status
//     code or non-standard header pattern (e.g. Bearer auth via the
//     registration token) that the typed client intentionally hides.

// registerNode registers a node via pkg/client and returns the
// resulting nodeID and apiKey. Signature kept 4-ary so existing tests
// that inline serverURL and token don't need touching; in practice
// every caller passes testServerURL + testRegToken.
func registerNode(serverURL, token, machineID, hostname string) (nodeID, apiKey string) {
	resp, err := client.New(serverURL).Nodes.Register(context.Background(),
		client.NodeRegisterRequest{
			RegistrationToken: token,
			MachineID:         machineID,
			Hostname:          hostname,
		})
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "register node should succeed")
	return resp.ID, resp.APIKey
}

// adminGet performs a GET request with admin auth. Kept for tests that
// want direct *http.Response access (e.g. to assert status codes other
// than 2xx, which the typed client turns into errors).
func adminGet(serverURL, path, password string) *http.Response {
	req, err := http.NewRequest("GET", serverURL+path, nil)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	req.Header.Set("Authorization", "Bearer "+password)
	resp, err := http.DefaultClient.Do(req)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return resp
}

// adminPost performs a POST request with admin auth.
func adminPost(serverURL, path, password string, body interface{}) *http.Response {
	return adminRequest("POST", serverURL, path, password, body)
}

// adminPut performs a PUT request with admin auth.
func adminPut(serverURL, path, password string, body interface{}) *http.Response {
	return adminRequest("PUT", serverURL, path, password, body)
}

// adminDelete performs a DELETE request with admin auth.
func adminDelete(serverURL, path, password string) *http.Response {
	req, err := http.NewRequest("DELETE", serverURL+path, nil)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	req.Header.Set("Authorization", "Bearer "+password)
	resp, err := http.DefaultClient.Do(req)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return resp
}

// adminRequest performs a request with admin auth and a JSON body.
func adminRequest(method, serverURL, path, password string, body interface{}) *http.Response {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		ExpectWithOffset(2, err).NotTo(HaveOccurred())
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, serverURL+path, bodyReader)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+password)
	resp, err := http.DefaultClient.Do(req)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	return resp
}

// doPost performs a POST request without auth (used for registration
// when a test needs the raw http.Response rather than pkg/client's
// typed return).
func doPost(serverURL, path, authToken string, body interface{}) *http.Response {
	b, err := json.Marshal(body)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	req, err := http.NewRequest("POST", serverURL+path, bytes.NewReader(b))
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := http.DefaultClient.Do(req)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	return resp
}

// connectWS opens a WebSocket connection to the agent channel using
// pkg/client's typed DialAgentWS under the hood. Signature kept 2-ary
// for the same reason as registerNode above.
func connectWS(serverURL, apiKey string) *websocket.Conn {
	cli := client.New(serverURL, client.WithNodeAPIKey(apiKey))
	conn, err := cli.DialAgentWS(context.Background())
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "WS dial should succeed")
	return conn
}

// decodeJSON decodes a JSON response body into the given target.
func decodeJSON(resp *http.Response, target interface{}) {
	defer resp.Body.Close()
	err := json.NewDecoder(resp.Body).Decode(target)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
}

// readBody reads the full body of a response as a string.
func readBody(resp *http.Response) string {
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return string(b)
}
