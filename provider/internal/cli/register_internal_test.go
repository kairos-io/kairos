package cli

import (
	"context"
	"time"

	nodepair "github.com/kairos-io/go-nodepair"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
)

var _ = Describe("pairingContext", func() {
	It("bounds the wait when given a positive timeout", func() {
		ctx, cancel := pairingContext(50 * time.Millisecond)
		defer cancel()

		deadline, ok := ctx.Deadline()
		Expect(ok).To(BeTrue())
		Expect(deadline).To(BeTemporally("~", time.Now().Add(50*time.Millisecond), time.Second))

		Eventually(ctx.Done()).Should(BeClosed())
		Expect(ctx.Err()).To(MatchError(context.DeadlineExceeded))
	})

	DescribeTable("waits forever",
		func(timeout time.Duration) {
			ctx, cancel := pairingContext(timeout)
			defer cancel()

			_, ok := ctx.Deadline()
			Expect(ok).To(BeFalse())
			Expect(ctx.Err()).ToNot(HaveOccurred())
		},
		Entry("when the timeout is zero", time.Duration(0)),
		Entry("when the timeout is negative", -time.Second),
	)

	It("is still cancellable when it waits forever", func() {
		ctx, cancel := pairingContext(0)
		cancel()

		Expect(ctx.Err()).To(MatchError(context.Canceled))
	})
})

var _ = Describe("register", func() {
	// nodepair.Send returns nil when the context ends, so without the ctx.Err()
	// check register reports a delivered payload for a node that never paired.
	// Drive that path through the sendPayload seam: the fake blocks until the
	// context is done and returns nil, which is exactly what the real Send does.
	It("fails when the node has not paired before the timeout", func() {
		orig := sendPayload
		sendPayload = func(ctx context.Context, _ interface{}, _ ...nodepair.PairOption) error {
			<-ctx.Done()
			return nil
		}
		DeferCleanup(func() { sendPayload = orig })

		err := register("fatal", "", "", "", false, false, time.Millisecond)
		Expect(err).To(MatchError(ContainSubstring("did not pair within 1ms")))
	})

	It("reports success when the payload is delivered in time", func() {
		orig := sendPayload
		sendPayload = func(context.Context, interface{}, ...nodepair.PairOption) error {
			return nil
		}
		DeferCleanup(func() { sendPayload = orig })

		Expect(register("fatal", "", "", "", false, false, time.Minute)).To(Succeed())
	})
})

// The default is what ends the hang for a user who passes no --timeout, so it
// is pinned here the way this package already pins flag defaults.
var _ = Describe("RegisterCMD", func() {
	It("defaults the pairing timeout to 15 minutes", func() {
		var timeoutFlag *cli.DurationFlag
		for _, f := range RegisterCMD("kairos provider").Flags {
			if d, ok := f.(*cli.DurationFlag); ok && d.Name == "timeout" {
				timeoutFlag = d
			}
		}

		Expect(timeoutFlag).ToNot(BeNil())
		Expect(timeoutFlag.Value).To(Equal(15 * time.Minute))
	})
})
