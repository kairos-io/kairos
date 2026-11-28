package diffupgrade

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// RootfsID identifies the root filesystem under dir by its contents: a
// sha256 over the sorted list of every entry that is not a directory, with
// its path, type, permissions, owner, link target, device number, extended
// attributes and, for a regular file, the sha256 of its contents.
//
// Two trees with the same ID hold the same files, wherever they came from:
// an image in a registry, an image archive, a tarball or a directory, however
// the image was compressed or layered. That is what a diff needs from its
// base. A diff only carries what changed, so it is correct exactly when every
// file it does not carry is the same on the system as on its base, and the
// ID is the cheapest way to say so for all of them at once.
//
// Some things are left out on purpose, because deploying an image changes
// them without changing the release:
//   - modification times, which every build changes;
//   - directories, so the empty mount points and lost+found a deployed image
//     gains do not count. A directory still counts through what is in it;
//   - SELinux labels, which are set by relabeling on the node (see xattrs).
//
// Hard links count as separate files.
func RootfsID(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		line, err := idLine(path, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		_, err = io.WriteString(h, line)
		return err
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// idLine is the part of RootfsID one entry contributes. Fields are separated
// by NUL, which no path or value in it can hold, so no two entries can be
// written the same way.
func idLine(path, rel string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&fs.ModeSocket != 0 {
		return "", nil
	}
	hdr, err := header(path, info)
	if err != nil {
		return "", err
	}
	x, err := xattrs(path)
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(x))
	for k := range x {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	attrs := ""
	for _, k := range keys {
		attrs += k + "=" + hex.EncodeToString([]byte(x[k])) + ","
	}
	sum := ""
	if hdr.Typeflag == tar.TypeReg {
		if sum, err = fileSum(path); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%s\x00%c\x00%o\x00%d\x00%d\x00%s\x00%d,%d\x00%s\x00%s\n",
		rel, hdr.Typeflag, hdr.Mode, hdr.Uid, hdr.Gid, hdr.Linkname, hdr.Devmajor, hdr.Devminor, attrs, sum), nil
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
