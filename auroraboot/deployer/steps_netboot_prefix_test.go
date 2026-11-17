package deployer

import (
	"path/filepath"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/schema"
)

// StepExtractNetboot names the three netboot artifacts after iso.name, and
// StepStartNetboot used to hand the server the kairos- names whatever iso.name
// said. A build with iso.name set therefore extracted <name>-kernel and then
// served kairos-kernel, which is not on disk, so the netboot server advertised
// three files that were never there.
//
// The expected names are literals rather than a second call to the helper the
// code uses, so that a rename of the artifacts has to be made on both sides
// before this passes again.
func TestNetbootArtifactNamesFollowISOName(t *testing.T) {
	tests := []struct {
		name                     string
		isoName                  string
		squashFS, kernel, initrd string
	}{
		{
			name:     "iso.name unset keeps the historical names",
			isoName:  "",
			squashFS: "kairos.squashfs",
			kernel:   "kairos-kernel",
			initrd:   "kairos-initrd",
		},
		{
			name:     "iso.name set moves the server with the extraction",
			isoName:  "custom",
			squashFS: "custom.squashfs",
			kernel:   "custom-kernel",
			initrd:   "custom-initrd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deployer{Config: schema.Config{
				State: "/state",
				ISO:   schema.ISO{Name: tt.isoName},
			}}

			netbootDir := d.dstNetboot()

			for _, c := range []struct {
				what, got, want string
			}{
				{"squashfs", d.squashFSfile(), filepath.Join(netbootDir, tt.squashFS)},
				{"kernel", d.kernelFile(), filepath.Join(netbootDir, tt.kernel)},
				{"initrd", d.initrdFile(), filepath.Join(netbootDir, tt.initrd)},
			} {
				if c.got != c.want {
					t.Errorf("%s: got %q, want %q", c.what, c.got, c.want)
				}
			}
		})
	}
}
