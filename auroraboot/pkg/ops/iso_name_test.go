package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/schema"
)

// writeRootfs lays down the one file NameFromRootfs reads, so the derived
// default below is a real derivation and not a stub.
func writeRootfs(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	release := `FLAVOR="ubuntu"
FLAVOR_RELEASE="24.04"
VARIANT="core"
ARCH="amd64"
MODEL="generic"
VERSION="v3.5.0"
`
	if err := os.WriteFile(filepath.Join(root, "etc/kairos-release"), []byte(release), 0644); err != nil {
		t.Fatal(err)
	}

	return root
}

// TestISOBaseNameHonoursISOName pins the precedence GenISO gives the two
// configured names. iso.name is documented as the final artifact base name and
// is what StepExtractNetboot names the netboot artifacts after, so an ISO built
// from the same configuration has to answer to it too.
func TestISOBaseNameHonoursISOName(t *testing.T) {
	rootfs := writeRootfs(t)
	derived := "kairos-ubuntu-24.04-core-amd64-generic-v3.5.0"

	for _, tt := range []struct {
		name string
		iso  schema.ISO
		want string
	}{
		{
			name: "iso.name names the ISO",
			iso:  schema.ISO{Name: "custom"},
			want: "custom",
		},
		{
			name: "override_name beats iso.name",
			iso:  schema.ISO{Name: "custom", OverrideName: "forced"},
			want: "forced",
		},
		{
			name: "override_name alone still wins",
			iso:  schema.ISO{OverrideName: "forced"},
			want: "forced",
		},
		{
			name: "neither set derives from the rootfs",
			iso:  schema.ISO{},
			want: derived,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := isoBaseName(tt.iso, rootfs); got != tt.want {
				t.Errorf("isoBaseName() = %q, want %q", got, tt.want)
			}
		})
	}
}
