package clusterplugin

import "github.com/twpayne/go-vfs"

var filesystem vfs.FS

func init() {
	filesystem = vfs.OSFS
}
