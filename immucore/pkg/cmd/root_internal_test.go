package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/spectrocloud-labs/herd"
)

// waitFor gives the watch goroutine a bounded window to report a signal.
func waitFor(t *testing.T, seen <-chan os.Signal) os.Signal {
	t.Helper()
	select {
	case sig := <-seen:
		return sig
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not report the signal")
		return nil
	}
}

func TestWatchSignalsReportsTheFirstSignal(t *testing.T) {
	ch := make(chan os.Signal, 1)
	seen := make(chan os.Signal, 1)

	stop := watchSignals(ch, 0, func(sig os.Signal) { seen <- sig })
	defer stop()

	ch <- syscall.SIGTERM
	if got := waitFor(t, seen); got != syscall.SIGTERM {
		t.Fatalf("reported %v, want SIGTERM", got)
	}
}

func TestWatchSignalsIgnoresSignalsAfterStop(t *testing.T) {
	ch := make(chan os.Signal, 1)
	seen := make(chan os.Signal, 1)

	stop := watchSignals(ch, 0, func(sig os.Signal) { seen <- sig })
	stop()

	// Give the goroutine time to observe the stop before the signal lands.
	time.Sleep(50 * time.Millisecond)
	ch <- syscall.SIGTERM

	select {
	case sig := <-seen:
		t.Fatalf("reported %v after the watch was stopped", sig)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestWatchSignalsStopIsIdempotent(t *testing.T) {
	stop := watchSignals(make(chan os.Signal), 0, func(os.Signal) {})
	stop()
	stop()
}

// A live-media or UKI boot must not arm the watch: there is no mid-mount
// interruption to protect against, and the returned stop must stay safe to
// call.
func TestWatchForTerminationIsInertOutsideANormalBoot(t *testing.T) {
	seen := make(chan os.Signal, 1)

	stop := watchForTermination(false, func(sig os.Signal) { seen <- sig })
	stop()
	stop()

	select {
	case sig := <-seen:
		t.Fatalf("reported %v on a boot that is not guarded", sig)
	default:
	}
}

// shortenGrace makes a signalled watch give up on the DAG almost at once, for
// tests that only care that the signal reaches onSignal.
func shortenGrace(t *testing.T) {
	t.Helper()
	prev := terminationGrace
	terminationGrace = 10 * time.Millisecond
	t.Cleanup(func() { terminationGrace = prev })
}

func TestWatchForTerminationInterceptsSigterm(t *testing.T) {
	shortenGrace(t)
	seen := make(chan os.Signal, 1)

	stop := watchForTermination(true, func(sig os.Signal) { seen <- sig })
	defer stop()

	// The watch registers for SIGTERM, so raising it is delivered to the watch
	// instead of terminating this process. A test that reaches the timeout has
	// therefore either lost the registration or been killed outright.
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Skipf("cannot signal self: %v", err)
	}

	if got := waitFor(t, seen); got != syscall.SIGTERM {
		t.Fatalf("reported %v, want SIGTERM", got)
	}
}

func TestWatchForTerminationStopsInterceptingAfterStop(t *testing.T) {
	seen := make(chan os.Signal, 1)

	stop := watchForTermination(true, func(sig os.Signal) { seen <- sig })
	stop()

	// Re-register so raising SIGTERM below cannot kill the test process, then
	// assert the stopped watch is the one that stayed quiet.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Skipf("cannot signal self: %v", err)
	}
	<-guard

	select {
	case sig := <-seen:
		t.Fatalf("reported %v after the watch was stopped", sig)
	default:
	}
}

// A Conflicts= stop job (initrd-cleanup.service isolating
// initrd-switch-root.target) can signal immucore while the mount DAG is still
// running. When the DAG goes on to finish within the grace, onSignal must not
// run: that path paints "KAIROS BOOT FAILED" and mutes the console on a boot
// that is healthy.
func TestSignalDuringDAGDoesNotHaltWhenDAGCompletes(t *testing.T) {
	const stepDuration = 300 * time.Millisecond

	prev := terminationGrace
	terminationGrace = time.Second
	t.Cleanup(func() { terminationGrace = prev })

	dagFinished := make(chan struct{})
	onSignalFired := make(chan struct{})

	g := herd.DAG(herd.EnableInit)
	err := g.Add("slow-mount-step", herd.WithCallback(func(_ context.Context) error {
		time.Sleep(stepDuration)
		close(dagFinished)
		return nil
	}))
	if err != nil {
		t.Fatalf("adding DAG step: %v", err)
	}

	stop := watchForTermination(true, func(os.Signal) { close(onSignalFired) })
	defer stop()

	runErr := make(chan error, 1)
	go func() { runErr <- g.Run(context.Background()) }()

	time.Sleep(stepDuration / 3)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Skipf("cannot signal self: %v", err)
	}

	select {
	case <-onSignalFired:
		t.Fatal("onSignal ran while the DAG was still in flight")
	case <-dagFinished:
	}

	if err := <-runErr; err != nil {
		t.Fatalf("running DAG: %v", err)
	}
	stop()

	// Wait past the grace: a stopped watch must not fire once its timer
	// would have expired.
	select {
	case <-onSignalFired:
		t.Fatal("onSignal ran after the DAG completed")
	case <-time.After(terminationGrace + 200*time.Millisecond):
	}
}
