package dag

import (
	cnst "github.com/kairos-io/immucore/internal/constants"
	internalUtils "github.com/kairos-io/immucore/internal/utils"
	"github.com/kairos-io/immucore/pkg/state"
	"github.com/spectrocloud-labs/herd"
)

// RegisterInRAMBoot registers the DAG for the kairos.ram workflow: the
// rootfs is a tmpfs staged by dracut's rd.live.ram (over the squashfs from ISO
// or netboot), while COS_OEM and COS_PERSISTENT are still discovered and
// mounted from local disk. Every workstation in a PXE-served fleet boots the
// same image but keeps per-machine cloud-init + user state on the HDD.
//
// This mirrors RegisterNormalBoot but drops LVM activation and the whole
// mount-root / discover-state / mount-state chain (dracut has already handed
// us /sysroot). It keeps the OEM + kcrypt ordering rules, the rootfs yip
// stage, cos-layout.env loading, overlay/custom/bind mounts, sysext/confext,
// fstab writing, and the initramfs stage — so the resulting system is
// indistinguishable from a real Active install for cloud-init and userland.
func RegisterInRAMBoot(s *state.State, g *herd.Graph) error {
	var err error

	// Maybe LogIfErrorAndPanic ? If no sentinel, a lot of config files are not going to run
	if err = s.LogIfErrorAndReturn(s.WriteSentinelDagStep(g), "write sentinel"); err != nil {
		return err
	}

	s.LogIfError(s.MountTmpfsDagStep(g), "tmpfs mount")

	// dracut's rd.live.ram already staged /sysroot for us; just wait for it.
	s.LogIfError(s.WaitForSysrootDagStep(g), "waiting for sysroot")

	// First-boot partition provisioning. Idempotent: no-op when both
	// COS_OEM and COS_PERSISTENT already exist. When either is missing this
	// either creates them (if kairos.ram.create_partitions is set) or
	// halts the boot with an actionable message. Must run BEFORE every
	// downstream step that expects those labels to exist (kcrypt, mount-oem,
	// custom-mounts).
	s.LogIfError(s.EnsurePartitionsDagStep(g, cnst.OpWaitForSysroot), "ensure partitions")

	// Upgrade kcrypt partitions to kcrypt 0.6.0 if any. Runs after we know
	// the partitions actually exist.
	s.LogIfError(s.RunKcryptUpgrade(g, herd.WithDeps(cnst.OpEnsurePartitions)), "upgrade kcrypt partitions")

	var kcryptDeps, oemMountDeps herd.OpOption
	isOemEncrypted := oemEncrypted()
	internalUtils.KLog.Logger.Info().Bool("oem_encrypted", isOemEncrypted).Msg("Checking OEM encryption status")
	if isOemEncrypted {
		internalUtils.KLog.Logger.Info().Msg("OEM is encrypted: kcrypt unlock will run before OEM mount")
		kcryptDeps = herd.WithDeps(cnst.OpEnsurePartitions, cnst.OpKcryptUpgrade)
		oemMountDeps = herd.WithDeps(cnst.OpEnsurePartitions, cnst.OpKcryptUnlock)
	} else {
		internalUtils.KLog.Logger.Info().Msg("OEM is NOT encrypted: OEM mount will run before kcrypt unlock")
		kcryptDeps = herd.WithDeps(cnst.OpEnsurePartitions, cnst.OpKcryptUpgrade, cnst.OpMountOEM)
		oemMountDeps = herd.WithDeps(cnst.OpEnsurePartitions)
	}

	s.LogIfError(s.RunKcrypt(g, kcryptDeps), "kcrypt unlock")
	s.LogIfError(s.MountOemDagStep(g, oemMountDeps), "oem mount")

	// Run yip stage rootfs. Requires sysroot+oem+sentinel to be ready.
	s.LogIfError(s.RootfsStageDagStep(g, herd.WithDeps(cnst.OpWaitForSysroot, cnst.OpMountOEM, cnst.OpSentinel)), "running rootfs stage")

	// Populate state bind mounts, overlay mounts, custom-mounts from /run/cos/cos-layout.env
	// Requires stage rootfs to have run, which usually creates the cos-layout.env file
	s.LogIfError(s.LoadEnvLayoutDagStep(g), "loading cos-layout.env")

	// Mount base overlay under /run/overlay
	s.LogIfError(s.MountBaseOverlayDagStep(g), "base overlay mount")

	// Mount custom overlays loaded from the /run/cos/cos-layout.env file
	s.LogIfError(s.MountCustomOverlayDagStep(g), "custom overlays mount")

	// Mount custom mounts — this is what mounts COS_PERSISTENT rw at /usr/local.
	s.LogIfError(s.MountCustomMountsDagStep(g), "custom mounts mount")

	// Bind mounts backed by the persistent-state target (COS_PERSISTENT).
	s.LogIfError(s.MountCustomBindsDagStep(g), "custom binds mount")

	s.LogIfError(s.EnableSysAndConfExtensions(g, herd.WithWeakDeps(cnst.OpMountBind)), "enable sysext and confexts")

	// Write fstab. Same deps as normal boot minus the mount-root chain.
	s.LogIfError(s.WriteFstabDagStep(g,
		herd.WithDeps(cnst.OpWaitForSysroot, cnst.OpLoadConfig),
		herd.WithWeakDeps(cnst.OpKcryptUnlock, cnst.OpMountOEM, cnst.OpCustomMounts, cnst.OpMountBind, cnst.OpOverlayMount)), "write fstab")

	s.LogIfError(s.InitramfsStageDagStep(g,
		herd.WithDeps(cnst.OpWaitForSysroot, cnst.OpLoadConfig, cnst.OpWriteFstab),
		herd.WithWeakDeps(cnst.OpMountBaseOverlay, cnst.OpKcryptUnlock, cnst.OpMountOEM, cnst.OpMountBind, cnst.OpCustomMounts, cnst.OpOverlayMount),
	), "initramfs stage")
	return err
}
