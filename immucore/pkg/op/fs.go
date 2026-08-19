package op

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/mount"
	"github.com/kairos-io/immucore/internal/constants"
	internalUtils "github.com/kairos-io/immucore/internal/utils"
	"github.com/kairos-io/immucore/pkg/schema"
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
		tmpMount := mount.Mount{Type: "tmpfs", Source: "tmpfs", Options: []string{fmt.Sprintf("size=%s", dat[1])}}
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

// https://github.com/kairos-io/packages/blob/94aa3bef3d1330cb6c6905ae164f5004b6a58b8c/packages/system/dracut/immutable-rootfs/30cos-immutable-rootfs/cos-mount-layout.sh#L183
func MountBind(mountpoint, root, stateTarget string) MountOperation {
	mountpoint = strings.TrimLeft(mountpoint, "/") // normalize, remove / upfront as we are going to re-use it in subdirs
	rootMount := filepath.Join(root, mountpoint)
	bindMountPath := strings.ReplaceAll(mountpoint, "/", "-")

	stateDir := filepath.Join(root, stateTarget, fmt.Sprintf("%s.bind", bindMountPath))

	tmpMount := mount.Mount{
		Type:   "overlay",
		Source: stateDir,
		Options: []string{
			"bind",
		},
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
			if err := internalUtils.CreateIfNotExists(rootMount); err != nil {
				return err
			}

			if err := internalUtils.CreateIfNotExists(stateDir); err != nil {
				return err
			}
			return internalUtils.SyncState(internalUtils.AppendSlash(rootMount), internalUtils.AppendSlash(stateDir))
		},
	}
}

// https://github.com/kairos-io/packages/blob/94aa3bef3d1330cb6c6905ae164f5004b6a58b8c/packages/system/dracut/immutable-rootfs/30cos-immutable-rootfs/cos-mount-layout.sh#L145
func MountWithBaseOverlay(mountpoint, root, base string) MountOperation {
	mountpoint = strings.TrimLeft(mountpoint, "/") // normalize, remove / upfront as we are going to re-use it in subdirs
	rootMount := filepath.Join(root, mountpoint)
	bindMountPath := strings.ReplaceAll(mountpoint, "/", "-")

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
