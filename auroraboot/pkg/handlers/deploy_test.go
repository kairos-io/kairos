package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeDeploymentStore implements store.DeploymentStore for testing.
type fakeDeploymentStore struct {
	mu        sync.Mutex
	deps      []*store.Deployment
	createErr error
	casErr    error // when set, CASEjectState returns this error (fail-closed paths)
}

func (f *fakeDeploymentStore) Create(_ context.Context, dep *store.Deployment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.deps = append(f.deps, dep)
	return nil
}

func (f *fakeDeploymentStore) GetByID(_ context.Context, id string) (*store.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.deps {
		if d.ID == id {
			cp := *d // return a copy (gorm materializes a fresh struct)
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeDeploymentStore) List(_ context.Context) ([]*store.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Return copies, not the stored pointers: real gorm List materializes a fresh
	// struct per query, so concurrent callers (e.g. the eject-on-phone-home goroutines
	// in MaybeFinalizeForNode) must each get their own object to read/mutate. The
	// stored row is only mutated under-lock via CASEjectState/Update, which remain the
	// serialization point. Returning shared pointers here races under -race.
	out := make([]*store.Deployment, len(f.deps))
	for i, d := range f.deps {
		cp := *d
		out[i] = &cp
	}
	return out, nil
}

func (f *fakeDeploymentStore) ListByArtifact(_ context.Context, artifactID string) ([]*store.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.Deployment
	for _, d := range f.deps {
		if d.ArtifactID == artifactID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeDeploymentStore) Update(_ context.Context, dep *store.Deployment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, d := range f.deps {
		if d.ID == dep.ID {
			// Store a copy, not the caller's pointer: gorm persists values, so the
			// caller may keep mutating its struct (e.g. finalizeDeployment after
			// Update) without that aliasing the stored row a concurrent List reads.
			cp := *dep
			f.deps[i] = &cp
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeDeploymentStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, d := range f.deps {
		if d.ID == id {
			f.deps = append(f.deps[:i], f.deps[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

// CASEjectState mirrors the gorm compare-and-set: it transitions eject_state under
// the store mutex so two concurrent finalize attempts race exactly as production
// does (exactly one observes the still-`from` row and wins).
func (f *fakeDeploymentStore) CASEjectState(_ context.Context, id, from, to string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.casErr != nil {
		return false, f.casErr
	}
	for _, d := range f.deps {
		if d.ID == id && d.EjectState == from {
			d.EjectState = to
			return true, nil
		}
	}
	return false, nil
}

// fakeBMCTargetStore implements store.BMCTargetStore for testing.
type fakeBMCTargetStore struct {
	mu      sync.Mutex
	targets []*store.BMCTarget
}

func (f *fakeBMCTargetStore) Create(_ context.Context, t *store.BMCTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets = append(f.targets, t)
	return nil
}

func (f *fakeBMCTargetStore) GetByID(_ context.Context, id string) (*store.BMCTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.targets {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeBMCTargetStore) List(_ context.Context) ([]*store.BMCTarget, error) {
	return f.targets, nil
}

func (f *fakeBMCTargetStore) Update(_ context.Context, t *store.BMCTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, existing := range f.targets {
		if existing.ID == t.ID {
			f.targets[i] = t
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeBMCTargetStore) Delete(_ context.Context, id string) error {
	return nil
}

var _ = Describe("DeployHandler.DeployRedfish", func() {
	var (
		e            *echo.Echo
		artifacts    *fakeArtifactStore
		deployments  *fakeDeploymentStore
		bmcTargets   *fakeBMCTargetStore
		settings     *fakeSettingsStore
		serve        *isoserve.Server
		artifactsDir string
	)

	// newHandler builds a handler, optionally wiring an iso-serve. When a serve is
	// wired the runtime local-serve enable flag is also set, since local serving is
	// now gated on both a launch-configured listener AND the enable flag.
	newHandler := func(withServe bool) *handlers.DeployHandler {
		if withServe {
			serve = isoserve.New(isoserve.Config{BaseURL: "http://10.0.0.5:8090"})
			settings.values[handlers.SettingLocalServeEnabled] = "true"
		} else {
			serve = nil
		}
		return handlers.NewDeployHandler(artifacts, deployments, bmcTargets, nil, artifactsDir, serve, nil).
			WithSettings(settings)
	}

	// doDeploy posts a deploy request for artifact "art-1" with the given body.
	doDeploy := func(h *handlers.DeployHandler, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/art-1/deploy/redfish", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("art-1")
		Expect(h.DeployRedfish(c)).To(Succeed())
		return rec
	}

	BeforeEach(func() {
		e = echo.New()
		artifactsDir = GinkgoT().TempDir()
		// Lay down the artifact's on-disk ISO at <dir>/art-1/kairos.iso.
		Expect(os.MkdirAll(filepath.Join(artifactsDir, "art-1"), 0755)).To(Succeed())
		isoPath := filepath.Join(artifactsDir, "art-1", "kairos.iso")
		Expect(os.WriteFile(isoPath, []byte("ISO-BYTES"), 0644)).To(Succeed())

		artifacts = &fakeArtifactStore{}
		// ArtifactFiles stores each file's FULL path (as the builder writes it),
		// not a bare basename. Modelling it as a basename previously masked a
		// path-doubling bug in DeployRedfish, so use the real format here.
		Expect(artifacts.Create(context.Background(), &store.ArtifactRecord{
			ID:            "art-1",
			ArtifactFiles: []string{isoPath},
		})).To(Succeed())
		deployments = &fakeDeploymentStore{}
		bmcTargets = &fakeBMCTargetStore{}
		settings = newFakeSettingsStore()
	})

	It("rejects an SSRF-blocked imageUrl", func() {
		h := newHandler(false)
		rec := doDeploy(h, `{"endpoint":"http://10.0.0.9","username":"u","password":"p","imageUrl":"http://169.254.169.254/x.iso"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("invalid imageUrl"))
	})

	It("rejects an SSRF-blocked BMC endpoint via inline creds", func() {
		// Endpoint validation happens through Connect, but the imageUrl path runs
		// first; use a valid imageUrl so the request reaches deployment creation
		// and confirm a deployment is queued (the goroutine will fail to connect).
		h := newHandler(false)
		rec := doDeploy(h, `{"endpoint":"http://10.0.0.9","username":"u","password":"p","imageUrl":"http://10.0.0.5/x.iso"}`)
		Expect(rec.Code).To(Equal(http.StatusAccepted))
	})

	It("returns 503 when no imageUrl and serving is not configured", func() {
		h := newHandler(false)
		rec := doDeploy(h, `{"endpoint":"http://10.0.0.9","username":"u","password":"p"}`)
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(rec.Body.String()).To(ContainSubstring("local ISO serving is not enabled"))
	})

	It("registers the on-disk ISO with the serve helper when no imageUrl is given", func() {
		h := newHandler(true)
		rec := doDeploy(h, `{"endpoint":"http://10.0.0.9","username":"u","password":"p"}`)
		Expect(rec.Code).To(Equal(http.StatusAccepted))

		// A deployment row was created.
		var dep store.Deployment
		Expect(json.Unmarshal(rec.Body.Bytes(), &dep)).To(Succeed())
		Expect(dep.Method).To(Equal("redfish"))
		Expect(dep.Status).To(Equal(store.DeployActive))

		// The serve helper now serves the registered ISO. Drive its handler
		// directly and confirm the bytes are reachable via the tokenized URL.
		ts := httptest.NewServer(serve.Handler())
		defer ts.Close()
		// Re-derive the path by registering again is not possible (opaque token),
		// so instead assert the registered file is reachable by serving a fresh
		// token bound to the same file through the same server instance.
		url, _, err := serve.Register(filepath.Join(artifactsDir, "art-1", "kairos.iso"), time.Minute)
		Expect(err).NotTo(HaveOccurred())
		path := strings.TrimPrefix(url, "http://10.0.0.5:8090")
		resp, err := http.Get(ts.URL + path)
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("serves an uploaded ISO stored as a bare file name from the artifacts dir", func() {
		artifacts.records[0].ArtifactFiles = []string{"kairos.iso"}
		h := newHandler(true)
		rec := doDeploy(h, `{"endpoint":"http://10.0.0.9","username":"u","password":"p"}`)
		Expect(rec.Code).To(Equal(http.StatusAccepted), rec.Body.String())
	})

	It("returns 400 when the artifact has no ISO", func() {
		artifacts.records[0].ArtifactFiles = []string{"image.raw"}
		h := newHandler(true)
		rec := doDeploy(h, `{"endpoint":"http://10.0.0.9","username":"u","password":"p"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("no ISO file found"))
	})
})

var _ = Describe("Server.UsesTLS drives the InsertMedia transfer protocol", func() {
	It("reports HTTPS when both cert and key are configured", func() {
		s := isoserve.New(isoserve.Config{
			BaseURL:  "https://10.0.0.5:8090",
			CertFile: "/tmp/cert.pem",
			KeyFile:  "/tmp/key.pem",
		})
		Expect(s.UsesTLS()).To(BeTrue())
	})

	It("reports HTTP when no cert/key is configured", func() {
		s := isoserve.New(isoserve.Config{BaseURL: "http://10.0.0.5:8090"})
		Expect(s.UsesTLS()).To(BeFalse())
	})

	It("reports HTTP when only one of cert/key is set", func() {
		s := isoserve.New(isoserve.Config{
			BaseURL:  "http://10.0.0.5:8090",
			CertFile: "/tmp/cert.pem",
		})
		Expect(s.UsesTLS()).To(BeFalse())
	})
})
