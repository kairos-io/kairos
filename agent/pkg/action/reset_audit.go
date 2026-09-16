package action

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkFS "github.com/kairos-io/kairos/v4/sdk/types/fs"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	"golang.org/x/sys/unix"
)

const (
	// auditStashPrefix is the prefix of the staging directory the audit trail
	// is held in while the persistent partition is being formatted.
	auditStashPrefix = "kairos-audit-"
	// auditStashRoot is where that staging directory is made. /run is a tmpfs
	// on every boot, including a recovery boot, which the default temp dir is
	// not: recovery sets no RW_PATHS, so immucore falls back to a list that
	// does not have /tmp on it and /tmp is then whatever the image left there,
	// read-only root included.
	auditStashRoot = "/run"
)

// StashAuditLog copies the audit trail off the persistent partition into a
// staging directory under /run, and returns that directory. It returns an
// empty string when there is nothing to preserve.
//
// A reset formats COS_PERSISTENT, which takes the backing directory of the
// /var/log/audit bind with it. Copying it out and back is the only way to keep
// it: there is no partition a reset leaves alone that is both writable and
// sized for log data (OEM defaults to 64MiB and can be formatted too). The
// staging directory is therefore on a tmpfs, which is what bounds this
// mechanism and why the copy is skipped when the audit trail would not
// comfortably fit in the memory that is left. Skipping is not fatal: a reset
// that keeps going without the audit trail is the behaviour we have today,
// while a reset that dies halfway leaves an unbootable machine.
func StashAuditLog(cfg *sdkConfig.Config, persistent *sdkPartitions.Partition) (string, error) {
	if persistent == nil {
		return "", nil
	}

	var stash string
	err := withPersistentMounted(cfg, persistent, func(root string) error {
		src := filepath.Join(root, cnst.AuditLogStatePath)
		exists, err := fsutils.Exists(cfg.Fs, src)
		if err != nil {
			return err
		}
		if !exists {
			// Named, because this is also what an install that moved
			// PERSISTENT_STATE_TARGET looks like from here: the trail is on
			// the partition, under a path this one does not point at. See
			// cnst.AuditLogStatePath.
			cfg.Logger.Infof("No audit trail at %s, nothing to preserve across the reset", src)
			return nil
		}

		size, err := fsutils.DirSize(cfg.Fs, src)
		if err != nil {
			return err
		}
		if size == 0 {
			cfg.Logger.Debugf("No audit trail to preserve in %s", src)
			return nil
		}

		stash, err = fsutils.TempDir(cfg.Fs, auditStashRoot, auditStashPrefix)
		if err != nil {
			return err
		}
		if err := roomFor(cfg.Fs, stash, size); err != nil {
			_ = cfg.Fs.RemoveAll(stash)
			stash = ""
			return err
		}
		if err := copyTree(cfg, src, stash); err != nil {
			// Half a copy is worse than none: the restore would put a
			// truncated audit trail back and nothing would say so.
			_ = cfg.Fs.RemoveAll(stash)
			stash = ""
			return err
		}
		cfg.Logger.Infof("Preserving %d bytes of %s across the reset", size, cnst.AuditLogPath)
		return nil
	})

	return stash, err
}

// RestoreAuditLog copies a staging directory made by StashAuditLog back onto
// the freshly formatted persistent partition and removes the staging
// directory. The restored directory is pinned to root-only, the same mode
// immucore pins the bind to on the next boot.
func RestoreAuditLog(cfg *sdkConfig.Config, persistent *sdkPartitions.Partition, stash string) error {
	if persistent == nil || stash == "" {
		return nil
	}
	defer func() {
		if err := cfg.Fs.RemoveAll(stash); err != nil {
			cfg.Logger.Warnf("could not remove the audit trail staging dir %s: %s", stash, err)
		}
	}()

	return withPersistentMounted(cfg, persistent, func(root string) error {
		dst := filepath.Join(root, cnst.AuditLogStatePath)
		if err := fsutils.MkdirAll(cfg.Fs, dst, cnst.AuditLogDirPerm); err != nil {
			return err
		}
		if err := copyTree(cfg, stash, dst); err != nil {
			return err
		}
		if err := pinRootOnly(cfg, dst); err != nil {
			return err
		}
		cfg.Logger.Infof("Restored %s onto the persistent partition", cnst.AuditLogPath)
		return nil
	})
}

// withPersistentMounted runs f with the persistent partition mounted and its
// mount point as the argument, and puts the mount state back the way it found
// it.
//
// Reset runs from the recovery system, which boots without the persistent
// volume on purpose, so the partition is usually neither mounted nor carrying
// a mount point at all. MountPartition reads the mount point off the partition
// struct, hence the temporary write here rather than a parameter.
//
// A failed unmount travels back to the caller and leaves the mount point on
// the partition. Both matter to what runs next: the format is only safe on a
// device nothing holds, and utils.IsMounted and elemental.UnmountPartition
// both read the mount point off the struct and treat an empty one as "not
// mounted", so putting it back while the device is still mounted would hide a
// live filesystem from the code that has to unmount it.
func withPersistentMounted(cfg *sdkConfig.Config, persistent *sdkPartitions.Partition, f func(root string) error) (err error) {
	if mounted, _ := utils.IsMounted(cfg, persistent); mounted {
		return f(persistent.MountPoint)
	}

	e := elemental.NewElemental(cfg)
	was := persistent.MountPoint
	if persistent.MountPoint == "" {
		persistent.MountPoint = cnst.PersistentDir
	}
	if mErr := e.MountPartition(persistent); mErr != nil {
		persistent.MountPoint = was
		return mErr
	}
	mountPoint := persistent.MountPoint
	defer func() {
		if uErr := e.UnmountPartition(persistent); uErr != nil {
			err = errors.Join(err, fmt.Errorf("unmounting the persistent partition from %s: %w", mountPoint, uErr))
			return
		}
		persistent.MountPoint = was
	}()

	return f(mountPoint)
}

// copyTree copies the contents of src into dst, directories and regular files
// only. Anything else under an audit log directory is not ours to guess at, so
// it is logged and left behind.
func copyTree(cfg *sdkConfig.Config, src, dst string) error {
	return fsutils.WalkDirFs(cfg.Fs, src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return fsutils.MkdirAll(cfg.Fs, target, info.Mode().Perm())
		case info.Mode().IsRegular():
			if err := fsutils.Copy(cfg.Fs, path, target); err != nil {
				return err
			}
			return cfg.Fs.Chmod(target, info.Mode().Perm())
		default:
			cfg.Logger.Debugf("Not copying %s: not a directory or a regular file", path)
			return nil
		}
	})
}

// pinRootOnly sets a directory to root-only, so the restored audit trail is no
// more readable than it was before the reset. Ownership is only changed when
// the process can change it, which outside of tests it always can.
func pinRootOnly(cfg *sdkConfig.Config, path string) error {
	if err := cfg.Fs.Chmod(path, cnst.AuditLogDirPerm); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return nil
	}
	raw, err := cfg.Fs.RawPath(path)
	if err != nil {
		return err
	}
	return os.Chown(raw, 0, 0)
}

// roomFor reports whether size bytes can be staged under dir, leaving as much
// free again for whatever else the reset needs. The staging area is a tmpfs,
// so filling it is a way to take the machine down in the middle of a reset.
func roomFor(vfs sdkFS.KairosFS, dir string, size int64) error {
	raw, err := vfs.RawPath(dir)
	if err != nil {
		return err
	}
	var st unix.Statfs_t
	if err := unix.Statfs(raw, &st); err != nil {
		return fmt.Errorf("checking the free space of %s: %w", dir, err)
	}
	available := int64(st.Bavail) * st.Bsize
	if size*2 > available {
		return fmt.Errorf("%s needs %d bytes and %s has %d free", cnst.AuditLogPath, size, dir, available)
	}
	return nil
}
