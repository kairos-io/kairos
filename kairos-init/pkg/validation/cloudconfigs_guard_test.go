package validation_test

import (
	"bytes"
	"errors"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
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
		reportSuspectGuard(command, exitErr.ExitCode(), stderr.String())
		return false
	}

	Expect(err).NotTo(HaveOccurred(), "the guard was never evaluated: sh -c %q said %q",
		command.Args[len(command.Args)-1], stderr.String())
	return false
}

// reportSuspectGuard records a guard that said no in a way a guard that is
// merely false does not.
//
// The shell starting is not the only way the harness can lose the question.
// These guards are built from grep and test, so a guard that is false exits 1
// and writes nothing: every one of them sends the diagnostics of its own
// commands to /dev/null. Any other exit status, or anything at all on standard
// error, comes from the shell rather than from the guard. A shell that cannot
// fork a subshell, a command it cannot find, a syntax error: each of those
// exits non-zero and is read here as "the stage does not apply", which is the
// same silent answer as before.
//
// This cannot be turned into a failure, because a guard is allowed to be false
// and nothing here knows which answer the caller expects. Write it to the
// Ginkgo writer instead, which is quiet until the spec fails and is printed
// with the failure when it does, so the next red says what the shell said.
func reportSuspectGuard(command *exec.Cmd, exitCode int, stderr string) {
	if exitCode == 1 && stderr == "" {
		return
	}

	GinkgoWriter.Printf("guard exited %d and wrote %q, which is not how a false guard answers: sh -c %q\n",
		exitCode, stderr, command.Args[len(command.Args)-1])
}

// guardPassesExpr is guardPasses for a guard that is still a string.
func guardPassesExpr(expr string) bool {
	return guardPasses(exec.Command("sh", "-c", expr))
}
