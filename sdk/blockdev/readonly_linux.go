//go:build linux

package blockdev

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// blkroget asks the kernel directly. BLKROGET is answered by bdev_read_only():
// see the ReadOnly doc comment.
func blkroget(device string) (bool, error) {
	// O_NONBLOCK so a removable device with no medium in it cannot hang the
	// open, which in the initramfs would hang the boot.
	fd, err := unix.Open(device, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = unix.Close(fd)
	}()

	ro, err := unix.IoctlGetInt(fd, unix.BLKROGET)
	if err != nil {
		return false, err
	}
	return ro != 0, nil
}

// statRdev returns the device's major:minor as sysfs spells it, from the node
// itself rather than from its name, so a real node under /dev/mapper or a node
// anywhere else resolves the same as a by-label symlink.
func statRdev(device string) (string, error) {
	var st unix.Stat_t
	if err := unix.Stat(device, &st); err != nil {
		return "", err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFBLK {
		return "", fmt.Errorf("%s is not a block device", device)
	}
	rdev := uint64(st.Rdev) //nolint:unconvert // Rdev is uint32 on some architectures
	return fmt.Sprintf("%d:%d", unix.Major(rdev), unix.Minor(rdev)), nil
}
