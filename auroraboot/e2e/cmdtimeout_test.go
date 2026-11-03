package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("runWithTimeout", Label("e2e", "helpers"), func() {
	It("kills a command that outlives its deadline and says so", func() {
		start := time.Now()
		out, err := runWithTimeout(200*time.Millisecond, "sleep", "60")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("sleep 60"))
		Expect(err.Error()).To(ContainSubstring("timed out after 200ms"))
		Expect(out).To(BeEmpty())
		// Proves the child was killed rather than waited out.
		Expect(time.Since(start)).To(BeNumerically("<", 30*time.Second))
	})

	It("returns the command's own output and error when it finishes in time", func() {
		out, err := runWithTimeout(time.Minute, "sh", "-c", "echo boom >&2; exit 3")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("timed out"))
		Expect(out).To(ContainSubstring("boom"))
	})

	It("returns no error when the command succeeds", func() {
		out, err := runWithTimeout(time.Minute, "sh", "-c", "echo ok")

		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("ok"))
	})
})
