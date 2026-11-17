package operator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	buildv1alpha2 "github.com/kairos-io/kairos-operator/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// stubArtifactStore is the minimum ArtifactStore surface the operator watcher
// uses. Records are keyed by ID and every access is mutex-guarded so a
// background goroutine and the test's assertion goroutine cannot race.
type stubArtifactStore struct {
	mu      sync.Mutex
	records map[string]*store.ArtifactRecord
	updates int
}

func newStubArtifactStore() *stubArtifactStore {
	return &stubArtifactStore{records: map[string]*store.ArtifactRecord{}}
}

func (s *stubArtifactStore) Create(_ context.Context, rec *store.ArtifactRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.ID] = rec
	return nil
}

func (s *stubArtifactStore) GetByID(_ context.Context, id string) (*store.ArtifactRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *r
	return &cp, nil
}

func (s *stubArtifactStore) List(_ context.Context) ([]*store.ArtifactRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*store.ArtifactRecord, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	return out, nil
}

func (s *stubArtifactStore) Update(_ context.Context, rec *store.ArtifactRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[rec.ID]; !ok {
		return fmt.Errorf("not found")
	}
	s.records[rec.ID] = rec
	s.updates++
	return nil
}

func (s *stubArtifactStore) UpdatePhaseMessage(_ context.Context, id, phase, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	rec.Phase = phase
	rec.Message = message
	s.updates++
	return nil
}

func (s *stubArtifactStore) UpdateFiles(_ context.Context, id string, files []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	rec.ArtifactFiles = files
	s.updates++
	return nil
}
func (s *stubArtifactStore) UpdateNameSaved(context.Context, string, *string, *bool) error {
	return nil
}

func (s *stubArtifactStore) ClearUploadToken(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	rec.UploadToken = ""
	s.updates++
	return nil
}

func (s *stubArtifactStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

func (s *stubArtifactStore) DeleteByPhase(_ context.Context, _ string) error { return nil }
func (s *stubArtifactStore) GetLogs(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *stubArtifactStore) AppendLog(_ context.Context, _ string, _ string) error {
	return nil
}

func (s *stubArtifactStore) updateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updates
}

// newFakeBuilder builds a Builder wired to controller-runtime's fake client so
// unit tests can drive Create/Get/List/Delete without a real apiserver. It
// also injects a typed-client fake so Build's spawned log-streaming goroutine
// has something to talk to without reaching the network.
func newFakeBuilder(namespace string, objs ...client.Object) (*Builder, client.Client) {
	return newFakeBuilderWith(namespace, nil, nil, objs...)
}

// newFakeBuilderWith is the extended constructor used by tests that need to
// inject an interceptor into the controller-runtime client (to simulate a
// failing Secret Create for the orphan-CR-cleanup path) or attach an
// ArtifactStore (to exercise the phase-watch goroutine).
func newFakeBuilderWith(namespace string, s store.ArtifactStore, funcs *interceptor.Funcs, objs ...client.Object) (*Builder, client.Client) {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(buildv1alpha2.AddToScheme(scheme)).To(Succeed())

	fcb := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...)
	if funcs != nil {
		fcb = fcb.WithInterceptorFuncs(*funcs)
	}
	fc := fcb.Build()
	cs := k8sfake.NewSimpleClientset()

	b, err := newWithFactory(Config{
		RESTConfig: &rest.Config{Host: "https://fake.invalid"},
		Namespace:  namespace,
		Store:      s,
	}, func(_ Config, _ *runtime.Scheme) (client.Client, error) {
		return fc, nil
	}, func(_ Config) (kubernetes.Interface, error) {
		return cs, nil
	})
	Expect(err).NotTo(HaveOccurred())
	return b, fc
}

// cancelOnCleanup registers a DeferCleanup that Cancels the given build so
// its watchCRPhase and streamPodLogs goroutines exit before the test binary
// does. Without it the fake clientset keeps polling every 2s until process
// teardown.
func cancelOnCleanup(b *Builder, id string) {
	DeferCleanup(func() { _ = b.Cancel(context.Background(), id) })
}

var _ = Describe("Operator Builder", func() {
	Describe("New", func() {
		It("returns an error when RESTConfig is nil", func() {
			b, err := New(Config{Namespace: "default"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("RESTConfig"))
			Expect(b).To(BeNil())
		})

		It("returns an error when Namespace is empty", func() {
			b, err := New(Config{RESTConfig: &rest.Config{Host: "https://example.invalid"}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Namespace"))
			Expect(b).To(BeNil())
		})
	})

	Describe("Build", func() {
		It("creates an OSArtifact and returns Pending status", func() {
			ctx := context.Background()
			b, fc := newFakeBuilder("kairos-builds")

			status, err := b.Build(ctx, builder.BuildOptions{
				ID:        "build-1",
				BaseImage: "quay.io/kairos/ubuntu:v3.6.0",
				Source:    builder.ImageSource{Arch: "amd64"},
				Outputs:   builder.OutputOptions{ISO: true},
			})
			Expect(err).NotTo(HaveOccurred())
			cancelOnCleanup(b, status.ID)
			Expect(status.ID).To(Equal("build-1"))
			Expect(status.Phase).To(Equal(builder.BuildPending))

			got := &buildv1alpha2.OSArtifact{}
			Expect(fc.Get(ctx, types.NamespacedName{Name: "build-1", Namespace: "kairos-builds"}, got)).To(Succeed())
			Expect(got.Spec.Image.Ref).To(Equal("quay.io/kairos/ubuntu:v3.6.0"))
			Expect(got.Labels).To(HaveKeyWithValue(buildIDLabel, "build-1"))
		})

		It("generates an ID when none is supplied", func() {
			ctx := context.Background()
			b, _ := newFakeBuilder("kairos-builds")

			status, err := b.Build(ctx, builder.BuildOptions{
				BaseImage: "quay.io/kairos/ubuntu:v3.6.0",
				Source:    builder.ImageSource{Arch: "amd64"},
				Outputs:   builder.OutputOptions{ISO: true},
			})
			Expect(err).NotTo(HaveOccurred())
			cancelOnCleanup(b, status.ID)
			Expect(status.ID).NotTo(BeEmpty())
		})

		It("materializes a cloud-config Secret owned by the CR", func() {
			ctx := context.Background()
			b, fc := newFakeBuilder("kairos-builds")

			_, err := b.Build(ctx, builder.BuildOptions{
				ID:          "build-cc",
				BaseImage:   "quay.io/kairos/ubuntu:v3.6.0",
				Source:      builder.ImageSource{Arch: "amd64"},
				Outputs:     builder.OutputOptions{ISO: true},
				CloudConfig: "#cloud-config\n",
			})
			Expect(err).NotTo(HaveOccurred())
			cancelOnCleanup(b, "build-cc")

			sec := &corev1.Secret{}
			Expect(fc.Get(ctx, types.NamespacedName{Name: "build-cc-cloud-config", Namespace: "kairos-builds"}, sec)).To(Succeed())
			Expect(sec.Data).To(HaveKeyWithValue("cloud-config", []byte("#cloud-config\n")))
			Expect(sec.OwnerReferences).To(HaveLen(1))
			Expect(sec.OwnerReferences[0].Kind).To(Equal("OSArtifact"))
			Expect(sec.OwnerReferences[0].Name).To(Equal("build-cc"))
		})

		It("returns ErrInvalidBuildOptions on invalid arch", func() {
			ctx := context.Background()
			b, _ := newFakeBuilder("kairos-builds")

			_, err := b.Build(ctx, builder.BuildOptions{
				ID:        "bad",
				BaseImage: "quay.io/kairos/ubuntu:v3.6.0",
				Source:    builder.ImageSource{Arch: "riscv64"},
				Outputs:   builder.OutputOptions{ISO: true},
			})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, builder.ErrInvalidBuildOptions)).To(BeTrue())
		})

		It("attaches an upload exporter and Secret when AuroraBootURL and UploadToken are set", func() {
			ctx := context.Background()

			// Same fake wiring as newFakeBuilder, plus AuroraBootURL on the
			// Config so Build injects the exporter path.
			scheme := runtime.NewScheme()
			Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
			Expect(buildv1alpha2.AddToScheme(scheme)).To(Succeed())
			fc := fake.NewClientBuilder().WithScheme(scheme).Build()
			cs := k8sfake.NewSimpleClientset()

			bld, err := newWithFactory(Config{
				RESTConfig:    &rest.Config{Host: "https://fake.invalid"},
				Namespace:     "kairos-builds",
				AuroraBootURL: "https://auroraboot.example",
			}, func(_ Config, _ *runtime.Scheme) (client.Client, error) { return fc, nil },
				func(_ Config) (kubernetes.Interface, error) { return cs, nil })
			Expect(err).NotTo(HaveOccurred())

			_, err = bld.Build(ctx, builder.BuildOptions{
				ID:          "build-up",
				UploadToken: "tok-abcdef",
				BaseImage:   "quay.io/kairos/ubuntu:v3.6.0",
				Source:      builder.ImageSource{Arch: "amd64"},
				Outputs:     builder.OutputOptions{ISO: true},
			})
			Expect(err).NotTo(HaveOccurred())
			cancelOnCleanup(bld, "build-up")

			got := &buildv1alpha2.OSArtifact{}
			Expect(fc.Get(ctx, types.NamespacedName{Name: "build-up", Namespace: "kairos-builds"}, got)).To(Succeed())
			Expect(got.Spec.Exporters).To(HaveLen(1))

			sec := &corev1.Secret{}
			Expect(fc.Get(ctx, types.NamespacedName{Name: "build-up-upload", Namespace: "kairos-builds"}, sec)).To(Succeed())
			Expect(sec.Data).To(HaveKeyWithValue(uploadURLKey, []byte("https://auroraboot.example")))
			Expect(sec.Data).To(HaveKeyWithValue(uploadTokenKey, []byte("tok-abcdef")))
		})
	})

	Describe("Status", func() {
		It("translates operator phases into AuroraBoot phases", func() {
			ctx := context.Background()
			base := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "build-s", Namespace: "kairos-builds", Labels: map[string]string{buildIDLabel: "build-s"}},
				Status:     buildv1alpha2.OSArtifactStatus{Phase: buildv1alpha2.Exporting, Message: "packing artifacts"},
			}
			b, _ := newFakeBuilder("kairos-builds", base)

			got, err := b.Status(ctx, "build-s")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Phase).To(Equal(builder.BuildBuilding))
			Expect(got.Message).To(Equal("packing artifacts"))
		})

		It("defaults an empty phase to Pending", func() {
			ctx := context.Background()
			base := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "build-fresh", Namespace: "kairos-builds"},
			}
			b, _ := newFakeBuilder("kairos-builds", base)

			got, err := b.Status(ctx, "build-fresh")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Phase).To(Equal(builder.BuildPending))
		})

		It("returns ErrNotFound for an unknown ID", func() {
			ctx := context.Background()
			b, _ := newFakeBuilder("kairos-builds")

			_, err := b.Status(ctx, "does-not-exist")
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrNotFound)).To(BeTrue())
		})
	})

	Describe("List", func() {
		It("returns every labelled OSArtifact translated into BuildStatus", func() {
			ctx := context.Background()
			a := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "kairos-builds", Labels: map[string]string{buildIDLabel: "a"}},
				Status:     buildv1alpha2.OSArtifactStatus{Phase: buildv1alpha2.Ready},
			}
			b := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "kairos-builds", Labels: map[string]string{buildIDLabel: "b"}},
				Status:     buildv1alpha2.OSArtifactStatus{Phase: buildv1alpha2.Error, Message: "boom"},
			}
			untagged := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "untagged", Namespace: "kairos-builds"},
			}

			bld, _ := newFakeBuilder("kairos-builds", a, b, untagged)

			got, err := bld.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(HaveLen(2))
			ids := []string{got[0].ID, got[1].ID}
			Expect(ids).To(ConsistOf("a", "b"))
		})
	})

	Describe("Cancel", func() {
		It("deletes the OSArtifact CR", func() {
			ctx := context.Background()
			a := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "victim", Namespace: "kairos-builds", Labels: map[string]string{buildIDLabel: "victim"}},
			}
			bld, fc := newFakeBuilder("kairos-builds", a)

			Expect(bld.Cancel(ctx, "victim")).To(Succeed())

			got := &buildv1alpha2.OSArtifact{}
			err := fc.Get(ctx, types.NamespacedName{Name: "victim", Namespace: "kairos-builds"}, got)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("is a no-op on an unknown ID", func() {
			ctx := context.Background()
			bld, _ := newFakeBuilder("kairos-builds")
			Expect(bld.Cancel(ctx, "ghost")).To(Succeed())
		})

		// After a successful CR delete, the store row for the build must
		// leave the Building phase; leaving it stale would keep the UI
		// showing the build as in-flight forever. BuildError with the
		// "cancelled" message matches how the local backend marks a
		// cancelled build so the UI treatment is uniform.
		It("marks the store row as BuildError/cancelled after Delete succeeds", func() {
			ctx := context.Background()
			s := newStubArtifactStore()
			Expect(s.Create(ctx, &store.ArtifactRecord{ID: "victim", Phase: builder.BuildBuilding})).To(Succeed())
			a := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "victim", Namespace: "kairos-builds", Labels: map[string]string{buildIDLabel: "victim"}},
			}
			bld, _ := newFakeBuilderWith("kairos-builds", s, nil, a)

			Expect(bld.Cancel(ctx, "victim")).To(Succeed())

			rec, err := s.GetByID(ctx, "victim")
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.Phase).To(Equal(builder.BuildError))
			Expect(rec.Message).To(Equal("cancelled"))
		})
	})

	Describe("Build orphan-CR cleanup", func() {
		// If a Secret Create fails after the OSArtifact CR has already been
		// Created, we must reap the CR so it does not linger orphaned in
		// the cluster (the operator would then reconcile a CR with a
		// dangling SecretKeySelector and land in a bad state).
		It("deletes the OSArtifact CR when a Secret Create fails", func() {
			ctx := context.Background()
			funcs := &interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*corev1.Secret); ok {
						return fmt.Errorf("simulated secret create failure")
					}
					return c.Create(ctx, obj, opts...)
				},
			}
			bld, fc := newFakeBuilderWith("kairos-builds", nil, funcs)

			_, err := bld.Build(ctx, builder.BuildOptions{
				ID:          "orphan-me",
				BaseImage:   "quay.io/kairos/ubuntu:v3.6.0",
				Source:      builder.ImageSource{Arch: "amd64"},
				Outputs:     builder.OutputOptions{ISO: true},
				CloudConfig: "#cloud-config\n",
			})
			Expect(err).To(HaveOccurred())

			got := &buildv1alpha2.OSArtifact{}
			getErr := fc.Get(ctx, types.NamespacedName{Name: "orphan-me", Namespace: "kairos-builds"}, got)
			Expect(apierrors.IsNotFound(getErr)).To(BeTrue(), "OSArtifact should have been reaped after secret failure")
		})
	})

	Describe("watchCRPhase", func() {
		// The store row must observe every phase transition the operator
		// reports, or the UI would forever show Pending for operator-backed
		// builds (the handler reads from the store, not the CR).
		It("writes phase transitions to the store as the CR advances", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := newStubArtifactStore()
			Expect(s.Create(ctx, &store.ArtifactRecord{ID: "watch-1", Phase: builder.BuildPending})).To(Succeed())

			a := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "watch-1", Namespace: "kairos-builds", Labels: map[string]string{buildIDLabel: "watch-1"}},
				Status:     buildv1alpha2.OSArtifactStatus{Phase: buildv1alpha2.Building, Message: "starting"},
			}
			bld, fc := newFakeBuilderWith("kairos-builds", s, nil, a)

			go bld.watchCRPhase(ctx, "watch-1", 20*time.Millisecond)

			Eventually(func() string {
				r, _ := s.GetByID(ctx, "watch-1")
				return r.Phase
			}, 2*time.Second, 20*time.Millisecond).Should(Equal(builder.BuildBuilding))

			// Flip to Ready and expect the watcher to notice and exit.
			// Plain Update (rather than Status().Update()) is fine here
			// because we did not configure the fake client's status
			// subresource; a plain Update touches every field including status.
			cur := &buildv1alpha2.OSArtifact{}
			Expect(fc.Get(ctx, types.NamespacedName{Name: "watch-1", Namespace: "kairos-builds"}, cur)).To(Succeed())
			cur.Status.Phase = buildv1alpha2.Ready
			cur.Status.Message = "done"
			Expect(fc.Update(ctx, cur)).To(Succeed())

			Eventually(func() string {
				r, _ := s.GetByID(ctx, "watch-1")
				return r.Phase
			}, 2*time.Second, 20*time.Millisecond).Should(Equal(builder.BuildReady))
		})

		It("clears the upload token when the CR reaches a terminal phase", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := newStubArtifactStore()
			Expect(s.Create(ctx, &store.ArtifactRecord{
				ID: "watch-token", Phase: builder.BuildBuilding, UploadToken: "tok-abc",
			})).To(Succeed())

			a := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "watch-token", Namespace: "kairos-builds", Labels: map[string]string{buildIDLabel: "watch-token"}},
				Status:     buildv1alpha2.OSArtifactStatus{Phase: buildv1alpha2.Ready, Message: "done"},
			}
			bld, _ := newFakeBuilderWith("kairos-builds", s, nil, a)

			done := make(chan struct{})
			go func() {
				bld.watchCRPhase(ctx, "watch-token", 20*time.Millisecond)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				Fail("watcher did not exit after terminal phase")
			}

			rec, err := s.GetByID(ctx, "watch-token")
			Expect(err).NotTo(HaveOccurred())
			Expect(rec.UploadToken).To(Equal(""),
				"upload token must be zeroed on terminal transition so a leaked bearer cannot overwrite artifacts")
		})

		It("exits when the CR is deleted", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := newStubArtifactStore()
			Expect(s.Create(ctx, &store.ArtifactRecord{ID: "watch-gone", Phase: builder.BuildBuilding})).To(Succeed())

			a := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "watch-gone", Namespace: "kairos-builds", Labels: map[string]string{buildIDLabel: "watch-gone"}},
				Status:     buildv1alpha2.OSArtifactStatus{Phase: buildv1alpha2.Building},
			}
			bld, fc := newFakeBuilderWith("kairos-builds", s, nil, a)

			done := make(chan struct{})
			go func() {
				bld.watchCRPhase(ctx, "watch-gone", 20*time.Millisecond)
				close(done)
			}()

			Expect(fc.Delete(ctx, a)).To(Succeed())

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				Fail("watcher did not return after CR deletion")
			}
		})

		It("exits when the context is cancelled", func() {
			ctx, cancel := context.WithCancel(context.Background())
			s := newStubArtifactStore()
			Expect(s.Create(ctx, &store.ArtifactRecord{ID: "watch-cancel", Phase: builder.BuildBuilding})).To(Succeed())

			a := &buildv1alpha2.OSArtifact{
				ObjectMeta: metav1.ObjectMeta{Name: "watch-cancel", Namespace: "kairos-builds", Labels: map[string]string{buildIDLabel: "watch-cancel"}},
				Status:     buildv1alpha2.OSArtifactStatus{Phase: buildv1alpha2.Building},
			}
			bld, _ := newFakeBuilderWith("kairos-builds", s, nil, a)

			done := make(chan struct{})
			go func() {
				bld.watchCRPhase(ctx, "watch-cancel", 20*time.Millisecond)
				close(done)
			}()

			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				Fail("watcher did not return after context cancel")
			}
		})

		It("is a no-op when Store is nil", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bld, _ := newFakeBuilder("kairos-builds")
			// Should return immediately; assert by finishing quickly.
			done := make(chan struct{})
			go func() {
				bld.watchCRPhase(ctx, "no-store", 20*time.Millisecond)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(200 * time.Millisecond):
				Fail("watcher should short-circuit when Store is nil")
			}
		})
	})
})
