package server_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/server"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// Minimal fake implementations for server integration tests.

type fakeNodeStore struct {
	mu    sync.Mutex
	nodes []*store.ManagedNode
}

func (f *fakeNodeStore) Register(_ context.Context, n *store.ManagedNode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodes = append(f.nodes, n)
	return nil
}
func (f *fakeNodeStore) GetByID(_ context.Context, id string) (*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeNodeStore) GetByMachineID(_ context.Context, mid string) (*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.MachineID == mid {
			return n, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeNodeStore) GetByAPIKey(_ context.Context, key string) (*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.APIKey == key {
			return n, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeNodeStore) List(_ context.Context) ([]*store.ManagedNode, error) {
	return f.nodes, nil
}
func (f *fakeNodeStore) ListByGroup(_ context.Context, _ string) ([]*store.ManagedNode, error) {
	return nil, nil
}
func (f *fakeNodeStore) ListByLabels(_ context.Context, _ map[string]string) ([]*store.ManagedNode, error) {
	return nil, nil
}
func (f *fakeNodeStore) ListBySelector(_ context.Context, _ store.CommandSelector) ([]*store.ManagedNode, error) {
	return nil, nil
}
func (f *fakeNodeStore) UpdateHeartbeat(_ context.Context, _ string, _ string, _ map[string]string, _ []store.NodeAddress, _ string, _ string, _ string) error {
	return nil
}
func (f *fakeNodeStore) UpdatePhase(_ context.Context, _ string, _ string) error { return nil }
func (f *fakeNodeStore) SetGroup(_ context.Context, _ string, _ *string) error   { return nil }
func (f *fakeNodeStore) SetLabels(_ context.Context, _ string, _ map[string]string) error {
	return nil
}
func (f *fakeNodeStore) ClaimNode(_ context.Context, _, _ string) (*store.ManagedNode, error) {
	return nil, store.ErrNoClaimCapacity
}
func (f *fakeNodeStore) ReleaseNode(_ context.Context, _, _ string) (bool, error) { return false, nil }
func (f *fakeNodeStore) AdvanceReset(_ context.Context, _ string, _ []string, _ string, _ bool) (bool, error) {
	return false, nil
}
func (f *fakeNodeStore) StampResetProgress(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (f *fakeNodeStore) FailResetBefore(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
}
func (f *fakeNodeStore) SettleAfterReboot(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func (f *fakeNodeStore) AdvanceUpgrade(_ context.Context, _ string, _ []string, _ string, _ bool) (bool, error) {
	return false, nil
}
func (f *fakeNodeStore) Delete(_ context.Context, _ string) error { return nil }

type fakeCommandStore struct {
	mu   sync.Mutex
	cmds []*store.NodeCommand
}

func (f *fakeCommandStore) Create(_ context.Context, _ *store.NodeCommand) error { return nil }
func (f *fakeCommandStore) CreateBatch(_ context.Context, _ []*store.NodeCommand) error {
	return nil
}
func (f *fakeCommandStore) GetByID(_ context.Context, _ string) (*store.NodeCommand, error) {
	return nil, fmt.Errorf("not found")
}
func (f *fakeCommandStore) GetPending(_ context.Context, nodeID string) ([]*store.NodeCommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.NodeCommand
	for _, c := range f.cmds {
		if c.ManagedNodeID == nodeID && c.Phase == store.CommandPending {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *fakeCommandStore) MarkDelivered(_ context.Context, _ []string) error { return nil }
func (f *fakeCommandStore) ClaimForDelivery(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if c.ID == id && c.Phase == store.CommandPending {
			c.Phase = store.CommandDelivered
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeCommandStore) UpdateStatus(_ context.Context, _ string, _ string, _ string) error {
	return nil
}
func (f *fakeCommandStore) UpdateStatusForNode(_ context.Context, id string, nodeID string, phase string, result string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if c.ID == id && c.ManagedNodeID == nodeID {
			c.Phase = phase
			c.Result = result
			return nil
		}
	}
	return store.ErrCommandNotFound
}
func (f *fakeCommandStore) ListByNode(_ context.Context, nodeID string) ([]*store.NodeCommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.NodeCommand
	for _, c := range f.cmds {
		if c.ManagedNodeID == nodeID {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *fakeCommandStore) ExpireBefore(_ context.Context, nodeID string, deadline time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if c.ManagedNodeID != nodeID || c.ExpiresAt == nil || c.ExpiresAt.After(deadline) {
			continue
		}
		if c.Phase == store.CommandPending || c.Phase == store.CommandDelivered || c.Phase == store.CommandRunning {
			c.Phase = store.CommandExpired
		}
	}
	return nil
}
func (f *fakeCommandStore) Delete(_ context.Context, _ string) error         { return nil }
func (f *fakeCommandStore) DeleteTerminal(_ context.Context, _ string) error { return nil }

type fakeGroupStore struct{}

func (f *fakeGroupStore) Create(_ context.Context, _ *store.NodeGroup) error { return nil }
func (f *fakeGroupStore) GetByID(_ context.Context, _ string) (*store.NodeGroup, error) {
	return nil, fmt.Errorf("not found")
}
func (f *fakeGroupStore) GetByName(_ context.Context, _ string) (*store.NodeGroup, error) {
	return nil, fmt.Errorf("not found")
}
func (f *fakeGroupStore) List(_ context.Context) ([]*store.NodeGroup, error) { return nil, nil }
func (f *fakeGroupStore) Update(_ context.Context, _ *store.NodeGroup) error { return nil }
func (f *fakeGroupStore) Delete(_ context.Context, _ string) error           { return nil }
func (f *fakeGroupStore) NodeCounts(_ context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}

type fakeBuilder struct{}

func (f *fakeBuilder) Build(_ context.Context, opts builder.BuildOptions) (*builder.BuildStatus, error) {
	return &builder.BuildStatus{ID: opts.ID, Phase: builder.BuildPending}, nil
}
func (f *fakeBuilder) Status(_ context.Context, _ string) (*builder.BuildStatus, error) {
	return nil, fmt.Errorf("not found")
}
func (f *fakeBuilder) List(_ context.Context) ([]*builder.BuildStatus, error) { return nil, nil }
func (f *fakeBuilder) Cancel(_ context.Context, _ string) error               { return nil }

// fakeArtifactStore is a minimal store.ArtifactStore whose GetByID reports
// not-found, so ExportImage returns 404 cleanly (rather than nil-dereferencing an
// absent store) once a request is authorized.
type fakeArtifactStore struct{}

func (f *fakeArtifactStore) Create(context.Context, *store.ArtifactRecord) error { return nil }
func (f *fakeArtifactStore) GetByID(context.Context, string) (*store.ArtifactRecord, error) {
	return nil, fmt.Errorf("not found")
}
func (f *fakeArtifactStore) List(context.Context) ([]*store.ArtifactRecord, error) { return nil, nil }
func (f *fakeArtifactStore) Update(context.Context, *store.ArtifactRecord) error   { return nil }
func (f *fakeArtifactStore) UpdatePhaseMessage(context.Context, string, string, string) error {
	return nil
}
func (f *fakeArtifactStore) UpdateFiles(context.Context, string, []string) error { return nil }
func (f *fakeArtifactStore) UpdateNameSaved(context.Context, string, *string, *bool) error {
	return nil
}
func (f *fakeArtifactStore) ClearUploadToken(context.Context, string) error  { return nil }
func (f *fakeArtifactStore) Delete(context.Context, string) error            { return nil }
func (f *fakeArtifactStore) DeleteByPhase(context.Context, string) error     { return nil }
func (f *fakeArtifactStore) GetLogs(context.Context, string) (string, error) { return "", nil }
func (f *fakeArtifactStore) AppendLog(context.Context, string, string) error { return nil }

var _ = Describe("Server", func() {
	var (
		e  *httptest.Server
		ns *fakeNodeStore
		cs *fakeCommandStore
	)

	BeforeEach(func() {
		ns = &fakeNodeStore{}
		cs = &fakeCommandStore{}
		echoApp := server.New(server.Config{
			NodeStore:     ns,
			CommandStore:  cs,
			GroupStore:    &fakeGroupStore{},
			Builder:       &fakeBuilder{},
			AdminPassword: "admin-pass",
			RegToken:      "reg-token",
			AuroraBootURL: "http://localhost:8080",
		})
		e = httptest.NewServer(echoApp)
	})

	AfterEach(func() {
		e.Close()
	})

	Describe("Public endpoints", func() {
		It("should serve the install script without auth", func() {
			resp, err := http.Get(e.URL + "/api/v1/install-agent")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("Admin auth", func() {
		It("should reject unauthenticated admin requests", func() {
			resp, err := http.Get(e.URL + "/api/v1/nodes")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("should reject unauthenticated metrics requests", func() {
			for _, path := range []string{"/api/v1/nodes/node-1/metrics", "/api/v1/metrics/latest"} {
				resp, err := http.Get(e.URL + path)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized), path)
			}
		})

		It("should allow authenticated metrics requests", func() {
			for _, path := range []string{"/api/v1/nodes/node-1/metrics", "/api/v1/metrics/latest"} {
				req, _ := http.NewRequest(http.MethodGet, e.URL+path, nil)
				req.Header.Set("Authorization", "Bearer admin-pass")
				resp, err := http.DefaultClient.Do(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK), path)
			}
		})

		It("should allow authenticated admin requests", func() {
			req, _ := http.NewRequest(http.MethodGet, e.URL+"/api/v1/nodes", nil)
			req.Header.Set("Authorization", "Bearer admin-pass")
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("Registration auth", func() {
		It("should reject registration without token", func() {
			body := `{"machineID":"m1","hostname":"h1"}`
			resp, err := http.Post(e.URL+"/api/v1/nodes/register", "application/json", strings.NewReader(body))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("should allow registration with valid token", func() {
			body := `{"registrationToken":"reg-token","machineID":"m1","hostname":"h1"}`
			resp, err := http.Post(e.URL+"/api/v1/nodes/register", "application/json", strings.NewReader(body))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		})
	})

	Describe("Node API key auth", func() {
		It("should reject agent requests without API key", func() {
			body := `{"agentVersion":"1.0"}`
			req, _ := http.NewRequest(http.MethodPost, e.URL+"/api/v1/nodes/node-1/heartbeat", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("should allow agent requests with valid API key", func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "node-1", APIKey: "agent-key"},
			}
			body := `{"agentVersion":"1.0"}`
			req, _ := http.NewRequest(http.MethodPost, e.URL+"/api/v1/nodes/node-1/heartbeat", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer agent-key")
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	// Node-impersonation / BOLA: an agent authenticates with its own API key
	// but the path carries a :nodeID. These specs prove a node can only act on
	// its own resources — never another node's — even with a valid key.
	Describe("Node impersonation (BOLA)", func() {
		BeforeEach(func() {
			ns.nodes = []*store.ManagedNode{
				{ID: "node-A", APIKey: "key-A"},
				{ID: "node-B", APIKey: "key-B"},
			}
		})

		doReq := func(method, path, key, body string) *http.Response {
			var r *http.Request
			if body != "" {
				r, _ = http.NewRequest(method, e.URL+path, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
			} else {
				r, _ = http.NewRequest(method, e.URL+path, nil)
			}
			if key != "" {
				r.Header.Set("Authorization", "Bearer "+key)
			}
			resp, err := http.DefaultClient.Do(r)
			Expect(err).NotTo(HaveOccurred())
			return resp
		}

		It("lets a node heartbeat for itself (200)", func() {
			resp := doReq(http.MethodPost, "/api/v1/nodes/node-A/heartbeat", "key-A", `{"agentVersion":"1.0"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("blocks a node from heartbeating as another node (403)", func() {
			resp := doReq(http.MethodPost, "/api/v1/nodes/node-B/heartbeat", "key-A", `{"agentVersion":"1.0"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("lets a node read its own commands (200)", func() {
			resp := doReq(http.MethodGet, "/api/v1/nodes/node-A/commands", "key-A", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("blocks a node from reading/consuming another node's commands (403)", func() {
			// Seed a pending command for node-B; node-A must not be able to
			// read it (and thereby mark it delivered).
			cs.cmds = []*store.NodeCommand{
				{ID: "cmd-B", ManagedNodeID: "node-B", Command: "upgrade", Phase: store.CommandPending},
			}
			resp := doReq(http.MethodGet, "/api/v1/nodes/node-B/commands", "key-A", "")
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			// The command must remain pending — not consumed by the attacker.
			Expect(cs.cmds[0].Phase).To(Equal(store.CommandPending))
		})

		It("lets a node update the status of its own command (200)", func() {
			cs.cmds = []*store.NodeCommand{
				{ID: "cmd-A", ManagedNodeID: "node-A", Phase: store.CommandDelivered},
			}
			resp := doReq(http.MethodPut, "/api/v1/nodes/node-A/commands/cmd-A/status", "key-A", `{"phase":"Completed","result":"ok"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(cs.cmds[0].Phase).To(Equal(store.CommandCompleted))
		})

		It("blocks a node from updating another node's command via a forged path (403)", func() {
			// Path nodeID mismatches the key's identity — RequireNodeMatch
			// rejects before the handler runs.
			cs.cmds = []*store.NodeCommand{
				{ID: "cmd-B", ManagedNodeID: "node-B", Phase: store.CommandDelivered},
			}
			resp := doReq(http.MethodPut, "/api/v1/nodes/node-B/commands/cmd-B/status", "key-A", `{"phase":"Completed","result":"pwned"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(cs.cmds[0].Phase).To(Equal(store.CommandDelivered))
		})

		It("blocks a node from updating a foreign command id under its own path (403)", func() {
			// Path nodeID matches the key (passes RequireNodeMatch), but the
			// commandID belongs to node-B. The node-scoped store update matches
			// zero rows and must surface as 403, not a silent 200.
			cs.cmds = []*store.NodeCommand{
				{ID: "cmd-B", ManagedNodeID: "node-B", Phase: store.CommandDelivered},
			}
			resp := doReq(http.MethodPut, "/api/v1/nodes/node-A/commands/cmd-B/status", "key-A", `{"phase":"Completed","result":"pwned"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			Expect(cs.cmds[0].Phase).To(Equal(store.CommandDelivered))
		})
	})
})

var _ = Describe("Artifact download scoping", func() {
	var (
		e  *httptest.Server
		ns *fakeNodeStore
		cs *fakeCommandStore
	)

	BeforeEach(func() {
		ns = &fakeNodeStore{nodes: []*store.ManagedNode{{ID: "node-1", APIKey: "agent-key"}}}
		cs = &fakeCommandStore{}
		echoApp := server.New(server.Config{
			NodeStore:     ns,
			CommandStore:  cs,
			GroupStore:    &fakeGroupStore{},
			ArtifactStore: &fakeArtifactStore{},
			Builder:       &fakeBuilder{},
			AdminPassword: "admin-pass",
			RegToken:      "reg-token",
			AuroraBootURL: "http://localhost:8080",
		})
		e = httptest.NewServer(echoApp)
	})

	AfterEach(func() { e.Close() })

	get := func(path, bearer string) int {
		req, _ := http.NewRequest(http.MethodGet, e.URL+path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()
		return resp.StatusCode
	}

	Describe("raw file download (/download/*)", func() {
		It("rejects a node API key — these files are admin-only", func() {
			Expect(get("/api/v1/artifacts/art-1/download/kairos.iso", "agent-key")).To(Equal(http.StatusUnauthorized))
		})
		It("admits the admin (auth passes; a missing file is a 404, not a 401)", func() {
			Expect(get("/api/v1/artifacts/art-1/download/kairos.iso", "admin-pass")).NotTo(Equal(http.StatusUnauthorized))
		})
	})

	Describe("container image (/image)", func() {
		It("403s a node not assigned this artifact by any upgrade command", func() {
			Expect(get("/api/v1/artifacts/art-1/image", "agent-key")).To(Equal(http.StatusForbidden))
		})
		It("admits a node assigned this artifact by an upgrade command (auth passes)", func() {
			cs.cmds = []*store.NodeCommand{
				{ID: "c1", ManagedNodeID: "node-1", Command: store.CmdUpgrade, Args: map[string]string{"source": "artifact:art-1"}},
			}
			code := get("/api/v1/artifacts/art-1/image", "agent-key")
			Expect(code).NotTo(Equal(http.StatusUnauthorized))
			Expect(code).NotTo(Equal(http.StatusForbidden))
		})
		It("admits the admin (auth passes)", func() {
			code := get("/api/v1/artifacts/art-1/image", "admin-pass")
			Expect(code).NotTo(Equal(http.StatusUnauthorized))
			Expect(code).NotTo(Equal(http.StatusForbidden))
		})
	})
})

var _ = Describe("Rate limiting wiring", func() {
	var (
		e  *httptest.Server
		ns *fakeNodeStore
	)

	BeforeEach(func() {
		ns = &fakeNodeStore{nodes: []*store.ManagedNode{{ID: "node-1", APIKey: "agent-key"}}}
		// Burst 2 so throttling is observable in a handful of requests (defaults
		// would need 20+), and a near-zero rate so the bucket refills negligibly
		// during the test — otherwise a slow CI run could top the bucket back up
		// mid-loop and no request would ever be throttled.
		echoApp := server.New(server.Config{
			NodeStore:              ns,
			CommandStore:           &fakeCommandStore{},
			GroupStore:             &fakeGroupStore{},
			Builder:                &fakeBuilder{},
			AdminPassword:          "admin-pass",
			RegToken:               "reg-token",
			AuroraBootURL:          "http://localhost:8080",
			NodeRateLimitRPS:       0.001,
			NodeRateLimitBurst:     2,
			RegisterRateLimitRPS:   0.001,
			RegisterRateLimitBurst: 2,
		})
		e = httptest.NewServer(echoApp)
	})

	AfterEach(func() { e.Close() })

	getCommands := func(bearer string) int {
		req, _ := http.NewRequest(http.MethodGet, e.URL+"/api/v1/nodes/node-1/commands", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()
		return resp.StatusCode
	}

	heartbeat := func(bearer string) int {
		req, _ := http.NewRequest(http.MethodPost, e.URL+"/api/v1/nodes/node-1/heartbeat", strings.NewReader(`{"agentVersion":"1.0"}`))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()
		return resp.StatusCode
	}

	It("never rate-limits the admin on the shared command route (CAPI provider path)", func() {
		// Far more than the node burst; the admin sets no node ID, so the node
		// limiter skips it entirely. This is the guarantee that keeps the CAPI
		// infra provider's command polling exempt.
		for i := 0; i < 10; i++ {
			Expect(getCommands("admin-pass")).NotTo(Equal(http.StatusTooManyRequests))
		}
	})

	It("rate-limits a node on the shared command route once its burst is spent", func() {
		codes := make([]int, 0, 5)
		for i := 0; i < 5; i++ {
			codes = append(codes, getCommands("agent-key"))
		}
		Expect(codes).To(ContainElement(http.StatusTooManyRequests))
	})

	It("shares one bucket per node across heartbeat and command polling", func() {
		// Burst is 2. Spend it with one heartbeat plus one command poll, then
		// either route is throttled for that node — proving a single shared bucket
		// rather than one bucket per endpoint group (which would double the rate).
		Expect(heartbeat("agent-key")).NotTo(Equal(http.StatusTooManyRequests))
		Expect(getCommands("agent-key")).NotTo(Equal(http.StatusTooManyRequests))
		Expect(getCommands("agent-key")).To(Equal(http.StatusTooManyRequests))
		Expect(heartbeat("agent-key")).To(Equal(http.StatusTooManyRequests))
	})

	It("rate-limits registration per client IP once its burst is spent", func() {
		body := `{"registrationToken":"reg-token","machineID":"m1","hostname":"h1"}`
		codes := make([]int, 0, 5)
		for i := 0; i < 5; i++ {
			resp, err := http.Post(e.URL+"/api/v1/nodes/register", "application/json", strings.NewReader(body))
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			codes = append(codes, resp.StatusCode)
		}
		Expect(codes).To(ContainElement(http.StatusTooManyRequests))
	})
})
