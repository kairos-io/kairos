package operator

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

const (
	// podDiscoveryPollInterval is how often streamAllArtifactPods re-lists
	// pods labelled with the build id, so a Pod (builder or exporter) that
	// appears after the previous pass gets picked up on the next tick.
	// Termination is driven by watchCRPhase cancelling the shared streamCtx
	// on the terminal transition.
	podDiscoveryPollInterval = 2 * time.Second

	// containerStartRetryInterval is the backoff between attempts when a
	// container's log endpoint reports the container is still waiting to
	// start. The default budget lets a container take up to ~10 minutes to
	// start before we bail (300 * 2s = 600s).
	containerStartRetryInterval = 2 * time.Second
	containerStartMaxRetries    = 300

	// scanLineBufferMax bounds bufio.Scanner's per-line buffer. Buildah and
	// auroraboot logs rarely exceed a few KB but a stray progress bar or
	// noisy tool can emit a very long line; 1MiB is well above anything
	// we've observed and keeps memory bounded.
	scanLineBufferMax = 1 << 20
)

// logSink receives one log line at a time from the streaming pipeline. The
// production implementation fans lines out to the persistent store and any
// subscribed LogBroadcaster; tests use an in-memory recorder.
type logSink interface {
	WriteLine(container, line string) error
}

// podSource abstracts pod discovery and per-container log streaming so tests
// can inject a fake and the production wiring can use a client-go clientset.
type podSource interface {
	// Find returns the first pod matching the build label, or nil if none
	// exist yet. Kept for callers that only care about the builder Pod.
	Find(ctx context.Context, buildID string) (*corev1.Pod, error)
	// List returns every pod matching the build label. The operator creates
	// a single builder Pod plus one exporter Pod per exporter Job (with
	// backoffLimit retries producing additional Pods), so callers that want
	// to stream logs from all of them need this rather than Find.
	List(ctx context.Context, buildID string) ([]corev1.Pod, error)
	Get(ctx context.Context, podName string) (*corev1.Pod, error)
	Open(ctx context.Context, podName, container string) (io.ReadCloser, error)
}

type clientsetPodSource struct {
	cs        kubernetes.Interface
	namespace string
}

func newClientsetPodSource(cs kubernetes.Interface, namespace string) podSource {
	return &clientsetPodSource{cs: cs, namespace: namespace}
}

func (c *clientsetPodSource) Find(ctx context.Context, buildID string) (*corev1.Pod, error) {
	items, err := c.List(ctx, buildID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return &items[0], nil
}

func (c *clientsetPodSource) List(ctx context.Context, buildID string) ([]corev1.Pod, error) {
	// Two-hop discovery. The operator labels the builder Pod directly
	// with the build id, but the CRD schema for
	// spec.exporters[].template.metadata drops labels via OpenAPI pruning,
	// so exporter Pods only carry the Job controller's default job-name
	// label. To catch both, we list Pods by the build label (builder) and
	// separately list Jobs by the build label - which the operator DOES
	// label - then list each Job's Pods by job-name (exporters).
	direct, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: podBuildLabel + "=" + buildID,
	})
	if err != nil {
		return nil, err
	}
	result := append([]corev1.Pod(nil), direct.Items...)

	jobs, err := c.cs.BatchV1().Jobs(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: podBuildLabel + "=" + buildID,
	})
	if err != nil {
		return nil, err
	}
	for _, j := range jobs.Items {
		jobPods, err := c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "job-name=" + j.Name,
		})
		if err != nil {
			return nil, err
		}
		result = append(result, jobPods.Items...)
	}
	return result, nil
}

func (c *clientsetPodSource) Get(ctx context.Context, podName string) (*corev1.Pod, error) {
	return c.cs.CoreV1().Pods(c.namespace).Get(ctx, podName, metav1.GetOptions{})
}

func (c *clientsetPodSource) Open(ctx context.Context, podName, container string) (io.ReadCloser, error) {
	return c.cs.CoreV1().Pods(c.namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: container,
		Follow:    true,
	}).Stream(ctx)
}

// podBuildLabel matches the operator's builder-pod label
// (kairos-operator/internal/controller/osartifact_controller.go: artifactLabel).
const podBuildLabel = "build.kairos.io/artifact"

// streamContainer streams one container's logs into sink. It first polls the
// pod until the container leaves Waiting state — the log API returns an empty
// body without error for a Waiting container in some kubelet versions, so
// merely retrying Open on that response would deliver zero lines. Once the
// container is Running or Terminated, it opens the log stream and scans lines
// until EOF, sink returns an error, or ctx is cancelled. Open errors that
// indicate the container is still starting are retried up to maxRetries times
// with retryInterval between attempts.
func streamContainer(ctx context.Context, src podSource, podName, container string, sink logSink, retryInterval time.Duration, maxRetries int) error {
	if err := waitContainerLeftWaiting(ctx, src, podName, container, retryInterval, maxRetries); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc, err := src.Open(ctx, podName, container)
		if err == nil {
			return scanLines(ctx, rc, container, sink)
		}
		if !isContainerNotReadyErr(err) {
			return err
		}
		if attempt >= maxRetries {
			return fmt.Errorf("container %q never became ready after %d attempts: %w", container, attempt, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
		}
	}
}

// waitContainerLeftWaiting polls the pod until the named container is either
// Running or Terminated. Returns ctx.Err() on cancellation, a NotFound error
// if the pod disappears, or a deadline error if maxRetries is exhausted.
func waitContainerLeftWaiting(ctx context.Context, src podSource, podName, container string, pollInterval time.Duration, maxRetries int) error {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		pod, err := src.Get(ctx, podName)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return err
			}
			// Transient API error; retry on the next tick rather than fail.
		} else if containerHasStarted(pod, container) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return fmt.Errorf("container %q never left Waiting state after %d polls", container, maxRetries)
}

// containerHasStarted returns true when the named container's status shows it
// is Running or Terminated. Waiting (image pull, init dependency) returns
// false — the log API produces an empty stream in that state.
func containerHasStarted(pod *corev1.Pod, name string) bool {
	check := func(list []corev1.ContainerStatus) bool {
		for _, s := range list {
			if s.Name != name {
				continue
			}
			return s.State.Running != nil || s.State.Terminated != nil
		}
		return false
	}
	return check(pod.Status.InitContainerStatuses) || check(pod.Status.ContainerStatuses)
}

// isContainerNotReadyErr recognises the transient errors kube-apiserver
// returns when a container has not started yet. The exact message varies
// across kubelet versions ("waiting to start", "PodInitializing",
// "ContainerCreating"), so we match on substrings and treat everything else
// as fatal to avoid retry loops that mask real bugs.
func isContainerNotReadyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "waiting to start") ||
		strings.Contains(msg, "ContainerCreating") ||
		strings.Contains(msg, "PodInitializing") {
		return true
	}
	return false
}

// scanLines reads rc line-by-line and hands each raw (unprefixed) line to
// sink.WriteLine along with the container name. It closes rc on return and
// exits promptly on ctx cancellation.
func scanLines(ctx context.Context, rc io.ReadCloser, container string, sink logSink) error {
	defer rc.Close()
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 0, 4096), scanLineBufferMax)
	var lines int
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "operator builder: scanLines exiting for container %q at line %d due to ctx: %v\n", container, lines, err)
			return err
		}
		lines++
		if err := sink.WriteLine(container, scanner.Text()); err != nil {
			return err
		}
	}
	scanErr := scanner.Err()
	fmt.Fprintf(os.Stderr, "operator builder: scanLines closed for container %q after %d lines (err=%v)\n", container, lines, scanErr)
	if scanErr != nil && !errors.Is(scanErr, context.Canceled) {
		return scanErr
	}
	return nil
}

// streamAllArtifactPods discovers every pod carrying the build-label for
// buildID (builder Pod, exporter Pod(s), any retries) and streams each one's
// logs through sink. It loops until ctx is cancelled, so pods that come
// online after the builder finishes (notably the exporter Job created when
// the operator transitions the CR into Exporting) also get their logs
// forwarded to the UI. Callers cancel ctx from watchCRPhase when the CR
// reaches a terminal phase, or from Cancel(id) when the admin bails out.
func streamAllArtifactPods(ctx context.Context, src podSource, buildID string, sink logSink, pollInterval, retryInterval time.Duration, maxRetries int) {
	seen := map[types.UID]struct{}{}
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		pods, err := src.List(ctx, buildID)
		if err != nil && !apierrors.IsNotFound(err) {
			// Transient discovery error - log and keep polling. A persistent
			// failure surfaces as "no pods ever streamed" from the caller's
			// perspective, which is preferable to killing log streaming on
			// the first blip.
			fmt.Fprintf(os.Stderr, "streamAllArtifactPods: transient list error for build %q: %v\n", buildID, err)
		}
		for i := range pods {
			pod := pods[i]
			if _, ok := seen[pod.UID]; ok {
				continue
			}
			seen[pod.UID] = struct{}{}
			wg.Add(1)
			go func(pod corev1.Pod) {
				defer wg.Done()
				if err := streamAll(ctx, src, &pod, sink, retryInterval, maxRetries); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					_ = sink.WriteLine("auroraboot", fmt.Sprintf("warning: log streaming for pod %q ended: %v", pod.Name, err))
				}
			}(pod)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// streamAll drives every container in pod: init containers sequentially in
// declaration order (they run in sequence), then all main containers in
// parallel (they run in parallel). It returns the first non-cancellation
// error encountered.
func streamAll(ctx context.Context, src podSource, pod *corev1.Pod, sink logSink, retryInterval time.Duration, maxRetries int) error {
	for _, c := range pod.Spec.InitContainers {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := streamContainer(ctx, src, pod.Name, c.Name, sink, retryInterval, maxRetries); err != nil {
			return err
		}
	}
	if len(pod.Spec.Containers) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		c := c
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := streamContainer(ctx, src, pod.Name, c.Name, sink, retryInterval, maxRetries); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}
	return nil
}

// broadcastingSink fans each line out to the artifact store (if configured)
// and the LogBroadcaster (if configured). Lines land in the store as
// "[<container>] <line>\n" so a post-mortem reader can tell which stage of
// the buildah/auroraboot pipeline produced them; broadcasts carry the same
// prefixed text so subscribers render matching output. Every line runs
// through builder.RedactLine against redactValues before persist/broadcast
// so a build step that echoes the injected cloud-config cannot land the
// fleet registration token or the default node password in either sink.
type broadcastingSink struct {
	ctx          context.Context
	buildID      string
	store        store.ArtifactStore
	broadcaster  builder.LogBroadcaster
	redactValues []string
}

func (b *broadcastingSink) WriteLine(container, line string) error {
	line = builder.RedactLine(line, b.redactValues)
	chunk := "[" + container + "] " + line + "\n"
	if b.store != nil {
		if err := b.store.AppendLog(b.ctx, b.buildID, chunk); err != nil {
			fmt.Fprintf(os.Stderr, "operator builder: AppendLog for build %q container %q failed: %v (line dropped: %q)\n", b.buildID, container, err, line)
		}
	}
	if b.broadcaster != nil {
		b.broadcaster.BroadcastLogChunk(b.buildID, chunk)
	}
	return nil
}
