package validation_test

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The guards in the bundled cloud configs are grep and test, and every one of
// them redirects the diagnostics of its own commands to /dev/null, so a guard
// that is false exits 1 and is silent. A shell that answers any other way
// answered a different question, and the caller cannot tell: it reads every
// non-zero exit as "the stage does not apply". These specs pin the one thing
// that does survive, the note the next failure prints.
var _ = Describe("Cloud config guard evaluation", func() {
	// captureGuard evaluates expr and returns both the answer and what the
	// helper wrote to the Ginkgo writer while it did.
	captureGuard := func(expr string) (bool, string) {
		var written bytes.Buffer
		GinkgoWriter.TeeTo(&written)
		defer GinkgoWriter.ClearTeeWriters()

		return guardPassesExpr(expr), written.String()
	}

	It("says nothing about a guard that is true", func() {
		passes, written := captureGuard("true")
		Expect(passes).To(BeTrue())
		Expect(written).To(BeEmpty())
	})

	It("says nothing about a guard that is false", func() {
		passes, written := captureGuard("false")
		Expect(passes).To(BeFalse())
		Expect(written).To(BeEmpty(), "exit 1 and silence is how a false guard answers")
	})

	It("reports a guard whose command is not there", func() {
		passes, written := captureGuard("kairos-no-such-command-here")
		Expect(passes).To(BeFalse(), "the caller still reads this as a stage that does not apply")
		Expect(written).To(ContainSubstring("127"))
		Expect(written).To(ContainSubstring("kairos-no-such-command-here"),
			"the note must quote the guard, so the red names itself")
	})

	It("reports a guard the shell could not parse", func() {
		_, written := captureGuard("if [")
		Expect(written).NotTo(BeEmpty(), "a syntax error is not an answer")
	})

	It("reports a guard that exits 1 but complains", func() {
		_, written := captureGuard("echo cannot fork >&2; exit 1")
		Expect(written).To(ContainSubstring("cannot fork"),
			"a shell that exits 1 after writing to stderr did not evaluate the guard either")
	})
})
