package uki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	events "github.com/kairos-io/kairos-sdk/bus"
	sdkutils "github.com/kairos-io/kairos-sdk/utils"
)

type InstallAction struct {
	cfg  *sdkConfig.Config
	spec *v1.InstallUkiSpec
}

func NewInstallAction(cfg *sdkConfig.Config, spec *v1.InstallUkiSpec) *InstallAction {
	return &InstallAction{cfg: cfg, spec: spec}
}

func (i *InstallAction) Run() (err error) {
	e := elemental.NewElemental(i.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()
	// Run pre-install stage
	if err = utils.RunStage(i.cfg, "kairos-uki-install.pre"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-install.pre stage: %s", err.Error())
	}
	if err = events.RunHookScript("/usr/bin/kairos-agent.uki.install.pre.hook"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-install.pre hook script: %s", err.Error())
	}

	if i.spec.NoFormat {
		i.cfg.Logger.Infof("NoFormat is true, skipping format and partitioning")
		if i.spec.Target == "" || i.spec.Target == "auto" {
			// This needs to run after the pre-install stage to give the user the
			// opportunity to prepare the target disk in the pre-install stage.
			device, err := config.DetectPreConfiguredDevice(i.cfg.Logger)
			if err != nil {
				return fmt.Errorf("no target device specified and no device found: %s", err)
			}
			i.cfg.Logger.Infof("No target device specified, using pre-configured device: %s", device)
			i.spec.Target = device
		}
	} else {
		// Deactivate any active volume on target
		err = e.DeactivateDevices()
		if err != nil {
			i.cfg.Logger.Errorf("deactivating devices: %s", err.Error())
			return err
		}

		// Partition device
		err = e.PartitionAndFormatDevice(i.spec)
		if err != nil {
			i.cfg.Logger.Errorf("partitioning and formating devices: %s", err.Error())
			return err
		}
	}

	parts := i.spec.GetPartitions()
	err = e.MountPartitions(parts.PartitionsByMountPoint(false))
	if err != nil {
		i.cfg.Logger.Errorf("mounting partitions: %s", err.Error())
		return err
	}
	cleanup.Push(func() error {
		return e.UnmountPartitions(parts.PartitionsByMountPoint(true))
	})

	// Before install hook happens after partitioning but before the image OS is applied (this is for compatibility with normal install, so users can reuse their configs)
	err = Hook(i.cfg, constants.BeforeInstallHook)
	if err != nil {
		i.cfg.Logger.Errorf("running before install hook: %s", err.Error())
		return err
	}

	// Check if we should fail the installation by checking the sentinel file FailInstallationFileSentinel
	if toFail, err := utils.CheckFailedInstallation(constants.FailInstallationFileSentinel); toFail {
		return err
	}

	// Store cloud-config in TPM or copy it to COS_OEM?
	// Copy cloud-init if any
	err = e.CopyCloudConfig(i.spec.CloudInit)
	if err != nil {
		i.cfg.Logger.Errorf("copying cloud config: %s", err.Error())
		return err
	}
	// Create dir structure
	//  - /EFI/Kairos/ -> Store our older efi images ?
	//  - /EFI/BOOT/ -> Default fallback dir (efi search for bootaa64.efi or bootx64.efi if no entries in the boot manager)

	err = fsutils.MkdirAll(i.cfg.Fs, filepath.Join(constants.EfiDir, "EFI", "BOOT"), constants.DirPerm)
	if err != nil {
		i.cfg.Logger.Errorf("creating efi directories: %s", err.Error())
		return err
	}

	// TODO: Check if the size of the files we are going to copy, will fit in the
	// partition. If not stop here.

	// Copy the efi file into the proper dir
	_, err = e.DumpSource(i.spec.Partitions.EFI.MountPoint, i.spec.Active.Source)
	if err != nil {
		i.cfg.Logger.Errorf("dumping source: %s", err.Error())
		return err
	}

	// Remove entries
	// Read all confs
	i.cfg.Logger.Debugf("Parsing efi partition files (skip SkipEntries, replace placeholders etc)")
	err = fsutils.WalkDirFs(i.cfg.Fs, filepath.Join(i.spec.Partitions.EFI.MountPoint), func(path string, info os.DirEntry, err error) error {
		filename := info.Name()
		if err != nil {
			i.cfg.Logger.Errorf("Error walking path: %s, %s", filename, err.Error())
			return err
		}

		i.cfg.Logger.Debugf("Checking file %s", path)
		if info.IsDir() {
			return nil
		}

		if filepath.Ext(filename) == ".conf" {
			// Extract the values
			conf, err := sdkutils.SystemdBootConfReader(path)
			if err != nil {
				i.cfg.Logger.Errorf("Error reading conf file to extract values %s: %s", path, err)
				return err
			}
			if len(conf["cmdline"]) == 0 {
				return nil
			}

			// Check if the cmdline matches any of the entries in the skip list
			skip := false
			for _, entry := range i.spec.SkipEntries {
				if strings.Contains(conf["cmdline"], entry) {
					i.cfg.Logger.Debugf("Found match for %s in %s", entry, path)
					skip = true
					break
				}
			}
			if skip {
				return i.SkipEntry(path, conf)
			}
		}

		return nil
	})
	if err != nil {
		i.cfg.Logger.Errorf("error happened: %s", err.Error())
		return err
	}

	for _, role := range []string{"active", "passive", "recovery", "statereset"} {
		if err = copyArtifactSetRole(i.cfg.Fs, i.spec.Partitions.EFI.MountPoint, UnassignedArtifactRole, role, i.cfg.Logger); err != nil {
			i.cfg.Logger.Errorf("installing the new artifact set as %s: %s", role, err.Error())
			return fmt.Errorf("installing the new artifact set as %s: %w", role, err)
		}
	}

	if err = removeArtifactSetWithRole(i.cfg.Fs, i.spec.Partitions.EFI.MountPoint, UnassignedArtifactRole); err != nil {
		i.cfg.Logger.Errorf("removing artifact set with role %s: %s", UnassignedArtifactRole, err.Error())
		return fmt.Errorf("removing artifact set with role %s: %w", UnassignedArtifactRole, err)
	}

	// add sort key to all files
	err = AddSystemdConfSortKey(i.cfg.Fs, i.spec.Partitions.EFI.MountPoint, i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Warnf("adding sort key: %s", err.Error())
	}

	// Add boot assessment to files by appending +3 to the name
	err = utils.AddBootAssessment(i.cfg.Fs, i.spec.Partitions.EFI.MountPoint, i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Warnf("adding boot assesment: %s", err.Error())
	}

	// SelectBootEntry sets the default boot entry to the selected entry
	err = action.SelectBootEntry(i.cfg, "cos")
	if err != nil {
		i.cfg.Logger.Warnf("selecting active boot entry: %s", err.Error())
	}

	// Remove any default keys in the loader.conf that might cause issues
	// This is in case the current image is built with an older AuroraBoot that still ships the default loader key
	err = removeDefaultKeysFromLoaderConf(i.cfg.Fs, i.spec.Partitions.EFI.MountPoint, i.cfg.Logger)

	err = hook.Run(*i.cfg, i.spec, hook.PostInstall...)
	if err != nil {
		i.cfg.Logger.Errorf("running uki encryption hooks: %s", err.Error())
		return err
	}

	// after install hook happens after install (this is for compatibility with normal install, so users can reuse their configs)
	err = Hook(i.cfg, constants.AfterInstallHook)
	if err != nil {
		i.cfg.Logger.Errorf("running after install hook: %s", err.Error())
		return err
	}
	// Remove all boot manager entries?
	// Create boot manager entry
	// Set default entry to the one we just created
	// Probably copy efi utils, like the Mokmanager and even the shim or grub efi to help with troubleshooting?
	if err = utils.RunStage(i.cfg, "kairos-uki-install.after"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-install.after stage: %s", err.Error())
	}

	if err = events.RunHookScript("/usr/bin/kairos-agent.uki.install.after.hook"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-install.after hook script: %s", err.Error())
	}

	return hook.Run(*i.cfg, i.spec, hook.FinishUKIInstall...)
}

func (i *InstallAction) SkipEntry(path string, conf map[string]string) (err error) {
	// If match, get the efi file and remove it
	if conf["efi"] != "" {
		i.cfg.Logger.Debugf("Removing efi file %s", conf["efi"])
		// First remove the efi file
		err = i.cfg.Fs.Remove(filepath.Join(i.spec.Partitions.EFI.MountPoint, conf["efi"]))
		if err != nil {
			i.cfg.Logger.Errorf("Error removing efi file %s: %s", conf["efi"], err)
			return err
		}
		// Then remove the conf file
		i.cfg.Logger.Debugf("Removing conf file %s", path)
		err = i.cfg.Fs.Remove(path)
		if err != nil {
			i.cfg.Logger.Errorf("Error removing conf file %s: %s", path, err)
			return err
		}
		// Do not continue checking the conf file, we already done all we needed
	}
	return err
}

// Hook is RunStage wrapper that only adds logic to ignore errors
// in case v1.Config.Strict is set to false
func Hook(config *sdkConfig.Config, hook string) error {
	config.Logger.Infof("Running %s hook", hook)
	err := utils.RunStage(config, hook)
	if !config.Strict {
		err = nil
	}
	return err
}
