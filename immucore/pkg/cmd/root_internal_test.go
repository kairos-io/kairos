package cmd

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// testGrace is the grace the tests hand to watchSignals in place of
// terminationGrace, short enough to keep the suite fast.
const testGrace = 50 * time.Millisecond

// neverDone stands in for a mount DAG that is still running.
func neverDone() <-chan struct{} { return make(chan struct{}) }

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

	stop := watchSignals(ch, neverDone(), testGrace, func(sig os.Signal) { seen <- sig })
	defer stop()

	ch <- syscall.SIGTERM
	if got := waitFor(t, seen); got != syscall.SIGTERM {
		t.Fatalf("reported %v, want SIGTERM", got)
	}
}

// The failure screen says the root filesystem was never mounted. A signal does
// not stop the mount DAG, so on a boot where the DAG finishes anyway every
// bind and overlay is in place and the screen would be a lie, on top of
// leaving the console silenced for the rest of the boot.
func TestWatchSignalsSkipsTheHaltWhenTheDagFinishes(t *testing.T) {
	ch := make(chan os.Signal, 1)
	seen := make(chan os.Signal, 1)
	dagDone := make(chan struct{})

	// The watch is never stopped here, so the only thing that can keep the
	// halt from running once the grace expires is dagDone.
	watchSignals(ch, dagDone, testGrace, func(sig os.Signal) { seen <- sig })

	ch <- syscall.SIGTERM
	close(dagDone)

	select {
	case sig := <-seen:
		t.Fatalf("halted with %v on a boot whose mount DAG completed", sig)
	case <-time.After(20 * testGrace):
	}
}

// The safety net itself: a DAG that is still in flight when the grace runs out
// is the case the failure screen exists for.
func TestWatchSignalsHaltsWhenTheDagNeverFinishes(t *testing.T) {
	ch := make(chan os.Signal, 1)
	seen := make(chan os.Signal, 1)

	stop := watchSignals(ch, neverDone(), testGrace, func(sig os.Signal) { seen <- sig })
	defer stop()

	ch <- syscall.SIGTERM
	if got := waitFor(t, seen); got != syscall.SIGTERM {
		t.Fatalf("reported %v, want SIGTERM", got)
	}
}

// stop must not return while the halt is painting the console, or the caller
// exits the process and takes the screen with it.
func TestWatchSignalsStopWaitsForTheHalt(t *testing.T) {
	ch := make(chan os.Signal, 1)
	halting := make(chan struct{})
	released := make(chan struct{})
	returned := make(chan struct{})

	stop := watchSignals(ch, neverDone(), testGrace, func(os.Signal) {
		close(halting)
		<-released
	})

	ch <- syscall.SIGTERM
	<-halting

	go func() {
		stop()
		close(returned)
	}()

	select {
	case <-returned:
		t.Fatal("stop returned while the halt was still running")
	case <-time.After(200 * time.Millisecond):
	}

	close(released)
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not return after the halt finished")
	}
}

func TestWatchSignalsIgnoresSignalsAfterStop(t *testing.T) {
	ch := make(chan os.Signal, 1)
	seen := make(chan os.Signal, 1)

	stop := watchSignals(ch, neverDone(), testGrace, func(sig os.Signal) { seen <- sig })
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
	stop := watchSignals(make(chan os.Signal), neverDone(), testGrace, func(os.Signal) {})
	stop()
	stop()
}

// A live-media or UKI boot must not arm the watch: there is no mid-mount
// interruption to protect against, and the returned stop must stay safe to
// call.
func TestWatchForTerminationIsInertOutsideANormalBoot(t *testing.T) {
	seen := make(chan os.Signal, 1)

	stop := watchForTermination(false, neverDone(), testGrace, func(sig os.Signal) { seen <- sig })
	stop()
	stop()

	select {
	case sig := <-seen:
		t.Fatalf("reported %v on a boot that is not guarded", sig)
	default:
	}
}

func TestWatchForTerminationInterceptsSigterm(t *testing.T) {
	seen := make(chan os.Signal, 1)
	dagDone := make(chan struct{})

	stop := watchForTermination(true, dagDone, testGrace, func(sig os.Signal) { seen <- sig })
	defer close(dagDone)
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

	stop := watchForTermination(true, neverDone(), testGrace, func(sig os.Signal) { seen <- sig })
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
