package redfish_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
)

// mockupBMC is the P4 recorded-mockup replay harness (design §4a, "approach A").
// It stands up an httptest TLS server that serves GET requests from a recorded
// DMTF mockup directory tree (the layout produced by redfish-mockup-creator: each
// resource URI is a directory whose index.json is the GET body for that URI) while
// keeping the synthesized POST/PATCH/DELETE action handlers a static mockup cannot
// record.
//
// This is the no-hardware fidelity substitute for metal: a contributor records the
// read-only GET tree of a real BMC, and CI replays the deploy flow's *request
// sequence* against those recorded responses. The InsertMedia / Reset / Task
// responses are still synthesized (202 + Task, 204, Completed) because a read-only
// walk cannot capture action results — this is the honest caveat the support tiers
// name.
//
// It reuses the existing fakeBMC for the synthesized write-side handlers and the
// per-request recording (method/path/auth, captured bodies), so the golden tests
// can assert the protocol flow exactly as deployer_test.go does. GETs that the
// mockup tree does not cover fall through to fakeBMC's synthesized GET handlers
// (session create, Task polling), so a full Connect→Deploy→Close runs end to end.
type mockupBMC struct {
	server *httptest.Server
	// root is the directory holding the recorded tree's "redfish/v1/..." hierarchy.
	root string
	// fake provides the synthesized action handlers, request recording, and the
	// captured-body fields (insertBody, resetBody, bootPatchBody, sessionBody,
	// resetSystemID, sessionLocation). Its own httptest server is never started;
	// only its handler logic is reused.
	fake *fakeBMC
}

// newMockupBMC builds a replay harness serving the recorded tree rooted at dir.
// dir is the directory that contains the top-level "redfish" folder (i.e. the
// mockup root, e.g. testdata/mockups/ilo/<firmware>).
func newMockupBMC(dir string) *mockupBMC {
	m := &mockupBMC{
		root: dir,
		// A bare fakeBMC supplies the synthesized write handlers and recording. We
		// never start fake.server (it would bind a port we do not use); newFakeBMC
		// does start one, so close it immediately and keep only the handler state.
		fake: newFakeBMC(),
	}
	m.fake.Close()
	m.server = httptest.NewTLSServer(http.HandlerFunc(m.handle))
	return m
}

func (m *mockupBMC) Close() { m.server.Close() }

func (m *mockupBMC) URL() string { return m.server.URL }

// handle serves GETs from the recorded mockup tree and delegates every write
// (POST/PATCH/DELETE) and the synthesized session/task GETs to the embedded
// fakeBMC. Requests are recorded on the fake so the golden tests can assert the
// full method+path+auth sequence and the captured bodies.
func (m *mockupBMC) handle(w http.ResponseWriter, r *http.Request) {
	// Static resource GETs come from the recorded tree.
	if r.Method == http.MethodGet {
		if body, ok := m.lookup(r.URL.Path); ok {
			m.fake.record(r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
	}

	// The one-time boot override PATCHes a recorded ComputerSystem resource. A
	// static mockup records the GET but not the PATCH result, so the harness
	// captures the boot body (for golden assertions) and echoes the recorded
	// resource back — keeping the discovered member IDs from the mockup rather than
	// the synthesized fake's hardcoded sys-xyz.
	if r.Method == http.MethodPatch {
		if body, ok := m.lookup(r.URL.Path); ok {
			m.fake.record(r)
			m.fake.mu.Lock()
			m.fake.bootPatchBody = decodeBody(r)
			m.fake.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
	}

	// Everything else — the session create POST, the InsertMedia/Reset actions, the
	// session DELETE, and the Task poll GET — is synthesized by the fakeBMC handler,
	// which records the request and serves the spec-shaped action responses (and
	// 404s anything genuinely unknown).
	m.fake.handle(w, r)
}

// lookup resolves a Redfish URI path to the bytes of the recorded index.json for
// that resource, following the DMTF mockup layout. It returns (body, true) on a
// hit and (nil, false) when the tree does not record that path (the caller then
// falls back to the synthesized handler). Trailing slashes are tolerated and the
// "/redfish/v1" root resolves to redfish/v1/index.json.
func (m *mockupBMC) lookup(urlPath string) ([]byte, bool) {
	clean := strings.Trim(urlPath, "/")
	if clean == "" {
		return nil, false
	}
	// Reject any traversal in the request path: the mockup tree is a fixed,
	// committed fixture and a request must never escape it.
	if strings.Contains(clean, "..") {
		return nil, false
	}
	indexPath := filepath.Join(m.root, filepath.FromSlash(clean), "index.json")
	body, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, false
	}
	return body, true
}

// The golden tests assert against the same captured state the deployer_test specs
// use; these accessors forward to the embedded fakeBMC so the harness reads like
// the synthesized one.

func (m *mockupBMC) sawRequest(method, pathPrefix string) bool {
	return m.fake.sawRequest(method, pathPrefix)
}

func (m *mockupBMC) sessionLocation() string { return m.fake.sessionLocation }

func (m *mockupBMC) insertBody() map[string]any { return m.fake.insertBody }

func (m *mockupBMC) bootPatchBody() map[string]any { return m.fake.bootPatchBody }

func (m *mockupBMC) resetBody() map[string]any { return m.fake.resetBody }

func (m *mockupBMC) resetSystemID() string { return m.fake.resetSystemID }
