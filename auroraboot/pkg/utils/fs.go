package utils

import (
	"crypto/sha256"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// MkdirAll directory and all parents if not existing
func MkdirAll(fs sdkFs.KairosFS, name string, mode os.FileMode) (err error) {
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

// Copies source file to target file using Fs interface
func CreateDirStructure(fs sdkFs.KairosFS, target string) error {
	for _, dir := range []string{"/run", "/dev", "/boot", "/usr/local", "/oem"} {
		err := MkdirAll(fs, filepath.Join(target, dir), constants.DirPerm)
		if err != nil {
			return err
		}
	}
	for _, dir := range []string{"/proc", "/sys"} {
		err := MkdirAll(fs, filepath.Join(target, dir), constants.NoWriteDirPerm)
		if err != nil {
			return err
		}
	}
	err := MkdirAll(fs, filepath.Join(target, "/tmp"), constants.DirPerm)
	if err != nil {
		return err
	}
	// Set /tmp permissions regardless the umask setup
	err = fs.Chmod(filepath.Join(target, "/tmp"), constants.TempDirPerm)
	if err != nil {
		return err
	}
	return nil
}

// TempDir creates a temp file in the virtual fs
// Took from afero.FS code and adapted
func TempDir(fs sdkFs.KairosFS, dir, prefix string) (name string, err error) {
	if dir == "" {
		dir = os.TempDir()
	}
	// This skips adding random stuff to the created temp dir so the temp dir created is predictable for testing
	if _, isTestFs := fs.(*vfst.TestFS); isTestFs {
		err = MkdirAll(fs, filepath.Join(dir, prefix), 0755)
		if err != nil {
			return "", err
		}
		name = filepath.Join(dir, prefix)
		return
	}
	nconflict := 0
	for i := 0; i < 10000; i++ {
		try := filepath.Join(dir, prefix+nextRandom())
		err = MkdirAll(fs, try, 0755)
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

// CopyFile Copies source file to target file using Fs interface. If target
// is  directory source is copied into that directory using source name file.
func CopyFile(fs sdkFs.KairosFS, source string, target string) (err error) {
	return ConcatFiles(fs, []string{source}, target)
}

// IsDir check if the path is a dir
func IsDir(fs sdkFs.KairosFS, path string) (bool, error) {
	fi, err := fs.Stat(path)
	if err != nil {
		return false, err
	}
	return fi.IsDir(), nil
}

// ConcatFiles Copies source files to target file using Fs interface.
// Source files are concatenated into target file in the given order.
// If target is a directory source is copied into that directory using
// 1st source name file.
func ConcatFiles(fs sdkFs.KairosFS, sources []string, target string) (err error) {
	if len(sources) == 0 {
		return fmt.Errorf("Empty sources list")
	}
	if dir, _ := IsDir(fs, target); dir {
		target = filepath.Join(target, filepath.Base(sources[0]))
	}

	targetFile, err := fs.Create(target)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			err = targetFile.Close()
		} else {
			_ = fs.Remove(target)
		}
	}()

	var sourceFile iofs.File
	for _, source := range sources {
		sourceFile, err = fs.Open(source)
		if err != nil {
			break
		}
		_, err = io.Copy(targetFile, sourceFile)
		if err != nil {
			break
		}
		err = sourceFile.Close()
		if err != nil {
			break
		}
	}

	return err
}

// DirSize returns the accumulated size of all files in folder
func DirSize(fs sdkFs.KairosFS, path string) (int64, error) {
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
func Exists(fs sdkFs.KairosFS, path string) (bool, error) {
	_, err := fs.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// CalcFileChecksum opens the given file and returns the sha256 checksum of it.
func CalcFileChecksum(fs sdkFs.KairosFS, fileName string) (string, error) {
	f, err := fs.Open(fileName)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// CopyDir copies the contents of the source directory to the destination directory.
func CopyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			// Keep same mode as the source directory
			err = os.MkdirAll(dstPath, info.Mode())
			if err != nil {
				return err
			}
			// Keep the same ownership as the source file
			sourceUid := info.Sys().(*syscall.Stat_t).Uid
			sourceGid := info.Sys().(*syscall.Stat_t).Gid
			return os.Chown(dstPath, int(sourceUid), int(sourceGid))
		}

		return copyFile(path, dstPath)
	})
}

// copyFile copies a file from src to dst.
func copyFile(src string, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}
	sourceStat, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	// Keep the same mode as the source file
	err = os.Chmod(dst, sourceStat.Mode())
	if err != nil {
		return err
	}
	// Keep the same ownership as the source file
	sourceUid := sourceStat.Sys().(*syscall.Stat_t).Uid
	sourceGid := sourceStat.Sys().(*syscall.Stat_t).Gid
	return os.Chown(dst, int(sourceUid), int(sourceGid))
}

// DD copies a file from input file to output file with the given block size, count, skip and seek.
// Mimics the dd utility
func DD(inputFile, outputFile string, bs, count, skip, seek int64) error {
	in, err := os.Open(inputFile)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(outputFile, os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	defer out.Close()

	if skip > 0 {
		_, err = in.Seek(skip*bs, io.SeekStart)
		if err != nil {
			return fmt.Errorf("failed to seek input file: %w", err)
		}
	}

	if seek > 0 {
		_, err = out.Seek(seek*bs, io.SeekStart)
		if err != nil {
			return fmt.Errorf("failed to seek output file: %w", err)
		}
	}

	buf := make([]byte, bs)
	for i := int64(0); i < count || count == 0; i++ {
		n, err := in.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read input file: %w", err)
		}
		if n == 0 {
			break
		}

		_, err = out.Write(buf[:n])
		if err != nil {
			return fmt.Errorf("failed to write to output file: %w", err)
		}

		if count > 0 && i+1 == count {
			break
		}
	}

	err = out.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync output file: %w", err)
	}

	return nil
}
