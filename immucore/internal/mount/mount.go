// Package mount provides the Linux mount operation used by immucore.
package mount

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Mount describes a filesystem mount.
type Mount struct {
	Type    string
	Source  string
	Options []string
}

var mountSyscall = unix.Mount

type mountFlag struct {
	clear bool
	value uintptr
}

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

// Mount invokes the mount syscall for this mount at target.
func (m Mount) Mount(target string) error {
	flags, data := parseOptions(m.Options)
	mountData := strings.Join(data, ",")
	if len(mountData) >= os.Getpagesize() {
		return fmt.Errorf("mount data is too long: %d bytes", len(mountData))
	}

	if err := mountSyscall(m.Source, target, m.Type, flags, mountData); err != nil {
		return fmt.Errorf("mount %q at %q: %w", m.Source, target, err)
	}
	return nil
}

func parseOptions(options []string) (uintptr, []string) {
	var flags uintptr
	var data []string
	for _, option := range options {
		flag, ok := mountFlags[option]
		if !ok {
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
