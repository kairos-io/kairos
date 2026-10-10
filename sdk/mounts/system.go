package mounts

import (
	"errors"
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
// flag from the first. That is a change to the running system that outlives
// the write, so every caller has to pair this with FinishWrite.
//
// A call that returns an error has left the mount table as it found it.
func PrepareWrite(partition state.PartitionState, mountpath string) error {
	if !remountsInPlace(partition) {
		return Mount(partition, mountpath)
	}

	if err := machine.Remount("rw", partition.MountPoint); err != nil {
		return err
	}
	if mountpath == partition.MountPoint {
		return nil
	}
	if err := Mount(partition, mountpath); err != nil {
		// Give the partition back the flag it was found with, rather than
		// leaving it writable with nothing mounted where the caller asked.
		return errors.Join(err, machine.Remount("ro", partition.MountPoint))
	}
	return nil
}

// FinishWrite undoes PrepareWrite. It unmounts the path PrepareWrite mounted,
// and puts a partition that was found read only back to read only.
//
// Without the second step a single write through sdk/system leaves the
// partition writable for the rest of the boot: the state partition holds the
// active and passive images and immucore mounts it read only on purpose
// (kairos-io/kairos#5245).
//
// It takes the same two arguments as PrepareWrite, because what has to be
// undone is decided by the partition's state at the time of the call, not by
// what is mounted now.
func FinishWrite(partition state.PartitionState, mountpath string) error {
	var errs []error

	// PrepareWrite only mounts when it did not already have the partition
	// where the caller wants it.
	if !remountsInPlace(partition) || mountpath != partition.MountPoint {
		errs = append(errs, machine.Umount(mountpath))
	}
	if remountsInPlace(partition) {
		errs = append(errs, machine.Remount("ro", partition.MountPoint))
	}

	return errors.Join(errs...)
}

// remountsInPlace reports whether PrepareWrite has to take the read only flag
// off the partition where it is already mounted.
func remountsInPlace(partition state.PartitionState) bool {
	return partition.Mounted && partition.IsReadOnly
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
