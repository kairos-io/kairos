// nolint:goheader

/*
Copyright © 2022 spf13/afero
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fsutils

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	sdkFS "github.com/kairos-io/kairos-sdk/types/fs"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// DirSize returns the accumulated size of all files in folder
func DirSize(fs sdkFS.KairosFS, path string) (int64, error) {
	var size int64
	err := vfs.Walk(fs, path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

// Check if a file or directory exists.
func Exists(fs sdkFS.KairosFS, path string) (bool, error) {
	_, err := fs.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// IsDir check if the path is a dir
func IsDir(fs sdkFS.KairosFS, path string) (bool, error) {
	fi, err := fs.Stat(path)
	if err != nil {
		return false, err
	}
	return fi.IsDir(), nil
}

// MkdirAll directory and all parents if not existing
func MkdirAll(fs sdkFS.KairosFS, name string, mode os.FileMode) (err error) {
	if _, isReadOnly := fs.(*vfs.ReadOnlyFS); isReadOnly {
		return permError("mkdir", name)
	}
	if name, err = fs.RawPath(name); err != nil {
		return &os.PathError{Op: "mkdir", Path: name, Err: err}
	}
	return os.MkdirAll(name, mode)
}

// permError returns an *os.PathError with Err syscall.EPERM.
func permError(op, path string) error {
	return &os.PathError{
		Op:   op,
		Path: path,
		Err:  syscall.EPERM,
	}
}

// Random number state.
// We generate random temporary file names so that there's a good
// chance the file doesn't exist yet - keeps the number of tries in
// TempFile to a minimum.
var rand uint32
var randmu sync.Mutex

func reseed() uint32 {
	return uint32(time.Now().UnixNano() + int64(os.Getpid()))
}

func nextRandom() string {
	randmu.Lock()
	r := rand
	if r == 0 {
		r = reseed()
	}
	r = r*1664525 + 1013904223 // constants from Numerical Recipes
	rand = r
	randmu.Unlock()
	return strconv.Itoa(int(1e9 + r%1e9))[1:]
}

// TempDir creates a temp dir in the virtual fs
// Took from afero.FS code and adapted
func TempDir(fs sdkFS.KairosFS, dir, prefix string) (name string, err error) {
	if dir == "" {
		dir = os.TempDir()
	}
	// This skips adding random stuff to the created temp dir so the temp dir created is predictable for testing
	if _, isTestFs := fs.(*vfst.TestFS); isTestFs {
		err = MkdirAll(fs, filepath.Join(dir, prefix), 0700)
		if err != nil {
			return "", err
		}
		name = filepath.Join(dir, prefix)
		return
	}
	nconflict := 0
	for i := 0; i < 10000; i++ {
		try := filepath.Join(dir, prefix+nextRandom())
		err = MkdirAll(fs, try, 0700)
		if os.IsExist(err) {
			if nconflict++; nconflict > 10 {
				randmu.Lock()
				rand = reseed()
				randmu.Unlock()
			}
			continue
		}
		if err == nil {
			name = try
		}
		break
	}
	return
}

// TempFile creates a temp file in the virtual fs
// Took from afero.FS code and adapted
func TempFile(fs sdkFS.KairosFS, dir, pattern string) (f *os.File, err error) {
	if dir == "" {
		dir = os.TempDir()
	}

	var prefix, suffix string
	if pos := strings.LastIndex(pattern, "*"); pos != -1 {
		prefix, suffix = pattern[:pos], pattern[pos+1:]
	} else {
		prefix = pattern
	}

	nconflict := 0
	for i := 0; i < 10000; i++ {
		name := filepath.Join(dir, prefix+nextRandom()+suffix)
		f, err = fs.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			if nconflict++; nconflict > 10 {
				randmu.Lock()
				rand = reseed()
				randmu.Unlock()
			}
			continue
		}
		break
	}
	return
}

// Walkdir with an FS implementation
type statDirEntry struct {
	info fs.FileInfo
}

func (d *statDirEntry) Name() string               { return d.info.Name() }
func (d *statDirEntry) IsDir() bool                { return d.info.IsDir() }
func (d *statDirEntry) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d *statDirEntry) Info() (fs.FileInfo, error) { return d.info, nil }

// WalkDirFs is the same as filepath.WalkDir but accepts a v1.Fs so it can be run on any v1.Fs type
func WalkDirFs(fs sdkFS.KairosFS, root string, fn fs.WalkDirFunc) error {
	info, err := fs.Stat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = walkDir(fs, root, &statDirEntry{info}, fn)
	}
	if errors.Is(err, filepath.SkipDir) {
		return nil
	}
	return err
}

func walkDir(fs sdkFS.KairosFS, path string, d fs.DirEntry, walkDirFn fs.WalkDirFunc) error {
	if err := walkDirFn(path, d, nil); err != nil || !d.IsDir() {
		if err == filepath.SkipDir && d.IsDir() {
			// Successfully skipped directory.
			err = nil
		}
		return err
	}

	dirs, err := readDir(fs, path)
	if err != nil {
		// Second call, to report ReadDir error.
		err = walkDirFn(path, d, err)
		if err != nil {
			return err
		}
	}

	for _, d1 := range dirs {
		path1 := filepath.Join(path, d1.Name())
		if err := walkDir(fs, path1, d1, walkDirFn); err != nil {
			if errors.Is(err, filepath.SkipDir) {
				break
			}
			return err
		}
	}
	return nil
}

func readDir(fs sdkFS.KairosFS, dirname string) ([]fs.DirEntry, error) {
	dirs, err := fs.ReadDir(dirname)
	if err != nil {
		return nil, err
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	return dirs, nil
}

// Copy copies src to dst like the cp command.
func Copy(fs sdkFS.KairosFS, src, dst string) error {
	if dst == src {
		return os.ErrInvalid
	}

	srcF, err := fs.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()

	info, err := srcF.Stat()
	if err != nil {
		return err
	}

	dstF, err := fs.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer dstF.Close()

	if _, err := io.Copy(dstF, srcF); err != nil {
		return err
	}
	return nil
}

// GlobFs returns the names of all files matching pattern or nil if there is no matching file.
// Only consider the names of files in the directory included in the pattern, not in subdirectories.
// So the pattern "dir/*" will return only the files in the directory "dir", not in "dir/subdir".
func GlobFs(fs sdkFS.KairosFS, pattern string) ([]string, error) {
	var matches []string

	// Check if the pattern is well formed.
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, err
	}

	// Split the pattern into directory and file parts.
	dir, file := filepath.Split(pattern)
	if dir == "" {
		dir = "."
	}

	// Read the directory.
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	// Match the entries against the pattern.
	for _, entry := range entries {
		if matched, err := filepath.Match(file, entry.Name()); err != nil {
			return nil, err
		} else if matched {
			matches = append(matches, filepath.Join(dir, entry.Name()))
		}
	}

	return matches, nil
}
