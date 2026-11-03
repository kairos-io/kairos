package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// subToolFor returns the canonical name of the sub-tool that argv0 dispatches
// to, the way the binary resolves it when run under that name, or "" when it
// resolves to no sub-tool.
func subToolFor(t *testing.T, argv0 string) string {
	t.Helper()
	root, _ := newRoot()
	cmd, _, err := root.Find(argv0Args([]string{argv0})[1:])
	if err != nil || cmd == root {
		return ""
	}
	return cmd.Name()
}

// Every name a symlink is installed under has to reach its sub-tool. These are
// the names kairos-init, AuroraBoot and the Makefile's symlinks target create,
// and a name that stops resolving fails only at boot: /init as pid 1 exits and
// the kernel panics.
func TestArgv0NamesDispatchToTheirSubTool(t *testing.T) {
	for argv0, want := range map[string]string{
		"/usr/bin/immucore":                    "immucore",
		"/init":                                "immucore",
		"/usr/bin/kairos-agent":                "agent",
		"kairos-agent":                         "agent",
		"/usr/bin/kcrypt-discovery-challenger": "kcrypt",
		"/usr/bin/kairos-splash":               "splash",
	} {
		if got := subToolFor(t, argv0); got != want {
			t.Errorf("argv[0] %q dispatches to %q, want %q", argv0, got, want)
		}
	}
}

func TestKairosNamesTheSubToolInItsFirstArgument(t *testing.T) {
	root, _ := newRoot()
	for _, args := range [][]string{
		{"agent", "version"},
		{"kairos-agent", "version"},
		{"immucore", "--dry-run"},
		{"kcrypt", "info"},
		{"edit-config", "--file", "/tmp/x.yaml"},
		{"provider", "role", "list"},
	} {
		cmd, _, err := root.Find(args)
		if err != nil {
			t.Errorf("kairos %s: %v", strings.Join(args, " "), err)
			continue
		}
		if subToolOf(cmd) == nil {
			t.Errorf("kairos %s resolves to the root, want a sub-tool", strings.Join(args, " "))
		}
	}
}

// The kernel passes init every command-line word it does not recognise, so
// /init runs with arguments. They have to reach immucore rather than be read
// as a sub-tool name.
func TestInitArgumentsReachImmucore(t *testing.T) {
	root, _ := newRoot()
	cmd, args, err := root.Find(argv0Args([]string{"/init", "splash", "single"})[1:])
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name() != "immucore" {
		t.Fatalf("/init splash single resolves to %q, want immucore", cmd.Name())
	}
	if strings.Join(args, " ") != "splash single" {
		t.Fatalf("immucore gets %q, want \"splash single\"", args)
	}
}

// `kairos-agent help upgrade` worked when the agent was a binary of its own,
// and scripts and muscle memory still use it.
func TestSubToolsKeepTheirHelpCommand(t *testing.T) {
	for _, args := range [][]string{
		{"/usr/bin/kairos-agent", "help"},
		{"/usr/bin/kairos-agent", "help", "upgrade", "list-releases"},
		{"/usr/bin/kcrypt-discovery-challenger", "help", "info"},
	} {
		var code int
		captureStdout(t, func() { code = run(args) })
		if code != 0 {
			t.Errorf("%q exited %d, want 0", args, code)
		}
	}
}

func TestAnUnknownSubToolIsAUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"kairos", "no-such-tool"},
		{"/usr/bin/no-such-tool"},
		{"kairos"},
	} {
		var code int
		captureStderr(t, func() { code = run(args) })
		if code != 2 {
			t.Errorf("%q exited %d, want 2", args, code)
		}
	}
}

// A passthrough sub-tool sees os.Args as if it had been run directly, keeps
// its flags, --help included, and decides the exit code.
func TestPassthroughHandsOverItsArgumentsAndExitCode(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	var seen []string
	root := &cobra.Command{Use: "kairos"}
	root.AddCommand(passthrough("tool", "", func() int {
		seen = append([]string(nil), os.Args...)
		return 3
	}, "tool-alias")())
	root.SetArgs([]string{"tool-alias", "--help", "x"})

	err := root.Execute()

	var code exitCode
	if !errors.As(err, &code) || code != 3 {
		t.Fatalf("err = %v, want exit code 3", err)
	}
	if strings.Join(seen, " ") != "tool-alias --help x" {
		t.Fatalf("os.Args = %q, want [tool-alias --help x]", seen)
	}
}

func TestVersionIsAnsweredForEverySubTool(t *testing.T) {
	for _, args := range [][]string{
		{"kairos", "--version"},
		{"/usr/bin/immucore", "--version"},
		{"kairos", "agent", "--version"},
		{"/usr/bin/kairos-agent", "-v"},
	} {
		if code := run(args); code != 0 {
			t.Errorf("%q exited %d, want 0", args, code)
		}
	}
}

// captureStdout runs fn with stdout redirected away.
func captureStdout(t *testing.T, fn func()) {
	t.Helper()
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	orig := os.Stdout
	os.Stdout = devNull
	defer func() { os.Stdout = orig }()
	fn()
}

// captureStderr runs fn with stderr redirected away, so a usage dump does not
// clutter the test output.
func captureStderr(t *testing.T, fn func()) {
	t.Helper()
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	orig := os.Stderr
	os.Stderr = devNull
	defer func() { os.Stderr = orig }()
	fn()
}
