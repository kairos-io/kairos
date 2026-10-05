package agent

import (
	"errors"
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("operatorAbortedReset", func() {
	It("treats a real answer as an abort", func() {
		Expect(operatorAbortedReset(func(string) (string, error) { return "", nil })).To(BeTrue())
	})

	It("does not treat EOF on stdin as an operator abort", func() {
		Expect(operatorAbortedReset(func(string) (string, error) { return "", io.EOF })).To(BeFalse())
	})

	It("does not treat other read errors as an operator abort", func() {
		Expect(operatorAbortedReset(func(string) (string, error) { return "", errors.New("input/output error") })).To(BeFalse())
	})
})

var _ = Describe("abortedResetExitCode", func() {
	It("returns 0 when the shell exits cleanly", func() {
		Expect(abortedResetExitCode(func() error { return nil })).To(Equal(0))
	})

	It("returns 1 without panicking when the shell fails", func() {
		var code int
		Expect(func() {
			code = abortedResetExitCode(func() error { return errors.New("exit status 127") })
		}).NotTo(Panic())
		Expect(code).To(Equal(1))
	})
})
