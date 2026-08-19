package stages

import (
	"testing"

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
