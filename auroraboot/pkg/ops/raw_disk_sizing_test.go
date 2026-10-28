package ops

import "testing"

func TestRecoveryImageSize(t *testing.T) {
	tests := []struct {
		name       string
		sourceSize uint64
		configured int64
		want       uint
	}{
		{name: "automatic adds headroom", sourceSize: 7_774, want: 7_974},
		{name: "configured override", sourceSize: 7_774, configured: 12_000, want: 12_000},
		{name: "negative override uses automatic sizing", sourceSize: 7_774, configured: -1, want: 7_974},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := recoveryImageSize(tt.sourceSize, tt.configured)
			if err != nil {
				t.Fatalf("recoveryImageSize(%d, %d) returned error: %v", tt.sourceSize, tt.configured, err)
			}
			if got != tt.want {
				t.Fatalf("recoveryImageSize(%d, %d) = %d, want %d", tt.sourceSize, tt.configured, got, tt.want)
			}
		})
	}
}

func TestRecoveryImageSizeRejectsOverrideBelowSource(t *testing.T) {
	const sourceSize = uint64(7_774)
	const configuredSize = int64(7_000)

	if _, err := recoveryImageSize(sourceSize, configuredSize); err == nil {
		t.Fatalf("recoveryImageSize(%d, %d) returned no error", sourceSize, configuredSize)
	}
}

func TestRecoveryPartitionSizeTracksRecoveryImage(t *testing.T) {
	const recoveryImageSize = uint(12_000)
	const want = uint(24_150)

	if got := recoveryPartitionSize(recoveryImageSize); got != want {
		t.Fatalf("recoveryPartitionSize(%d) = %d, want %d", recoveryImageSize, got, want)
	}
}
