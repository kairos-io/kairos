package mounts

import (
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos-sdk/state"
	"github.com/kairos-io/kairos-sdk/utils"
)

func PrepareWrite(partition state.PartitionState, mountpath string) error {
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
		return fmt.Errorf("failed umounting: %s: %w", out, err)
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
