package debugbundle

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/jaypipes/ghw/pkg/option"
	"github.com/jaypipes/ghw/pkg/util"
)

// sysRoot is empty in production, so that sysfs and procfs are read where they
// belong. Tests point it at a fixture tree, which both ghw and diskIsUSB
// honour.
var sysRoot = ""

// run executes a command and returns its combined output. Tests replace it so
// that the mount and umount calls below can be exercised without a real drive.
var run = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// usbHostDir matches the root hub directory a USB controller creates in the
// sysfs device tree, for example usb2 in
// /sys/devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.0/host6/...
var usbHostDir = regexp.MustCompile(`^usb[0-9]+$`)

// Target is a partition the debug bundle can be copied to. MountPoint is empty
// when nothing has mounted the partition yet, which in a live installer is the
// normal state of a drive the user has just plugged in: CopyBundleTo mounts it.
type Target struct {
	Device     string // e.g. /dev/sdc1
	MountPoint string // empty when the partition is not mounted
	Label      string // filesystem label, empty when the partition has none
	SizeBytes  uint64
}

// CopyTargets lists the places the bundle can be copied to: every partition of
// every USB or otherwise removable disk, mounted or not, and the disk itself
// when it has no partition table.
//
// Listing only what is already mounted would list only the drive the live
// system booted from, because nothing in a live installer mounts a drive the
// user plugs in afterwards. That drive is left out instead, along with floppy
// and optical drives: a target that cannot be written to is worse than no
// target at all.
func CopyTargets() ([]Target, error) {
	opts := []any{option.WithDisableTools(), option.WithNullAlerter()}
	if sysRoot != "" {
		opts = append(opts, option.WithChroot(sysRoot))
	}
	bl, err := block.New(opts...)
	if err != nil {
		return nil, err
	}
	mounted := mountPoints()
	var targets []Target
	for _, disk := range bl.Disks {
		if !disk.IsRemovable && !diskIsUSB(disk.Name) {
			continue
		}
		// A floppy drive and an optical drive are removable but are not
		// somewhere a bundle can be written, and an empty drive is offered
		// just the same as a loaded one.
		if disk.DriveType == block.DriveTypeFDD || disk.DriveType == block.DriveTypeODD {
			continue
		}
		// The drive the live system booted from has a partition mounted read
		// only. Nothing on that drive can be written to, including the EFI
		// partition beside the read only one, which is unmounted and would
		// otherwise pass every test below.
		if bootMedium(disk) {
			continue
		}
		// A drive formatted with no partition table carries its filesystem on
		// the disk itself, and ghw reports no partitions for it. Offer the
		// disk, or it is as invisible as an unmounted partition was.
		if len(disk.Partitions) == 0 {
			device := "/dev/" + disk.Name
			entry := mounted[device]
			if entry.point != "" && entry.readOnly {
				continue
			}
			targets = append(targets, Target{
				Device:     device,
				MountPoint: entry.point,
				SizeBytes:  disk.SizeBytes,
			})
			continue
		}
		for _, part := range disk.Partitions {
			targets = append(targets, Target{
				Device:     "/dev/" + part.Name,
				MountPoint: part.MountPoint,
				Label:      known(part.FilesystemLabel),
				SizeBytes:  part.SizeBytes,
			})
		}
	}
	return targets, nil
}

// bootMedium reports whether the disk is the one the live system booted from,
// which it is when any of its partitions is mounted read only.
func bootMedium(disk *block.Disk) bool {
	for _, part := range disk.Partitions {
		if part.MountPoint != "" && part.IsReadOnly {
			return true
		}
	}
	return false
}

// mountEntry is where a device is mounted, and whether that mount is read only.
type mountEntry struct {
	point    string
	readOnly bool
}

// mountPoints maps a device to its mount. ghw reads the same table, but only
// for partitions, and a drive with no partition table has none.
func mountPoints() map[string]mountEntry {
	out := map[string]mountEntry{}
	table, err := os.ReadFile(filepath.Join(sysRoot, "/proc/self/mounts"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(table), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		readOnly := true
		for _, opt := range strings.Split(fields[3], ",") {
			if opt == "rw" {
				readOnly = false
			}
		}
		out[fields[0]] = mountEntry{point: fields[1], readOnly: readOnly}
	}
	return out
}

// known turns the placeholder ghw reports for a value it could not read into
// an empty string.
func known(value string) string {
	if value == util.UNKNOWN {
		return ""
	}
	return value
}

// diskIsUSB reports whether the named disk hangs off the USB bus.
//
// The sysfs removable flag is 0 for an SSD or a spinning disk in a USB
// enclosure, so it cannot be the only test: it hides every USB drive that is
// not a flash stick. The bus a disk sits on is in the device tree path that
// /sys/block/<name> points at.
func diskIsUSB(name string) bool {
	path, err := filepath.EvalSymlinks(filepath.Join(sysRoot, "/sys/block", name))
	if err != nil {
		return false
	}
	for _, element := range strings.Split(path, string(os.PathSeparator)) {
		if usbHostDir.MatchString(element) {
			return true
		}
	}
	return false
}

// CopyBundleTo copies the bundle at srcPath onto the given target, mounting it
// first when nothing else has, and unmounting it again afterwards so that the
// user can pull the drive out as soon as the copy reports success. It returns
// the name the bundle was written under.
func CopyBundleTo(srcPath string, target Target) (string, error) {
	dir := target.MountPoint
	if dir == "" {
		mountPoint, err := os.MkdirTemp("", "kairos-bundle-")
		if err != nil {
			return "", err
		}
		defer os.Remove(mountPoint)

		if out, err := run("mount", target.Device, mountPoint); err != nil {
			return "", fmt.Errorf("mounting %s: %w: %s", target.Device, err, strings.TrimSpace(string(out)))
		}
		defer func() {
			_, _ = run("umount", mountPoint)
		}()
		dir = mountPoint
	}

	if _, err := CopyTo(srcPath, dir); err != nil {
		return "", err
	}
	return filepath.Base(srcPath), nil
}

// CopyTo copies srcPath into destDir and returns the destination path.
func CopyTo(srcPath, destDir string) (string, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dest := filepath.Join(destDir, filepath.Base(srcPath))
	dst, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return dest, dst.Sync()
}
