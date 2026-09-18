package stages

import (
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
