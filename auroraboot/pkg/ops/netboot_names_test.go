package ops

import (
	"path/filepath"
	"testing"
)

// The three names below are the contract between ExtractNetboot, which writes
// the files, and the netboot server, which serves them. They are asserted as
// literals on purpose: a test that re-derives them from the same helper the
// code uses agrees with any rename, including a rename that leaves one of the
// two sides behind.
func TestNetbootArtifactPaths(t *testing.T) {
	tests := []struct {
		name                     string
		prefix                   string
		squashFS, kernel, initrd string
	}{
		{
			name:     "no prefix falls back to the default",
			prefix:   "",
			squashFS: "kairos.squashfs",
			kernel:   "kairos-kernel",
			initrd:   "kairos-initrd",
		},
		{
			name:     "iso.name becomes the artifact base name",
			prefix:   "custom",
			squashFS: "custom.squashfs",
			kernel:   "custom-kernel",
			initrd:   "custom-initrd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := "/artifacts/netboot"
			squashFS, kernel, initrd := NetbootArtifactPaths(dst, tt.prefix)

			for _, c := range []struct{ got, want string }{
				{squashFS, filepath.Join(dst, tt.squashFS)},
				{kernel, filepath.Join(dst, tt.kernel)},
				{initrd, filepath.Join(dst, tt.initrd)},
			} {
				if c.got != c.want {
					t.Errorf("got %q, want %q", c.got, c.want)
				}
			}
		})
	}
}
