package clusterplugin

import (
	"github.com/twpayne/go-vfs/v4"
)

var filesystem vfs.FS

func init() {
	filesystem = vfs.OSFS
}
