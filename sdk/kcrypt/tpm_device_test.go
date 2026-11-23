package kcrypt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

func setTPMDeviceForTest(t *testing.T, devicePath, cmdline string, pollInterval time.Duration) {
	t.Helper()
	cmdlinePath := filepath.Join(t.TempDir(), "cmdline")
	if err := os.WriteFile(cmdlinePath, []byte(cmdline), 0o600); err != nil {
		t.Fatal(err)
	}

	oldDevicePath, oldCmdlinePath, oldPollInterval := tpmDevicePath, tpmCmdlinePath, tpmDevicePollInterval
	tpmDevicePath = devicePath
	tpmCmdlinePath = cmdlinePath
	tpmDevicePollInterval = pollInterval
	t.Cleanup(func() {
		tpmDevicePath = oldDevicePath
		tpmCmdlinePath = oldCmdlinePath
		tpmDevicePollInterval = oldPollInterval
	})
}

func TestTPMDeviceRetryTimeout(t *testing.T) {
	tests := []struct {
		name    string
		cmdline string
		want    time.Duration
	}{
		{name: "no retry by default", cmdline: "root=LABEL=COS_ACTIVE", want: 0},
		{name: "positive retry window", cmdline: "quiet rd.immucore.retry-tpm=12", want: 12 * time.Second},
		{name: "zero disables retry", cmdline: "rd.immucore.retry-tpm=0", want: 0},
		{name: "invalid disables retry", cmdline: "rd.immucore.retry-tpm=forever", want: 0},
		{name: "prefix is not a match", cmdline: "rd.immucore.retry-tpm-extra=12", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tpmDeviceRetryTimeout(tt.cmdline); got != tt.want {
				t.Fatalf("tpmDeviceRetryTimeout(%q) = %s, want %s", tt.cmdline, got, tt.want)
			}
		})
	}
}

func TestValidateTPMDeviceFailsImmediatelyWithoutRetry(t *testing.T) {
	device := filepath.Join(t.TempDir(), "missing-tpmrm0")
	setTPMDeviceForTest(t, device, "root=LABEL=COS_ACTIVE quiet", time.Millisecond)

	start := time.Now()
	err := validateTPMDevice(logger.NewNullLogger())
	if err == nil {
		t.Fatal("validateTPMDevice() returned nil, but the TPM device does not exist")
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Errorf("validateTPMDevice waited %s without retry-tpm being set", elapsed)
	}
}

func TestValidateTPMDeviceRetriesWhenOptedIn(t *testing.T) {
	device := filepath.Join(t.TempDir(), "tpmrm0")
	setTPMDeviceForTest(t, device, "rd.immucore.retry-tpm=2", 10*time.Millisecond)

	const appearsAfter = 40 * time.Millisecond
	go func() {
		time.Sleep(appearsAfter)
		_ = os.WriteFile(device, nil, 0o600)
	}()

	start := time.Now()
	err := validateTPMDevice(logger.NewNullLogger())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("validateTPMDevice returned after %s with error %v, want nil once the device appears", elapsed, err)
	}
	if elapsed < appearsAfter {
		t.Errorf("validateTPMDevice returned after %s, before the device appeared", elapsed)
	}
}

func TestValidateTPMDeviceReturnsImmediatelyWhenPresent(t *testing.T) {
	device := filepath.Join(t.TempDir(), "tpmrm0")
	if err := os.WriteFile(device, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	setTPMDeviceForTest(t, device, "rd.immucore.retry-tpm=10", time.Second)

	start := time.Now()
	if err := validateTPMDevice(logger.NewNullLogger()); err != nil {
		t.Fatalf("validateTPMDevice() returned error %v, but the device exists", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("validateTPMDevice returned after %s, want it to return at once when the device exists", elapsed)
	}
}
