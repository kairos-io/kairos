package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeExe creates a file that looks like an installed provider plugin.
func writeExe(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveProviderFindsTheInstalledOne(t *testing.T) {
	dir := t.TempDir()
	want := writeExe(t, dir, "agent-provider-kairos", 0o755)

	got, err := resolveProvider([]string{dir})
	if err != nil {
		t.Fatalf("resolveProvider: %v", err)
	}
	if got != want {
		t.Errorf("resolved %q, want %q", got, want)
	}
}

// Providers may be installed under either of the bus's search paths, and a
// missing directory is the normal case rather than an error: core images have
// no /system/providers at all.
func TestResolveProviderSearchesEveryPathAndSkipsMissingOnes(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "does-not-exist")
	dir := t.TempDir()
	want := writeExe(t, dir, "agent-provider-kairos", 0o755)

	got, err := resolveProvider([]string{absent, dir})
	if err != nil {
		t.Fatalf("resolveProvider: %v", err)
	}
	if got != want {
		t.Errorf("resolved %q, want %q", got, want)
	}
}

// Core images ship no provider. The error has to say that plainly, because
// "kairos provider get-kubeconfig" on a core node is a reasonable thing for
// someone to try after reading standard-image docs.
func TestResolveProviderErrorsWhenNoneInstalled(t *testing.T) {
	dir := t.TempDir()

	_, err := resolveProvider([]string{dir})
	if err == nil {
		t.Fatal("expected an error when no provider is installed")
	}
	if !strings.Contains(err.Error(), "no provider installed") {
		t.Errorf("error %q does not say no provider is installed", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the directories searched", err)
	}
}

// Running two providers side by side is not supported today
// (kairos-io/kairos#3926). Rather than silently picking one, say which ones
// were found so the caller can invoke the one they meant by its own path.
func TestResolveProviderErrorsWhenSeveralInstalled(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, dir, "agent-provider-kairos", 0o755)
	writeExe(t, dir, "agent-provider-rke2", 0o755)

	_, err := resolveProvider([]string{dir})
	if err == nil {
		t.Fatal("expected an error when several providers are installed")
	}
	for _, name := range []string{"agent-provider-kairos", "agent-provider-rke2"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %q", err, name)
		}
	}
}

func TestResolveProviderIgnoresNonProviderFiles(t *testing.T) {
	dir := t.TempDir()
	want := writeExe(t, dir, "agent-provider-kairos", 0o755)
	writeExe(t, dir, "README", 0o755)
	writeExe(t, dir, "kairos-agent", 0o755)

	got, err := resolveProvider([]string{dir})
	if err != nil {
		t.Fatalf("resolveProvider: %v", err)
	}
	if got != want {
		t.Errorf("resolved %q, want %q", got, want)
	}
}

// A leftover config or log named like a plugin is not something to exec.
func TestResolveProviderIgnoresNonExecutableFiles(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, dir, "agent-provider-kairos", 0o644)

	if _, err := resolveProvider([]string{dir}); err == nil {
		t.Fatal("expected a non-executable file to be ignored")
	}
}

// The same provider installed in both search paths is one provider, not two.
func TestResolveProviderDeduplicatesAcrossPaths(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	want := writeExe(t, first, "agent-provider-kairos", 0o755)
	writeExe(t, second, "agent-provider-kairos", 0o755)

	got, err := resolveProvider([]string{first, second})
	if err != nil {
		t.Fatalf("resolveProvider: %v", err)
	}
	if got != want {
		t.Errorf("resolved %q, want the first path %q", got, want)
	}
}

// The plugin has to see its own name as argv[0] and the user's arguments
// unchanged. "provider" is the dispatcher's token, not part of the command the
// provider is being asked to run.
func TestRunProviderExecsWithTheUsersArguments(t *testing.T) {
	dir := t.TempDir()
	want := writeExe(t, dir, "agent-provider-kairos", 0o755)

	var gotPath string
	var gotArgv []string
	old := execProvider
	execProvider = func(path string, argv []string, _ []string) error {
		gotPath, gotArgv = path, argv
		return nil
	}
	t.Cleanup(func() { execProvider = old })

	runProviderIn([]string{dir}, []string{"provider", "get-kubeconfig", "--api", "unix:///run/e.sock"})

	if gotPath != want {
		t.Errorf("exec'd %q, want %q", gotPath, want)
	}
	wantArgv := []string{"agent-provider-kairos", "get-kubeconfig", "--api", "unix:///run/e.sock"}
	if strings.Join(gotArgv, " ") != strings.Join(wantArgv, " ") {
		t.Errorf("argv %q, want %q", gotArgv, wantArgv)
	}
}

func TestRunProviderReportsFailureToExec(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, dir, "agent-provider-kairos", 0o755)

	old := execProvider
	execProvider = func(string, []string, []string) error { return os.ErrPermission }
	t.Cleanup(func() { execProvider = old })

	if code := runProviderIn([]string{dir}, []string{"provider", "role", "list"}); code != 1 {
		t.Errorf("exit code %d, want 1 when exec fails", code)
	}
}

func TestRunProviderExitsNonZeroWhenNoProviderInstalled(t *testing.T) {
	if code := runProviderIn([]string{t.TempDir()}, []string{"provider", "role", "list"}); code != 1 {
		t.Errorf("exit code %d, want 1 when no provider is installed", code)
	}
}
