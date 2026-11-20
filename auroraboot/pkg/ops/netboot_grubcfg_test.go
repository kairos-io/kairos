package ops

import (
	"path/filepath"
	"testing"
)

func TestNetbootGrubCfgPath(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{
			name:   "an empty prefix falls back to the default the extraction uses",
			prefix: "",
			want:   "kairos-grub.cfg",
		},
		{
			name:   "a configured prefix names the file",
			prefix: "custom",
			want:   "custom-grub.cfg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := filepath.Join("/state/netboot", tt.want)
			if got := NetbootGrubCfgPath("/state/netboot", tt.prefix); got != want {
				t.Errorf("NetbootGrubCfgPath: got %q, want %q", got, want)
			}
		})
	}
}
