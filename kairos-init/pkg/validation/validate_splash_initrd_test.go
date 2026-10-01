package validation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
)

// lsinitrdWithSplash is the shape lsinitrd prints for an initrd the
// 50kairos-splash module was installed into: the symlink to the multi-call
// binary, its target, and the unit under dracut's systemdsystemunitdir.
const lsinitrdWithSplash = `Image: /boot/initrd: 61M
========================================================================
Version:

dracut modules:
systemd
kairos-splash
28immucore
========================================================================
drwxr-xr-x   1 root     root            0 Jan  1 00:00 usr/bin
-rwxr-xr-x   1 root     root     45678901 Jan  1 00:00 usr/bin/kairos
lrwxrwxrwx   1 root     root           15 Jan  1 00:00 usr/bin/kairos-splash -> /usr/bin/kairos
-rwxr-xr-x   1 root     root     34567890 Jan  1 00:00 usr/bin/immucore
-rw-r--r--   1 root     root          412 Jan  1 00:00 usr/lib/systemd/system/kairos-splash.service
lrwxrwxrwx   1 root     root           24 Jan  1 00:00 usr/lib/systemd/system/initrd.target.wants/kairos-splash.service -> ../kairos-splash.service
-rw-r--r--   1 root     root           68 Jan  1 00:00 usr/lib/systemd/system/immucore.service.d/10-quiet.conf
========================================================================
`

// lsinitrdWithoutSplash is the same initrd built before the splash binary was
// installed, or served from a BuildKit cache. immucore is still there, so
// every other initrd check passes.
const lsinitrdWithoutSplash = `Image: /boot/initrd: 61M
========================================================================
Version:

dracut modules:
systemd
28immucore
========================================================================
drwxr-xr-x   1 root     root            0 Jan  1 00:00 usr/bin
-rwxr-xr-x   1 root     root     34567890 Jan  1 00:00 usr/bin/immucore
-rw-r--r--   1 root     root          512 Jan  1 00:00 usr/lib/systemd/system/immucore.service
========================================================================
`

// writeSplashBinary puts an entry at the splash path inside dir and returns
// it, mirroring what installSplashBinary leaves on the rootfs: a symlink to
// the multi-call binary.
func writeSplashBinary(t *testing.T, dir string) string {
	t.Helper()

	kairos := filepath.Join(dir, "kairos")
	if err := os.WriteFile(kairos, []byte("multi-call binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	splash := filepath.Join(dir, filepath.Base(bundled.SplashBinaryPath))
	if err := os.Symlink(kairos, splash); err != nil {
		t.Fatal(err)
	}
	return splash
}

func TestValidateSplashInInitrd(t *testing.T) {
	t.Run("binary installed and module in the initrd", func(t *testing.T) {
		splash := writeSplashBinary(t, t.TempDir())

		if err := validateSplashInInitrd(splash, lsinitrdWithSplash); err != nil {
			t.Fatalf("expected a splash-carrying initrd to validate, got: %v", err)
		}
	})

	t.Run("binary installed but module missing from the initrd", func(t *testing.T) {
		splash := writeSplashBinary(t, t.TempDir())

		err := validateSplashInInitrd(splash, lsinitrdWithoutSplash)
		if err == nil {
			t.Fatal("expected an error when the splash binary is installed and the module is not in the initrd")
		}
		// The message has to name both halves, because the two causes
		// (initrd built too early, cached initrd) are told apart by hand.
		for _, want := range []string{"usr/bin/kairos-splash", "kairos-splash.service", splash} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("expected the error to mention %q, got: %v", want, err)
			}
		}
	})

	t.Run("no splash binary on the rootfs is not a failure", func(t *testing.T) {
		// Every binary pinned through --version-overrides: the multi-call
		// binary is never written, installSplashBinary writes no symlink,
		// and the dracut module's check() drops the module. An image with
		// no splash at all is a valid image.
		absent := filepath.Join(t.TempDir(), filepath.Base(bundled.SplashBinaryPath))

		if err := validateSplashInInitrd(absent, lsinitrdWithoutSplash); err != nil {
			t.Fatalf("expected an image with no splash binary to validate, got: %v", err)
		}
	})

	t.Run("a dangling splash symlink still demands the module", func(t *testing.T) {
		// Lstat, not Stat. A downstream that pointed the path at an
		// executable it later dropped leaves a dangling link; Stat would
		// read that as "no splash" and skip the check.
		dir := t.TempDir()
		splash := filepath.Join(dir, filepath.Base(bundled.SplashBinaryPath))
		if err := os.Symlink(filepath.Join(dir, "gone"), splash); err != nil {
			t.Fatal(err)
		}

		if err := validateSplashInInitrd(splash, lsinitrdWithoutSplash); err == nil {
			t.Fatal("expected a dangling splash symlink to still require the module in the initrd")
		}
	})

	t.Run("the unit alone is not enough", func(t *testing.T) {
		// inst_multiple failing to resolve the binary while inst_simple
		// still copies the unit: the splash cannot exec, so the animation
		// never draws even though the module name is in the initrd.
		splash := writeSplashBinary(t, t.TempDir())
		unitOnly := strings.ReplaceAll(lsinitrdWithSplash,
			"usr/bin/kairos-splash -> /usr/bin/kairos", "usr/bin/kairos")

		err := validateSplashInInitrd(splash, unitOnly)
		if err == nil {
			t.Fatal("expected an error when the unit is in the initrd but the binary is not")
		}
		if !strings.Contains(err.Error(), "usr/bin/kairos-splash") {
			t.Errorf("expected the error to name the missing binary, got: %v", err)
		}
		if strings.Contains(err.Error(), "kairos-splash.service") {
			t.Errorf("the unit is present, it should not be reported missing: %v", err)
		}
	})

	t.Run("the binary alone is not enough", func(t *testing.T) {
		splash := writeSplashBinary(t, t.TempDir())
		binaryOnly := strings.ReplaceAll(lsinitrdWithSplash, "kairos-splash.service", "kairos-other.service")

		err := validateSplashInInitrd(splash, binaryOnly)
		if err == nil {
			t.Fatal("expected an error when the binary is in the initrd but the unit is not")
		}
		if !strings.Contains(err.Error(), "kairos-splash.service") {
			t.Errorf("expected the error to name the missing unit, got: %v", err)
		}
	})
}
