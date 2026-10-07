package stages

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/schema"
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

// TestInitramfsDracutConfigsCoverHadron pins the OS filter on the dracut
// configuration files that GetKairosInitramfsFilesStage drops into
// /etc/dracut.conf.d.
//
// yip matches only_os against os-release's PRETTY_NAME, which is "Hadron Linux"
// on the hadron base image, so a filter that does not name Hadron silently
// skips the stage on the flavor Kairos builds by default.
func TestInitramfsDracutConfigsCoverHadron(t *testing.T) {
	const hadronPrettyName = "Hadron Linux"

	want := []string{
		"Add pmem modules to initramfs",
		"Add xhci_pci_renesas module to initramfs",
		"Add sysext module to initramfs",
		"Add network module to initramfs",
		"Add immucore module to initramfs",
	}

	stages, err := GetKairosInitramfsFilesStage(values.System{
		Name:    "Hadron Linux",
		Distro:  values.Hadron,
		Family:  values.HadronFamily,
		Version: "0.5.1",
		Arch:    values.ArchAMD64,
	}, logger.NewKairosLogger("test", "error", true))
	if err != nil {
		t.Fatalf("GetKairosInitramfsFilesStage() returned an error: %s", err)
	}

	byName := map[string]schema.Stage{}
	for _, stage := range stages {
		byName[stage.Name] = stage
	}

	for _, name := range want {
		stage, found := byName[name]
		if !found {
			t.Fatalf("stage %q is gone; update this test with its new name", name)
		}

		matched, err := regexp.MatchString(stage.OnlyIfOs, hadronPrettyName)
		if err != nil {
			t.Fatalf("stage %q has an only_os that does not compile: %s", name, err)
		}
		if !matched {
			t.Errorf("stage %q does not run on Hadron: only_os %q does not match %q", name, stage.OnlyIfOs, hadronPrettyName)
		}
	}
}

// TestPmemStageKeepsTheOtherFlavors guards the other half of the same filter:
// pmem support is what makes HTTP EFI boot find the served ISO, and it is
// needed on every flavor that boots that way, not only on Hadron.
func TestPmemStageKeepsTheOtherFlavors(t *testing.T) {
	prettyNames := []string{
		"Ubuntu 24.04.1 LTS",
		"Debian GNU/Linux 12 (bookworm)",
		"Fedora Linux 40 (Container Image)",
		"Rocky Linux 9.4 (Blue Onyx)",
		"AlmaLinux 9.4 (Seafoam Ocelot)",
		"Red Hat Enterprise Linux 9.4 (Plow)",
		"Oracle Linux Server 9.4",
		"openSUSE Leap 15.6",
	}

	stages, err := GetKairosInitramfsFilesStage(values.System{
		Name:    "Ubuntu 24.04.1 LTS",
		Distro:  values.Ubuntu,
		Family:  values.DebianFamily,
		Version: "24.04",
		Arch:    values.ArchAMD64,
	}, logger.NewKairosLogger("test", "error", true))
	if err != nil {
		t.Fatalf("GetKairosInitramfsFilesStage() returned an error: %s", err)
	}

	var onlyIfOs string
	for _, stage := range stages {
		if stage.Name == "Add pmem modules to initramfs" {
			onlyIfOs = stage.OnlyIfOs
		}
	}
	if onlyIfOs == "" {
		t.Fatal(`no stage named "Add pmem modules to initramfs" with an only_os filter`)
	}

	for _, prettyName := range prettyNames {
		matched, err := regexp.MatchString(onlyIfOs, prettyName)
		if err != nil {
			t.Fatalf("only_os %q does not compile: %s", onlyIfOs, err)
		}
		if !matched {
			t.Errorf("only_os %q does not match %q", onlyIfOs, prettyName)
		}
	}
}

// Paths a fabricated build root can contain, to drive the daemon and dracut
// module probes in dracutNetworkModules.
const (
	fixtureNetworkManager = "usr/sbin/NetworkManager"
	fixtureNetworkd       = "usr/lib/systemd/systemd-networkd"
	fixtureResolved       = "usr/lib/systemd/systemd-resolved"
	fixtureResolvectl     = "usr/bin/resolvectl"
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

// TestResolvedModuleAvailable exercises resolvedModuleAvailable directly, over
// the combinations of "resolved daemon present" x "resolvectl present" x
// "dracut module present". The dracutNetworkModules table below only ever
// drives this through the full selection and never covers the partial roots,
// so it is worth pinning on its own: all three have to be there, and a root
// missing any one of them must not get the module. Asking dracut for a module
// whose check() fails aborts the initramfs build outright.
func TestResolvedModuleAvailable(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  bool
	}{
		{
			name:  "daemon, resolvectl and module all present",
			files: []string{fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			want:  true,
		},
		{
			name:  "daemon and resolvectl present, module missing",
			files: []string{fixtureResolved, fixtureResolvectl},
			want:  false,
		},
		{
			name:  "daemon and module present, resolvectl missing",
			files: []string{fixtureResolved, fixtureResolvedModule},
			want:  false,
		},
		{
			name:  "resolvectl and module present, daemon missing",
			files: []string{fixtureResolvectl, fixtureResolvedModule},
			want:  false,
		},
		{
			name:  "module present, both binaries missing",
			files: []string{fixtureResolvedModule},
			want:  false,
		},
		{
			name:  "nothing present",
			files: nil,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvedModuleAvailable(buildRoot(t, tt.files...)); got != tt.want {
				t.Errorf("resolvedModuleAvailable() = %t, want %t", got, tt.want)
			}
		})
	}
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
			// No jammy package ships the systemd-resolved dracut module: the
			// release is stuck on dracut 051 and the module landed in 054. This
			// row drives the selection logic against a backported or vendored
			// dracut, not a stock 22.04 build root.
			name:       "ubuntu 22.04 with a dracut that ships the module adds resolved",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "22.04"},
			files:      []string{fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			wantModule: "systemd-networkd network-legacy systemd-resolved",
			wantSysext: false,
		},
		{
			// What jammy actually looks like: the daemon and resolvectl are
			// there, dracut 051 has no systemd-resolved module to ask for.
			name:       "ubuntu 22.04 without the resolved dracut module",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "22.04"},
			files:      []string{fixtureResolved, fixtureResolvectl},
			wantModule: "systemd-networkd network-legacy",
			wantSysext: false,
		},
		{
			// The plain network module writes /etc/resolv.conf itself, so
			// resolved has nothing to do here.
			name:       "ubuntu 20.04 uses the plain network module",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "20.04"},
			files:      []string{fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			wantModule: "network",
			wantSysext: false,
		},
		{
			name:       "ubuntu 24.04 adds resolved",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "24.04"},
			files:      []string{fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			wantModule: "systemd-networkd network-legacy systemd-resolved",
			wantSysext: true,
		},
		{
			// 24.04 ships resolved, but a build root that lost the dracut
			// module still cannot be asked for it.
			name:       "ubuntu 24.04 without the resolved dracut module",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "24.04"},
			files:      []string{fixtureResolved, fixtureResolvectl},
			wantModule: "systemd-networkd network-legacy",
			wantSysext: true,
		},
		{
			name:       "ubuntu 26.04 drops network-legacy",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "26.04"},
			files:      []string{fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			wantModule: "systemd-networkd systemd-resolved",
			wantSysext: true,
		},
		{
			name:       "ubuntu 26.04 without resolvectl",
			sis:        values.System{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "26.04"},
			files:      []string{fixtureResolved, fixtureResolvedModule},
			wantModule: "systemd-networkd",
			wantSysext: true,
		},
		{
			name:       "opensuse leap adds resolved",
			sis:        values.System{Distro: values.OpenSUSELeap, Family: values.SUSEFamily, Version: "15.6"},
			files:      []string{fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			wantModule: "systemd-networkd network-legacy systemd-resolved",
			wantSysext: true,
		},
		{
			name:       "opensuse leap without resolved installed",
			sis:        values.System{Distro: values.OpenSUSELeap, Family: values.SUSEFamily, Version: "15.6"},
			wantModule: "systemd-networkd network-legacy",
			wantSysext: true,
		},
		{
			name:       "debian adds resolved",
			sis:        values.System{Distro: values.Debian, Family: values.DebianFamily, Version: "13"},
			files:      []string{fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			wantModule: "systemd-networkd network-legacy systemd-resolved",
			wantSysext: true,
		},
		{
			// The module's check() wants resolvectl too, and dracut fails the
			// build outright when a module it was told to add cannot install
			// itself, so a root without it has to be left alone.
			name:       "debian without resolvectl",
			sis:        values.System{Distro: values.Debian, Family: values.DebianFamily, Version: "13"},
			files:      []string{fixtureResolved, fixtureResolvedModule},
			wantModule: "systemd-networkd network-legacy",
			wantSysext: true,
		},
		{
			// NetworkManager and the legacy network modules write
			// /etc/resolv.conf themselves, so they get no resolved.
			name:       "rhel 9 uses NetworkManager",
			sis:        values.System{Distro: values.RedHat, Family: values.RedHatFamily, Version: "9.5"},
			files:      []string{fixtureNetworkManager, fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			wantModule: "network-manager",
			wantSysext: true,
		},
		{
			name:       "rocky 9 without NetworkManager falls back to network-legacy",
			sis:        values.System{Distro: values.RockyLinux, Family: values.RedHatFamily, Version: "9.5"},
			files:      []string{fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
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
			files:      []string{fixtureNetworkd, fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			wantModule: "systemd-networkd systemd-resolved",
			wantSysext: true,
		},
		{
			// Asking dracut for a module it does not ship fails the initramfs
			// build, so the daemon alone is not enough.
			name:       "fedora with networkd and no resolved dracut module",
			sis:        values.System{Distro: values.Fedora, Family: values.RedHatFamily, Version: "42"},
			files:      []string{fixtureNetworkd, fixtureResolved, fixtureResolvectl},
			wantModule: "systemd-networkd",
			wantSysext: true,
		},
		{
			name:       "hadron uses networkd and resolved",
			sis:        values.System{Distro: values.Hadron, Family: values.HadronFamily, Version: "1.0"},
			files:      []string{fixtureResolved, fixtureResolvectl, fixtureResolvedModule},
			wantModule: "systemd-networkd systemd-resolved",
			wantSysext: true,
		},
		{
			name:       "hadron without the resolved dracut module",
			sis:        values.System{Distro: values.Hadron, Family: values.HadronFamily, Version: "1.0"},
			files:      []string{fixtureResolved, fixtureResolvectl},
			wantModule: "systemd-networkd",
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
