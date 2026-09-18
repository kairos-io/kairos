package utils

import (
	"fmt"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/ghw"
	"github.com/kairos-io/kairos/v4/sdk/state"
)

const (
	// DefaultDeviceEnumerationTimeout bounds how long immucore waits for udev
	// to publish the partition holding this boot's images. It is deliberately
	// shorter than the 180s systemd-udev-settle.service used to spend: a disk
	// that has not shown up in half a minute is not going to, and the steps
	// that follow report a far better error than a silent three-minute stall.
	DefaultDeviceEnumerationTimeout = 30 * time.Second

	// deviceEnumerationPollInterval is how often the scan is repeated while
	// waiting. GetState() already polls labels at 1s, so match it.
	deviceEnumerationPollInterval = 1 * time.Second
)

// WaitForBootDevices blocks until the partition that holds this boot's images
// is visible to a block device scan, or until timeout elapses.
//
// immucore used to get this guarantee from the initramfs, by ordering itself
// after systemd-udev-settle.service. That unit is deprecated upstream and
// dracut installs it only optionally, so the guarantee can disappear from an
// image without anything in Kairos changing (kairos-io/kairos#1378). Losing it
// is not a loud failure: GetOemLabel() falls back to a block device scan when
// the cmdline names no label and returns "" when the scan comes up empty, so
// OEM is then silently not mounted, and oemEncrypted() reads the same empty
// scan as "OEM is not encrypted" and wires the DAG to mount a LUKS container
// as if it were ext4.
//
// Waiting here, in immucore itself, is what lets the unit drop the dependency.
//
// A timeout is not fatal. Every step that needs a device still does its own
// error reporting, and those messages name the device and the operation;
// aborting the boot here would replace them with a worse one.
func WaitForBootDevices(timeout time.Duration) error {
	runtime, err := state.NewRuntimeWithLogger(KLog.Logger)
	if err != nil {
		KLog.Logger.Debug().Err(err).Msg("Could not read the runtime, not waiting for device enumeration")
		return nil
	}
	label := bootStateToImagesLabel(runtime.BootState)
	if label == "" {
		// LiveCD and Unknown have no images partition to wait for.
		return nil
	}
	return WaitForLabel(label, timeout)
}

// WaitForLabel blocks until a partition carrying the given filesystem or
// partition label is visible to a block device scan, or until timeout elapses.
func WaitForLabel(label string, timeout time.Duration) error {
	return waitForLabel(label, timeout, deviceEnumerationPollInterval, labelEnumerated)
}

// labelEnumerated reports whether any partition currently carries label as its
// filesystem label or its GPT partition label. It uses the kairos-sdk ghw for
// the same reason GetOemLabel does: it honors GHW_CHROOT, so tests can mock the
// device tree.
func labelEnumerated(label string) bool {
	for _, disk := range ghw.GetDisks(ghw.NewPaths(""), &KLog) {
		for _, p := range disk.Partitions {
			if p.FilesystemLabel == label || p.PartitionLabel == label {
				return true
			}
		}
	}
	return false
}

// waitForLabel is the testable core of WaitForLabel. The device scan is always
// attempted once before the deadline is consulted, so a zero timeout still
// answers correctly for a device that is already there.
func waitForLabel(label string, timeout, interval time.Duration, present func(string) bool) error {
	deadline := time.Now().Add(timeout)
	for attempt := 1; ; attempt++ {
		if present(label) {
			if attempt > 1 {
				KLog.Logger.Info().Str("label", label).Int("attempts", attempt).
					Msg("Waited for the device to be enumerated")
			}
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("label %q was not enumerated after %s", label, timeout)
		}
		KLog.Logger.Debug().Str("label", label).Int("attempt", attempt).
			Msg("Device not enumerated yet, retrying")
		time.Sleep(interval)
	}
}
