package operator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	buildv1alpha2 "github.com/kairos-io/kairos-operator/api/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// phaseWatchInterval is how often watchCRPhase re-reads the CR. Two seconds
// is fast enough that the UI notices a Pending -> Building transition without
// waiting on a page reload, but slow enough that we do not hammer the
// apiserver over the ten-plus-minute lifetime of a real build.
const phaseWatchInterval = 2 * time.Second

// ErrNotFound is returned when a build ID does not correspond to an OSArtifact
// in the target namespace. Wraps a sentinel so handlers can errors.Is-map it.
var ErrNotFound = errors.New("operator builder: build not found")

type Config struct {
	RESTConfig *rest.Config
	Namespace  string
	// Store is optional; when set, streamed pod log chunks are appended to
	// the record via AppendLog. Nil is safe (broadcast-only).
	Store store.ArtifactStore
	// AuroraBootURL is the externally-reachable URL the exporter Job's
	// curl-uploader targets to ship finished artifacts back to the
	// AuroraBoot upload endpoint. Populated from --url at runWeb time;
	// the empty string skips the exporter injection so a caller can
	// deliberately build without a return channel (e.g. tests that only
	// care about Spec.Image translation).
	AuroraBootURL string
}

type Builder struct {
	cfg            Config
	k8s            client.Client
	clientset      kubernetes.Interface
	logBroadcaster builder.LogBroadcaster

	// active tracks in-flight streaming goroutines so Cancel can stop them
	// without waiting for the pod-discovery budget to elapse.
	activeMu sync.Mutex
	active   map[string]context.CancelFunc
}

// clientFactory lets tests swap in a fake controller-runtime client; production
// callers get the default which builds a real one from cfg.RESTConfig.
type clientFactory func(cfg Config, scheme *runtime.Scheme) (client.Client, error)

// clientsetFactory lets tests swap in a fake typed clientset. Production
// callers get one built from cfg.RESTConfig.
type clientsetFactory func(cfg Config) (kubernetes.Interface, error)

var defaultClientFactory clientFactory = func(cfg Config, scheme *runtime.Scheme) (client.Client, error) {
	return client.New(cfg.RESTConfig, client.Options{Scheme: scheme})
}

var defaultClientsetFactory clientsetFactory = func(cfg Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(cfg.RESTConfig)
}

func New(cfg Config) (*Builder, error) {
	return newWithFactory(cfg, defaultClientFactory, defaultClientsetFactory)
}

func newWithFactory(cfg Config, factory clientFactory, csFactory clientsetFactory) (*Builder, error) {
	if cfg.RESTConfig == nil {
		return nil, errors.New("operator builder: RESTConfig is required")
	}
	if cfg.Namespace == "" {
		return nil, errors.New("operator builder: Namespace is required")
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("register client-go scheme: %w", err)
	}
	if err := buildv1alpha2.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("register kairos-operator v1alpha2 scheme: %w", err)
	}

	k8s, err := factory(cfg, scheme)
	if err != nil {
		return nil, fmt.Errorf("build controller-runtime client: %w", err)
	}
	cs, err := csFactory(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes clientset: %w", err)
	}
	return &Builder{
		cfg:       cfg,
		k8s:       k8s,
		clientset: cs,
		active:    make(map[string]context.CancelFunc),
	}, nil
}

// WithLogBroadcaster attaches a broadcaster that receives every log line
// produced by the build Pod. Returns the receiver so callers can chain.
func (b *Builder) WithLogBroadcaster(lb builder.LogBroadcaster) *Builder {
	b.logBroadcaster = lb
	return b
}

// Build submits an OSArtifact CR plus any inline-Secret inputs it references.
//
// AuroraBoot BuildOptions with no operator equivalent surface as
// ErrInvalidBuildOptions from translateBuildOptions (invalid arch, FIPS or
// TrustedBoot on a pre-built ref, outputs.maas, extensions). Fields that the operator cannot consume at
// all (OverlayRootfs, BuildContextDir, Signing.*, Source.Variant,
// Source.AllowInsecureRegistries, Provisioning.*, KairosInitImage) are silently
// ignored today; they belong on the local backend and should be rejected by
// the handler before reaching Build once the UI exposes the backend choice.
func (b *Builder) Build(ctx context.Context, opts builder.BuildOptions) (*builder.BuildStatus, error) {
	id := opts.ID
	if id == "" {
		id = uuid.NewString()
	}

	spec, err := translateBuildOptions(id, opts)
	if err != nil {
		return nil, err
	}

	// Inject the exporter that ships finished artifacts back to AuroraBoot
	// (via PUT /api/v1/artifacts/:id/upload/*). Only emit when we have both
	// a target URL (from Config) and a per-build token (from opts) - a
	// builder constructed without a URL (e.g. tests that only care about
	// Spec.Image translation) simply skips it, and the paired upload Secret
	// is likewise skipped by materializeSecrets.
	if b.cfg.AuroraBootURL != "" && opts.UploadToken != "" {
		spec.Exporters = append(spec.Exporters, uploadExporter(id))
	}

	art := &buildv1alpha2.OSArtifact{
		ObjectMeta: metav1.ObjectMeta{
			Name:      id,
			Namespace: b.cfg.Namespace,
			Labels:    map[string]string{buildIDLabel: id},
		},
		Spec: spec,
	}
	if err := b.k8s.Create(ctx, art); err != nil {
		return nil, fmt.Errorf("create OSArtifact %q: %w", id, err)
	}

	// Owner refs bind Secret lifetime to the CR so kubectl delete on the
	// OSArtifact cleans the Secrets up. That requires the CR to already have
	// a UID, which is why we Create it first. If any Secret Create fails,
	// the CR is left orphaned and the operator will try to reconcile it
	// against a dangling SecretKeySelector; reap the CR best-effort so the
	// caller can safely retry.
	if err := b.createSecrets(ctx, art, id, opts); err != nil {
		if delErr := b.k8s.Delete(context.Background(), art); delErr != nil && !apierrors.IsNotFound(delErr) {
			fmt.Fprintf(os.Stderr, "operator builder: orphan-CR cleanup for %q failed: %v\n", id, delErr)
		}
		return nil, err
	}

	// Detach from the request context: the HTTP handler's ctx is cancelled
	// once the response returns, but the build (and its log stream) outlives
	// that. Cancel is wired via b.active so the streaming goroutine still
	// stops promptly when the user cancels the build.
	streamCtx, cancel := context.WithCancel(context.Background())
	b.activeMu.Lock()
	b.active[id] = cancel
	b.activeMu.Unlock()
	go b.streamPodLogs(streamCtx, id, opts.LogRedactValues)
	go b.watchCRPhase(streamCtx, id, phaseWatchInterval)

	return &builder.BuildStatus{ID: id, Phase: builder.BuildPending}, nil
}

// createSecrets materializes and Creates every inline Secret the CR references,
// setting the owner reference to the (already-Created) OSArtifact so the
// operator's GC reclaims them when the CR is deleted.
func (b *Builder) createSecrets(ctx context.Context, art *buildv1alpha2.OSArtifact, id string, opts builder.BuildOptions) error {
	for _, secret := range materializeSecrets(id, b.cfg.Namespace, b.cfg.AuroraBootURL, opts) {
		secret := secret
		if err := controllerutil.SetOwnerReference(art, &secret, b.k8s.Scheme()); err != nil {
			return fmt.Errorf("set owner on secret %q: %w", secret.Name, err)
		}
		if err := b.k8s.Create(ctx, &secret); err != nil {
			return fmt.Errorf("create secret %q: %w", secret.Name, err)
		}
	}
	return nil
}

// streamPodLogs waits for the operator to create the build Pod and then
// streams every container's log into the persistent store and any attached
// LogBroadcaster. It exits when the pod completes, ctx is cancelled, or the
// pod-discovery budget expires.
func (b *Builder) streamPodLogs(ctx context.Context, id string, redactValues []string) {
	defer func() {
		b.activeMu.Lock()
		delete(b.active, id)
		b.activeMu.Unlock()
	}()
	if b.clientset == nil {
		return
	}

	sink := &broadcastingSink{
		ctx:          context.Background(),
		buildID:      id,
		store:        b.cfg.Store,
		broadcaster:  b.logBroadcaster,
		redactValues: redactValues,
	}
	src := newClientsetPodSource(b.clientset, b.cfg.Namespace)

	// streamAllArtifactPods loops until ctx is cancelled. That happens
	// either when Cancel(id) invalidates the build or when watchCRPhase
	// sees a terminal CR phase (Ready/Error/NotFound) and calls the shared
	// cancel func. The loop picks up the exporter Pod created after the
	// builder Pod terminates, so its curl-uploader logs make it to the
	// UI's live-log pane alongside the buildah/auroraboot output.
	streamAllArtifactPods(ctx, src, id, sink,
		podDiscoveryPollInterval,
		containerStartRetryInterval, containerStartMaxRetries)
}

func (b *Builder) Status(ctx context.Context, id string) (*builder.BuildStatus, error) {
	art := &buildv1alpha2.OSArtifact{}
	err := b.k8s.Get(ctx, types.NamespacedName{Name: id, Namespace: b.cfg.Namespace}, art)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
		}
		return nil, fmt.Errorf("get OSArtifact %q: %w", id, err)
	}
	return statusFromArtifact(art), nil
}

func (b *Builder) List(ctx context.Context) ([]*builder.BuildStatus, error) {
	sel, err := labels.Parse(buildIDLabel)
	if err != nil {
		return nil, fmt.Errorf("build label selector: %w", err)
	}
	list := &buildv1alpha2.OSArtifactList{}
	if err := b.k8s.List(ctx, list,
		client.InNamespace(b.cfg.Namespace),
		client.MatchingLabelsSelector{Selector: sel},
	); err != nil {
		return nil, fmt.Errorf("list OSArtifacts: %w", err)
	}
	out := make([]*builder.BuildStatus, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, statusFromArtifact(&list.Items[i]))
	}
	return out, nil
}

func (b *Builder) Cancel(ctx context.Context, id string) error {
	// Cancel the streaming goroutine before deleting the CR so it stops
	// polling / streaming instead of racing against the operator's own GC.
	b.activeMu.Lock()
	if cancel, ok := b.active[id]; ok {
		cancel()
		delete(b.active, id)
	}
	b.activeMu.Unlock()

	art := &buildv1alpha2.OSArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: b.cfg.Namespace},
	}
	if err := b.k8s.Delete(ctx, art); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete OSArtifact %q: %w", id, err)
	}

	// The CR is gone; the phase-watch goroutine will exit on its next poll,
	// but until it does the store row still shows the last observed phase
	// (typically Building). Mark it BuildError/cancelled proactively so the
	// UI never returns a stale "still running" record after Cancel resolves.
	// Match the local backend's exact wording so the two are indistinguishable
	// from the operator's perspective.
	if b.cfg.Store != nil {
		if upErr := b.cfg.Store.UpdatePhaseMessage(ctx, id, builder.BuildError, "cancelled"); upErr != nil {
			fmt.Fprintf(os.Stderr, "operator builder: cancel store update for %q failed: %v\n", id, upErr)
		}
	}
	return nil
}

// watchCRPhase polls the OSArtifact CR at pollInterval and writes every
// observed phase transition (and message change) into the ArtifactStore so
// the HTTP layer, which reads from the store, can surface progress to the
// UI. It exits when the CR reaches a terminal phase (Ready or Error), when
// the CR is deleted, or when ctx is cancelled. A nil Store short-circuits
// so unit tests that omit the store still work.
func (b *Builder) watchCRPhase(ctx context.Context, id string, pollInterval time.Duration) {
	if b.cfg.Store == nil {
		return
	}
	// cancelShared cancels the streamCtx used by both goroutines Build
	// spawns (this one and streamPodLogs). We call it whenever the CR
	// reaches a terminal state or disappears, so streamPodLogs unblocks
	// from its polling loop instead of running forever chasing pods that
	// no longer exist. Cancel(id) still hits the same func via a direct
	// b.active lookup.
	cancelShared := func() {
		b.activeMu.Lock()
		if cancel, ok := b.active[id]; ok {
			cancel()
		}
		b.activeMu.Unlock()
	}
	key := types.NamespacedName{Name: id, Namespace: b.cfg.Namespace}
	var lastPhase, lastMessage string
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		art := &buildv1alpha2.OSArtifact{}
		err := b.k8s.Get(ctx, key, art)
		if apierrors.IsNotFound(err) {
			cancelShared()
			return
		}
		if err == nil {
			st := statusFromArtifact(art)
			if st.Phase != lastPhase || st.Message != lastMessage {
				if upErr := b.cfg.Store.UpdatePhaseMessage(ctx, id, st.Phase, st.Message); upErr != nil {
					fmt.Fprintf(os.Stderr, "operator builder: phase-watch update for %q failed: %v\n", id, upErr)
				}
				lastPhase = st.Phase
				lastMessage = st.Message
			}
			if st.Phase == builder.BuildReady || st.Phase == builder.BuildError {
				// The exporter has either completed (Ready) or given up (Error).
				// Invalidate the per-build upload token so a leaked credential
				// cannot be replayed to overwrite finished artifacts. Upload also
				// phase-gates rejects post-terminal PUTs; clearing here closes
				// the window where a stale in-flight request slips through
				// between the last legit exporter PUT and the phase transition.
				if clearErr := b.cfg.Store.ClearUploadToken(ctx, id); clearErr != nil {
					fmt.Fprintf(os.Stderr, "operator builder: clear upload token for %q failed: %v\n", id, clearErr)
				}
				cancelShared()
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// statusFromArtifact folds the operator's five-phase model into AuroraBoot's
// four-phase model (Exporting collapses into Building), and defaults an empty
// phase (fresh CR the controller has not seen) to Pending. Artifacts is left
// empty; retrieval from the operator's PVC is tracked in plan section 3.3.
func statusFromArtifact(art *buildv1alpha2.OSArtifact) *builder.BuildStatus {
	phase := builder.BuildPending
	switch art.Status.Phase {
	case "", buildv1alpha2.Pending:
		phase = builder.BuildPending
	case buildv1alpha2.Building, buildv1alpha2.Exporting:
		phase = builder.BuildBuilding
	case buildv1alpha2.Ready:
		phase = builder.BuildReady
	case buildv1alpha2.Error:
		phase = builder.BuildError
	}
	return &builder.BuildStatus{
		ID:      art.Name,
		Phase:   phase,
		Message: art.Status.Message,
	}
}

var _ builder.ArtifactBuilder = (*Builder)(nil)
