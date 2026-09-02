// Package mount performs the mount syscall for immucore.
//
// It replaces github.com/containerd/containerd/mount, of which immucore used
// one function and one struct. Only the parts immucore reaches are here:
// fstab-style option parsing, and the two-step remount a read-only bind mount
// needs. Loop devices, FUSE helper binaries, overlay lowerdir compaction and
// unmounting are all left out, since nothing in immucore asks for them.
package mount

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Mount describes a filesystem to mount. The fields carry the same meaning as
// the arguments of mount(2), with Options in fstab form.
type Mount struct {
	Type    string
	Source  string
	Options []string
}

// mountSyscall is a seam for the tests. Nothing else reassigns it.
var mountSyscall = unix.Mount

type mountFlag struct {
	// clear means the option removes its flag rather than setting it, as "rw"
	// does for MS_RDONLY.
	clear bool
	value uintptr
}

// mountFlags holds the fstab option names that map to a mount(2) flag. Anything
// absent from this table is filesystem-specific data and goes to the data
// argument instead, which is how "size=", "lowerdir=" and friends get through.
//
// Propagation options ("shared", "rslave" and the rest) are deliberately absent,
// as they were from the containerd table this replaces. They need their own
// mount(2) call rather than a flag on this one, and immucore does not pass them.
var mountFlags = map[string]mountFlag{
	"async":         {clear: true, value: unix.MS_SYNCHRONOUS},
	"atime":         {clear: true, value: unix.MS_NOATIME},
	"bind":          {value: unix.MS_BIND},
	"defaults":      {},
	"dev":           {clear: true, value: unix.MS_NODEV},
	"diratime":      {clear: true, value: unix.MS_NODIRATIME},
	"dirsync":       {value: unix.MS_DIRSYNC},
	"exec":          {clear: true, value: unix.MS_NOEXEC},
	"mand":          {value: unix.MS_MANDLOCK},
	"noatime":       {value: unix.MS_NOATIME},
	"nodev":         {value: unix.MS_NODEV},
	"nodiratime":    {value: unix.MS_NODIRATIME},
	"noexec":        {value: unix.MS_NOEXEC},
	"nomand":        {clear: true, value: unix.MS_MANDLOCK},
	"norelatime":    {clear: true, value: unix.MS_RELATIME},
	"nostrictatime": {clear: true, value: unix.MS_STRICTATIME},
	"nosuid":        {value: unix.MS_NOSUID},
	"rbind":         {value: unix.MS_BIND | unix.MS_REC},
	"relatime":      {value: unix.MS_RELATIME},
	"remount":       {value: unix.MS_REMOUNT},
	"ro":            {value: unix.MS_RDONLY},
	"rw":            {clear: true, value: unix.MS_RDONLY},
	"strictatime":   {value: unix.MS_STRICTATIME},
	"suid":          {clear: true, value: unix.MS_NOSUID},
	"sync":          {value: unix.MS_SYNCHRONOUS},
}

// All mounts every entry at target, in order. It keeps the name and signature of
// the containerd function it replaces so the call site does not have to change.
func All(mounts []Mount, target string) error {
	for _, m := range mounts {
		if err := m.Mount(target); err != nil {
			return err
		}
	}
	return nil
}

// Mount mounts m at target.
func (m Mount) Mount(target string) error {
	flags, data := ParseOptions(m.Options)

	mountData := strings.Join(data, ",")
	// The kernel copies the data argument into a single page.
	if len(mountData) >= os.Getpagesize() {
		return fmt.Errorf("mount data for %q is longer than a page: %d bytes", target, len(mountData))
	}

	if err := mountSyscall(m.Source, target, m.Type, flags, mountData); err != nil {
		return fmt.Errorf("mounting %q at %q: %w", m.Source, target, err)
	}

	// A bind mount cannot be made read-only in one call: mount(2) ignores
	// MS_RDONLY when MS_BIND is set, and the mount silently comes up writable.
	// A second MS_REMOUNT pass is what applies it.
	const readOnlyBind = unix.MS_BIND | unix.MS_RDONLY
	if flags&readOnlyBind == readOnlyBind {
		if err := mountSyscall("", target, "", flags|unix.MS_REMOUNT, ""); err != nil {
			return fmt.Errorf("remounting %q read-only: %w", target, err)
		}
	}

	return nil
}

// ParseOptions splits fstab-style options into mount(2) flags and the
// filesystem-specific data string.
func ParseOptions(options []string) (uintptr, []string) {
	var flags uintptr
	var data []string

	for _, option := range options {
		flag, known := mountFlags[option]
		if !known {
			data = append(data, option)
			continue
		}
		if flag.clear {
			flags &^= flag.value
		} else {
			flags |= flag.value
		}
	}

	return flags, data
}
