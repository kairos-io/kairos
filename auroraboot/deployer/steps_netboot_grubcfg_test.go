package deployer

import (
	"path/filepath"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/schema"
)

// StepExtractNetboot writes the livecd grub config as <iso.name>-grub.cfg, and
// StepStartNetboot used to hand the server the literal kairos-grub.cfg. A build
// that sets iso.name therefore extracted <name>-grub.cfg and then read back a
// file that was never written, so the netboot cmdline silently fell back to the
// default instead of the one the ISO boots with.
//
// The expected names are literals rather than a second call to the helper the
// code uses, so a rename of the artifact has to be made on both sides before
// this passes again.
func TestNetbootGrubCfgNameFollowsISOName(t *testing.T) {
	tests := []struct {
		name    string
		isoName string
		want    string
	}{
		{
			name:    "iso.name unset keeps the historical name",
			isoName: "",
			want:    "kairos-grub.cfg",
		},
		{
			name:    "iso.name set moves the reader with the extraction",
			isoName: "custom",
			want:    "custom-grub.cfg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deployer{Config: schema.Config{
				State: "/state",
				ISO:   schema.ISO{Name: tt.isoName},
			}}

			want := filepath.Join(d.dstNetboot(), tt.want)
			if got := d.netbootGrubCfgFile(); got != want {
				t.Errorf("netbootGrubCfgFile: got %q, want %q", got, want)
			}
		})
	}
}
