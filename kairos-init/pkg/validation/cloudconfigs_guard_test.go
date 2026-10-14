package validation_test

import (
	"bytes"
	"errors"
	"os/exec"

	. "github.com/onsi/gomega"
)

// guardPasses reports whether a cloud-config stage guard holds, the way yip
// decides it: the guard is a shell command, and it holds when the shell exits
// zero.
//
// Only an exit status answers the question. If the shell never ran, the guard
// was never evaluated, and reading that as "the stage does not apply" turns a
// broken harness into a result. A spec that expects no match then passes
// without testing anything, and a spec that expects a match reports an empty
// command list rather than the reason. Fail on it instead, and quote the
// command and whatever the shell wrote.
func guardPasses(command *exec.Cmd) bool {
	var stderr bytes.Buffer
	command.Stderr = &stderr

	err := command.Run()
	if err == nil {
		return true
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false
	}

	Expect(err).NotTo(HaveOccurred(), "the guard was never evaluated: sh -c %q said %q",
		command.Args[len(command.Args)-1], stderr.String())
	return false
}

// guardPassesExpr is guardPasses for a guard that is still a string.
func guardPassesExpr(expr string) bool {
	return guardPasses(exec.Command("sh", "-c", expr))
}
