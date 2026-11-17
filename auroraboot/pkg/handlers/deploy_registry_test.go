package handlers

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// memDeploymentStore is a minimal in-package fake for the white-box progress test.
type memDeploymentStore struct {
	mu   sync.Mutex
	deps map[string]*store.Deployment
}

func (m *memDeploymentStore) Create(_ context.Context, dep *store.Deployment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deps == nil {
		m.deps = map[string]*store.Deployment{}
	}
	cp := *dep
	m.deps[dep.ID] = &cp
	return nil
}
func (m *memDeploymentStore) GetByID(_ context.Context, id string) (*store.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deps[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *d
	return &cp, nil
}
func (m *memDeploymentStore) Update(_ context.Context, dep *store.Deployment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *dep
	m.deps[dep.ID] = &cp
	return nil
}
func (m *memDeploymentStore) List(_ context.Context) ([]*store.Deployment, error) { return nil, nil }
func (m *memDeploymentStore) ListByArtifact(_ context.Context, _ string) ([]*store.Deployment, error) {
	return nil, nil
}
func (m *memDeploymentStore) Delete(_ context.Context, _ string) error { return nil }
func (m *memDeploymentStore) CASEjectState(_ context.Context, id, from, to string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deps[id]
	if !ok || d.EjectState != from {
		return false, nil
	}
	d.EjectState = to
	return true, nil
}

// TestRunRegistryCancel verifies the run-registry stores a cancel func that, when
// invoked via CancelRun, actually cancels the associated context and that the run
// is then deregistered.
func TestRunRegistryCancel(t *testing.T) {
	h := &DeployHandler{runs: map[string]context.CancelFunc{}}

	ctx, cancel := context.WithCancel(context.Background())
	h.registerRun("dep-1", cancel)

	select {
	case <-ctx.Done():
		t.Fatal("context cancelled before CancelRun")
	default:
	}

	if !h.CancelRun("dep-1") {
		t.Fatal("CancelRun should report a matching run was found")
	}

	select {
	case <-ctx.Done():
		// cancelled as expected
	case <-time.After(time.Second):
		t.Fatal("CancelRun did not cancel the registered context")
	}

	// Unknown id reports false.
	if h.CancelRun("missing") {
		t.Fatal("CancelRun on an unknown id should report false")
	}

	// After the run completes it must be deregistered.
	h.deregisterRun("dep-1")
	if h.CancelRun("dep-1") {
		t.Fatal("a deregistered run must no longer be cancellable")
	}
}

// TestProgressUpdater verifies the deploy progress callback writes increasing
// progress/step onto the Deployment row and never regresses Progress.
func TestProgressUpdater(t *testing.T) {
	ds := &memDeploymentStore{}
	if err := ds.Create(context.Background(), &store.Deployment{ID: "dep-1", Status: store.DeployActive}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := &DeployHandler{deployments: ds}
	update := h.progressUpdater("dep-1")

	update("discovering", 10)
	update("inserting media", 30)
	// A lower percent must not regress Progress, but may update the step label.
	update("inserting media", 20)

	got, err := ds.GetByID(context.Background(), "dep-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Progress != 30 {
		t.Fatalf("Progress = %d, want 30 (must not regress)", got.Progress)
	}
	if got.Message != "inserting media" {
		t.Fatalf("Message = %q, want %q", got.Message, "inserting media")
	}
}
