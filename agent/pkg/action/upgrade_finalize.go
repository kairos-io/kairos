package action

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkFS "github.com/kairos-io/kairos/v4/sdk/types/fs"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// UpgradeMode discriminates finalize dispatch: "grub" is the non-UKI /
// non-Kubernetes path (chroot handoff into the deployed rootfs), "uki" is
// the trusted-boot path (agent binary extracted from the signed .efi's
// .initrd section, no chroot). The zero value defaults to "grub" so a
// context written by an older host still deserializes to the historical
// behavior.
type UpgradeMode string

const (
	UpgradeModeGrub UpgradeMode = "grub"
	UpgradeModeUki  UpgradeMode = "uki"
)

// FinalizeContext carries the inputs the upgrade finalize step needs.
//
// Paths in the struct are always resolved as the caller sees them. When
// RunFinalize runs as a fallback on the host, they are host paths. When it
// runs inside the target rootfs via the upgrade-finalize subcommand
// (grub mode), the host filesystem is bind-mounted at /host,
// ImgMountPoint is "/", and every path that describes host state
// (transition image, state / recovery / OEM / persistent mount points,
// EFI partition) carries the /host prefix. In uki mode the target agent
// runs directly on the host filesystem (no chroot, the binary is
// extracted from the .initrd of the signed .efi) so paths are just the
// host's real ones; only the UKI-specific fields (EFI partition, entry,
// arch) are consulted.
type FinalizeContext struct {
	// Mode selects the finalize dispatch. Empty is treated as "grub" for
	// backward compatibility with contexts written before UKI support.
	Mode UpgradeMode `json:"mode,omitempty"`

	// --- grub-mode fields ---------------------------------------------------
	ImgMountPoint        string               `json:"imgMountPoint,omitempty"`
	TransitionImgFile    string               `json:"transitionImgFile,omitempty"`
	TransitionImgFS      string               `json:"transitionImgFS,omitempty"`
	RecoveryUpgrade      bool                 `json:"recoveryUpgrade,omitempty"`
	GrubDefEntry         string               `json:"grubDefEntry,omitempty"`
	ExtraDirsRootfs      []string             `json:"extraDirsRootfs,omitempty"`
	StateMountPoint      string               `json:"stateMountPoint,omitempty"`
	StateFSLabel         string               `json:"stateFSLabel,omitempty"`
	ActiveImgFile        string               `json:"activeImgFile,omitempty"`
	OEMMountPoint        string               `json:"oemMountPoint,omitempty"`
	PersistentMountPoint string               `json:"persistentMountPoint,omitempty"`
	EFIPartition         *SerializedPartition `json:"efiPartition,omitempty"`
	Arch                 string               `json:"arch,omitempty"`

	// --- uki-mode fields ----------------------------------------------------
	// UkiEntry is set on single-entry upgrades ("kairos-agent upgrade
	// --boot-entry <name>"). Empty means an active/passive rotation.
	UkiEntry string `json:"ukiEntry,omitempty"`
}

// SerializedPartition is the JSON-friendly subset of sdkPartitions.Partition
// that RunFinalize needs. It is defined here rather than reused from the SDK
// so the on-disk contract of upgrade-finalize does not silently drift the day
// the SDK adds a new field.
type SerializedPartition struct {
	Path            string `json:"path,omitempty"`
	Name            string `json:"name,omitempty"`
	MountPoint      string `json:"mountPoint,omitempty"`
	FS              string `json:"fs,omitempty"`
	FilesystemLabel string `json:"filesystemLabel,omitempty"`
}

func (p *SerializedPartition) toPartition() *sdkPartitions.Partition {
	if p == nil {
		return nil
	}
	return &sdkPartitions.Partition{
		Path:            p.Path,
		Name:            p.Name,
		MountPoint:      p.MountPoint,
		FS:              p.FS,
		FilesystemLabel: p.FilesystemLabel,
	}
}

func fromPartition(p *sdkPartitions.Partition) *SerializedPartition {
	if p == nil {
		return nil
	}
	return &SerializedPartition{
		Path:            p.Path,
		Name:            p.Name,
		MountPoint:      p.MountPoint,
		FS:              p.FS,
		FilesystemLabel: p.FilesystemLabel,
	}
}

// ReadFinalizeContext reads and decodes the context from the given file path
// on the given filesystem. Threading the fs through (rather than reaching
// for os.ReadFile) keeps this callable with the mockable vfs the rest of
// the action package uses in tests.
func ReadFinalizeContext(fs sdkFS.KairosFS, path string) (FinalizeContext, error) {
	var ctx FinalizeContext
	b, err := fs.ReadFile(path)
	if err != nil {
		return ctx, err
	}
	if err := json.Unmarshal(b, &ctx); err != nil {
		return ctx, err
	}
	return ctx, nil
}

// WriteFinalizeContext writes the context to the given file path as JSON on
// the given filesystem.
func WriteFinalizeContext(fs sdkFS.KairosFS, path string, ctx FinalizeContext) error {
	b, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return err
	}
	return fs.WriteFile(path, b, 0o600)
}

// newFinalizeContextForHost builds a context whose paths are the ones the
// running (host) agent would see: no /host prefix, ImgMountPoint is the
// mount point of the transition image, everything else is a real host
// path. Used as the base for NewFinalizeContextForHandoff, which then
// rewrites paths to look correct from inside the target chroot.
func newFinalizeContextForHost(spec *v1.UpgradeSpec, upgradeImg *sdkImages.Image) FinalizeContext {
	ctx := FinalizeContext{
		ImgMountPoint:     upgradeImg.MountPoint,
		TransitionImgFile: upgradeImg.File,
		TransitionImgFS:   upgradeImg.FS,
		RecoveryUpgrade:   spec.RecoveryUpgrade(),
		GrubDefEntry:      spec.GrubDefEntry,
		ExtraDirsRootfs:   spec.ExtraDirsRootfs,
	}
	if spec.Partitions.State != nil {
		ctx.StateMountPoint = spec.Partitions.State.MountPoint
		ctx.StateFSLabel = spec.Partitions.State.FilesystemLabel
		ctx.ActiveImgFile = filepath.Join(spec.Partitions.State.MountPoint, "cOS", constants.ActiveImgFile)
	}
	if spec.Partitions.OEM != nil {
		ctx.OEMMountPoint = spec.Partitions.OEM.MountPoint
	}
	if spec.Partitions.Persistent != nil {
		ctx.PersistentMountPoint = spec.Partitions.Persistent.MountPoint
	}
	if spec.Partitions.EFI != nil {
		ctx.EFIPartition = fromPartition(spec.Partitions.EFI)
	}
	return ctx
}

// NewFinalizeContextForHandoff builds a context for RunFinalize running as
// the target agent inside the chroot handoff. Every host-side path is
// rewritten to sit under hostPrefix (typically /host) so the target agent
// can reach it through the bind-mounts the host set up before entering the
// chroot; ImgMountPoint becomes "/" because we chroot into the mount point
// of the transition image.
func NewFinalizeContextForHandoff(spec *v1.UpgradeSpec, upgradeImg *sdkImages.Image, hostPrefix string) FinalizeContext {
	ctx := newFinalizeContextForHost(spec, upgradeImg)
	ctx.ImgMountPoint = "/"
	ctx.TransitionImgFile = joinHost(hostPrefix, ctx.TransitionImgFile)
	ctx.StateMountPoint = joinHost(hostPrefix, ctx.StateMountPoint)
	ctx.ActiveImgFile = joinHost(hostPrefix, ctx.ActiveImgFile)
	ctx.OEMMountPoint = joinHost(hostPrefix, ctx.OEMMountPoint)
	ctx.PersistentMountPoint = joinHost(hostPrefix, ctx.PersistentMountPoint)
	if ctx.EFIPartition != nil && ctx.EFIPartition.MountPoint != "" {
		ctx.EFIPartition.MountPoint = joinHost(hostPrefix, ctx.EFIPartition.MountPoint)
	}
	return ctx
}

func joinHost(prefix, path string) string {
	if path == "" || prefix == "" {
		return path
	}
	return filepath.Join(prefix, path)
}

// RunFinalize runs the post-deploy upgrade finalize steps that the target
// image's code owns: label state images, create rootfs extra dirs, SELinux
// relabel the target rootfs, run the after-upgrade-chroot hook, rebrand
// the default GRUB entry and refresh the ESP. Runs on the target side of
// the chroot handoff, invoked by the target's kairos-agent
// upgrade-finalize subcommand so a format change in any of these files
// ships with the image that owns it.
func RunFinalize(cfg *sdkConfig.Config, ctx FinalizeContext) error {
	e := elemental.NewElemental(cfg)
	inTargetChroot := ctx.ImgMountPoint == "/"

	// Label the state images for system upgrades. The existing active image
	// is relabeled so a boot back into it (failed upgrade, fallback boot)
	// finds it as boot_t even if it was deployed by an agent version that
	// did not label it. The transition image keeps the label through the
	// rename to active.img.
	if !ctx.RecoveryUpgrade {
		if ctx.ActiveImgFile != "" {
			e.LabelStateImage(ctx.ActiveImgFile)
		}
		if ctx.TransitionImgFile != "" {
			e.LabelStateImage(ctx.TransitionImgFile)
		}
	}

	// Create extra dirs in the rootfs while it is still writable.
	createExtraDirsInRootfs(cfg, ctx.ExtraDirsRootfs, ctx.ImgMountPoint)

	// SELinux relabel does not make sense on a read-only filesystem, so
	// squashfs upgrade images skip both the relabel and the chrooted hook
	// (the same skip the pre-refactor code applied).
	if ctx.TransitionImgFS != constants.SquashFs {
		if err := runSelinuxRelabel(cfg, e, ctx, inTargetChroot); err != nil {
			return err
		}
	}

	if err := runAfterUpgradeChrootHook(cfg, ctx, inTargetChroot); err != nil {
		cfg.Logger.Errorf("Error running hook after-upgrade-chroot: %s", err)
		return err
	}

	// Only apply rebrand and ESP refresh for system upgrades.
	if !ctx.RecoveryUpgrade {
		cfg.Logger.Info("rebranding")
		if err := e.SetDefaultGrubEntry(ctx.StateMountPoint, ctx.ImgMountPoint, ctx.GrubDefEntry); err != nil {
			cfg.Logger.Warn("failure while rebranding GRUB default entry (ignoring), run with --debug to see more details")
			cfg.Logger.Debug(err.Error())
		}

		if err := refreshESPFromCtx(cfg, ctx); err != nil {
			cfg.Logger.Errorf("Failed to refresh the ESP: %s", err)
			return err
		}
	}

	return nil
}

func runSelinuxRelabel(cfg *sdkConfig.Config, e *elemental.Elemental, ctx FinalizeContext, inTargetChroot bool) error {
	if inTargetChroot {
		// The outer handoff already chrooted us into the target rootfs and
		// bind-mounted OEM and persistent at their standard paths, so
		// SELinux can be relabeled in place without another chroot.
		return e.SelinuxRelabel("/", true)
	}
	return utils.ChrootedCallback(cfg, ctx.ImgMountPoint, chrootBindsForHooks(cfg, ctx), func() error {
		return e.SelinuxRelabel("/", true)
	})
}

func runAfterUpgradeChrootHook(cfg *sdkConfig.Config, ctx FinalizeContext, inTargetChroot bool) error {
	cfg.Logger.Infof("Applying '%s' hook", constants.AfterUpgradeChrootHook)
	if inTargetChroot {
		return Hook(cfg, constants.AfterUpgradeChrootHook)
	}
	return ChrootHook(cfg, constants.AfterUpgradeChrootHook, ctx.ImgMountPoint, chrootBindsForHooks(cfg, ctx))
}

func chrootBindsForHooks(cfg *sdkConfig.Config, ctx FinalizeContext) map[string]string {
	binds := map[string]string{}
	if ctx.OEMMountPoint != "" {
		if mnt, _ := utils.IsMounted(cfg, &sdkPartitions.Partition{MountPoint: ctx.OEMMountPoint}); mnt {
			binds[ctx.OEMMountPoint] = constants.OEMPath
		}
	}
	if ctx.PersistentMountPoint != "" {
		if mnt, _ := utils.IsMounted(cfg, &sdkPartitions.Partition{MountPoint: ctx.PersistentMountPoint}); mnt {
			binds[ctx.PersistentMountPoint] = constants.UsrLocalPath
		}
	}
	return binds
}

// refreshESPFromCtx is the RunFinalize-friendly form of
// UpgradeAction.refreshESP: it takes the EFI partition, source dir, arch,
// and state label out of a FinalizeContext instead of an UpgradeSpec so the
// same code covers both the inline-fallback and target-chroot call sites.
func refreshESPFromCtx(cfg *sdkConfig.Config, ctx FinalizeContext) error {
	if ctx.EFIPartition == nil {
		cfg.Logger.Debug("No EFI partition on this machine, skipping ESP refresh")
		return nil
	}

	efiPart := ctx.EFIPartition.toPartition()
	sourceDir := ctx.ImgMountPoint
	arch := ctx.Arch
	if arch == "" && cfg.Arch != "" {
		arch = cfg.Arch
	}

	e := elemental.NewElemental(cfg)
	umount, err := e.MountRWPartition(efiPart)
	if err != nil {
		// A machine that cannot mount its EFI partition has a problem the
		// upgrade did not cause; warn and continue rather than turning a
		// system upgrade that would otherwise have completed into a hard
		// failure over a best-effort bootloader refresh.
		cfg.Logger.Warnf("Skipping ESP refresh, could not mount the EFI partition: %s", err)
		return nil
	}
	defer func() {
		if uerr := umount(); uerr != nil {
			cfg.Logger.Warnf("failed to unmount the EFI partition after ESP refresh: %s", uerr)
		}
	}()

	if err := utils.CheckESPRefresh(cfg.Fs, arch, sourceDir, efiPart.MountPoint); err != nil {
		cfg.Logger.Warnf("Skipping ESP refresh: %s", err)
		return nil
	}

	grub := utils.NewGrub(cfg)
	if err := grub.RefreshESP(sourceDir, efiPart.MountPoint, ctx.StateFSLabel, "grub2"); err != nil {
		return fmt.Errorf("refreshing the ESP from %s: %w", sourceDir, err)
	}

	cfg.Logger.Infof("Refreshed shim, grub and grub.cfg on the EFI partition from %s", sourceDir)
	return nil
}
