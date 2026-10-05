package mcp

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderCloudConfig(t *testing.T) {
	got, err := renderCloudConfig("/dev/sda", "oci:quay.io/kairos/test:v1", FinishReboot, "")
	if err != nil {
		t.Fatalf("renderCloudConfig: %v", err)
	}

	if !strings.HasPrefix(got, "#cloud-config\n") {
		t.Fatalf("no cloud-config header:\n%s", got)
	}

	install := installBlock(t, got)
	if install["device"] != "/dev/sda" {
		t.Errorf("device = %v", install["device"])
	}
	if install["source"] != "oci:quay.io/kairos/test:v1" {
		t.Errorf("source = %v", install["source"])
	}
	if install["reboot"] != true {
		t.Errorf("reboot = %v, want true", install["reboot"])
	}
	if install["poweroff"] == true {
		t.Error("poweroff is set as well as reboot")
	}
}

func TestRenderCloudConfigPoweroff(t *testing.T) {
	got, err := renderCloudConfig("/dev/sda", "", FinishPoweroff, "")
	if err != nil {
		t.Fatalf("renderCloudConfig: %v", err)
	}

	install := installBlock(t, got)
	if install["poweroff"] != true {
		t.Errorf("poweroff = %v, want true", install["poweroff"])
	}
	if install["reboot"] == true {
		t.Error("reboot is set as well as poweroff")
	}
}

// An install with no users would fail the agent's own validation, so say so
// rather than leaving the agent to hit it.
func TestRenderCloudConfigSaysNoUsersWhenNoneWereAskedFor(t *testing.T) {
	got, err := renderCloudConfig("/dev/sda", "", FinishNone, "")
	if err != nil {
		t.Fatalf("renderCloudConfig: %v", err)
	}

	// nousers is the key kairos-agent's own validation reads
	// (agent/pkg/config/config.go), so it is the key that has to be written.
	if install := installBlock(t, got); install["nousers"] != true {
		t.Errorf("nousers is not set:\n%s", got)
	}
}

// The caller's own YAML is what makes this useful: users, SSH keys and yip
// stages are not modelled here and must pass straight through.
func TestRenderCloudConfigMergesTheCallersYAML(t *testing.T) {
	extra := `
stages:
  network:
    - users:
        kairos:
          groups: [admin]
          ssh_authorized_keys: ["github:someone"]
`

	got, err := renderCloudConfig("/dev/sda", "", FinishNone, extra)
	if err != nil {
		t.Fatalf("renderCloudConfig: %v", err)
	}

	if !strings.Contains(got, "github:someone") {
		t.Errorf("the caller's SSH key was dropped:\n%s", got)
	}
	if install := installBlock(t, got); install["device"] != "/dev/sda" {
		t.Errorf("device = %v, want the confirmed device", install["device"])
	}
	// The caller asked for a user, so do not also declare there are none.
	if install := installBlock(t, got); install["nousers"] == true {
		t.Errorf("nousers is set even though the caller created a user:\n%s", got)
	}
}

// The device is the parameter that was confirmed. A cloud_config that names
// another one must not be able to move the install there.
func TestRenderCloudConfigCannotBeRedirectedByTheCallersYAML(t *testing.T) {
	extra := "install:\n  device: /dev/sdb\n  reboot: true\n"

	got, err := renderCloudConfig("/dev/sda", "", FinishNone, extra)
	if err != nil {
		t.Fatalf("renderCloudConfig: %v", err)
	}

	if install := installBlock(t, got); install["device"] != "/dev/sda" {
		t.Fatalf("device = %v, want /dev/sda: the caller's YAML redirected the install", install["device"])
	}
	if strings.Contains(got, "/dev/sdb") {
		t.Errorf("the other device is still in the config:\n%s", got)
	}
	if install := installBlock(t, got); install["reboot"] != false {
		t.Errorf("reboot = %v, want false: the caller's YAML changed the finish action", install["reboot"])
	}
}

// reboot and poweroff are omitempty, so the confirmed finish action renders no
// key at all when it is "none". Unless it is written back explicitly, the
// caller's own value is what the machine ends up obeying.
func TestRenderCloudConfigFinishActionCannotBeOverriddenByTheCallersYAML(t *testing.T) {
	for _, tc := range []struct {
		name             string
		extra            string
		finish           string
		reboot, poweroff bool
	}{
		{
			name:  "poweroff asked for in the YAML, finish action none",
			extra: "install:\n  poweroff: true\n",
		},
		{
			name:  "reboot asked for in the YAML, finish action none",
			extra: "install:\n  reboot: true\n",
		},
		{
			name:     "reboot asked for in the YAML, finish action poweroff",
			extra:    "install:\n  reboot: true\n",
			finish:   FinishPoweroff,
			poweroff: true,
		},
		{
			name:   "poweroff asked for in the YAML, finish action reboot",
			extra:  "install:\n  poweroff: true\n",
			finish: FinishReboot,
			reboot: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finish := tc.finish
			if finish == "" {
				finish = FinishNone
			}

			got, err := renderCloudConfig("/dev/sda", "", finish, tc.extra)
			if err != nil {
				t.Fatalf("renderCloudConfig: %v", err)
			}

			install := installBlock(t, got)
			if install["reboot"] != tc.reboot {
				t.Errorf("reboot = %v, want %v for finish action %q:\n%s",
					install["reboot"], tc.reboot, finish, got)
			}
			if install["poweroff"] != tc.poweroff {
				t.Errorf("poweroff = %v, want %v for finish action %q:\n%s",
					install["poweroff"], tc.poweroff, finish, got)
			}
		})
	}
}

// An omitted source is the one key the caller's YAML may still set: writing an
// empty string in its place would override the source kairos-agent resolves for
// itself. A source that was passed still wins.
func TestRenderCloudConfigSourceOnlyWinsWhenGiven(t *testing.T) {
	extra := "install:\n  source: oci:quay.io/kairos/from-yaml:v1\n"

	got, err := renderCloudConfig("/dev/sda", "", FinishNone, extra)
	if err != nil {
		t.Fatalf("renderCloudConfig: %v", err)
	}
	if install := installBlock(t, got); install["source"] != "oci:quay.io/kairos/from-yaml:v1" {
		t.Errorf("source = %v, want the caller's: an omitted source must not blank it out", install["source"])
	}

	got, err = renderCloudConfig("/dev/sda", "oci:quay.io/kairos/confirmed:v1", FinishNone, extra)
	if err != nil {
		t.Fatalf("renderCloudConfig: %v", err)
	}
	if install := installBlock(t, got); install["source"] != "oci:quay.io/kairos/confirmed:v1" {
		t.Errorf("source = %v, want the source the tool was called with", install["source"])
	}
}

// Install options this tool does not model still have to be settable, or the
// agent cannot do anything the TUI can.
func TestRenderCloudConfigKeepsOtherInstallKeys(t *testing.T) {
	extra := "install:\n  grub-entry-name: Kairos\n  skip-copy-kcrypt: true\n"

	got, err := renderCloudConfig("/dev/sda", "", FinishNone, extra)
	if err != nil {
		t.Fatalf("renderCloudConfig: %v", err)
	}

	install := installBlock(t, got)
	if install["grub-entry-name"] != "Kairos" {
		t.Errorf("grub-entry-name was dropped: %v", install["grub-entry-name"])
	}
	if install["device"] != "/dev/sda" {
		t.Errorf("device = %v, want /dev/sda", install["device"])
	}
}

func TestRenderCloudConfigRejectsBrokenYAML(t *testing.T) {
	if _, err := renderCloudConfig("/dev/sda", "", FinishNone, "\tnot: [valid"); err == nil {
		t.Fatal("expected an error for YAML that does not parse, got none")
	}
}

func installBlock(t *testing.T, doc string) map[string]any {
	t.Helper()

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatalf("the rendered config does not parse: %v\n%s", err, doc)
	}

	install, ok := parsed["install"].(map[string]any)
	if !ok {
		t.Fatalf("no install block in:\n%s", doc)
	}

	return install
}
