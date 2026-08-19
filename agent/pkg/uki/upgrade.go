package uki

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	elementalUtils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/signatures"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"github.com/kairos-io/kairos-sdk/utils"
)

type UpgradeAction struct {
	cfg  *sdkConfig.Config
	spec *v1.UpgradeUkiSpec
}

func NewUpgradeAction(cfg *sdkConfig.Config, spec *v1.UpgradeUkiSpec) *UpgradeAction {
	return &UpgradeAction{cfg: cfg, spec: spec}
}

func (i *UpgradeAction) Run() (err error) {
	e := elemental.NewElemental(i.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()
	// Run pre-install stage
	if err = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.pre"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.pre stage: %s", err.Error())
	}

	if err = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.pre.hook"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.pre hook script: %s", err.Error())
	}

	// REMOUNT /efi as RW (its RO by default)
	umount, err := e.MountRWPartition(i.spec.EfiPartition)
	if err != nil {
		i.cfg.Logger.Errorf("remounting efi as RW: %s", err.Error())
		return err
	}
	cleanup.Push(umount)

	// TODO: Get the size of the efi partition and decide if the images can fit
	// before trying the upgrade.
	// If we decide to first copy and then rotate, we need ~4 times the size of
	// the artifact set [TBD]

	// When upgrading recovery or single entries, we don't want to replace loader.conf or any other
	// files, thus we take a simpler approach and only install the new efi file
	// and the relevant conf
	if i.spec.RecoveryUpgrade() {
		i.cfg.Logger.Infof("installing entry: recovery")
		return i.installRecovery()
	}

	if i.spec.Entry != "" { // single entry upgrade
		i.cfg.Logger.Infof("installing entry: %s", i.spec.Entry)
		return i.installEntry(i.spec.Entry)
	}

	i.cfg.Logger.Infof("installing entry: active")
	// Dump artifact to efi dir
	_, err = e.DumpSource(constants.UkiEfiDir, i.spec.Active.Source)
	if err != nil {
		i.cfg.Logger.Errorf("dumping the source: %s", err.Error())
		return err
	}

	// Check if the upgrade artifact contains the proper signature before copying
	err = signatures.CheckArtifactSignatureIsValid(i.cfg.Fs, filepath.Join(constants.UkiEfiDir, "EFI", "Kairos", fmt.Sprintf("%s.efi", UnassignedArtifactRole)), i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Logger.Error().Err(err).Msg("Checking signature before upgrading")
		// Remove efi file to not occupy space and leave stuff around
		cleanup.Push(func() error {
			return removeArtifactSetWithRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole)
		})
		i.cfg.Logger.Logger.Warn().Msg("Upgrade artifact signature does not match, upgrading to this source would result in an unbootable active system.\n" +
			"Check the upgrade source and confirm that its signed with a valid key, that key is in the machine DB and it has not been blacklisted.")
		return err
	}

	// Rotate first
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.UkiEfiDir, "active", "passive", i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Errorf("rotating active to passive: %s", err.Error())
		return fmt.Errorf("rotating active to passive: %w", err)
	}

	// Install the new artifacts as "active"
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole, "active", i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Errorf("installing the new artifacts as active: %s", err.Error())
		return fmt.Errorf("installing the new artifacts as active: %w", err)
	}

	if err = removeArtifactSetWithRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole); err != nil {
		i.cfg.Logger.Errorf("removing artifact set: %s", err.Error())
		return fmt.Errorf("removing artifact set: %w", err)
	}

	// add sort key to all files
	err = AddSystemdConfSortKey(i.cfg.Fs, i.spec.EfiPartition.MountPoint, i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Warnf("adding sort key: %s", err.Error())
	}

	// Add boot assessment to files by appending +3 to the name
	err = elementalUtils.AddBootAssessment(i.cfg.Fs, i.spec.EfiPartition.MountPoint, i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Warnf("adding boot assesment: %s", err.Error())
	}
	// SelectBootEntry sets the default boot entry to the selected entry
	err = action.SelectBootEntry(i.cfg, constants.BootEntryActive)
	// Should we fail? Or warn?
	if err != nil {
		i.cfg.Logger.Errorf("selecting boot entry: %s", err.Error())
		return err
	}

	// Remove any default keys in the loader.conf that might cause issues
	err = removeDefaultKeysFromLoaderConf(i.cfg.Fs, i.spec.EfiPartition.MountPoint, i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Warnf("removing default keys from loader.conf: %s", err.Error())
	}

	// Upgrade any efi keys in the entries by the new uki key
	err = upgradeEfiKeysInLoaderEntries(i.cfg.Arch, i.cfg.Fs, i.spec.EfiPartition.MountPoint, i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Warnf("upgrading efi keys in loader entries: %s", err.Error())
	}
	if err = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.after"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.after stage: %s", err.Error())
	}

	if err = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.after.hook"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.after hook script: %s", err.Error())
	}

	return nil
}

func (i *UpgradeAction) installEntry(entry string) error {
	targetEntryFile := filepath.Join(constants.UkiEfiDir, "EFI", "kairos", fmt.Sprintf("%s.efi", entry))
	if _, err := os.Stat(targetEntryFile); err != nil {
		return fmt.Errorf("could not stat target efi file for entry %s: %s", entry, err)
	}

	tmpDir, err := os.MkdirTemp("", "")
	if err != nil {
		i.cfg.Logger.Errorf("creating a tmp dir: %s", err.Error())
		return fmt.Errorf("creating a tmp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Dump artifact to tmp dir
	e := elemental.NewElemental(i.cfg)
	_, err = e.DumpSource(tmpDir, i.spec.Active.Source)
	if err != nil {
		i.cfg.Logger.Errorf("dumping the source to the tmp dir: %s", err.Error())
		return err
	}

	err = copyFile(filepath.Join(tmpDir, "EFI", "kairos", UnassignedArtifactRole+".efi"), targetEntryFile)
	if err != nil {
		i.cfg.Logger.Errorf("copying efi files: %s", err.Error())
		return err
	}

	targetConfPath := filepath.Join(constants.UkiEfiDir, "loader", "entries", fmt.Sprintf("%s.conf", entry))
	err = copyFile(
		filepath.Join(tmpDir, "loader", "entries", UnassignedArtifactRole+".conf"),
		targetConfPath)
	if err != nil {
		i.cfg.Logger.Errorf("copying conf files: %s", err.Error())
		return err
	}
	err = replaceRoleInKey(targetConfPath, "efi", UnassignedArtifactRole, entry, i.cfg.Logger)
	if err != nil {
		// Maybe a newer system where we use the "uki" key instead of "efi"
		if err := replaceRoleInKey(targetConfPath, "uki", UnassignedArtifactRole, entry, i.cfg.Logger); err != nil {
			i.cfg.Logger.Errorf("replacing role in in key %s: %s", "uki", err.Error())
			return err
		}
	}

	return nil
}

// installRecovery replaces the "recovery" role efi and conf files with
// the UnassignedArtifactRole efi and loader files from dir
func (i *UpgradeAction) installRecovery() error {
	if err := i.installEntry("recovery"); err != nil {
		return err
	}

	targetConfPath := filepath.Join(constants.UkiEfiDir, "loader", "entries", "recovery.conf")
	err := replaceConfTitle(targetConfPath, "recovery")
	if err != nil {
		i.cfg.Logger.Errorf("replacing conf title: %s", err.Error())
		return err
	}

	return nil
}
