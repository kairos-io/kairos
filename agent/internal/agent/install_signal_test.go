package agent

import (
	"os"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("handleInstallSignal", func() {
	DescribeTable("gives the console back and then exits with 128+signal",
		func(sig os.Signal, want int) {
			var order []string
			code := -1
			handleInstallSignal(sig,
				func() { order = append(order, "getty") },
				func(c int) { order = append(order, "exit"); code = c })
			Expect(order).To(Equal([]string{"getty", "exit"}))
			Expect(code).To(Equal(want))
		},
		Entry("SIGINT", syscall.SIGINT, 130),
		Entry("SIGTERM", syscall.SIGTERM, 143),
	)
})
