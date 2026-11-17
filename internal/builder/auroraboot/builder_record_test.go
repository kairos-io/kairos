package auroraboot_test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/internal/builder/auroraboot"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

func noopDeploy(_ context.Context, _ schema.Config, _ schema.ReleaseArtifact, _ string, _ io.Writer) error {
	return nil
}

// noopKairosify stands in for the kairos-init derivation step. These specs
// have to pass a store to see the record the builder writes, which means they
// cannot use the store==nil test-mode short circuit in ensureKairosified, so
// without this the build goroutine runs a real `docker build` and the phase
// never leaves Building on any machine that has a container runtime.
func noopKairosify(_ context.Context, image string, _ builder.BuildOptions, _ string, _ io.Writer) (string, error) {
	return image, nil
}

// recStore is a minimal ArtifactStore that captures the record the builder
// persists at Build time so a test can assert every field the frontend later
// clones from is on the row, and the log it appends. Guarded because b.run() spawns a goroutine that
// also touches the record via UpdatePhaseMessage.
type recStore struct {
	mu   sync.Mutex
	recs map[string]*store.ArtifactRecord
	logs map[string]string
}

func newRecStore() *recStore {
	return &recStore{recs: map[string]*store.ArtifactRecord{}, logs: map[string]string{}}
}

func (s *recStore) Create(_ context.Context, rec *store.ArtifactRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *rec
	s.recs[rec.ID] = &cp
	return nil
}
func (s *recStore) GetByID(_ context.Context, id string) (*store.ArtifactRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *r
	return &cp, nil
}
func (s *recStore) List(_ context.Context) ([]*store.ArtifactRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*store.ArtifactRecord, 0, len(s.recs))
	for _, r := range s.recs {
		out = append(out, r)
	}
	return out, nil
}
func (s *recStore) Update(_ context.Context, rec *store.ArtifactRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.recs[rec.ID]; !ok {
		return fmt.Errorf("not found")
	}
	cp := *rec
	s.recs[rec.ID] = &cp
	return nil
}
func (s *recStore) UpdatePhaseMessage(_ context.Context, id, phase, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.recs[id]; ok {
		r.Phase = phase
		r.Message = message
	}
	return nil
}
func (s *recStore) UpdateFiles(_ context.Context, id string, files []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.recs[id]; ok {
		r.ArtifactFiles = files
	}
	return nil
}
func (s *recStore) UpdateNameSaved(context.Context, string, *string, *bool) error { return nil }
func (s *recStore) ClearUploadToken(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.recs[id]; ok {
		r.UploadToken = ""
	}
	return nil
}
func (s *recStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.recs, id)
	return nil
}
func (s *recStore) DeleteByPhase(_ context.Context, _ string) error { return nil }
func (s *recStore) GetLogs(_ context.Context, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs[id], nil
}
func (s *recStore) AppendLog(_ context.Context, id string, chunk string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs[id] += chunk
	return nil
}

var _ = Describe("AuroraBoot Builder record persistence", func() {
	// This spec pins the fields the ArtifactBuilder's clone flow reads back
	// out of the artifact row. The handler's own store.Create at
	// pkg/handlers/artifacts.go is skipped when the builder already wrote
	// the row, so anything the builder omits here is silently dropped for
	// local builds and the cloned form comes up empty.
	It("persists the Kubernetes fields the clone flow reads from the row", func() {
		s := newRecStore()
		// Inject the platform probe: without it the cross-arch preflight shells
		// out to the developer's own docker and the spec depends on the host's
		// binfmt state.
		b := auroraboot.New(GinkgoT().TempDir(), noopDeploy, s).
			WithPlatformsFunc(func(context.Context) ([]string, error) { return nil, nil }).
			WithKairosifyFunc(noopKairosify)

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:            "clone-k8s",
			BaseImage:     "quay.io/kairos/ubuntu:latest",
			KairosVersion: "v4.1.2",
			Model:         "generic",
			Source: builder.ImageSource{
				BaseImage:         "quay.io/kairos/ubuntu:latest",
				KairosVersion:     "v4.1.2",
				Model:             "generic",
				Arch:              "amd64",
				Variant:           "standard",
				KubernetesDistro:  "k3s",
				KubernetesVersion: "v1.31.4+k3s1",
			},
			Provisioning: builder.ProvisioningOptions{
				KubernetesEnabled: true,
				TargetGroupID:     "grp-1",
			},
			OverlayRootfs: "/srv/artifacts/overlays/6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a",
			OverlayID:     "6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a",
		})
		Expect(err).NotTo(HaveOccurred())

		rec, err := s.GetByID(context.Background(), "clone-k8s")
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Variant).To(Equal("standard"))
		Expect(rec.KubernetesDistro).To(Equal("k3s"))
		Expect(rec.KubernetesVersion).To(Equal("v1.31.4+k3s1"))
		Expect(rec.KubernetesEnabled).NotTo(BeNil())
		Expect(*rec.KubernetesEnabled).To(BeTrue())
		Expect(rec.TargetGroupID).To(Equal("grp-1"))
		Expect(rec.OverlayID).To(Equal("6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a"))
	})
})

var _ = Describe("AuroraBoot Builder record persistence of catalog extensions", func() {
	// The artifact page's Build summary and the Clone flow read the catalog
	// extensions back from the row (kairos-io/kairos#5274). The handler's
	// own record is skipped when the builder already wrote one, so the
	// builder must persist them itself.
	It("persists Extensions and ExtensionsCatalogs", func() {
		s := newRecStore()
		b := auroraboot.New(GinkgoT().TempDir(), noopDeploy, s).WithKairosifyFunc(noopKairosify)

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:                 "with-extensions",
			BaseImage:          "quay.io/kairos/hadron:latest",
			Extensions:         []string{"tailscale", "k3s@v1.31.4"},
			ExtensionsCatalogs: []string{"https://example.com/releases.json"},
		})
		Expect(err).NotTo(HaveOccurred())

		// Wait for the build goroutine to finish so the record is read after
		// the post-build phase and file writes, and so the goroutine is not
		// still writing into the TempDir when Ginkgo removes it.
		Eventually(func() string {
			r, err := s.GetByID(context.Background(), "with-extensions")
			if err != nil {
				return ""
			}
			return r.Phase
		}, 10*time.Second).Should(BeElementOf(store.ArtifactReady, store.ArtifactError))

		rec, err := s.GetByID(context.Background(), "with-extensions")
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Extensions).To(Equal([]string{"tailscale", "k3s@v1.31.4"}))
		Expect(rec.ExtensionsCatalogs).To(Equal([]string{"https://example.com/releases.json"}))
	})
})

var _ = Describe("AuroraBoot Builder Cancel of a finished build", func() {
	// A finished build stays in the builder's map, so a cancel that races
	// completion, or any API call to cancel, used to flip its stored row to
	// Error. "Clear failed" then deleted the good artifact
	// (kairos-io/kairos#5873).
	It("leaves a Ready row alone", func() {
		s := newRecStore()
		b := auroraboot.New(GinkgoT().TempDir(), noopDeploy, s).WithKairosifyFunc(noopKairosify)

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:        "ready-1",
			BaseImage: "quay.io/kairos/hadron:latest",
		})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			r, err := s.GetByID(context.Background(), "ready-1")
			if err != nil {
				return ""
			}
			return r.Phase
		}, 10*time.Second).Should(Equal(store.ArtifactReady))

		Expect(b.Cancel(context.Background(), "ready-1")).To(Succeed())

		rec, err := s.GetByID(context.Background(), "ready-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Phase).To(Equal(store.ArtifactReady))
		Expect(rec.Message).NotTo(Equal("cancelled"))

		st, err := b.Status(context.Background(), "ready-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Phase).To(Equal(builder.BuildReady))
	})
})
