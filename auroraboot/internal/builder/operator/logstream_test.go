package operator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktest "k8s.io/client-go/testing"
)

func buildPodFixture(id string, initContainers, mainContainers []string) *corev1.Pod {
	toContainers := func(names []string) []corev1.Container {
		out := make([]corev1.Container, len(names))
		for i, n := range names {
			out[i] = corev1.Container{Name: n}
		}
		return out
	}
	// Every container is reported as Running so streamContainer's
	// containerHasStarted check clears immediately. Individual tests can
	// override this before handing the pod to the fake clientset.
	statuses := func(names []string) []corev1.ContainerStatus {
		out := make([]corev1.ContainerStatus, len(names))
		for i, n := range names {
			out[i] = corev1.ContainerStatus{
				Name:  n,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}
		}
		return out
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      id + "-builder",
			Namespace: "kairos-builds",
			Labels:    map[string]string{"build.kairos.io/artifact": id},
		},
		Spec: corev1.PodSpec{
			InitContainers: toContainers(initContainers),
			Containers:     toContainers(mainContainers),
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: statuses(initContainers),
			ContainerStatuses:     statuses(mainContainers),
		},
	}
}

// recordingSink captures WriteLine calls in-order for assertions.
type recordingSink struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordingSink) WriteLine(container, line string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, fmt.Sprintf("[%s] %s", container, line))
	return nil
}

func (r *recordingSink) Snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// installLogReactor makes the fake clientset return per-container bodies
// (or errors) for GetLogs, keyed by attempt number. containerBodies maps a
// container name to the sequence of responses to serve on successive calls;
// after the sequence is exhausted the final response is repeated.
type logResponse struct {
	body string
	err  error
}

func installLogReactor(cs *fake.Clientset, containerBodies map[string][]logResponse) {
	attempts := make(map[string]int)
	var mu sync.Mutex

	cs.PrependReactor("get", "pods", func(action ktest.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "log" {
			return false, nil, nil
		}
		ga, ok := action.(ktest.GenericAction)
		if !ok {
			return false, nil, nil
		}
		opts, _ := ga.GetValue().(*corev1.PodLogOptions)
		container := ""
		if opts != nil {
			container = opts.Container
		}

		mu.Lock()
		seq, hasSeq := containerBodies[container]
		idx := attempts[container]
		if hasSeq && idx < len(seq) {
			attempts[container] = idx + 1
		}
		mu.Unlock()

		if !hasSeq || len(seq) == 0 {
			return true, &runtime.Unknown{Raw: []byte{}}, nil
		}
		if idx >= len(seq) {
			idx = len(seq) - 1
		}
		resp := seq[idx]
		if resp.err != nil {
			return true, nil, resp.err
		}
		return true, &runtime.Unknown{Raw: []byte(resp.body)}, nil
	})
}

func TestStreamContainer_RetriesWhenWaiting(t *testing.T) {
	pod := buildPodFixture("b2", nil, []string{"main-a"})
	cs := fake.NewSimpleClientset(pod)
	installLogReactor(cs, map[string][]logResponse{
		"main-a": {
			{err: errors.New("container \"main-a\" in pod \"b2-builder\" is waiting to start: ContainerCreating")},
			{err: errors.New("container \"main-a\" in pod \"b2-builder\" is waiting to start: PodInitializing")},
			{body: "hello from main-a\ngoodbye from main-a\n"},
		},
	})
	src := newClientsetPodSource(cs, "kairos-builds")

	sink := &recordingSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := streamContainer(ctx, src, pod.Name, "main-a", sink, 20*time.Millisecond, 20)
	if err != nil {
		t.Fatalf("streamContainer: %v", err)
	}
	lines := sink.Snapshot()
	if len(lines) != 2 || lines[0] != "[main-a] hello from main-a" || lines[1] != "[main-a] goodbye from main-a" {
		t.Fatalf("unexpected sink lines: %#v", lines)
	}
}

func TestStreamAll_SequentialInitThenParallelMain(t *testing.T) {
	pod := buildPodFixture("b3", []string{"init-1", "init-2"}, []string{"main-1", "main-2"})
	cs := fake.NewSimpleClientset(pod)
	installLogReactor(cs, map[string][]logResponse{
		"init-1": {{body: "init-1-line\n"}},
		"init-2": {{body: "init-2-line\n"}},
		"main-1": {{body: "main-1-line\n"}},
		"main-2": {{body: "main-2-line\n"}},
	})
	src := newClientsetPodSource(cs, "kairos-builds")

	sink := &recordingSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := streamAll(ctx, src, pod, sink, 20*time.Millisecond, 5)
	if err != nil {
		t.Fatalf("streamAll: %v", err)
	}

	lines := sink.Snapshot()
	// The two init containers must appear before either main container, and
	// init-1 must appear before init-2 (sequential ordering).
	posInit1, posInit2, posMain1, posMain2 := -1, -1, -1, -1
	for i, l := range lines {
		switch l {
		case "[init-1] init-1-line":
			posInit1 = i
		case "[init-2] init-2-line":
			posInit2 = i
		case "[main-1] main-1-line":
			posMain1 = i
		case "[main-2] main-2-line":
			posMain2 = i
		}
	}
	if posInit1 == -1 || posInit2 == -1 || posMain1 == -1 || posMain2 == -1 {
		t.Fatalf("missing expected lines: %#v", lines)
	}
	if !(posInit1 < posInit2 && posInit2 < posMain1 && posInit2 < posMain2) {
		t.Fatalf("wrong ordering (init-1=%d init-2=%d main-1=%d main-2=%d): %#v",
			posInit1, posInit2, posMain1, posMain2, lines)
	}
}

func TestStreamContainer_ContextCancellationIsPrompt(t *testing.T) {
	pod := buildPodFixture("b4", nil, []string{"main-slow"})
	cs := fake.NewSimpleClientset(pod)
	// Reactor keeps returning "waiting" forever; only cancellation ends the loop.
	installLogReactor(cs, map[string][]logResponse{
		"main-slow": {{err: errors.New("container \"main-slow\" in pod \"b4-builder\" is waiting to start")}},
	})
	src := newClientsetPodSource(cs, "kairos-builds")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- streamContainer(ctx, src, pod.Name, "main-slow", &recordingSink{}, 100*time.Millisecond, 1_000)
	}()

	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("streamContainer took %s to return after cancel; want < 1s (err=%v)", elapsed, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streamContainer did not return within 2s of cancel")
	}
}

// fakePodSource lets us script both Get and Open sequences precisely,
// including the "container still Waiting" state that the fake clientset's
// tracker cannot represent as easily.
type fakePodSource struct {
	mu           sync.Mutex
	pod          *corev1.Pod
	getCalls     int
	openCalls    map[string]int
	runningAfter map[string]int // container -> Get call at which state flips to Running
	openBodies   map[string]string
}

func newFakePodSource(pod *corev1.Pod) *fakePodSource {
	return &fakePodSource{
		pod:          pod,
		openCalls:    map[string]int{},
		runningAfter: map[string]int{},
		openBodies:   map[string]string{},
	}
}

func (f *fakePodSource) Find(_ context.Context, _ string) (*corev1.Pod, error) {
	return f.pod, nil
}

func (f *fakePodSource) List(_ context.Context, _ string) ([]corev1.Pod, error) {
	if f.pod == nil {
		return nil, nil
	}
	return []corev1.Pod{*f.pod}, nil
}

func (f *fakePodSource) Get(_ context.Context, _ string) (*corev1.Pod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	// Copy the pod so mutations to status stay local to this Get.
	p := f.pod.DeepCopy()
	setState := func(list []corev1.ContainerStatus, cascade map[string]int, currentCall int) []corev1.ContainerStatus {
		out := make([]corev1.ContainerStatus, len(list))
		for i, s := range list {
			threshold, ok := cascade[s.Name]
			if ok && currentCall < threshold {
				out[i] = corev1.ContainerStatus{
					Name:  s.Name,
					State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}},
				}
			} else {
				out[i] = corev1.ContainerStatus{
					Name:  s.Name,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				}
			}
		}
		return out
	}
	p.Status.InitContainerStatuses = setState(f.pod.Status.InitContainerStatuses, f.runningAfter, f.getCalls)
	p.Status.ContainerStatuses = setState(f.pod.Status.ContainerStatuses, f.runningAfter, f.getCalls)
	return p, nil
}

func (f *fakePodSource) Open(_ context.Context, _ string, container string) (io.ReadCloser, error) {
	f.mu.Lock()
	f.openCalls[container]++
	body := f.openBodies[container]
	f.mu.Unlock()
	return io.NopCloser(strings.NewReader(body)), nil
}

func TestStreamContainer_WaitsForContainerToLeaveWaitingState(t *testing.T) {
	pod := buildPodFixture("b5", nil, []string{"main-slow"})
	// Pod's status starts fully Running (from fixture); scripting overrides.
	src := newFakePodSource(pod)
	// main-slow stays Waiting until the 3rd Get call, then flips to Running.
	src.runningAfter["main-slow"] = 3
	src.openBodies["main-slow"] = "main-slow line 1\nmain-slow line 2\n"

	sink := &recordingSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := streamContainer(ctx, src, pod.Name, "main-slow", sink, 20*time.Millisecond, 100)
	if err != nil {
		t.Fatalf("streamContainer: %v", err)
	}
	if src.openCalls["main-slow"] != 1 {
		t.Fatalf("expected exactly one Open call after container became Running, got %d", src.openCalls["main-slow"])
	}
	if src.getCalls < 3 {
		t.Fatalf("expected at least 3 Get calls before Open, got %d", src.getCalls)
	}
	lines := sink.Snapshot()
	if len(lines) != 2 || lines[0] != "[main-slow] main-slow line 1" {
		t.Fatalf("unexpected sink lines: %#v", lines)
	}
}

func TestScanLines_HandlesLongLines(t *testing.T) {
	long := strings.Repeat("A", 128*1024)
	sink := &recordingSink{}
	rc := io.NopCloser(strings.NewReader(long + "\n"))
	if err := scanLines(context.Background(), rc, "big", sink); err != nil {
		t.Fatalf("scanLines: %v", err)
	}
	got := sink.Snapshot()
	if len(got) != 1 || got[0] != "[big] "+long {
		t.Fatalf("scanLines dropped a long line; len=%d first-100=%q", len(got), func() string {
			if len(got) == 0 {
				return ""
			}
			if len(got[0]) < 100 {
				return got[0]
			}
			return got[0][:100]
		}())
	}
}
