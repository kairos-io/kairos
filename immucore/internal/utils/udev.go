package utils

import (
	"fmt"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/constants"
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

	// DefaultOEMEnumerationTimeout bounds the second leg of the wait, the one
	// that covers OEM detection. It is much shorter than the images budget
	// because an installation is allowed to carry no OEM partition at all,
	// and such a boot must not pay the full budget for a partition that is
	// never going to appear. By the time this leg starts, the images
	// partition is already enumerated, so the disk is awake.
	DefaultOEMEnumerationTimeout = 5 * time.Second

	// deviceEnumerationPollInterval is how often the scan is repeated while
	// waiting. GetState() already polls labels at 1s, so match it.
	deviceEnumerationPollInterval = 1 * time.Second
)

// WaitForBootDevices blocks until the partitions this boot has to see are
// visible to a block device scan, or until the budgets below elapse.
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
	return waitForBootDevices(
		state.DetectBoot(KLog.Logger),
		timeout, DefaultOEMEnumerationTimeout, deviceEnumerationPollInterval,
		labelsEnumerated,
	)
}

// waitForBootDevices is the testable core of WaitForBootDevices. It takes the
// boot state rather than detecting it, because detection reads the real
// /proc/cmdline and cannot be mocked.
func waitForBootDevices(boot state.Boot, imagesTimeout, oemTimeout, interval time.Duration, present func(...string) bool) error {
	label := bootStateToImagesLabel(boot)
	if label == "" {
		// LiveCD and Unknown have no images partition to wait for.
		return nil
	}
	if err := waitForLabels([]string{label}, imagesTimeout, interval, present); err != nil {
		return err
	}
	// The images partition is up, so udev has processed this disk. Give OEM
	// its own short budget: it is the detection that fails open, and nothing
	// above makes it visible. An installation with no OEM partition reaches
	// the timeout, which is why this leg is not an error.
	if err := waitForLabels(oemLabelsToWaitFor(), oemTimeout, interval, present); err != nil {
		KLog.Logger.Debug().Err(err).
			Msg("No OEM partition was enumerated; continuing as an installation without one")
	}
	return nil
}

// oemLabelsToWaitFor returns the labels whose appearance settles OEM
// detection for this boot: the one the cmdline names, or every label
// GetOemLabel falls back to scanning for.
func oemLabelsToWaitFor() []string {
	if label := oemLabelFromCmdline(); label != "" {
		return []string{label}
	}
	return []string{constants.OEMLabel, constants.OEMLUKSLabel, constants.OEMPartName}
}

// labelsEnumerated reports whether any partition currently carries one of the
// given labels as its filesystem label or its GPT partition label. It scans
// once for the whole set, so waiting on three OEM labels costs one scan per
// poll and not three. It uses the kairos-sdk ghw for the same reason
// GetOemLabel does: it honors GHW_CHROOT, so tests can mock the device tree.
func labelsEnumerated(labels ...string) bool {
	for _, disk := range ghw.GetDisks(ghw.NewPaths(""), &KLog) {
		for _, p := range disk.Partitions {
			for _, label := range labels {
				if p.FilesystemLabel == label || p.PartitionLabel == label {
					return true
				}
			}
		}
	}
	return false
}

// waitForLabels is the testable core of WaitForLabel. The device scan is
// always attempted once before the deadline is consulted, so a zero timeout
// still answers correctly for a device that is already there.
func waitForLabels(labels []string, timeout, interval time.Duration, present func(...string) bool) error {
	deadline := time.Now().Add(timeout)
	for attempt := 1; ; attempt++ {
		if present(labels...) {
			if attempt > 1 {
				KLog.Logger.Info().Strs("labels", labels).Int("attempts", attempt).
					Msg("Waited for the device to be enumerated")
			}
			return nil
		}
		if !time.Now().Before(deadline) {
			if len(labels) == 1 {
				return fmt.Errorf("label %q was not enumerated after %s", labels[0], timeout)
			}
			return fmt.Errorf("none of the labels %q were enumerated after %s", labels, timeout)
		}
		KLog.Logger.Debug().Strs("labels", labels).Int("attempt", attempt).
			Msg("Device not enumerated yet, retrying")
		time.Sleep(interval)
	}
}
