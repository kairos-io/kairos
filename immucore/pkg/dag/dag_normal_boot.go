package dag

import (
	"fmt"
	"strings"

	cnst "github.com/kairos-io/immucore/internal/constants"
	internalUtils "github.com/kairos-io/immucore/internal/utils"
	"github.com/kairos-io/immucore/pkg/state"
	"github.com/kairos-io/kairos-sdk/ghw"
	"github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/spectrocloud-labs/herd"
)

// oemEncrypted checks if the OEM partition is encrypted (LUKS).
// It uses kairos-sdk's lightweight ghw to find the partition and blkid to check if it's LUKS encrypted.
func oemEncrypted() bool {
	oemLabel := internalUtils.GetOemLabel()
	if oemLabel == "" {
		// No OEM label found, assume not encrypted
		return false
	}

	// Use kairos-sdk's lightweight ghw to get disks
	disks := ghw.GetDisks(ghw.NewPaths(""), new(internalUtils.KLog))
	if disks == nil {
		// If we can't read block devices, assume not encrypted to be safe
		internalUtils.KLog.Logger.Warn().Msg("Error reading partitions, assuming OEM is not encrypted")
		return false
	}

	// Find the partition with the OEM label
	var oemPartition *partitions.Partition
	for _, disk := range disks {
		for _, p := range disk.Partitions {
			if p.FilesystemLabel == oemLabel {
				oemPartition = p
				break
			}
		}
		if oemPartition != nil {
			break
		}
	}

	if oemPartition == nil {
		// Partition not found, assume not encrypted
		return false
	}

	devicePath := oemPartition.Path
	if devicePath == "" {
		// No device path found, assume not encrypted
		internalUtils.KLog.Logger.Debug().Str("label", oemLabel).Msg("OEM partition found but no device path, assuming not encrypted")
		return false
	}

	// Use blkid to check if this specific device is LUKS encrypted
	// blkid -p <device> -s TYPE -o value returns "crypto_LUKS" if encrypted, or filesystem type if not
	deviceType, err := utils.SH(fmt.Sprintf("blkid -p %s -s TYPE -o value", devicePath))
	deviceType = strings.TrimSpace(deviceType)

	if err != nil || deviceType == "" {
		// Error checking or no type found, assume not encrypted to be safe
		internalUtils.KLog.Logger.Debug().Str("label", oemLabel).Str("device", devicePath).Msg("Could not determine device type, assuming OEM is not encrypted")
		return false
	}

	isEncrypted := deviceType == "crypto_LUKS"
	internalUtils.KLog.Logger.Debug().Str("label", oemLabel).Str("device", devicePath).Str("type", deviceType).Bool("encrypted", isEncrypted).Msg("Checked OEM partition encryption status")
	return isEncrypted
}

// RegisterNormalBoot registers a dag for a normal boot, where we want to mount all the pieces that make up the
// final system. This mounts root, oem, runs rootfs, loads the cos-layout.env file and mounts custom stuff from that file
// and finally writes the fstab.
// This is all done on initramfs, very early, and ends up pivoting to the final system, usually under /sysroot.
func RegisterNormalBoot(s *state.State, g *herd.Graph) error {
	var err error

	s.LogIfError(s.LVMActivation(g), "lvm activation")

	// Maybe LogIfErrorAndPanic ? If no sentinel, a lot of config files are not going to run
	if err = s.LogIfErrorAndReturn(s.WriteSentinelDagStep(g), "write sentinel"); err != nil {
		return err
	}

	s.LogIfError(s.MountTmpfsDagStep(g), "tmpfs mount")

	// Mount Root (COS_STATE or COS_RECOVERY and then the image active/passive/recovery under s.Rootdir)
	s.LogIfError(s.MountRootDagStep(g), "running mount root stage")

	// Upgrade kcrypt partitions to kcrypt 0.6.0 if any
	// Depend on LVM in case the LVM is encrypted somehow? Not sure if possible.
	s.LogIfError(s.RunKcryptUpgrade(g, herd.WithDeps(cnst.OpLvmActivate)), "upgrade kcrypt partitions")

	var kcryptDeps, oemMountDeps herd.OpOption
	isOemEncrypted := oemEncrypted()
	internalUtils.KLog.Logger.Info().Bool("oem_encrypted", isOemEncrypted).Msg("Checking OEM encryption status")
	if isOemEncrypted {
		// We need to run partition unlocking before we mount OEM
		internalUtils.KLog.Logger.Info().Msg("OEM is encrypted: kcrypt unlock will run before OEM mount")
		kcryptDeps = herd.WithDeps(cnst.OpMountRoot, cnst.OpKcryptUpgrade)
		oemMountDeps = herd.WithDeps(cnst.OpMountRoot, cnst.OpLvmActivate, cnst.OpKcryptUnlock)
	} else {
		// We need to mount OEM before we run partition unlocking because old installations
		// may not have the needed KMS configuration in the cmdline.
		internalUtils.KLog.Logger.Info().Msg("OEM is NOT encrypted: OEM mount will run before kcrypt unlock")
		kcryptDeps = herd.WithDeps(cnst.OpMountRoot, cnst.OpKcryptUpgrade, cnst.OpMountOEM)
		oemMountDeps = herd.WithDeps(cnst.OpMountRoot, cnst.OpLvmActivate)
	}

	s.LogIfError(s.RunKcrypt(g, kcryptDeps), "kcrypt unlock")
	s.LogIfError(s.MountOemDagStep(g, oemMountDeps), "oem mount")

	// Run yip stage rootfs. Requires root+oem+sentinel to be mounted
	s.LogIfError(s.RootfsStageDagStep(g, herd.WithDeps(cnst.OpMountRoot, cnst.OpMountOEM, cnst.OpSentinel)), "running rootfs stage")

	// Populate state bind mounts, overlay mounts, custom-mounts from /run/cos/cos-layout.env
	// Requires stage rootfs to have run, which usually creates the cos-layout.env file
	s.LogIfError(s.LoadEnvLayoutDagStep(g), "loading cos-layout.env")

	// Mount base overlay under /run/overlay
	s.LogIfError(s.MountBaseOverlayDagStep(g), "base overlay mount")

	// Mount custom overlays loaded from the /run/cos/cos-layout.env file
	s.LogIfError(s.MountCustomOverlayDagStep(g), "custom overlays mount")

	s.LogIfError(s.MountCustomMountsDagStep(g), "custom mounts mount")

	// Mount custom binds loaded from the /run/cos/cos-layout.env file
	// Depends on mount binds as that usually mounts COS_PERSISTENT
	s.LogIfError(s.MountCustomBindsDagStep(g), "custom binds mount")

	//
	s.LogIfError(s.EnableSysAndConfExtensions(g, herd.WithWeakDeps(cnst.OpMountBind)), "enable sysext and confexts")

	// Write fstab file
	s.LogIfError(s.WriteFstabDagStep(g,
		herd.WithDeps(cnst.OpMountRoot, cnst.OpDiscoverState, cnst.OpLoadConfig),
		herd.WithWeakDeps(cnst.OpKcryptUnlock, cnst.OpMountOEM, cnst.OpCustomMounts, cnst.OpMountBind, cnst.OpOverlayMount)), "write fstab")

	// do it after fstab is created
	s.LogIfError(s.InitramfsStageDagStep(g,
		herd.WithDeps(cnst.OpMountRoot, cnst.OpDiscoverState, cnst.OpLoadConfig, cnst.OpWriteFstab),
		herd.WithWeakDeps(cnst.OpMountBaseOverlay, cnst.OpKcryptUnlock, cnst.OpMountOEM, cnst.OpMountBind, cnst.OpMountBind, cnst.OpCustomMounts, cnst.OpOverlayMount),
	), "initramfs stage")
	return err
}
