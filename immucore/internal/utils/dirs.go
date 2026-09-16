package utils

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// seLinuxXattr is where the SELinux label of a file lives.
const seLinuxXattr = "security.selinux"

// EnforceRootOwnedDir pins a directory to mode and to root:root.
//
// CreateIfNotExists creates with os.ModePerm, and a directory that came from
// an older image or from an older layout keeps whatever mode it had, so the
// mode a caller needs has to be set explicitly rather than assumed.
//
// The ownership change is skipped when immucore is not running as root, which
// only happens in tests: in the initramfs it always is.
func EnforceRootOwnedDir(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("setting mode %o on %s: %w", mode, path, err)
	}
	if os.Geteuid() != 0 {
		KLog.Logger.Debug().Str("path", path).Msg("Not root, leaving directory ownership alone")
		return nil
	}
	if err := os.Chown(path, 0, 0); err != nil {
		return fmt.Errorf("setting ownership of %s: %w", path, err)
	}
	return nil
}

// DirHasContent reports whether path is a directory that has at least one
// entry in it. A path that is not there at all has no content and is not an
// error: the callers are deciding whether there is something to keep, not
// whether the path is valid.
func DirHasContent(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	return len(entries) > 0, nil
}

// ReadSELinuxLabel returns the SELinux label of a path, or the empty string
// when the path carries none or the filesystem does not support labels.
func ReadSELinuxLabel(path string) (string, error) {
	size, err := unix.Getxattr(path, seLinuxXattr, nil)
	if err != nil {
		if unlabelled(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading the SELinux label of %s: %w", path, err)
	}
	if size <= 0 {
		return "", nil
	}
	buf := make([]byte, size)
	n, err := unix.Getxattr(path, seLinuxXattr, buf)
	if err != nil {
		if unlabelled(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading the SELinux label of %s: %w", path, err)
	}
	return strings.TrimRight(string(buf[:n]), "\x00"), nil
}

// CopySELinuxLabel copies the SELinux label of src onto dst, and does nothing
// when src has no label to copy.
//
// The rsync in SyncState already preserves labels for the contents of a bind,
// but the backing directory itself can be created by immucore, in which case
// it starts out with the label of its parent instead of the one the mount is
// going to expose. Copying it here is what keeps the label from changing when
// a directory moves into the persistent state target.
func CopySELinuxLabel(src, dst string) error {
	label, err := ReadSELinuxLabel(src)
	if err != nil {
		return err
	}
	if label == "" {
		KLog.Logger.Debug().Str("src", src).Msg("No SELinux label to copy")
		return nil
	}
	if err := unix.Setxattr(dst, seLinuxXattr, []byte(label), 0); err != nil {
		if unlabelled(err) {
			KLog.Logger.Debug().Str("dst", dst).Str("label", label).
				Msg("Filesystem does not take SELinux labels")
			return nil
		}
		return fmt.Errorf("setting the SELinux label of %s to %s: %w", dst, label, err)
	}
	KLog.Logger.Debug().Str("src", src).Str("dst", dst).Str("label", label).Msg("Copied SELinux label")
	return nil
}

// unlabelled reports whether an xattr error means "there are no SELinux labels
// here": the path was never labelled (ENODATA), the filesystem or kernel has
// no support for the security namespace (EOPNOTSUPP, ENOSYS), or we are not
// privileged enough to write it (EPERM, which outside of tests cannot happen
// since immucore runs as root in the initramfs). None of those is a reason to
// take a boot down.
func unlabelled(err error) bool {
	return errors.Is(err, unix.ENODATA) ||
		errors.Is(err, unix.EOPNOTSUPP) ||
		errors.Is(err, unix.ENOSYS) ||
		errors.Is(err, unix.EPERM)
}
