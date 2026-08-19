package fs

import (
	"io/fs"
	"os"
)

// KairosFS is our interface for methods that need an FS.
type KairosFS interface {
	Open(name string) (fs.File, error)
	Chmod(name string, mode os.FileMode) error
	Create(name string) (*os.File, error)
	Mkdir(name string, perm os.FileMode) error
	Stat(name string) (os.FileInfo, error)
	Lstat(name string) (os.FileInfo, error)
	RemoveAll(path string) error
	ReadFile(filename string) ([]byte, error)
	Readlink(name string) (string, error)
	Symlink(oldname, newname string) error
	RawPath(name string) (string, error)
	ReadDir(dirname string) ([]fs.DirEntry, error)
	Remove(name string) error
	OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error)
	WriteFile(filename string, data []byte, perm os.FileMode) error
	Rename(oldpath, newpath string) error
	Truncate(name string, size int64) error
}
