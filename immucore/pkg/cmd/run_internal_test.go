package cmd

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
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

func TestRunAppWritesTheFatalErrorToStderr(t *testing.T) {
	app := &cli.App{
		Name:   "immucore",
		Action: func(*cli.Context) error { return errors.New("mount DAG failed") },
	}

	var code int
	stdout, stderr := captureStdoutStderr(t, func() {
		code = runApp(app, []string{"immucore"})
	})

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "mount DAG failed") {
		t.Errorf("stderr = %q, want it to carry the error", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want it empty: the failure summary is already on stderr", stdout)
	}
}

func TestRunAppLeavesStdoutToTheCommandOnSuccess(t *testing.T) {
	app := &cli.App{
		Name: "immucore",
		Action: func(c *cli.Context) error {
			_, err := c.App.Writer.Write([]byte("ok\n"))
			return err
		},
	}

	var code int
	stdout, stderr := captureStdoutStderr(t, func() {
		code = runApp(app, []string{"immucore"})
	})

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stdout != "ok\n" {
		t.Errorf("stdout = %q, want the command output", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want it empty", stderr)
	}
}
