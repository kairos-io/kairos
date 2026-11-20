package netboot

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("serveUntilDone", Label("netboot"), func() {
	It("stops a blocked server when the context is done", func() {
		// The real Serve blocks on a channel receive until a fatal error or
		// Shutdown, so a serve that never returns on its own is the case that
		// matters: without a shutdown on cancellation the caller hangs forever.
		release := make(chan error)
		var shutdowns atomic.Int32
		serve := func() error { return <-release }
		shutdown := func() {
			// serveUntilDone repeats the shutdown while the grace lasts, so
			// only the first call may close the channel.
			if shutdowns.Add(1) == 1 {
				close(release)
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		done := make(chan error, 1)
		go func() { done <- serveUntilDone(ctx, serve, shutdown, shutdownGrace) }()

		var err error
		Eventually(done, 5*time.Second).Should(Receive(&err))
		Expect(err).To(MatchError(context.DeadlineExceeded))
		Expect(shutdowns.Load()).To(BeNumerically(">=", int32(1)))
	})

	It("keeps asking until the server can take the shutdown", func() {
		// The real Shutdown is a non-blocking send on a channel Serve only
		// allocates once all four listeners are bound, so a shutdown that
		// arrives inside that window is dropped and the listeners stay up for
		// the life of the process. Model that: the first shutdowns do nothing.
		const dropped = 3
		release := make(chan error)
		var shutdowns atomic.Int32
		serve := func() error { return <-release }
		shutdown := func() {
			if shutdowns.Add(1) == dropped+1 {
				close(release)
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		done := make(chan error, 1)
		go func() { done <- serveUntilDone(ctx, serve, shutdown, 5*time.Second) }()

		var err error
		Eventually(done, 5*time.Second).Should(Receive(&err))
		Expect(err).To(MatchError(context.Canceled))
		// serve returned, so the retries landed rather than the grace expiring.
		Expect(shutdowns.Load()).To(BeNumerically(">", int32(dropped)))
		Eventually(release).Should(BeClosed())
	})

	It("returns the server's own error when it fails first", func() {
		boom := errors.New("listen udp :69: permission denied")
		var shutdowns atomic.Int32

		err := serveUntilDone(context.Background(), func() error { return boom },
			func() { shutdowns.Add(1) }, shutdownGrace)

		Expect(err).To(MatchError(boom))
		Expect(shutdowns.Load()).To(BeZero())
	})

	It("gives up on the drain once the grace is spent", func() {
		// A server that never takes the shutdown keeps blocking, so the grace
		// is the only thing standing between that and the original two-hour
		// hang: serveUntilDone must return even though serve never does.
		var shutdowns atomic.Int32
		neverReturns := func() error { <-make(chan error); return nil }

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		done := make(chan error, 1)
		start := time.Now()
		go func() {
			done <- serveUntilDone(ctx, neverReturns, func() { shutdowns.Add(1) },
				30*time.Millisecond)
		}()

		var err error
		Eventually(done, 5*time.Second).Should(Receive(&err))
		Expect(err).To(MatchError(context.Canceled))
		Expect(shutdowns.Load()).To(BeNumerically(">=", 1))
		// Waited for the grace rather than returning straight away, and did
		// not wait on serve, which is still blocked.
		Expect(time.Since(start)).To(BeNumerically(">=", 30*time.Millisecond))
	})
})
