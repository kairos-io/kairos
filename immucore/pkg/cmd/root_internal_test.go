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

// TestSignalDuringDAGFiresOnSignalBeforeDAGCompletes reproduces
// kairos-io/kairos#4813: root.go arms watchForTermination before g.Run, and
// the watch's onSignal callback (production: haltTerminated, which paints
// "KAIROS BOOT FAILED" and mutes the console) fires the instant a signal
// arrives, with no check of whether the mount DAG actually finished. A
// Conflicts= unit (initrd-cleanup.service against initrd-switch-root.target)
// can deliver that signal mid-DAG even though the DAG goes on to complete
// successfully, so a healthy boot gets painted as a hard failure.
//
// This test builds a real herd DAG with one slow step, arms the watch the
// same way root.go does, raises SIGTERM partway through the step, and
// asserts that onSignal already ran -- unconditionally -- while the DAG step
// was still in flight. This is the bug: today there is no coordination
// between the signal watch and DAG completion. Once root.go is fixed to only
// halt when the DAG truly has not finished, this assertion should no longer
// hold and the test must be flipped to assert the corrected behaviour.
func TestSignalDuringDAGFiresOnSignalBeforeDAGCompletes(t *testing.T) {
	const stepDuration = 300 * time.Millisecond

	dagFinished := make(chan struct{})
	onSignalFired := make(chan struct{})

	g := herd.DAG()
	err := g.Add("slow-mount-step", herd.WithCallback(func(_ context.Context) error {
		time.Sleep(stepDuration)
		close(dagFinished)
		return nil
	}))
	if err != nil {
		t.Fatalf("adding DAG step: %v", err)
	}

	// Arm the watch before starting the DAG, exactly as root.go's
	// NewApp Action does at lines 112/115.
	stop := watchForTermination(true, func(os.Signal) { close(onSignalFired) })
	defer stop()

	runErr := make(chan error, 1)
	go func() { runErr <- g.Run(context.Background()) }()

	// Signal partway through the slow step: the DAG is demonstrably still
	// in flight, matching initrd-cleanup.service isolating
	// initrd-switch-root.target mid-mount on a real boot.
	time.Sleep(stepDuration / 3)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Skipf("cannot signal self: %v", err)
	}

	select {
	case <-onSignalFired:
		// Bug reproduced: onSignal already ran.
	case <-time.After(2 * time.Second):
		t.Fatal("onSignal was never invoked after SIGTERM")
	}

	select {
	case <-dagFinished:
		t.Fatal("DAG had already finished by the time onSignal fired; this no longer reproduces the race")
	default:
		// This is the bug: onSignal fired while the DAG step was still
		// running, with no check of DAG completion state.
	}

	<-runErr
}
