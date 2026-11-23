package kcrypt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// setTPMDeviceForTest points the TPM check at path and restores the
// package settings when the test ends.
func setTPMDeviceForTest(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	oldPath, oldTimeout := tpmDevicePath, tpmDeviceWaitTimeout
	tpmDevicePath = path
	tpmDeviceWaitTimeout = timeout
	t.Cleanup(func() {
		tpmDevicePath = oldPath
		tpmDeviceWaitTimeout = oldTimeout
	})
}

// The TPM character device can appear seconds after the preflight runs, on
// boards whose TPM driver probes slowly. validateTPMDevice must wait for it
// rather than fail on the first look.
func TestValidateTPMDeviceWaitsForSlowDevice(t *testing.T) {
	device := filepath.Join(t.TempDir(), "tpmrm0")
	setTPMDeviceForTest(t, device, 10*time.Second)

	const appearsAfter = 1500 * time.Millisecond
	go func() {
		time.Sleep(appearsAfter)
		_ = os.WriteFile(device, nil, 0o600)
	}()

	start := time.Now()
	err := validateTPMDevice(logger.NewNullLogger())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("validateTPMDevice returned after %s with error %v, want nil once the device appears at %s", elapsed, err, appearsAfter)
	}
	if elapsed < appearsAfter {
		t.Errorf("validateTPMDevice returned after %s, before the device appeared at %s", elapsed, appearsAfter)
	}
	if elapsed > 5*time.Second {
		t.Errorf("validateTPMDevice returned after %s, want it to return soon after the device appears", elapsed)
	}
}

// When the device is already present, the check must not wait at all.
func TestValidateTPMDeviceReturnsImmediatelyWhenPresent(t *testing.T) {
	device := filepath.Join(t.TempDir(), "tpmrm0")
	if err := os.WriteFile(device, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	setTPMDeviceForTest(t, device, 10*time.Second)

	start := time.Now()
	if err := validateTPMDevice(logger.NewNullLogger()); err != nil {
		t.Fatalf("validateTPMDevice() returned error %v, but the device exists", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("validateTPMDevice returned after %s, want it to return at once when the device exists", elapsed)
	}
}

// When no TPM ever appears, the check must fail once its bounded wait ends.
func TestValidateTPMDeviceFailsAfterWaitWhenMissing(t *testing.T) {
	const timeout = 1 * time.Second
	device := filepath.Join(t.TempDir(), "tpmrm0")
	setTPMDeviceForTest(t, device, timeout)

	start := time.Now()
	err := validateTPMDevice(logger.NewNullLogger())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("validateTPMDevice() returned nil, but %s does not exist", device)
	}
	if elapsed < timeout {
		t.Errorf("validateTPMDevice returned after %s, want it to poll for the device for %s before failing", elapsed, timeout)
	}
}
