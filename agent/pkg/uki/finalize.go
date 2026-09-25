package uki

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	elementalUtils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	events "github.com/kairos-io/kairos/v4/sdk/bus"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
)

// RunFinalize runs the post-rotation UKI upgrade steps that the target
// image's code owns: systemd-boot sort key, boot assessment, default boot
// entry selection, loader.conf key cleanup and EFI key upgrades, plus the
// kairos-uki-upgrade.after yip stage and hook script. Running these from
// the target's own binary — extracted from the signed .efi's .initrd, see
// ExtractFromInitrd — is what lets a format change in loader/entries or
// the EFI key layout ship in the image without requiring every host
// agent already in the field to already understand the new shape.
//
// The context.EFIPartition mount point is where the artifacts live
// (constants.UkiEfiDir in a running system). No chroot: the extracted
// target agent runs directly on the host mount namespace, so the paths
// here are the host's real ones.
func RunFinalize(cfg *sdkConfig.Config, ctx action.FinalizeContext) error {
	if ctx.EFIPartition == nil || ctx.EFIPartition.MountPoint == "" {
		return fmt.Errorf("uki finalize: no EFI partition in context")
	}
	efiDir := ctx.EFIPartition.MountPoint
	arch := ctx.Arch
	if arch == "" {
		arch = cfg.Arch
	}

	if err := AddSystemdConfSortKey(cfg.Fs, efiDir, cfg.Logger); err != nil {
		cfg.Logger.Warnf("adding sort key: %s", err.Error())
	}

	if err := elementalUtils.AddBootAssessment(cfg.Fs, efiDir, cfg.Logger); err != nil {
		cfg.Logger.Warnf("adding boot assessment: %s", err.Error())
	}

	if err := action.SelectBootEntry(cfg, constants.BootEntryActive); err != nil {
		cfg.Logger.Errorf("selecting boot entry: %s", err.Error())
		return err
	}

	if err := removeDefaultKeysFromLoaderConf(cfg.Fs, efiDir, cfg.Logger); err != nil {
		cfg.Logger.Warnf("removing default keys from loader.conf: %s", err.Error())
	}

	if err := upgradeEfiKeysInLoaderEntries(arch, cfg.Fs, efiDir, cfg.Logger); err != nil {
		cfg.Logger.Warnf("upgrading efi keys in loader entries: %s", err.Error())
	}

	if err := elementalUtils.RunStage(cfg, "kairos-uki-upgrade.after"); err != nil {
		cfg.Logger.Errorf("running kairos-uki-upgrade.after stage: %s", err.Error())
	}

	if err := events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.after.hook"); err != nil {
		cfg.Logger.Errorf("running kairos-uki-upgrade.after hook script: %s", err.Error())
	}

	return nil
}
