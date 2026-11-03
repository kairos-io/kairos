package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/agent/pkg/progress"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
	"github.com/spf13/cobra"
)

// captureStdoutStderr redirects the process stdout and stderr to temporary
// files for the duration of fn and returns what each one received. The real
// descriptors are what runApp writes to, so nothing short of swapping them
// proves which stream the error went to.
func captureStdoutStderr(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	outFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout file: %v", err)
	}
	errFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("create stderr file: %v", err)
	}

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	defer func() {
		os.Stdout, os.Stderr = origOut, origErr
		outFile.Close()
		errFile.Close()
	}()

	fn()

	outBytes, err := os.ReadFile(outFile.Name())
	if err != nil {
		t.Fatalf("read stdout file: %v", err)
	}
	errBytes, err := os.ReadFile(errFile.Name())
	if err != nil {
		t.Fatalf("read stderr file: %v", err)
	}
	return string(outBytes), string(errBytes)
}

// failingCommand stands in for a kairos-agent command that fails with msg.
func failingCommand(msg string) *cobra.Command {
	return &cobra.Command{
		Use:           "kairos-agent",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          func(*cobra.Command, []string) error { return errors.New(msg) },
	}
}

func TestRunAppWritesTheFatalErrorToStderr(t *testing.T) {
	app := failingCommand("exploded while upgrading")

	var code int
	stdout, stderr := captureStdoutStderr(t, func() {
		code = runApp(app, []string{"kairos-agent"})
	})

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "exploded while upgrading") {
		t.Errorf("stderr = %q, want it to carry the error", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want it empty: an error on stdout corrupts --output json", stdout)
	}
}

func TestRunAppLeavesStdoutToTheCommandOnSuccess(t *testing.T) {
	app := &cobra.Command{
		Use: "kairos-agent",
		RunE: func(c *cobra.Command, _ []string) error {
			_, err := c.OutOrStdout().Write([]byte("{}\n"))
			return err
		},
	}

	var code int
	stdout, stderr := captureStdoutStderr(t, func() {
		code = runApp(app, []string{"kairos-agent"})
	})

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stdout != "{}\n" {
		t.Errorf("stdout = %q, want the command output", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want it empty", stderr)
	}
}

// The real CLI, not a stub: a command that fails must not put a single byte on
// stdout, because `--output json` is read from there.
func TestTheAgentCLIKeepsStdoutCleanWhenACommandFails(t *testing.T) {
	missing := "/kairos-agent-test/no/such/cloud-config.yaml"

	var code int
	stdout, stderr := captureStdoutStderr(t, func() {
		code = runApp(NewCommand(), []string{"kairos-agent", "validate", missing})
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr: %q, stdout: %q)", code, stderr, stdout)
	}
	if stderr == "" {
		t.Errorf("stderr is empty, want the validation error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want it empty", stdout)
	}
}

// progressLines returns the error events fn wrote to the progress stream.
// progress.Output is captured at package init, so swapping os.Stdout the way
// captureStdoutStderr does would not redirect it.
func progressErrorEvents(t *testing.T, fn func()) []string {
	t.Helper()

	t.Setenv(agentrun.EnvProgress, "1")
	buf := &bytes.Buffer{}
	orig := progress.Output
	progress.Output = buf
	defer func() { progress.Output = orig }()

	fn()

	events := []string{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, `"event":"`+agentrun.EventError+`"`) {
			events = append(events, line)
		}
	}
	return events
}

// A manual-install that fails before the install action ever runs is the case
// an installer frontend could not see: sdk/agentrun reads progress events from
// the agent's stdout, and the reason used to reach the browser only as part of
// the stdout line this PR moved to stderr. The PreRunE hook of manual-install
// is the earliest such failure, the config scan, CheckConfigForUsers and
// installSpec.Sanitize are later ones on the same path.
func TestAManualInstallThatFailsBeforeInstallingEmitsAProgressError(t *testing.T) {
	var code int
	events := progressErrorEvents(t, func() {
		captureStdoutStderr(t, func() {
			code = runApp(NewCommand(), []string{
				"kairos-agent", "manual-install",
				"--source", "https://example.com/not-a-supported-scheme",
				"/kairos-agent-test/no/such/cloud-config.yaml",
			})
		})
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if len(events) != 1 {
		t.Fatalf("got %d error events, want exactly 1: %v", len(events), events)
	}

	var event struct {
		Event   string `json:"event"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(events[0]), &event); err != nil {
		t.Fatalf("error event is not JSON: %v (%q)", err, events[0])
	}
	if !strings.Contains(event.Message, "does not match any of") {
		t.Errorf("message = %q, want the reason the install was refused", event.Message)
	}
}

// Every install path returns its failure to runApp, so one failure is one
// event. This is what lets the emit live here instead of inside
// InstallAction.Run, which could not see the failures above it.
func TestRunAppEmitsOneErrorEventPerFailure(t *testing.T) {
	app := failingCommand("exploded while partitioning")

	events := progressErrorEvents(t, func() {
		captureStdoutStderr(t, func() {
			runApp(app, []string{"kairos-agent"})
		})
	})

	if len(events) != 1 {
		t.Fatalf("got %d error events, want exactly 1: %v", len(events), events)
	}
	if !strings.Contains(events[0], "exploded while partitioning") {
		t.Errorf("event = %q, want it to carry the error", events[0])
	}
}

// Without KAIROS_AGENT_PROGRESS nothing may appear on stdout, which is the
// guarantee the rest of this file is about.
func TestRunAppEmitsNoProgressEventWhenProgressIsOff(t *testing.T) {
	t.Setenv(agentrun.EnvProgress, "")
	buf := &bytes.Buffer{}
	orig := progress.Output
	progress.Output = buf
	defer func() { progress.Output = orig }()

	app := failingCommand("exploded while upgrading")
	captureStdoutStderr(t, func() {
		runApp(app, []string{"kairos-agent"})
	})

	if buf.Len() != 0 {
		t.Errorf("progress stream = %q, want it empty", buf.String())
	}
}
