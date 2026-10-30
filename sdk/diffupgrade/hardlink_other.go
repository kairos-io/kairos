//go:build !linux

package diffupgrade

import "io/fs"

// inode is not implemented off Linux, where hard links are written as
// separate files.
func inode(_ fs.FileInfo) (inodeKey, bool) {
	return inodeKey{}, false
}
