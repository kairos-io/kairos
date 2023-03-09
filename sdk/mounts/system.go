package mounts

import (
	"fmt"

	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/sdk/state"
)

func PrepareWrite(partition state.PartitionState, mountpath string) error {
	if partition.Mounted && partition.IsReadOnly {
		if mountpath == partition.MountPoint {
			return machine.Remount("rw", partition.MountPoint)
		}
		err := machine.Remount("rw", partition.MountPoint)
		if err != nil {
			return err
		}
		return machine.Mount(partition.FilesystemLabel, mountpath)
	}

	return machine.Mount(partition.FilesystemLabel, mountpath)
}

func Mount(partition state.PartitionState, mountpath string) error {
	return machine.Mount(partition.FilesystemLabel, mountpath)
}

func Umount(partition state.PartitionState) error {
	if !partition.Mounted {
		return fmt.Errorf("partition not mounted")
	}
	return machine.Umount(partition.MountPoint)
}
