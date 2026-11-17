//go:build operator_e2e

package operator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	buildv1alpha2 "github.com/kairos-io/kairos-operator/api/v1alpha2"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"

	opbuilder "github.com/kairos-io/AuroraBoot/internal/builder/operator"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// memArtifactStore is the minimum ArtifactStore surface the phase-watch
// goroutine needs. It lets the cluster spec assert store writeback without
// standing up gorm / SQLite for the e2e run.
type memArtifactStore struct {
	mu      sync.Mutex
	records map[string]*store.ArtifactRecord
}

func newMemArtifactStore() *memArtifactStore {
	return &memArtifactStore{records: map[string]*store.ArtifactRecord{}}
}

func (m *memArtifactStore) Create(_ context.Context, rec *store.ArtifactRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[rec.ID] = rec
	return nil
}

func (m *memArtifactStore) GetByID(_ context.Context, id string) (*store.ArtifactRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *r
	return &cp, nil
}

func (m *memArtifactStore) List(_ context.Context) ([]*store.ArtifactRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.ArtifactRecord, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, r)
	}
	return out, nil
}

func (m *memArtifactStore) Update(_ context.Context, rec *store.ArtifactRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[rec.ID]; !ok {
		return fmt.Errorf("not found")
	}
	m.records[rec.ID] = rec
	return nil
}

// UpdatePhaseMessage mirrors the column-scoped write the phase-watch
// goroutine performs. It has to mutate the stored record, not just
// succeed: the spec below asserts the Pending -> Building transition by
// reading the row back.
func (m *memArtifactStore) UpdatePhaseMessage(_ context.Context, id, phase, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	rec.Phase = phase
	rec.Message = message
	return nil
}

func (m *memArtifactStore) UpdateFiles(_ context.Context, id string, files []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	rec.ArtifactFiles = files
	return nil
}
func (m *memArtifactStore) UpdateNameSaved(context.Context, string, *string, *bool) error { return nil }

func (m *memArtifactStore) ClearUploadToken(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	rec.UploadToken = ""
	return nil
}

func (m *memArtifactStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, id)
	return nil
}

func (m *memArtifactStore) DeleteByPhase(_ context.Context, _ string) error     { return nil }
func (m *memArtifactStore) GetLogs(_ context.Context, _ string) (string, error) { return "", nil }
func (m *memArtifactStore) AppendLog(_ context.Context, _, _ string) error      { return nil }

// testLogSink implements builder.LogBroadcaster by collecting every chunk
// into a mutex-guarded slice so specs can Eventually-assert log arrival.
type testLogSink struct {
	mu     sync.Mutex
	chunks []string
}

func (s *testLogSink) BroadcastLogChunk(_ string, chunk string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, chunk)
}

func (s *testLogSink) Snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.chunks))
	copy(out, s.chunks)
	return out
}

var _ = Describe("Operator builder against a real cluster", func() {
	const buildID = "auroraboot-builder-e2e"

	It("Build creates a CR that progresses toward Building; List reports it; Cancel removes it", func() {
		ctx := context.Background()

		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules, &clientcmd.ConfigOverrides{},
		).ClientConfig()
		Expect(err).NotTo(HaveOccurred(), "load REST config from KUBECONFIG")

		// Attach an ArtifactStore so the operator builder's phase-watch
		// goroutine has somewhere to write transitions. We pre-Create the
		// row (as the HTTP handler does in production) and then assert the
		// watcher advances it Pending -> Building within the reconcile
		// window; without this, operator-backed builds sit at Pending in
		// the DB forever and the UI never leaves the "queued" state.
		artStore := newMemArtifactStore()
		Expect(artStore.Create(ctx, &store.ArtifactRecord{ID: buildID, Phase: builder.BuildPending})).To(Succeed())

		b, err := opbuilder.New(opbuilder.Config{
			RESTConfig: cfg,
			Namespace:  testNamespace,
			Store:      artStore,
		})
		Expect(err).NotTo(HaveOccurred(), "construct operator.Builder")

		status, err := b.Build(ctx, builder.BuildOptions{
			ID:        buildID,
			BaseImage: "quay.io/kairos/opensuse:leap-15.6-core-amd64-generic-v3.6.0",
			Source:    builder.ImageSource{Arch: "amd64"},
			Outputs:   builder.OutputOptions{ISO: true},
		})
		Expect(err).NotTo(HaveOccurred(), "Build should submit the CR")
		Expect(status.ID).To(Equal(buildID))
		Expect(status.Phase).To(Equal(builder.BuildPending))

		DeferCleanup(cleanupArtifact, ctx, buildID)
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				collectDebugLogs(ctx, buildID)
			}
		})

		got := &buildv1alpha2.OSArtifact{}
		Expect(testClient.Get(ctx, types.NamespacedName{Name: buildID, Namespace: testNamespace}, got)).To(Succeed())
		Expect(got.Spec.Image.Ref).To(Equal("quay.io/kairos/opensuse:leap-15.6-core-amd64-generic-v3.6.0"))

		Eventually(func() (string, error) {
			s, err := b.Status(ctx, buildID)
			if err != nil {
				return "", err
			}
			return s.Phase, nil
		}, 2*time.Minute, 2*time.Second).Should(Equal(builder.BuildBuilding), "Status reports Building")

		// The phase-watch goroutine must mirror the operator-reported phase
		// into the store. Give it up to 90s past the operator reaching
		// Building; the watcher polls every 2s so the writeback lag is a
		// small fixed number of intervals on top of the reconcile itself.
		Eventually(func() string {
			rec, err := artStore.GetByID(ctx, buildID)
			if err != nil {
				return ""
			}
			return rec.Phase
		}, 90*time.Second, 2*time.Second).Should(Equal(builder.BuildBuilding),
			"store row observes phase transition from Pending to Building")

		builds, err := b.List(ctx)
		Expect(err).NotTo(HaveOccurred())
		ids := make([]string, 0, len(builds))
		for _, s := range builds {
			ids = append(ids, s.ID)
		}
		Expect(ids).To(ContainElement(buildID))

		Expect(b.Cancel(ctx, buildID)).To(Succeed())

		Eventually(func() bool {
			_, err := b.Status(ctx, buildID)
			return err != nil && errors.Is(err, opbuilder.ErrNotFound)
		}, 30*time.Second, 2*time.Second).Should(BeTrue(), "Status reports NotFound after Cancel")
	})

	It("streams build Pod logs to the attached LogBroadcaster", func() {
		const logsBuildID = "auroraboot-builder-e2e-logs"
		ctx := context.Background()

		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules, &clientcmd.ConfigOverrides{},
		).ClientConfig()
		Expect(err).NotTo(HaveOccurred(), "load REST config from KUBECONFIG")

		sink := &testLogSink{}
		b, err := opbuilder.New(opbuilder.Config{
			RESTConfig: cfg,
			Namespace:  testNamespace,
		})
		Expect(err).NotTo(HaveOccurred(), "construct operator.Builder")
		b = b.WithLogBroadcaster(sink)

		status, err := b.Build(ctx, builder.BuildOptions{
			ID:        logsBuildID,
			BaseImage: "quay.io/kairos/opensuse:leap-15.6-core-amd64-generic-v3.6.0",
			Source:    builder.ImageSource{Arch: "amd64"},
			Outputs:   builder.OutputOptions{ISO: true},
		})
		Expect(err).NotTo(HaveOccurred(), "Build should submit the CR")
		Expect(status.ID).To(Equal(logsBuildID))

		DeferCleanup(cleanupArtifact, ctx, logsBuildID)
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				collectDebugLogs(ctx, logsBuildID)
			}
		})

		// The 3-minute budget covers a cold-cache first run in CI: kubelet
		// must pull the operator tool image (quay.io/kairos/auroraboot)
		// before the first init container can produce any output. Warm
		// runs finish in well under a minute.
		Eventually(func() bool {
			for _, chunk := range sink.Snapshot() {
				if strings.Contains(chunk, "auroraboot") ||
					strings.Contains(chunk, "buildah") ||
					strings.Contains(chunk, "kairos-release") ||
					strings.Contains(chunk, logsBuildID) {
					return true
				}
			}
			return false
		}, 3*time.Minute, 2*time.Second).Should(BeTrue(),
			"expected sink to collect at least one recognisable log line within 3m")
	})
})
