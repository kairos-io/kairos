package mounts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/blockdev"
	"github.com/kairos-io/kairos/v4/sdk/state"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// ErrReadOnlyDevice is returned when the caller asked to write to a partition on
// media the kernel refuses writes to. It lets a caller tell "this cannot be
// persisted, and no remount will change that" from an ordinary mount failure;
// the two callers today simply propagate it.
var ErrReadOnlyDevice = errors.New("device is read-only")

func PrepareWrite(partition state.PartitionState, mountpath string) error {
	// Checked before the remount below, because that remount is the wrong answer
	// here. PartitionState.IsReadOnly is derived from the mount options (ghw
	// reads /proc/mounts and looks for "rw"), so it means "this mount is ro",
	// for which remounting rw is a perfectly good fix. Hardware write protection
	// is a different thing that no remount can undo, and attempting it is
	// refused with a bare EACCES that says nothing about why.
	if partition.Name != "" {
		device := partition.Name
		if !filepath.IsAbs(device) {
			device = filepath.Join("/dev", device)
		}
		if ro, err := blockdev.ReadOnly(device); err == nil && ro {
			return fmt.Errorf("%w: %s is write-protected, so remounting it read-write cannot succeed", ErrReadOnlyDevice, device)
		}
	}

	if partition.Mounted && partition.IsReadOnly {
		if mountpath == partition.MountPoint {
			return remount("rw", partition.MountPoint)
		}
		err := remount("rw", partition.MountPoint)
		if err != nil {
			return err
		}
		return mount(partition.FilesystemLabel, mountpath)
	}

	return mount(partition.FilesystemLabel, mountpath)
}

func Mount(partition state.PartitionState, mountpath string) error {
	return mount(partition.FilesystemLabel, mountpath)
}

func Umount(partition state.PartitionState) error {
	if !partition.Mounted {
		return fmt.Errorf("partition not mounted")
	}
	return umount(partition.MountPoint)
}

func umount(path string) error {
	out, err := utils.SH(fmt.Sprintf("umount %s", path))
	if err != nil {
		return fmt.Errorf("failed umounting: %s: %w", out, err)
	}
	return nil
}

func remount(opt, path string) error {
	out, err := utils.SH(fmt.Sprintf("mount -o %s,remount %s", opt, path))
	if err != nil {
		return fmt.Errorf("failed remounting %s as %s: %s: %w", path, opt, out, err)
	}
	return nil
}

func mount(label, mountpoint string) error {
	part, _ := utils.SH(fmt.Sprintf("blkid -L %s", label))
	if part == "" {
		fmt.Printf("%s partition not found\n", label)
		return fmt.Errorf("partition not found")
	}

	part = strings.TrimSuffix(part, "\n")

	if !utils.Exists(mountpoint) {
		err := os.MkdirAll(mountpoint, 0755)
		if err != nil {
			return err
		}
	}
	mount, err := utils.SH(fmt.Sprintf("mount %s %s", part, mountpoint))
	if err != nil {
		fmt.Printf("could not mount: %s\n", mount+err.Error())
		return err
	}
	return nil
}
