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

	// Create extra dirs in rootfs as afterwards this will be impossible due to RO system
	createExtraDirsInRootfs(u.config, u.spec.ExtraDirsRootfs, upgradeImg.MountPoint)

	// Selinux relabel
	// Doesn't make sense to relabel a readonly filesystem
	if upgradeImg.FS != constants.SquashFs {
		// Relabel SELinux
		// TODO probably relabelling persistent volumes should be an opt in feature, it could
		// have undesired effects in case of failures
		binds := map[string]string{}
		if mnt, _ := utils.IsMounted(u.config, u.spec.Partitions.Persistent); mnt {
			binds[u.spec.Partitions.Persistent.MountPoint] = constants.UsrLocalPath
		}
		if mnt, _ := utils.IsMounted(u.config, u.spec.Partitions.OEM); mnt {
			binds[u.spec.Partitions.OEM.MountPoint] = constants.OEMPath
		}
		err = utils.ChrootedCallback(
			u.config, upgradeImg.MountPoint, binds,
			func() error { return e.SelinuxRelabel("/", true) },
		)
		if err != nil {
			return err
		}
	}

	err = u.upgradeHook(constants.AfterUpgradeChrootHook, true)
	if err != nil {
		u.Error("Error running hook after-upgrade-chroot: %s", err)
		return err
	}

	// Only apply rebrand stage for system upgrades
	if !u.spec.RecoveryUpgrade() {
		u.Info("rebranding")
		if rebrandingErr := e.SetDefaultGrubEntry(u.spec.Partitions.State.MountPoint, upgradeImg.MountPoint, u.spec.GrubDefEntry); rebrandingErr != nil {
			u.config.Logger.Warn("failure while rebranding GRUB default entry (ignoring), run with --debug to see more details")
			u.config.Logger.Debug(rebrandingErr.Error())
		}

		// Refresh shim.efi, grub.efi and EFI/boot/grub.cfg on the ESP from
		// the freshly deployed image, so a signed-shim rotation or a
		// grub.cfg change reaches installed nodes on upgrade rather than
		// only on install/reset. The image is still mounted at
		// upgradeImg.MountPoint, so the shim/grub binaries are readable
		// from here. Failure is fatal only if the ESP write itself fails;
		// missing EFI partition, missing shim/grub in the source, or a
		// space check that will not fit are all logged and skipped.
		if err := u.refreshESP(upgradeImg.MountPoint); err != nil {
			u.Error("Failed to refresh the ESP: %s", err)
			return err
		}
	}

	err = e.UnmountImage(&upgradeImg)
	if err != nil {
		u.Error("failed unmounting transition image")
		return err
	}

	err = u.backupActiveToPassive(bootedFrom)
	if err != nil {
		return err
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

func (u *UpgradeAction) backupActiveToPassive(bootedFrom state.Boot) error {
	// If not upgrading recovery and booting from non passive, backup active into passive.
	// We dont want to overwrite passive if we are booting from passive as it could mean that active is broken and we would
	// be overriding a working passive with a broken/unknown active.
	if u.spec.RecoveryUpgrade() || bootedFrom == state.Passive {
		return nil
	}

	// backup current active.img to passive.img before overwriting the active.img
	u.Info("Backing up current active image")
	source := filepath.Join(u.spec.Partitions.State.MountPoint, "cOS", constants.ActiveImgFile)
	u.Info("Moving %s to %s", source, u.spec.Passive.File)
	if err := u.config.Fs.Rename(source, u.spec.Passive.File); err != nil {
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

// refreshESP copies the shim, grub and stub grub.cfg from the newly deployed
// image into the ESP, so the bootloader is refreshed on upgrade for the same
// reasons install and reset already refresh it (signed-shim rotation, grub
// CVEs, grub.cfg schema changes).
//
// This handles GRUB-mode systems only. UKI-mode upgrades refresh the ESP
// through agent/pkg/uki/upgrade.go. Machines with no EFI partition (BIOS
// GRUB) skip cleanly because Partitions.EFI is nil. Machines with too little
// ESP room to fit the refresh, and images that ship no shim or grub at a path
// Kairos knows, skip with a warning, so neither a tight partition nor a moved
// binary turns a working upgrade into a boot failure.
func (u *UpgradeAction) refreshESP(sourceDir string) error {
	efiPart := u.spec.Partitions.EFI
	if efiPart == nil {
		u.Debug("No EFI partition on this machine, skipping ESP refresh")
		return nil
	}

	e := elemental.NewElemental(u.config)
	umount, err := e.MountRWPartition(efiPart)
	if err != nil {
		// The ESP refresh is a best-effort enhancement over the pre-existing
		// upgrade contract (which never touched the ESP). A machine that
		// cannot mount its EFI partition has a pre-existing problem the
		// upgrade did not cause, so warn and continue rather than aborting a
		// system upgrade that would otherwise have completed.
		u.config.Logger.Warnf("Skipping ESP refresh, could not mount the EFI partition: %s", err)
		return nil
	}
	defer func() {
		if uerr := umount(); uerr != nil {
			u.config.Logger.Warnf("failed to unmount the EFI partition after ESP refresh: %s", uerr)
		}
	}()

	if err := utils.CheckESPRefresh(u.config.Fs, u.config.Arch, sourceDir, efiPart.MountPoint); err != nil {
		u.config.Logger.Warnf("Skipping ESP refresh: %s", err)
		return nil
	}

	grub := utils.NewGrub(u.config)
	if err := grub.RefreshESP(sourceDir, efiPart.MountPoint, u.spec.Partitions.State.FilesystemLabel, "grub2"); err != nil {
		return fmt.Errorf("refreshing the ESP from %s: %w", sourceDir, err)
	}

	u.Info("Refreshed shim, grub and grub.cfg on the EFI partition from %s", sourceDir)
	return nil
}
