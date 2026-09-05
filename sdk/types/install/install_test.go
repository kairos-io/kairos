package install

import (
	"testing"

	"github.com/go-viper/mapstructure/v2"
)

// TestPassiveDoesNotAliasRecovery guards the mapstructure tag on the Passive
// field. It used to be "recovery-system", the same tag Recovery already owned,
// so a config setting install.passive alone was dropped and setting
// install.recovery-system leaked into both fields.
func TestPassiveDoesNotAliasRecovery(t *testing.T) {
	src := map[string]interface{}{
		"passive": map[string]interface{}{
			"size": 1234,
		},
		"recovery-system": map[string]interface{}{
			"size": 5678,
		},
	}
	var got Install
	if err := mapstructure.Decode(src, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Passive.Size != 1234 {
		t.Errorf("passive.size: want 1234, got %d", got.Passive.Size)
	}
	if got.Recovery.Size != 5678 {
		t.Errorf("recovery-system.size: want 5678, got %d", got.Recovery.Size)
	}
}

// TestNoFormatDecodesFromKebabCase pins the "no-format" spelling the runtime
// InstallSpec reads. Any drift to "no_format" would leave install.no-format
// silently ignored.
func TestNoFormatDecodesFromKebabCase(t *testing.T) {
	var got Install
	if err := mapstructure.Decode(map[string]interface{}{"no-format": true}, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.NoFormat {
		t.Error("install.no-format: want true, got false")
	}
}
