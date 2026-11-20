package cmd

import (
	"os"
	"os/signal"
	"syscall"
)

// removeOnSignal removes dir and exits when the process gets SIGINT or SIGTERM.
// Without it a Ctrl-C would skip the deferred cleanup and leave the work dir
// behind. The returned function stops the watcher. Call it when the run ends.
func removeOnSignal(dir string) (stop func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	stopWatch := watchSignals(dir, sigs, os.Exit)

	return func() {
		signal.Stop(sigs)
		stopWatch()
	}
}

// watchSignals waits on sigs. On a signal it removes dir and calls exit with the
// conventional shell code for that signal (130 for SIGINT, 143 for SIGTERM).
// The returned function stops the watcher without removing anything.
func watchSignals(dir string, sigs <-chan os.Signal, exit func(int)) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-sigs:
			_ = os.RemoveAll(dir)
			if sig == syscall.SIGTERM {
				exit(143)
				return
			}
			exit(130)
		case <-done:
		}
	}()

	return func() { close(done) }
}
