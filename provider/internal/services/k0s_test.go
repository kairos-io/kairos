package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/machine/service"
)

// The path the openrc script sources must be the one that writes command_args.
// A literal path in the script would drift from K0sEnvFile and fail silently at
// boot, so the script carries the placeholder and nothing else.
func TestK0sOpenrcScriptsCarryNoLiteralEnvPath(t *testing.T) {
	for name, script := range map[string]string{
		"controller": K0sControllerOpenrc,
		"worker":     K0sWorkerOpenrc,
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(script, service.EnvFilePlaceholder) {
				t.Errorf("script does not source %s, so the provider's arguments never reach k0s", service.EnvFilePlaceholder)
			}
			if strings.Contains(script, "/etc/k0s/") || strings.Contains(script, "/etc/sysconfig/") {
				t.Error("script hardcodes an env file path instead of using the placeholder")
			}
		})
	}
}

// The unit an openrc host writes must source exactly the file that host's
// OverrideCmd writes to, for both services.
func TestK0sSpecUnitSourcesItsOwnEnvFile(t *testing.T) {
	for _, name := range K0sServiceNames {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			spec := K0sSpec(name)
			spec.Root = root

			svc, err := service.NewFor(service.OpenRC, spec)
			if err != nil {
				t.Fatalf("building the service: %v", err)
			}

			if err := svc.WriteUnit(); err != nil {
				t.Fatalf("writing the unit: %v", err)
			}

			unit, err := os.ReadFile(filepath.Join(root, "etc/init.d", name))
			if err != nil {
				t.Fatalf("reading back the unit: %v", err)
			}

			if strings.Contains(string(unit), service.EnvFilePlaceholder) {
				t.Fatal("the placeholder survived writing the unit")
			}

			envFile := svc.EnvFile()
			if envFile != K0sEnvFile(name) {
				t.Fatalf("env file is %q, want %q", envFile, K0sEnvFile(name))
			}
			if !strings.Contains(string(unit), "[ -f "+envFile+" ]") {
				t.Fatalf("the unit does not source %q:\n%s", envFile, unit)
			}

			// And what OverrideCmd writes has to land in that same file.
			if err := svc.OverrideCmd("/usr/bin/k0s controller --config /etc/k0s/k0s.yaml"); err != nil {
				t.Fatalf("overriding the command: %v", err)
			}
			written, err := os.ReadFile(filepath.Join(root, envFile))
			if err != nil {
				t.Fatalf("the unit sources %q but nothing was written there: %v", envFile, err)
			}
			if !strings.Contains(string(written), "--config /etc/k0s/k0s.yaml") {
				t.Fatalf("env file does not carry the arguments:\n%s", written)
			}
		})
	}
}

// systemd takes its arguments in the unit, so the k0s units it gets are the
// packaged ones and the env file is the distro default.
func TestK0sSpecOnSystemd(t *testing.T) {
	root := t.TempDir()
	spec := K0sSpec("k0scontroller")
	spec.Root = root
	spec.NoReload = true

	svc, err := service.NewFor(service.Systemd, spec)
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}
	if err := svc.WriteUnit(); err != nil {
		t.Fatalf("writing the unit: %v", err)
	}

	unit, err := os.ReadFile(filepath.Join(root, "etc/systemd/system/k0scontroller.service"))
	if err != nil {
		t.Fatalf("reading back the unit: %v", err)
	}
	if !strings.Contains(string(unit), "ExecStart=/usr/bin/k0s controller") {
		t.Fatalf("not the k0s controller unit:\n%s", unit)
	}
	if got, want := svc.EnvFile(), "/etc/sysconfig/k0scontroller"; got != want {
		t.Fatalf("env file is %q, want %q", got, want)
	}
}
