package kcrypt

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// A failed TPM unlock is followed by the passphrase prompt on the console. The
// attach output used to be part of the error, so the warning printed above the
// prompt was a whole screen of systemd-cryptsetup debug lines.
func TestTPMAttachErrorKeepsTheOutputInTheDebugLog(t *testing.T) {
	out := "Requesting JSON for token 0.\nTPM policy does not match current system state, skipping."
	var buf bytes.Buffer

	err := tpmAttachError(sdkLogger.NewBufferLogger(&buf), "COS_OEM", out, errors.New("exit status 1"))

	if got, want := err.Error(), "TPM unlock failed: exit status 1"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if !strings.Contains(buf.String(), `"level":"debug"`) || !strings.Contains(buf.String(), "TPM policy does not match") {
		t.Errorf("debug log does not hold the attach output: %s", buf.String())
	}
}
