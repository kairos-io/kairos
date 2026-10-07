package config

import (
	"bytes"
	"strings"
	"testing"

	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/spf13/viper"
)

// subFor builds the same *viper.Viper that unmarshallFullSpec hands to
// warnDeprecatedKeys, so the test covers viper's own key handling too.
func subFor(t *testing.T, cc, subkey string) *viper.Viper {
	t.Helper()
	viper.Reset()
	viper.SetConfigType("yaml")
	if err := viper.ReadConfig(strings.NewReader(cc)); err != nil {
		t.Fatalf("reading cloud config: %v", err)
	}
	vp := viper.Sub(subkey)
	if vp == nil {
		vp = viper.New()
	}
	return vp
}

func TestWarnDeprecatedKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		cc   string
		warn bool
	}{
		{"deprecated no_format warns", "install:\n  no_format: true\n", true},
		{"deprecated key warns even when false", "install:\n  no_format: false\n", true},
		{"supported no-format stays quiet", "install:\n  no-format: true\n", false},
		{"empty install block stays quiet", "install: {}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := sdkLogger.NewBufferLogger(&buf)
			logger.SetLevel("warn")

			warnDeprecatedKeys(logger, "install", subFor(t, tc.cc, "install"))

			got := buf.String()
			if tc.warn {
				if !strings.Contains(got, "install.no_format") || !strings.Contains(got, "install.no-format") {
					t.Fatalf("want a warning naming both keys, got %q", got)
				}
			} else if strings.Contains(got, "no_format") {
				t.Fatalf("want no warning, got %q", got)
			}
		})
	}
}

// The upgrade block has no deprecated keys, so it must never warn.
func TestWarnDeprecatedKeysIgnoresOtherBlocks(t *testing.T) {
	var buf bytes.Buffer
	logger := sdkLogger.NewBufferLogger(&buf)
	logger.SetLevel("warn")

	warnDeprecatedKeys(logger, "upgrade", subFor(t, "upgrade:\n  no_format: true\n", "upgrade"))

	if got := buf.String(); strings.Contains(got, "no_format") {
		t.Fatalf("want no warning for the upgrade block, got %q", got)
	}
}
