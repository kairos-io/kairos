package stages

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/rs/zerolog"
)

func TestGetDracutCommand(t *testing.T) {
	tests := []struct {
		name  string
		level zerolog.Level
		want  string
	}{
		{
			name:  "info uses normal output",
			level: zerolog.InfoLevel,
			want:  "dracut -f /boot/initrd 6.12.0",
		},
		{
			name:  "debug uses normal output",
			level: zerolog.DebugLevel,
			want:  "dracut -f /boot/initrd 6.12.0",
		},
		{
			name:  "trace enables verbose output",
			level: zerolog.TraceLevel,
			want:  "dracut -v -f /boot/initrd 6.12.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getDracutCommand("6.12.0", tt.level); got != tt.want {
				t.Fatalf("getDracutCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Paths a fabricated build root can contain, to drive the daemon and dracut
// module probes in dracutNetworkModules.
const (
	fixtureNetworkManager = "usr/sbin/NetworkManager"
	fixtureNetworkd       = "usr/lib/systemd/systemd-networkd"
	fixtureResolved       = "usr/lib/systemd/systemd-resolved"
	fixtureResolvedModule = "usr/lib/dracut/modules.d/11systemd-resolved/module-setup.sh"
)

// buildRoot fabricates a build root containing the given files.
func buildRoot(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		path := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("failed to create %s: %s", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, nil, 0o755); err != nil {
			t.Fatalf("failed to write %s: %s", path, err)
		}
	}
	return root
}

func TestDracutNetworkModules(t *testing.T) {
	log := logger.NewKairosLogger("test", "fatal", true)

	tests := []struct {
		name       string
		sis        values.System
		files      []string
		wantModule string
		wantSysext bool
	}{
		{
			// Bug: systemd-networkd without systemd-resolved leaves the
			// initramfs without a resolver. kairos-io/kairos#835.
			name:       "ubuntu 22.04 skips resolved",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "22.04"},
			files:      []string{fixtureResolved, fixtureResolvedModule},
			wantModule: "systemd-networkd network-legacy",
			wantSysext: false,
		},
		{
			name:       "ubuntu 22.04 without the resolved dracut module",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "22.04"},
			files:      []string{fixtureResolved},
			wantModule: "systemd-networkd network-legacy",
			wantSysext: false,
		},
		{
			name:       "ubuntu 20.04 uses the plain network module",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "20.04"},
			files:      []string{fixtureResolved, fixtureResolvedModule},
			wantModule: "network",
			wantSysext: false,
		},
		{
			name:       "ubuntu 24.04 adds resolved",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "24.04"},
			files:      []string{fixtureResolved, fixtureResolvedModule},
			wantModule: "systemd-networkd network-legacy systemd-resolved",
			wantSysext: true,
		},
		{
			name:       "ubuntu 26.04 drops network-legacy",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "26.04"},
			files:      []string{fixtureResolved, fixtureResolvedModule},
			wantModule: "systemd-networkd systemd-resolved",
			wantSysext: true,
		},
		{
			// Bug: kairos-io/kairos#835.
			name:       "opensuse leap skips resolved",
			sis:        values.System{Distro: values.OpenSUSELeap, Family: values.SUSEFamily, Version: "15.6"},
			files:      []string{fixtureResolved, fixtureResolvedModule},
			wantModule: "systemd-networkd network-legacy",
			wantSysext: true,
		},
		{
			// Bug: kairos-io/kairos#835.
			name:       "debian skips resolved",
			sis:        values.System{Distro: values.Debian, Family: values.DebianFamily, Version: "13"},
			files:      []string{fixtureResolved, fixtureResolvedModule},
			wantModule: "systemd-networkd network-legacy",
			wantSysext: true,
		},
		{
			name:       "rhel 9 uses NetworkManager",
			sis:        values.System{Distro: values.RedHat, Family: values.RedHatFamily, Version: "9.5"},
			files:      []string{fixtureNetworkManager, fixtureResolved, fixtureResolvedModule},
			wantModule: "network-manager",
			wantSysext: true,
		},
		{
			name:       "rocky 9 without NetworkManager falls back to network-legacy",
			sis:        values.System{Distro: values.RockyLinux, Family: values.RedHatFamily, Version: "9.5"},
			files:      []string{fixtureResolved, fixtureResolvedModule},
			wantModule: "network-legacy",
			wantSysext: true,
		},
		{
			name:       "rhel 8 disables sysext",
			sis:        values.System{Distro: values.RedHat, Family: values.RedHatFamily, Version: "8.10"},
			files:      []string{fixtureNetworkManager},
			wantModule: "network-manager",
			wantSysext: false,
		},
		{
			name:       "fedora with networkd adds resolved",
			sis:        values.System{Distro: values.Fedora, Family: values.RedHatFamily, Version: "42"},
			files:      []string{fixtureNetworkd, fixtureResolved, fixtureResolvedModule},
			wantModule: "systemd-networkd systemd-resolved",
			wantSysext: true,
		},
		{
			name:       "fedora with networkd and no resolved dracut module",
			sis:        values.System{Distro: values.Fedora, Family: values.RedHatFamily, Version: "42"},
			files:      []string{fixtureNetworkd, fixtureResolved},
			wantModule: "systemd-networkd systemd-resolved",
			wantSysext: true,
		},
		{
			name:       "hadron uses networkd and resolved",
			sis:        values.System{Distro: values.Hadron, Family: values.HadronFamily, Version: "1.0"},
			wantModule: "systemd-networkd systemd-resolved",
			wantSysext: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			module, sysext, err := dracutNetworkModules(buildRoot(t, tt.files...), tt.sis, log)
			if err != nil {
				t.Fatalf("dracutNetworkModules() returned an error: %s", err)
			}
			if module != tt.wantModule {
				t.Errorf("dracutNetworkModules() module = %q, want %q", module, tt.wantModule)
			}
			if sysext != tt.wantSysext {
				t.Errorf("dracutNetworkModules() sysext = %t, want %t", sysext, tt.wantSysext)
			}
		})
	}
}
