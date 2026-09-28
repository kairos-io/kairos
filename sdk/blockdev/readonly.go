// Package blockdev answers questions about a block device that need a syscall
// rather than a shell out.
package blockdev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sysDevBlock is where the kernel indexes every block device by major:minor,
// disks, partitions and mappers alike. A var so the tests can point it at a
// fixture tree.
var sysDevBlock = "/sys/dev/block"

// probe asks the kernel directly whether it refuses writes to the device at
// this path. A seam for the tests: the real one needs a block device to open,
// which a unit test does not have.
var probe = blkroget

// rdevOf returns the major:minor of the block device at path. A seam for the
// tests, which have no block devices to stat.
var rdevOf = statRdev

// ReadOnly reports whether the kernel refuses writes to device.
//
// It asks with the BLKROGET ioctl. That is the same bdev_read_only() answer a
// partition's /sys/class/block/<name>/ro prints, so there is no difference in
// what the two say; the ioctl is used because it takes the path the caller has
// and needs no derivation of the kernel name from it.
//
// A device-mapper node backed by dm-crypt is handled by asking the devices
// underneath it as well, through /sys/dev/block/<maj:min>/slaves. A mapper
// created with the read-only flag already reports read-only itself, and a
// read-write mapper cannot be created on a write-protected device at all (the
// kernel refuses the write open of the backing device), so on a Kairos boot the
// mapper's own answer is expected to be right. The descent is cheap insurance
// for a stack that was assembled some other way, and it is limited to crypt
// mappers on purpose: a snapshot or thin target writes to a cow device on top
// of a read-only origin, and calling that read-only would be wrong.
//
// An error means the question could not be answered, not that the device is
// writable: callers decide what a missing answer implies, and none of them
// should read it as permission to write.
func ReadOnly(device string) (bool, error) {
	ro, err := probe(device)
	if err != nil {
		return false, err
	}
	if ro {
		return true, nil
	}

	rdev, err := rdevOf(device)
	if err != nil {
		// The ioctl answered, so this is a device; not being able to find it
		// in sysfs is not a reason to doubt what it said.
		return false, nil
	}
	if !isCryptMapper(rdev) {
		return false, nil
	}
	return slavesReadOnly(rdev, 0)
}

// maxStackDepth bounds the descent through stacked mappers. dm rejects a table
// that names its own device, so a cycle cannot exist, but a bound costs nothing
// and turns a corrupt sysfs into an error instead of a hang.
const maxStackDepth = 8

// slavesReadOnly reports read-only if any device under the mapper is, reading
// each one's answer from its own sysfs ro attribute, which prints the same
// bdev_read_only() the ioctl returns and needs no node under /dev.
func slavesReadOnly(rdev string, depth int) (bool, error) {
	if depth > maxStackDepth {
		return false, fmt.Errorf("device-mapper stack under %s is deeper than %d", rdev, maxStackDepth)
	}

	slavesDir := filepath.Join(sysDevBlock, rdev, "slaves")
	entries, err := os.ReadDir(slavesDir)
	if err != nil {
		return false, fmt.Errorf("listing the devices under %s: %w", rdev, err)
	}

	var unanswered error
	for _, e := range entries {
		slave, err := os.ReadFile(filepath.Join(slavesDir, e.Name(), "dev"))
		if err != nil {
			unanswered = errors.Join(unanswered, err)
			continue
		}
		slaveRdev := strings.TrimSpace(string(slave))

		ro, err := sysfsReadOnly(slaveRdev)
		if err != nil {
			unanswered = errors.Join(unanswered, err)
			continue
		}
		if ro {
			return true, nil
		}
		// A mapper on a mapper, an encrypted LVM volume say: keep going down.
		if isCryptMapper(slaveRdev) {
			ro, err := slavesReadOnly(slaveRdev, depth+1)
			if err != nil {
				unanswered = errors.Join(unanswered, err)
				continue
			}
			if ro {
				return true, nil
			}
		}
	}
	// Every device we could ask said writable. If some could not be asked,
	// hand that back rather than reporting a clean "writable".
	return false, unanswered
}

// isCryptMapper reports whether the device at this major:minor is a dm-crypt
// mapper, which is the only kind whose stack this package descends.
func isCryptMapper(rdev string) bool {
	uuid, err := os.ReadFile(filepath.Join(sysDevBlock, rdev, "dm", "uuid"))
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(uuid)), "CRYPT-")
}

// sysfsReadOnly reads the kernel's read-only flag for a device by major:minor.
func sysfsReadOnly(rdev string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(sysDevBlock, rdev, "ro"))
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(raw)) == "1", nil
}
