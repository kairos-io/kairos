//go:build !linux

package blockdev

import (
	"fmt"
	"runtime"
)

// blkroget has no meaning off Linux: BLKROGET is a Linux block-layer ioctl.
// Kairos only ever runs on Linux, so this exists so that the package still
// builds and the portable half of ReadOnly stays unit-testable on a developer's
// machine.
func blkroget(device string) (bool, error) {
	return false, fmt.Errorf("cannot ask %s whether it is read-only on %s", device, runtime.GOOS)
}

// statRdev likewise: sysfs is Linux.
func statRdev(device string) (string, error) {
	return "", fmt.Errorf("cannot find %s in sysfs on %s", device, runtime.GOOS)
}
