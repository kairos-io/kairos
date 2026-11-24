package action

// SetSpaceProbes replaces how the no-format capacity check reads a mounted
// filesystem and the space an existing image takes, and returns a restorer.
func SetSpaceProbes(stat func(path string) (total, available int64, err error), allocated func(path string) int64) func() {
	prevStat, prevAlloc := statFilesystem, allocatedBytes
	statFilesystem, allocatedBytes = stat, allocated
	return func() { statFilesystem, allocatedBytes = prevStat, prevAlloc }
}
