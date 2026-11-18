package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
)

func TestResolveProbeOutput(t *testing.T) {
	tests := []struct {
		flag    string
		want    string
		wantErr bool
	}{
		{"", "both", false},
		{"both", "both", false},
		{"text", "text", false},
		{"yaml", "yaml", false},
		{"YAML", "yaml", false},
		{"  text  ", "text", false},
		{"json", "", true},
	}
	for _, tt := range tests {
		got, err := resolveProbeOutput(tt.flag)
		if tt.wantErr {
			if err == nil {
				t.Errorf("resolveProbeOutput(%q): expected error, got nil", tt.flag)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveProbeOutput(%q): unexpected error %v", tt.flag, err)
		}
		if got != tt.want {
			t.Errorf("resolveProbeOutput(%q) = %q, want %q", tt.flag, got, tt.want)
		}
	}
}

// sampleReport is a manager-hosted-CD probe report (the iLO shape) used to exercise
// the rendering seam: it yields a starter profile WITH a mediaSearch order.
func sampleReport() *redfish.ProbeReport {
	return &redfish.ProbeReport{
		Endpoint:          "https://bmc.example",
		HasSessionService: true,
		AuthModeUsed:      "session",
		SystemIDs:         []string{"sys-1"},
		SelectedSystemID:  "sys-1",
		System: redfish.SystemInfo{
			ID:             "sys-1",
			Manufacturer:   "HPE",
			Model:          "ProLiant DL360",
			SerialNumber:   "SN-1",
			MemoryGiB:      64,
			ProcessorCount: 8,
			Features:       map[string]bool{redfish.FeatureUEFI: true},
		},
		FirmwareVersion: "iLO 5 v2.44",
		Media: []redfish.MediaView{
			{Index: 0, ID: "Cd", Location: "manager:mgr-1", MediaTypes: []string{"CD", "DVD"}},
		},
		DefaultCDIndex:      0,
		ManagerHostedCDOnly: true,
		PowerState:          "On",
		AllowableResetTypes: []string{"On", "ForceRestart", "GracefulRestart"},
		DefaultResetType:    "ForceRestart",
	}
}

func TestRenderProbeReportYAMLOnlyParses(t *testing.T) {
	var buf bytes.Buffer
	renderProbeReport(&buf, sampleReport(), "yaml")

	out := buf.String()
	if strings.Contains(out, "RedFish probe:") {
		t.Fatalf("yaml-only output must not contain the human report header:\n%s", out)
	}

	profile, err := redfish.ParseProfile(buf.Bytes())
	if err != nil {
		t.Fatalf("yaml-only output must parse as a profile: %v\n---\n%s", err, out)
	}
	if profile.Name != "hpe" {
		t.Errorf("profile name = %q, want %q", profile.Name, "hpe")
	}
	if profile.MediaSearch == nil {
		t.Fatalf("manager-hosted media must yield a mediaSearch")
	}
	if got := strings.Join(profile.MediaSearch.Order, ","); got != "manager,system" {
		t.Errorf("mediaSearch.order = %q, want %q", got, "manager,system")
	}
	if profile.ValidatedFirmware != "iLO 5 v2.44" {
		t.Errorf("validatedFirmware = %q, want %q", profile.ValidatedFirmware, "iLO 5 v2.44")
	}
}

func TestRenderProbeReportBothHasTextAndProfile(t *testing.T) {
	var buf bytes.Buffer
	renderProbeReport(&buf, sampleReport(), "both")

	out := buf.String()
	for _, want := range []string{
		"RedFish probe: https://bmc.example",
		"member Ids: sys-1",
		"manufacturer: HPE",
		"firmware:     iLO 5 v2.44",
		"only CD/DVD media is Manager-hosted",
		"suggested profile (tier C",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("both output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderProbeReportTextOnlyHasNoProfile(t *testing.T) {
	var buf bytes.Buffer
	renderProbeReport(&buf, sampleReport(), "text")

	out := buf.String()
	if !strings.Contains(out, "RedFish probe:") {
		t.Errorf("text output must contain the human report")
	}
	if strings.Contains(out, "name: hpe") {
		t.Errorf("text-only output must not contain the starter profile body")
	}
}
