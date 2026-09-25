/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package action

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	agentState "github.com/kairos-io/kairos/v4/agent/pkg/state"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/state"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
)

// UpgradeAction represents the struct that will run the upgrade from start to finish
type UpgradeAction struct {
	config *sdkConfig.Config
	spec   *v1.UpgradeSpec
}

func NewUpgradeAction(config *sdkConfig.Config, spec *v1.UpgradeSpec) *UpgradeAction {
	return &UpgradeAction{config: config, spec: spec}
}

func (u UpgradeAction) Info(s string, args ...interface{}) {
	u.config.Logger.Infof(s, args...)
}

func (u UpgradeAction) Debug(s string, args ...interface{}) {
	u.config.Logger.Debugf(s, args...)
}

func (u UpgradeAction) Error(s string, args ...interface{}) {
	u.config.Logger.Errorf(s, args...)
}

func (u UpgradeAction) upgradeHook(hook string, chroot bool) error {
	u.Info("Applying '%s' hook", hook)
	if chroot {
		extraMounts := map[string]string{}

		oemDevice := u.spec.Partitions.OEM
		if oemDevice != nil && oemDevice.MountPoint != "" {
			extraMounts[oemDevice.MountPoint] = constants.OEMPath
		}

		persistentDevice := u.spec.Partitions.Persistent
		if persistentDevice != nil && persistentDevice.MountPoint != "" {
			extraMounts[persistentDevice.MountPoint] = constants.UsrLocalPath
		}

		return ChrootHook(u.config, hook, u.spec.Active.MountPoint, extraMounts)
	}
	return Hook(u.config, hook)
}

// Run will upgrade the system from a given configuration
// nolint:gocyclo
func (u *UpgradeAction) Run() (err error) {
	var upgradeImg sdkImages.Image
	var finalImageFile string

	// Track where we booted from to have a different workflow
	// Booted from active: backup active into passive, upgrade active
	// Booted from passive: Upgrade active, leave passive as is
	var bootedFrom state.Boot

	bootedFrom, err = state.DetectBootWithVFS(u.config.Fs)
	if err != nil {
		u.config.Logger.Warnf("error detecting boot: %s", err)
	}

	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()

	e := elemental.NewElemental(u.config)

	if u.spec.RecoveryUpgrade() {
		upgradeImg = u.spec.Recovery
		if upgradeImg.FS == constants.SquashFs {
			finalImageFile = filepath.Join(u.spec.Partitions.Recovery.MountPoint, "cOS", constants.RecoverySquashFile)
		} else {
			finalImageFile = filepath.Join(u.spec.Partitions.Recovery.MountPoint, "cOS", constants.RecoveryImgFile)
		}
	} else {
		upgradeImg = u.spec.Active
		finalImageFile = filepath.Join(u.spec.Partitions.State.MountPoint, "cOS", constants.ActiveImgFile)
	}

	umount, err := e.MountRWPartition(u.spec.Partitions.State)
	if err != nil {
		return err
	}
	cleanup.Push(umount)
	umount, err = e.MountRWPartition(u.spec.Partitions.Recovery)
	if err != nil {
		return err
	}
	cleanup.Push(umount)

	// Cleanup transition image file before leaving
	cleanup.Push(func() error { return u.remove(upgradeImg.File) })

	// Recovery does not mount persistent, so try to mount it. Ignore errors, as it's not mandatory.
	// This was used by luet extraction IIRC to not exhaust the /tmp dir
	// Not sure if its on use anymore and we should drop it
	// TODO: Check if we really need persistent mounted here
	persistentPart := u.spec.Partitions.Persistent
	if persistentPart != nil {
		// Create the dir otherwise the check for mounted dir fails
		_ = fsutils.MkdirAll(u.config.Fs, persistentPart.MountPoint, constants.DirPerm)
		if mnt, err := utils.IsMounted(u.config, persistentPart); !mnt && err == nil {
			umount, err = e.MountRWPartition(persistentPart)
			if err != nil {
				u.config.Logger.Warnf("could not mount persistent partition: %s", err.Error())
			} else {
				cleanup.Push(umount)
			}
		}
	}

	// before upgrade hook happens once partitions are RW mounted, just before image OS is deployed
	err = u.upgradeHook(constants.BeforeUpgradeHook, false)
	if err != nil {
		u.Error("Error while running hook before-upgrade: %s", err)
		return err
	}

	u.Info("deploying image %s to %s", upgradeImg.Source.Value(), upgradeImg.File)
	_, err = e.DeployImage(&upgradeImg, true, u.spec.ExcludedPaths...)
	if err != nil {
		u.Error("Failed deploying image to file '%s': %s", upgradeImg.File, err)
		return err
	}
	cleanup.Push(func() error { return e.UnmountImage(&upgradeImg) })

	// Everything from labeling the state images to refreshing the ESP is
	// finalize work whose on-disk formats the target image owns (GRUB
	// entries, loader/entries/*.conf keys, boot-assessment flags,
	// after-upgrade-chroot hooks). Delegating it to the target agent lets a
	// format change ship in an image without needing every previously
	// released host agent to already understand that format (see
	// https://github.com/kairos-io/kairos/issues/4456). We first try to
	// hand off to the target's own kairos-agent inside a chroot; if the
	// target predates the upgrade-finalize subcommand we run the same
	// steps inline, preserving the historical behavior.
	if err = u.runFinalizeStep(&upgradeImg); err != nil {
		return err
	}

	err = e.UnmountImage(&upgradeImg)
	if err != nil {
		u.Error("failed unmounting transition image")
		return err
	}

	// If not upgrading recovery and booting from non passive, backup active into passive
	// We dont want to overwrite passive if we are booting from passive as it could mean that active is broken and we would
	// be overriding a working passive with a broken/unknown  active
	if !u.spec.RecoveryUpgrade() && bootedFrom != state.Passive {
		// backup current active.img to passive.img before overwriting the active.img
		u.Info("Backing up current active image")
		source := filepath.Join(u.spec.Partitions.State.MountPoint, "cOS", constants.ActiveImgFile)
		u.Info("Moving %s to %s", source, u.spec.Passive.File)
		err = u.config.Fs.Rename(source, u.spec.Passive.File)
		if err != nil {
			u.Error("Failed to move %s to %s: %s", source, u.spec.Passive.File, err)
			return err
		}
		u.Info("Finished moving %s to %s", source, u.spec.Passive.File)
		// Label the image to passive!
		out, err := u.config.Runner.Run("tune2fs", "-L", u.spec.Passive.Label, u.spec.Passive.File)
		if err != nil {
			u.Error("Error while labeling the passive image %s: %s", u.spec.Passive.File, err)
			u.Debug("Error while labeling the passive image %s, command output: %s", u.spec.Passive.File, out)
			return err
		}
		syscall.Sync()
	}

	u.Info("Moving %s to %s", upgradeImg.File, finalImageFile)
	err = u.config.Fs.Rename(upgradeImg.File, finalImageFile)
	if err != nil {
		u.Error("Failed to move %s to %s: %s", upgradeImg.File, finalImageFile, err)
		return err
	}
	u.Info("Finished moving %s to %s", upgradeImg.File, finalImageFile)

	syscall.Sync()

	err = u.upgradeHook(constants.AfterUpgradeHook, false)
	if err != nil {
		u.Error("Error running hook after-upgrade: %s", err)
		return err
	}

	u.Info("Upgrade completed")
	if !u.spec.RecoveryUpgrade() {
		u.config.Logger.Warn("Remember that recovery is upgraded separately by passing the --recovery flag to the upgrade command!\n" +
			"See more info about this on https://kairos.io/docs/upgrade/")

		// Point the next boot at the active entry so the newly upgraded image
		// is tried on reboot even if we upgraded from passive. next_entry is
		// one-shot: if active fails, the normal boot-assessment fallback still
		// applies on subsequent boots.
		if err := SelectBootEntry(u.config, constants.BootEntryActive); err != nil {
			u.config.Logger.Warnf("could not set next boot entry to %s: %s", constants.BootEntryActive, err)
		}
	}

	u.recordState()

	// Do not reboot/poweroff on cleanup errors
	if cleanErr := cleanup.Cleanup(err); cleanErr != nil {
		u.config.Logger.Warn("failure during cleanup (ignoring), run with --debug to see more details")
		u.config.Logger.Debug(cleanErr.Error())
	}

	return nil
}

func (u *UpgradeAction) recordState() {
	if u.spec.Partitions.Persistent == nil || u.spec.Partitions.Persistent.MountPoint == "" {
		u.config.Logger.Debug("persistent partition not available, skipping kairos state file update")
		return
	}
	mount := u.spec.Partitions.Persistent.MountPoint
	var err error
	if u.spec.RecoveryUpgrade() {
		err = agentState.RecordRecoveryUpgrade(u.config.Fs, mount, u.spec.Recovery.Source.String(), time.Now)
	} else {
		err = agentState.RecordActiveUpgrade(u.config.Fs, mount, u.spec.Active.Source.String(), time.Now)
	}
	if err != nil {
		u.config.Logger.Warnf("failed to update kairos state file: %s", err)
	}
}

// remove attempts to remove the given path. Does nothing if it doesn't exist
func (u *UpgradeAction) remove(path string) error {
	if exists, _ := fsutils.Exists(u.config.Fs, path); exists {
		u.Debug("[Cleanup] Removing %s", path)
		return u.config.Fs.RemoveAll(path)
	}
	return nil
}

// runFinalizeStep gates the upgrade on KAIROS_INIT_VERSION (target must
// be >= current — kairos-io/kairos#4917, kairos-io/kairos#4953) and, on
// pass, hands off to the target's own kairos-agent for the finalize step.
// The gate reads KAIROS_INIT_VERSION rather than KAIROS_VERSION so a
// derivative image whose own product version rolls backwards is not
// refused when its build tooling is fine, and — the more important half
// — an image with a hand-picked KAIROS_VERSION cannot bypass the gate
// when its build tooling actually predates upgrade-finalize.
// The gate runs after DeployImage but before any rename or
// after-upgrade-hook side effect, so a refusal unwinds through cleanup
// and leaves active.img untouched. A runtime failure of the handoff
// itself is propagated, not retried inline: by then the target has
// likely already labeled images, rebranded GRUB or refreshed the ESP,
// and an older host doing the same work would double-write. The
// transition image is still mounted at upgradeImg.MountPoint on return;
// unmounting and renaming remain the host's responsibility (see
// UpgradeAction.Run below).
func (u *UpgradeAction) runFinalizeStep(upgradeImg *sdkImages.Image) error {
	current, err := ReadKairosInitVersionFromFs(u.config.Fs, constants.KairosReleaseFile)
	if err != nil {
		return fmt.Errorf("reading current KAIROS_INIT_VERSION: %w", err)
	}
	target, err := ReadKairosInitVersionFromFs(u.config.Fs, filepath.Join(upgradeImg.MountPoint, constants.KairosReleaseFile))
	if err != nil {
		return fmt.Errorf("reading target KAIROS_INIT_VERSION from the deployed rootfs: %w", err)
	}
	if err := RefuseIfTargetIsDowngrade(current, target); err != nil {
		return err
	}
	return u.handoffFinalizeToTarget(upgradeImg)
}

// handoffFinalizeToTarget runs the finalize step by chrooting into the
// deployed target rootfs and executing its kairos-agent upgrade-finalize
// subcommand. Host filesystem paths the target needs (state partition,
// recovery partition, OEM, persistent, EFI) are bind-mounted under
// /host inside the target chroot; OEM and persistent are also bind-mounted
// at their standard /oem and /usr/local paths so the after-upgrade-chroot
// yip stages see them where they expect. The upgrade-finalize context is
// serialized to a JSON file inside the target rootfs (under /tmp/) and
// read back by the target agent.
func (u *UpgradeAction) handoffFinalizeToTarget(upgradeImg *sdkImages.Image) error {
	ctxPathInsideTarget := constants.UpgradeFinalizeContextPath
	ctxPathOnHost := filepath.Join(upgradeImg.MountPoint, ctxPathInsideTarget)

	handoffCtx := NewFinalizeContextForHandoff(u.spec, upgradeImg, constants.HandoffHostPrefix)
	handoffCtx.Arch = u.config.Arch
	// Ensure the target's /tmp exists before writing the context there;
	// on a freshly-deployed rootfs the standard /tmp directory is present
	// under the default dir structure, but for tests using a bare mount
	// point we might get here before anything created it.
	if err := fsutils.MkdirAll(u.config.Fs, filepath.Dir(ctxPathOnHost), constants.DirPerm); err != nil {
		return fmt.Errorf("preparing %s for finalize context: %w", filepath.Dir(ctxPathOnHost), err)
	}
	if err := WriteFinalizeContext(u.config.Fs, ctxPathOnHost, handoffCtx); err != nil {
		return fmt.Errorf("writing finalize context to %s: %w", ctxPathOnHost, err)
	}
	// Remove the context file on return whether the handoff succeeded or
	// failed. The file lives inside the ext4 image, so unmounting the
	// transition image or renaming it to active.img does NOT delete it;
	// Kairos overlays /tmp with a tmpfs at boot so it becomes invisible
	// on the upgraded system, but the bytes stay on disk forever without
	// this Remove.
	defer func() {
		if err := u.config.Fs.Remove(ctxPathOnHost); err != nil && !os.IsNotExist(err) {
			u.config.Logger.Debugf("could not remove finalize context %s: %s", ctxPathOnHost, err)
		}
	}()

	binds := u.finalizeHandoffBinds()
	callback := func() error {
		u.Info("Handing off upgrade finalize to target kairos-agent (%s)", constants.TargetKairosAgentPath)
		out, err := u.config.Runner.Run(constants.TargetKairosAgentPath, "upgrade-finalize", "--context-file", ctxPathInsideTarget)
		if len(out) > 0 {
			u.config.Logger.Infof("upgrade-finalize output: %s", string(out))
		}
		return err
	}
	return utils.ChrootedCallback(u.config, upgradeImg.MountPoint, binds, callback)
}

// finalizeHandoffBinds returns the bind mounts the target chroot needs so
// the target agent can reach the host filesystem the same way it does under
// Kubernetes (host at /host), and so the after-upgrade-chroot hook still
// finds OEM at /oem and persistent at /usr/local inside the chroot.
func (u *UpgradeAction) finalizeHandoffBinds() map[string]string {
	binds := map[string]string{}
	addBind := func(src, dst string) {
		if src == "" || dst == "" {
			return
		}
		binds[src] = dst
	}
	if u.spec.Partitions.State != nil {
		addBind(u.spec.Partitions.State.MountPoint, filepath.Join(constants.HandoffHostPrefix, u.spec.Partitions.State.MountPoint))
	}
	if u.spec.Partitions.Recovery != nil {
		addBind(u.spec.Partitions.Recovery.MountPoint, filepath.Join(constants.HandoffHostPrefix, u.spec.Partitions.Recovery.MountPoint))
	}
	if u.spec.Partitions.OEM != nil {
		addBind(u.spec.Partitions.OEM.MountPoint, filepath.Join(constants.HandoffHostPrefix, u.spec.Partitions.OEM.MountPoint))
		// Also expose OEM at its standard path so hooks in the
		// after-upgrade-chroot stage keep finding it where they expect.
		if mnt, _ := utils.IsMounted(u.config, u.spec.Partitions.OEM); mnt {
			addBind(u.spec.Partitions.OEM.MountPoint, constants.OEMPath)
		}
	}
	if u.spec.Partitions.Persistent != nil {
		addBind(u.spec.Partitions.Persistent.MountPoint, filepath.Join(constants.HandoffHostPrefix, u.spec.Partitions.Persistent.MountPoint))
		if mnt, _ := utils.IsMounted(u.config, u.spec.Partitions.Persistent); mnt {
			addBind(u.spec.Partitions.Persistent.MountPoint, constants.UsrLocalPath)
		}
	}
	if u.spec.Partitions.EFI != nil && u.spec.Partitions.EFI.MountPoint != "" {
		if mnt, _ := utils.IsMounted(u.config, u.spec.Partitions.EFI); mnt {
			addBind(u.spec.Partitions.EFI.MountPoint, filepath.Join(constants.HandoffHostPrefix, u.spec.Partitions.EFI.MountPoint))
		}
	}
	return binds
}
