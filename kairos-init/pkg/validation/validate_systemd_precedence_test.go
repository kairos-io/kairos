package validation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// rootedDefaultPaths maps the default systemd search paths under root so the
// real lookup order can be exercised without touching the host.
func rootedDefaultPaths(t *testing.T, root string) []string {
	t.Helper()
	paths := make([]string, 0, len(defaultSystemdSearchPaths))
	for _, p := range defaultSystemdSearchPaths {
		dir := filepath.Join(root, p)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, dir)
	}
	return paths
}

func TestGettyMaskInOverrideDirIsSeen(t *testing.T) {
	for _, override := range []string{"/etc/systemd/system", "/run/systemd/system"} {
		t.Run(override, func(t *testing.T) {
			root := t.TempDir()
			paths := rootedDefaultPaths(t, root)

			unit := filepath.Join(root, "/usr/lib/systemd/system/getty.target")
			if err := os.WriteFile(unit, []byte("[Unit]\nDescription=Getty Target\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/dev/null", filepath.Join(root, override, "getty.target")); err != nil {
				t.Fatal(err)
			}

			v := &Validator{
				Log:    logger.NewKairosLogger("test", "info", false),
				System: values.System{Family: values.DebianFamily},
			}
			if err := v.ValidateGettyServicesWithPaths(paths); err == nil {
				t.Fatalf("a getty.target masked in %s must be reported as masked", override)
			}
		})
	}
}

func TestRHELServiceMaskInEtcIsSeen(t *testing.T) {
	root := t.TempDir()
	paths := rootedDefaultPaths(t, root)

	for _, svc := range []string{"systemd-udevd.service", "systemd-logind.service"} {
		if err := os.WriteFile(filepath.Join(root, "/usr/lib/systemd/system", svc), []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/dev/null", filepath.Join(root, "/etc/systemd/system/systemd-udevd.service")); err != nil {
		t.Fatal(err)
	}

	v := &Validator{
		Log:    logger.NewKairosLogger("test", "info", false),
		System: values.System{Family: values.RedHatFamily},
	}
	if err := v.ValidateRHELServicesWithPaths(paths); err == nil {
		t.Fatal("a systemd-udevd.service masked in /etc must be reported as masked")
	}
}

func TestUnmaskedUnitInUsrLibStillPasses(t *testing.T) {
	root := t.TempDir()
	paths := rootedDefaultPaths(t, root)
	unit := filepath.Join(root, "/usr/lib/systemd/system/getty.target")
	if err := os.WriteFile(unit, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := &Validator{
		Log:    logger.NewKairosLogger("test", "info", false),
		System: values.System{Family: values.DebianFamily},
	}
	if err := v.ValidateGettyServicesWithPaths(paths); err != nil {
		t.Fatalf("unmasked unit must pass: %v", err)
	}
}
