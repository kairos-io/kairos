package stages

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// installSplashBinary writes to absolute paths in a real rootfs, so these
// exercise it against a temporary tree rather than asserting on source.

func splashLog() logger.KairosLogger {
	return logger.NewKairosLogger("test", "fatal", true)
}

// The default image has no /usr/bin/kairos-splash, so kairos-init is what puts
// one there. Without it both units' ConditionPathExists= fails and the dracut
// module's check() excludes itself: the feature ships and never draws.
func TestInstallSplashBinaryLinksToKairos(t *testing.T) {
	dir := t.TempDir()
	kairos := filepath.Join(dir, "kairos")
	splash := filepath.Join(dir, "bin", "kairos-splash")
	if err := os.WriteFile(kairos, []byte("ELF"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installSplashBinary(splashLog(), kairos, splash); err != nil {
		t.Fatalf("installSplashBinary: %v", err)
	}

	target, err := os.Readlink(splash)
	if err != nil {
		t.Fatalf("not a symlink: %v", err)
	}
	if target != kairos {
		t.Fatalf("symlink points at %q, want %q", target, kairos)
	}
}

// This is the whole of mudler's override: an image that already has something
// at the path keeps it. If kairos-init replaced it, a downstream could not
// ship its own animation without forking the units, and a rebuild of an
// already-initialized image would silently undo the customization.
func TestInstallSplashBinaryKeepsAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	kairos := filepath.Join(dir, "kairos")
	splash := filepath.Join(dir, "kairos-splash")
	if err := os.WriteFile(kairos, []byte("ELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(splash, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installSplashBinary(splashLog(), kairos, splash); err != nil {
		t.Fatalf("installSplashBinary: %v", err)
	}

	got, err := os.ReadFile(splash)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "#!/bin/sh\nexit 0\n" {
		t.Fatalf("downstream splash was replaced, content is now %q", got)
	}
}

// A dangling symlink is still an entry someone put there. os.Stat follows
// symlinks and would report the path as free, so the guard has to be Lstat:
// otherwise os.Symlink fails with EEXIST and the whole install stage errors
// out on an image that merely has a stale link.
func TestInstallSplashBinaryKeepsADanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	kairos := filepath.Join(dir, "kairos")
	splash := filepath.Join(dir, "kairos-splash")
	if err := os.WriteFile(kairos, []byte("ELF"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), splash); err != nil {
		t.Fatal(err)
	}

	if err := installSplashBinary(splashLog(), kairos, splash); err != nil {
		t.Fatalf("installSplashBinary: %v", err)
	}

	target, err := os.Readlink(splash)
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(dir, "gone") {
		t.Fatalf("dangling symlink was rewritten to %q", target)
	}
}

// Every binary pinned through --version-overrides means the multi-call binary
// is never written. A symlink to it would be a dangling ExecStart that
// require_binaries cannot see through, so the splash must be absent instead,
// and the install stage must not fail over it.
func TestInstallSplashBinarySkipsWithoutTheMultiCallBinary(t *testing.T) {
	dir := t.TempDir()
	splash := filepath.Join(dir, "kairos-splash")

	if err := installSplashBinary(splashLog(), filepath.Join(dir, "kairos"), splash); err != nil {
		t.Fatalf("installSplashBinary: %v", err)
	}

	if _, err := os.Lstat(splash); !os.IsNotExist(err) {
		t.Fatalf("expected no splash binary, Lstat returned %v", err)
	}
}
