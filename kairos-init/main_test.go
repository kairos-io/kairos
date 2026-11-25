package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/check"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
)

func TestEnumSliceSetAccumulates(t *testing.T) {
	allowed := []string{"install", "init", "initrd"}
	cases := []struct {
		name string
		sets []string
		want []string
	}{
		{"comma separated", []string{"install,init"}, []string{"install", "init"}},
		{"repeated flag", []string{"install", "init"}, []string{"install", "init"}},
		{"mixed", []string{"install,init", "initrd"}, []string{"install", "init", "initrd"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnumSliceFlag(allowed, []string{})
			for _, s := range c.sets {
				if err := e.Set(s); err != nil {
					t.Fatalf("Set(%q): %v", s, err)
				}
			}
			if !reflect.DeepEqual(e.Value, c.want) {
				t.Fatalf("got %v, want %v", e.Value, c.want)
			}
		})
	}
}

func TestEnumSliceSetRejectsUnknownValue(t *testing.T) {
	e := newEnumSliceFlag([]string{"install"}, []string{})
	if err := e.Set("nope"); err == nil {
		t.Fatal("expected an error for a value that is not allowed")
	}
	if len(e.Value) != 0 {
		t.Fatalf("a rejected value must not be stored, got %v", e.Value)
	}
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var exit *exitError
	if !errors.As(err, &exit) {
		t.Fatalf("got %v, want an exit status", err)
	}
	return exit.code
}

func TestCheckCommandExitStatus(t *testing.T) {
	empty := t.TempDir()
	cases := []struct {
		name string
		args []string
		opts check.Options
		want int
	}{
		{"an argument", []string{"/some/dir"}, check.Options{}, 2},
		{"--arch without --root", nil, check.Options{Arch: "aarch64"}, 2},
		{"a root that is not there", nil, check.Options{Root: filepath.Join(empty, "missing")}, 2},
		{"a root with nothing in it", nil, check.Options{Root: empty}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkOpts = c.opts
			t.Cleanup(func() { checkOpts = check.Options{} })
			err := checkCmd.Args(checkCmd, c.args)
			if err == nil {
				checkCmd.SetOut(&bytes.Buffer{})
				err = checkCmd.RunE(checkCmd, c.args)
			}
			if got := exitCode(t, err); got != c.want {
				t.Fatalf("exit status %d, want %d", got, c.want)
			}
			if err.Error() == "" {
				t.Fatal("no message")
			}
		})
	}
}

func TestPreCheck(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	osRelease := "PRETTY_NAME=\"Arch Linux\"\nID=arch\nVERSION_ID=\"1\"\n"
	if err := os.WriteFile(filepath.Join(root, "etc/os-release"), []byte(osRelease), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := check.Options{Root: root, Arch: "x86_64"}

	var out bytes.Buffer
	err := runPreCheck(&out, opts, false)
	if err == nil || !strings.Contains(err.Error(), "no package list for the Arch family") {
		t.Fatalf("a system that cannot convert has to stop the conversion, got %v", err)
	}
	if !strings.Contains(out.String(), "  fail  ") {
		t.Fatalf("the result is not printed:\n%s", out.String())
	}
	// A dry run prints what it would do on any machine.
	out.Reset()
	if err := runPreCheck(&out, opts, true); err != nil {
		t.Fatalf("a dry run does not stop: %v", err)
	}
}

func TestPreCheckOptions(t *testing.T) {
	config.DefaultConfig.SkipSteps = []string{"installKernel"}
	t.Cleanup(func() { config.DefaultConfig.SkipSteps = nil })
	got := preCheckOptions("init").SkipSteps
	want := []string{"installKernel", "install"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skipped steps %v, want %v", got, want)
	}
	if got := preCheckOptions("all").SkipSteps; !reflect.DeepEqual(got, []string{"installKernel"}) {
		t.Fatalf("skipped steps %v", got)
	}
}
