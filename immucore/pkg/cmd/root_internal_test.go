package cmd

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
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

	stop := watchSignals(ch, func(sig os.Signal) { seen <- sig })
	defer stop()

	ch <- syscall.SIGTERM
	if got := waitFor(t, seen); got != syscall.SIGTERM {
		t.Fatalf("reported %v, want SIGTERM", got)
	}
}

func TestWatchSignalsIgnoresSignalsAfterStop(t *testing.T) {
	ch := make(chan os.Signal, 1)
	seen := make(chan os.Signal, 1)

	stop := watchSignals(ch, func(sig os.Signal) { seen <- sig })
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
	stop := watchSignals(make(chan os.Signal), func(os.Signal) {})
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

func TestWatchForTerminationInterceptsSigterm(t *testing.T) {
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
