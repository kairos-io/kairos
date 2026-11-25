package action

import (
	sdkFS "github.com/kairos-io/kairos/v4/sdk/types/fs"
)

// SetSpaceProbes replaces how the no-format capacity check reads a mounted
// filesystem and the space an existing image takes, and returns a restorer.
func SetSpaceProbes(stat func(path string) (total, available int64, err error), allocated func(path string) int64) func() {
	prevStat, prevAlloc := statFilesystem, allocatedBytes
	statFilesystem, allocatedBytes = stat, allocated
	return func() { statFilesystem, allocatedBytes = prevStat, prevAlloc }
}

// SetFreeBytesFunc replaces how the upgrade measures the free space on the
// state partition and returns a function that puts the real one back.
func SetFreeBytesFunc(f func(fs sdkFS.KairosFS, path string) (int64, error)) (restore func()) {
	orig := freeBytesOn
	freeBytesOn = f
	return func() { freeBytesOn = orig }
}
