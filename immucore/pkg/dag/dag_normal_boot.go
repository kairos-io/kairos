package dag

import (
	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	"github.com/spectrocloud-labs/herd"
)

// oemEncrypted reports whether the OEM partition on disk is a LUKS container.
// lookup.FindLUKSContainerByLabel handles both the pre-#4403 case (LUKS
// header carries constants.OEMLabel) and the post-#4403 case (LUKS header
// carries constants.OEMLUKSLabel, mapper's ext4 carries constants.OEMLabel),
// so callers see a single answer regardless of which install shape they are
// booting on.
func oemEncrypted() bool {
	oemLabel := internalUtils.GetOemLabel()
	if oemLabel == "" {
		return false
	}
	_, err := lookup.FindLUKSContainerByLabel(oemLabel)
	if err != nil {
		internalUtils.KLog.Logger.Debug().Str("label", oemLabel).Err(err).
			Msg("No LUKS container for OEM label; treating as unencrypted")
		return false
	}
	internalUtils.KLog.Logger.Debug().Str("label", oemLabel).
		Msg("OEM partition is a LUKS container")
	return true
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

	// Drop unit symlinks an earlier image left in the persistent /etc/systemd
	// bind, before the initramfs stage and switch_root.
	s.LogIfError(s.CleanStaleUnitsDagStep(g, herd.WithWeakDeps(cnst.OpMountBind)), "clean stale systemd units")

	//
	s.LogIfError(s.EnableSysAndConfExtensions(g, herd.WithWeakDeps(cnst.OpMountBind)), "enable sysext and confexts")

	// Write fstab file
	s.LogIfError(s.WriteFstabDagStep(g,
		herd.WithDeps(cnst.OpMountRoot, cnst.OpDiscoverState, cnst.OpLoadConfig),
		herd.WithWeakDeps(cnst.OpKcryptUnlock, cnst.OpMountOEM, cnst.OpCustomMounts, cnst.OpMountBind, cnst.OpOverlayMount)), "write fstab")

	// do it after fstab is created
	s.LogIfError(s.InitramfsStageDagStep(g,
		herd.WithDeps(cnst.OpMountRoot, cnst.OpDiscoverState, cnst.OpLoadConfig, cnst.OpWriteFstab),
		herd.WithWeakDeps(cnst.OpMountBaseOverlay, cnst.OpKcryptUnlock, cnst.OpMountOEM, cnst.OpMountBind, cnst.OpMountBind, cnst.OpCustomMounts, cnst.OpOverlayMount, cnst.OpCleanStaleUnits),
	), "initramfs stage")
	return err
}
