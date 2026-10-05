package agent

import (
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Reproduces kairos-io/kairos#5229: with stdin at EOF (not a terminal) the
// reset prompt returns an error immediately and that is treated as the
// operator aborting the reset. This test pins the CURRENT, wrong behaviour.
var _ = Describe("operatorAbortedReset", func() {
	It("treats a real answer as an abort", func() {
		Expect(operatorAbortedReset(func(string) (string, error) { return "", nil })).To(BeTrue())
	})

	It("wrongly treats EOF on stdin as an operator abort (#5229)", func() {
		Expect(operatorAbortedReset(func(string) (string, error) { return "", io.EOF })).To(BeTrue())
	})
})
