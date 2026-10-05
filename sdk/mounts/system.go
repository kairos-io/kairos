package mounts

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/sdk/machine"
	"github.com/kairos-io/kairos/v4/sdk/state"
)

// This package used to carry its own copy of umount, remount and mount. The
// copy in sdk/machine was fixed to resolve a filesystem label through
// lookup.MountSourceForLabel, because `blkid -L <label>` races with udev
// between a LUKS container and its unlocked mapper and so can answer with the
// encrypted device (kairos-io/kairos#4685, kairos-io/kairos#4403). This copy
// kept the blkid spelling and so kept the bug. There is one implementation
// now, in sdk/machine, and these are the label-free wrappers the
// state.PartitionState callers in sdk/system want.

// PrepareWrite mounts partition at mountpath so the caller can write to it.
//
// A partition that is already mounted read only is remounted read write where
// it is, because a second mount of the same device inherits the read only
// flag from the first.
func PrepareWrite(partition state.PartitionState, mountpath string) error {
	if partition.Mounted && partition.IsReadOnly {
		if err := machine.Remount("rw", partition.MountPoint); err != nil {
			return err
		}
		if mountpath == partition.MountPoint {
			return nil
		}
	}

	return Mount(partition, mountpath)
}

// Mount mounts the partition carrying partition's filesystem label at
// mountpath, creating mountpath if it is not there.
func Mount(partition state.PartitionState, mountpath string) error {
	return machine.Mount(partition.FilesystemLabel, mountpath)
}

// Umount unmounts partition from where it is mounted.
func Umount(partition state.PartitionState) error {
	if !partition.Mounted {
		return fmt.Errorf("partition not mounted")
	}
	return machine.Umount(partition.MountPoint)
}
