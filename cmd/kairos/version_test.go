package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/internal/version"
)

// stdoutOf runs fn and returns what it wrote to stdout.
func stdoutOf(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	os.Stdout = orig
	_ = w.Close()
	return <-done
}

// AuroraBoot's install script runs `kairos-agent version` and parses the bare
// version the agent prints, so that query has to reach the agent.
func TestAgentVersionCommandReachesTheAgent(t *testing.T) {
	for _, args := range [][]string{
		{"/usr/bin/kairos-agent", "version"},
		{"kairos-agent", "version"},
		{"kairos", "agent", "version"},
		{"kairos", "kairos-agent", "version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var code int
			out := stdoutOf(t, func() { code = run(args) })
			if code != 0 {
				t.Fatalf("exit code %d", code)
			}
			if want := version.Version + "\n"; out != want {
				t.Errorf("printed %q, want the agent's bare %q", out, want)
			}
		})
	}

	t.Run("version --long", func(t *testing.T) {
		out := stdoutOf(t, func() { run([]string{"kairos-agent", "version", "--long"}) })
		if !strings.Contains(out, "\ngo ") {
			t.Errorf("printed %q, want the agent's long form with the Go version", out)
		}
	})
}

func TestVersionQueriesKairosHandsToTheSubTool(t *testing.T) {
	root, _ := newRoot()
	for _, args := range [][]string{
		{"kairos", "agent", "version"},
		{"kairos", "kairos-agent", "version", "--long"},
		{"kairos", "provider", "version"},
		{"kairos", "provider", "--version"},
		{"kairos", "provider", "-v"},
	} {
		if answersVersion(root, args) {
			t.Errorf("%q is answered by kairos, want it handed to the sub-tool", args)
		}
	}
}

// A sub-tool without its own version command must not be started by a version
// query: immucore's default action is the boot flow.
func TestVersionQueriesKairosAnswers(t *testing.T) {
	root, _ := newRoot()
	for _, args := range [][]string{
		{"kairos", "--version"},
		{"kairos", "version"},
		{"kairos", "-v"},
		{"kairos", "immucore", "version"},
		{"kairos", "immucore", "--version"},
		{"kairos", "kcrypt", "version"},
		// The agent has no --version flag, so kairos keeps it.
		{"kairos", "agent", "--version"},
		{"kairos", "kairos-agent", "-v"},
	} {
		if !answersVersion(root, args) {
			t.Errorf("%q is handed to the sub-tool, want kairos to answer it", args)
		}
	}

	for _, args := range [][]string{
		{"kairos", "--version"},
		{"/usr/bin/immucore", "version"},
		{"/init", "--version"},
	} {
		out := stdoutOf(t, func() { run(args) })
		if want := "kairos " + version.Version + "\n"; out != want {
			t.Errorf("%q printed %q, want %q", args, out, want)
		}
	}
}
