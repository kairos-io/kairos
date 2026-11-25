package check

import (
	"errors"
	"io/fs"
	"os"
	"strings"
)

// maxLinkHops is how many symbolic links a path may go through. It is the
// limit the kernel uses.
const maxLinkHops = 40

// binDirs are the directories a command is looked up in. /usr/local does not
// count: the persistent partition is mounted over it on a Kairos system.
var binDirs = []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"}

// rootFS reads a root filesystem, the running one or an unpacked one. A path
// is always looked up inside the root: an absolute link in an unpacked image
// points into that image and never at the host.
type rootFS struct {
	// root is the directory of the root filesystem without a trailing slash.
	// It is empty for the running system.
	root string
	// denied collects the paths that exist but cannot be read by this user. They
	// are not "missing", and the report says so.
	denied *[]string
}

func newRootFS(root string) rootFS {
	return rootFS{root: strings.TrimRight(root, "/"), denied: &[]string{}}
}

// note keeps a path that could not be read for a reason other than not being there.
func (r rootFS) note(path string, err error) {
	if err == nil || !errors.Is(err, fs.ErrPermission) || r.denied == nil {
		return
	}
	for _, known := range *r.denied {
		if known == path {
			return
		}
	}
	*r.denied = append(*r.denied, path)
}

// host is whether this is the running system and not an unpacked one.
func (r rootFS) host() bool { return r.root == "" }

// resolve gives the path on the host of a path in the root, with every link
// followed inside the root. It reports false when the path does not exist, also
// when it is a link to nowhere.
func (r rootFS) resolve(path string) (string, bool) {
	rest := path
	done := ""
	hops := 0
	for rest != "" {
		part, after, found := strings.Cut(rest, "/")
		if found {
			rest = after
		} else {
			rest = ""
		}
		switch part {
		case "", ".":
			continue
		case "..":
			if i := strings.LastIndex(done, "/"); i >= 0 {
				done = done[:i]
			}
			continue
		}
		full := r.root + done + "/" + part
		info, err := os.Lstat(full)
		r.note(full, err)
		if err == nil && info.Mode()&fs.ModeSymlink != 0 {
			hops++
			if hops > maxLinkHops {
				return "", false
			}
			target, err := os.Readlink(full)
			if err != nil {
				return "", false
			}
			if strings.HasPrefix(target, "/") {
				done = ""
			}
			if rest != "" {
				rest = target + "/" + rest
			} else {
				rest = target
			}
			continue
		}
		done += "/" + part
	}
	full := r.root + done
	if full == "" {
		full = "/"
	}
	if _, err := os.Stat(full); err != nil {
		r.note(full, err)
		return "", false
	}
	return full, true
}

// exists is whether the path is in the root, and not as a link to nowhere.
func (r rootFS) exists(path string) bool {
	_, ok := r.resolve(path)
	return ok
}

// regular gives the host path of a regular file of the root that is not
// oversized: a device, a pipe or a huge file can be under any name in an
// unpacked image.
func (r rootFS) regular(path string) (string, error) {
	full, ok := r.resolve(path)
	if !ok {
		return "", fs.ErrNotExist
	}
	info, err := os.Stat(full)
	if err != nil {
		r.note(full, err)
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileSize {
		return "", fs.ErrInvalid
	}
	return full, nil
}

// maxFileSize is the biggest file of the root that is read. The module lists of
// a kernel are a few hundred kilobytes.
const maxFileSize = 64 << 20

// open opens a regular file of the root.
func (r rootFS) open(path string) (*os.File, error) {
	full, err := r.regular(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	r.note(full, err)
	return f, err
}

// readFile reads a regular file of the root.
func (r rootFS) readFile(path string) ([]byte, error) {
	full, err := r.regular(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	r.note(full, err)
	return data, err
}

// findBin gives the path in the root of the first of the commands that is
// there as an executable file.
func (r rootFS) findBin(names ...string) (string, bool) {
	for _, name := range names {
		for _, dir := range binDirs {
			full, ok := r.resolve(dir + "/" + name)
			if !ok {
				continue
			}
			info, err := os.Stat(full)
			if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
				return dir + "/" + name, true
			}
		}
	}
	return "", false
}

// firstOf gives the first of the paths that is in the root.
func (r rootFS) firstOf(paths []string) (string, bool) {
	for _, path := range paths {
		if r.exists(path) {
			return path, true
		}
	}
	return "", false
}
