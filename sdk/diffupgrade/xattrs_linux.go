package diffupgrade

import (
	"bytes"
	"errors"

	"golang.org/x/sys/unix"
)

// ignoredXattrs are extended attributes that describe the node rather than
// the release. SELinux labels are set by relabeling on the node, against
// the policy it runs, and a host that unpacks or reads an image can add its
// own. Counting them would make two copies of one release look different.
var ignoredXattrs = map[string]bool{"security.selinux": true}

// xattrs returns the extended attributes of path, without following a
// symlink, minus ignoredXattrs. File capabilities live here, and an upgrade
// that dropped them would leave binaries such as ping unable to run.
func xattrs(path string) (map[string]string, error) {
	size, err := unix.Llistxattr(path, nil)
	if errors.Is(err, unix.ENOTSUP) || size == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	size, err = unix.Llistxattr(path, buf)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, key := range bytes.Split(buf[:size], []byte{0}) {
		if len(key) == 0 {
			continue
		}
		k := string(key)
		if ignoredXattrs[k] {
			continue
		}
		vsize, err := unix.Lgetxattr(path, k, nil)
		if err != nil {
			return nil, err
		}
		val := make([]byte, vsize)
		vsize, err = unix.Lgetxattr(path, k, val)
		if err != nil {
			return nil, err
		}
		out[k] = string(val[:vsize])
	}
	return out, nil
}
