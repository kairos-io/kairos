package diffupgrade

import (
	"io/fs"
	"syscall"
)

// inode identifies the file behind info for hard link detection. It reports
// false for a file with a single name, which needs no tracking.
func inode(info fs.FileInfo) (inodeKey, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Nlink < 2 {
		return inodeKey{}, false
	}
	return inodeKey{dev: uint64(st.Dev), ino: st.Ino}, true //nolint:unconvert // Dev is not uint64 on every architecture
}
