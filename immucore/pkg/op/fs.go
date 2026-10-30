package op

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/internal/mount"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/schema"
)

// https://github.com/kairos-io/packages/blob/94aa3bef3d1330cb6c6905ae164f5004b6a58b8c/packages/system/dracut/immutable-rootfs/30cos-immutable-rootfs/cos-mount-layout.sh#L129
func BaseOverlay(overlay schema.Overlay) (MountOperation, error) {
	var dat []string
	if err := os.MkdirAll(overlay.Base, 0700); err != nil {
		return MountOperation{}, err
	}

	// BackingBase can be a device (LABEL=COS_PERSISTENT) or a tmpfs+size (tmpfs:20%)
	// We need to properly parse to understand what it is
	// We probably should deprecate changing the overlay but leave the size, I don't see much use of this

	// Load both separated
	datTmpfs := strings.Split(overlay.BackingBase, ":")
	datDevice := strings.Split(overlay.BackingBase, "=")

	// Add whichever has 2 len as that indicates that it's the correct one
	if len(datDevice) == 2 {
		dat = datDevice
	}
	if len(datTmpfs) == 2 {
		dat = datTmpfs
	}
	if len(dat) != 2 {
		return MountOperation{}, fmt.Errorf("invalid backing base. must be a tmpfs with a size or a LABEL/UUID device. e.g. tmpfs:30%%, LABEL:COS_PERSISTENT. Input: %s", overlay.BackingBase)
	}

	t := dat[0]
	switch t {
	case "tmpfs":
		tmpMount := mount.Mount{Type: "tmpfs", Source: "tmpfs", Options: []string{"nosuid", "nodev", "mode=0755", fmt.Sprintf("size=%s", dat[1])}}
		tmpFstab := internalUtils.MountToFstab(tmpMount)
		tmpFstab.File = internalUtils.CleanSysrootForFstab(overlay.Base)
		return MountOperation{
			MountOption: tmpMount,
			FstabEntry:  *tmpFstab,
			Target:      overlay.Base,
		}, nil
	case "LABEL", "UUID":
		fsType := internalUtils.DiskFSType(internalUtils.ParseMount(overlay.BackingBase))
		blockMount := mount.Mount{Type: fsType, Source: internalUtils.ParseMount(overlay.BackingBase)}
		tmpFstab := internalUtils.MountToFstab(blockMount)
		// TODO: Check if this is properly written to fstab, currently have no examples
		tmpFstab.File = internalUtils.CleanSysrootForFstab(overlay.Base)
		tmpFstab.MntOps["default"] = ""

		return MountOperation{
			MountOption: blockMount,
			FstabEntry:  *tmpFstab,
			Target:      overlay.Base,
		}, nil
	default:
		return MountOperation{}, fmt.Errorf("invalid overlay backing base type")
	}
}

// BindStateDir returns the directory under the persistent state target that
// backs the bind mount of mountpoint, e.g. /var/log/audit is backed by
// <root>/<stateTarget>/var-log-audit.bind.
func BindStateDir(mountpoint, root, stateTarget string) string {
	return filepath.Join(root, stateTarget, fmt.Sprintf("%s.bind", StateName(mountpoint)))
}

// StateName is the flat name the backing directories of mountpoint are named
// after, by BindStateDir and MountWithBaseOverlay: /var/log/audit is
// var-log-audit. Every "/" becomes "-", and a "-" or "%" already in the path is
// written as %2d or %25, so /var/lib/foo-bar is var-lib-foo%2dbar and does not
// share a directory with /var/lib/foo/bar, which is var-lib-foo-bar. The escape
// is reversible: reading the name back, "-" is a "/" and %XX is the byte XX.
// It does not use a backslash, which overlayfs strips from upperdir= and
// workdir=.
func StateName(mountpoint string) string {
	var b strings.Builder
	for _, c := range []byte(strings.TrimLeft(mountpoint, "/")) {
		switch c {
		case '/':
			b.WriteByte('-')
		case '-', '%':
			fmt.Fprintf(&b, "%%%02x", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// legacyStateName is the name StateName gave before it escaped "-", under which
// the data of a path with a "-" in it sits on machines installed before.
func legacyStateName(mountpoint string) string {
	return strings.ReplaceAll(strings.TrimLeft(mountpoint, "/"), "/", "-")
}

// MigrateStateDirs renames the directory each of mountpoints was backed by
// under its legacy name, dir(legacyStateName), to dir(StateName), the first
// time it is mounted with the escaped name. It returns the error of each
// mountpoint whose directory could not be moved, which must not be mounted
// over an empty directory while its data sits under the old name.
//
// A legacy name is left alone when it is the current name of another of
// mountpoints, /var/lib/foo/bar's var-lib-foo-bar for /var/lib/foo-bar, as
// that directory holds the other path's data. So is one that two of
// mountpoints had in common, /a-b/c and /a/b-c, as there is no telling whose
// data it holds.
func MigrateStateDirs(mountpoints []string, dir func(name string) string) map[string]error {
	current := map[string]bool{}
	legacy := map[string]int{}
	for _, p := range mountpoints {
		current[StateName(p)] = true
		if l := legacyStateName(p); l != StateName(p) {
			legacy[l]++
		}
	}

	failed := map[string]error{}
	for _, p := range mountpoints {
		l, name := legacyStateName(p), StateName(p)
		if l == name {
			continue
		}
		if current[l] || legacy[l] > 1 {
			internalUtils.KLog.Logger.Warn().Str("path", p).Str("dir", dir(l)).
				Msg("Not renaming a state directory another path uses or used; the path starts from an empty one")
			continue
		}
		if err := migrateStateDir(dir(l), dir(name)); err != nil {
			failed[p] = err
		}
	}
	return failed
}

// migrateStateDir renames legacy to current. Nothing is done if current is
// already there or legacy is not.
func migrateStateDir(legacy, current string) error {
	if _, err := os.Stat(current); err == nil || !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(legacy); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	internalUtils.KLog.Logger.Info().Str("from", legacy).Str("to", current).Msg("Renaming state directory to its escaped name")
	return os.Rename(legacy, current)
}

// https://github.com/kairos-io/packages/blob/94aa3bef3d1330cb6c6905ae164f5004b6a58b8c/packages/system/dracut/immutable-rootfs/30cos-immutable-rootfs/cos-mount-layout.sh#L183
func MountBind(mountpoint, root, stateTarget string) MountOperation {
	mountpoint = strings.TrimLeft(mountpoint, "/") // normalize, remove / upfront as we are going to re-use it in subdirs
	rootMount := filepath.Join(root, mountpoint)

	stateDir := BindStateDir(mountpoint, root, stateTarget)

	tmpMount := mount.Mount{
		Type:    "overlay",
		Source:  stateDir,
		Options: append([]string{"bind"}, constants.BindMountOptions(mountpoint)...),
	}
	internalUtils.KLog.Logger.Debug().Str("where", rootMount).Str("what", stateDir).Msg("Bind mount")
	tmpFstab := internalUtils.MountToFstab(tmpMount)
	tmpFstab.File = internalUtils.CleanSysrootForFstab(fmt.Sprintf("/%s", mountpoint))
	tmpFstab.Spec = internalUtils.CleanSysrootForFstab(tmpFstab.Spec)
	return MountOperation{
		MountOption: tmpMount,
		FstabEntry:  *tmpFstab,
		Target:      rootMount,
		PrepareCallback: func() error {
			// The state directory takes the mode of the mountpoint, and a
			// mountpoint the image does not ship is created by the call below
			// with a default of its own. That default would then be the mode
			// the bind exposes for the life of the machine, so a path that
			// needs a mode of its own has to be created before that happens.
			if mode, ok := constants.BindMountMode(mountpoint); ok {
				if err := internalUtils.CreateDirIfNotExists(rootMount, mode); err != nil {
					return err
				}
			}

			if err := internalUtils.CreateIfNotExists(rootMount); err != nil {
				return err
			}

			if err := internalUtils.CreateBindStateDir(rootMount, stateDir); err != nil {
				return err
			}
			// The sync has no --delete and no one-time guard, so it re-runs on
			// every boot. A machine that upgraded from an image without a
			// dedicated entry for a path keeps the snapshot it left under the
			// shared bind that sorts ahead of it -- /var/log/audit inherits
			// var-log.bind/audit -- and nothing ever empties that directory, so
			// a file deleted from the new backing directory reappears from the
			// old one on the next boot. Fresh installs never see it.
			return internalUtils.SyncState(internalUtils.AppendSlash(rootMount), internalUtils.AppendSlash(stateDir))
		},
	}
}

// https://github.com/kairos-io/packages/blob/94aa3bef3d1330cb6c6905ae164f5004b6a58b8c/packages/system/dracut/immutable-rootfs/30cos-immutable-rootfs/cos-mount-layout.sh#L145
func MountWithBaseOverlay(mountpoint, root, base string) MountOperation {
	mountpoint = strings.TrimLeft(mountpoint, "/") // normalize, remove / upfront as we are going to re-use it in subdirs
	rootMount := filepath.Join(root, mountpoint)
	bindMountPath := StateName(mountpoint)

	upperdir := filepath.Join(base, bindMountPath, ".overlay", "upper")
	workdir := filepath.Join(base, bindMountPath, ".overlay", "work")

	tmpMount := mount.Mount{
		Type:   "overlay",
		Source: "overlay",
		Options: []string{
			//"defaults",
			fmt.Sprintf("lowerdir=%s", rootMount),
			fmt.Sprintf("upperdir=%s", upperdir),
			fmt.Sprintf("workdir=%s", workdir),
		},
	}

	tmpFstab := internalUtils.MountToFstab(tmpMount)
	tmpFstab.File = internalUtils.CleanSysrootForFstab(rootMount)
	// TODO: update fstab with x-systemd info
	// https://github.com/kairos-io/packages/blob/94aa3bef3d1330cb6c6905ae164f5004b6a58b8c/packages/system/dracut/immutable-rootfs/30cos-immutable-rootfs/cos-mount-layout.sh#L170
	return MountOperation{
		MountOption: tmpMount,
		FstabEntry:  *tmpFstab,
		Target:      rootMount,
		PrepareCallback: func() error {
			// The lowerdir has to exist before we can stack an overlay on it. It
			// usually ships in the OS image, but if it does not we cannot create it
			// either: the rootfs is still mounted read-only at this point. Report
			// that clearly instead of letting the mount syscall fail later with a
			// bare "lstat <path>: no such file or directory".
			if err := internalUtils.CreateIfNotExists(rootMount); err != nil {
				return fmt.Errorf("%w: %s: %w", constants.ErrMountTargetMissing, rootMount, err)
			}
			// Make sure workdir and/or upper exists
			_ = os.MkdirAll(upperdir, os.ModePerm)
			_ = os.MkdirAll(workdir, os.ModePerm)
			return nil
		},
	}
}
