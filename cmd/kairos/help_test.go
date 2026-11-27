package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// Asking for help is not an error: the usage goes to stdout and the exit code
// is 0, so `kairos --help | less` and scripts that probe the binary work.
func TestHelpPrintsTheUsageAndExitsZero(t *testing.T) {
	for _, args := range [][]string{
		{"kairos", "--help"},
		{"kairos", "-h"},
		{"kairos", "help"},
	} {
		var code int
		out := captureOutput(t, &os.Stdout, func() { code = run(args) })
		if code != 0 {
			t.Errorf("%q exited %d, want 0", args, code)
		}
		assertSortedSubTools(t, args, out)
	}
}

func TestAnUnknownSubToolPrintsTheUsageToStderr(t *testing.T) {
	args := []string{"kairos", "no-such-tool"}
	var code int
	out := captureOutput(t, &os.Stderr, func() { code = run(args) })
	if code != 2 {
		t.Errorf("%q exited %d, want 2", args, code)
	}
	if !strings.Contains(out, `unknown sub-tool "no-such-tool"`) {
		t.Errorf("%q stderr does not name the unknown sub-tool:\n%s", args, out)
	}
	assertSortedSubTools(t, args, out)
}

// assertSortedSubTools checks that the usage in out lists every registered
// sub-tool, in alphabetical order.
func assertSortedSubTools(t *testing.T, args []string, out string) {
	t.Helper()
	var listed []string
	_, commands, ok := strings.Cut(out, "Available Commands:\n")
	if !ok {
		t.Errorf("%q printed no sub-tool list:\n%s", args, out)
		return
	}
	for _, line := range strings.Split(commands, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		listed = append(listed, fields[0])
	}
	if len(listed) < len(subTools) {
		t.Errorf("%q lists %q, want all %d sub-tools", args, listed, len(subTools))
	}
	if !slices.IsSorted(listed) {
		t.Errorf("%q lists %q, want them sorted", args, listed)
	}
}

// captureOutput runs fn with *stream redirected to a file and returns what fn
// wrote there.
func captureOutput(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	orig := *stream
	*stream = f
	defer func() { *stream = orig }()
	fn()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
