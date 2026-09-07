package cli

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
