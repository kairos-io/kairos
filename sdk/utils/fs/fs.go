package fs

import (
	"os"
	"syscall"

	sdkTypes "github.com/kairos-io/kairos-sdk/types/fs"
	"github.com/twpayne/go-vfs/v4"
)

// permError returns an *os.PathError with Err syscall.EPERM.
func permError(op, path string) error {
	return &os.PathError{
		Op:   op,
		Path: path,
		Err:  syscall.EPERM,
	}
}

// MkdirAll directory and all parents if not existing
func MkdirAll(fs sdkTypes.KairosFS, name string, mode os.FileMode) (err error) {
	if _, isReadOnly := fs.(*vfs.ReadOnlyFS); isReadOnly {
		return permError("mkdir", name)
	}
	if name, err = fs.RawPath(name); err != nil {
		return &os.PathError{Op: "mkdir", Path: name, Err: err}
	}
	return os.MkdirAll(name, mode)
}
