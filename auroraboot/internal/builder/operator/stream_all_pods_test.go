package operator

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// dynamicPodSource is a podSource whose visible pods can grow across polls,
// so tests can model the operator creating a builder Pod first and an
// exporter Pod several polls later. Each container's log body is a static
// string that Open returns; a nil body means "return an empty reader".
type dynamicPodSource struct {
	mu     sync.Mutex
	pods   []corev1.Pod
	bodies map[string]string // key: podName + "/" + container
}

func (d *dynamicPodSource) appendPod(p corev1.Pod) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pods = append(d.pods, p)
}

func (d *dynamicPodSource) Find(_ context.Context, _ string) (*corev1.Pod, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.pods) == 0 {
		return nil, nil
	}
	p := d.pods[0]
	return &p, nil
}

func (d *dynamicPodSource) List(_ context.Context, _ string) ([]corev1.Pod, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]corev1.Pod, len(d.pods))
	copy(out, d.pods)
	return out, nil
}

func (d *dynamicPodSource) Get(_ context.Context, podName string) (*corev1.Pod, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.pods {
		if d.pods[i].Name == podName {
			p := d.pods[i].DeepCopy()
			mkRunning := func(list []corev1.Container) []corev1.ContainerStatus {
				out := make([]corev1.ContainerStatus, len(list))
				for i, c := range list {
					out[i] = corev1.ContainerStatus{
						Name:  c.Name,
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					}
				}
				return out
			}
			p.Status.InitContainerStatuses = mkRunning(p.Spec.InitContainers)
			p.Status.ContainerStatuses = mkRunning(p.Spec.Containers)
			return p, nil
		}
	}
	return nil, nil
}

func (d *dynamicPodSource) Open(_ context.Context, podName, container string) (io.ReadCloser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	body := d.bodies[podName+"/"+container]
	return io.NopCloser(strings.NewReader(body)), nil
}

// recordingSinkGinkgo is the mutex-guarded log sink used by these Ginkgo
// specs. Kept separate from the testing.T-side recordingSink in
// logstream_test.go so the two suites stay independent.
type recordingSinkGinkgo struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordingSinkGinkgo) WriteLine(container, line string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, "["+container+"] "+line)
	return nil
}

func (r *recordingSinkGinkgo) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

func makePod(name, container, uid string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(uid)},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: container}},
		},
	}
}

var _ = Describe("streamAllArtifactPods", func() {
	It("streams the builder pod and picks up an exporter pod that appears later", func() {
		src := &dynamicPodSource{bodies: map[string]string{
			"builder-1/build":   "build line 1\nbuild line 2\n",
			"exporter-1/upload": "upload line 1\nupload done\n",
		}}
		src.appendPod(makePod("builder-1", "build", "uid-builder"))

		sink := &recordingSinkGinkgo{}
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		done := make(chan struct{})
		go func() {
			streamAllArtifactPods(ctx, src, "b", sink, 20*time.Millisecond, 1*time.Millisecond, 5)
			close(done)
		}()

		// Wait for the builder pod's lines to land, then reveal the
		// exporter Pod. The loop must discover it on its next poll.
		Eventually(sink.snapshot).Should(ContainElement("[build] build line 1"))
		Eventually(sink.snapshot).Should(ContainElement("[build] build line 2"))
		src.appendPod(makePod("exporter-1", "upload", "uid-exporter"))
		Eventually(sink.snapshot).Should(ContainElement("[upload] upload line 1"))
		Eventually(sink.snapshot).Should(ContainElement("[upload] upload done"))

		cancel()
		Eventually(done).Should(BeClosed())
	})

	It("exits promptly on ctx cancel even when no pods ever appear", func() {
		src := &dynamicPodSource{}
		sink := &recordingSinkGinkgo{}
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			streamAllArtifactPods(ctx, src, "b", sink, 20*time.Millisecond, 1*time.Millisecond, 5)
			close(done)
		}()

		cancel()
		Eventually(done, 2*time.Second).Should(BeClosed(), "cancel should unblock the loop within 2s")
	})

	It("does not restream a pod it has already picked up", func() {
		src := &dynamicPodSource{bodies: map[string]string{
			"only-1/main": "only-line\n",
		}}
		src.appendPod(makePod("only-1", "main", "uid-only"))

		sink := &recordingSinkGinkgo{}
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			streamAllArtifactPods(ctx, src, "b", sink, 5*time.Millisecond, 1*time.Millisecond, 5)
			close(done)
		}()

		Eventually(sink.snapshot).Should(ContainElement("[main] only-line"))
		// Let the loop tick a few more times to confirm no duplicate stream.
		time.Sleep(50 * time.Millisecond)
		cancel()
		Eventually(done).Should(BeClosed())

		got := sink.snapshot()
		var count int
		for _, l := range got {
			if l == "[main] only-line" {
				count++
			}
		}
		Expect(count).To(Equal(1), "expected one only-line, got %d in %v", count, got)
	})
})
