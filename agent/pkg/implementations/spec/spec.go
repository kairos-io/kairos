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

package spec

import (
	"fmt"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-sdk/ghw"
	"github.com/kairos-io/kairos-sdk/types/images"
	"github.com/kairos-io/kairos-sdk/types/partitions"
)

// InstallSpec struct represents all the installation action details
type InstallSpec struct {
	Target                  string                         `yaml:"device,omitempty" mapstructure:"device"`
	Firmware                string                         `yaml:"firmware,omitempty" mapstructure:"firmware"`
	PartTable               string                         `yaml:"part-table,omitempty" mapstructure:"part-table"`
	Partitions              partitions.ElementalPartitions `yaml:"partitions,omitempty" mapstructure:"partitions"`
	ExtraPartitions         partitions.PartitionList       `yaml:"extra-partitions,omitempty" mapstructure:"extra-partitions"`
	NoFormat                bool                           `yaml:"no-format,omitempty" mapstructure:"no-format"`
	Force                   bool                           `yaml:"force,omitempty" mapstructure:"force"`
	CloudInit               []string                       `yaml:"cloud-init,omitempty" mapstructure:"cloud-init"`
	Iso                     string                         `yaml:"iso,omitempty" mapstructure:"iso"`
	GrubDefEntry            string                         `yaml:"grub-entry-name,omitempty" mapstructure:"grub-entry-name"`
	Tty                     string                         `yaml:"tty,omitempty" mapstructure:"tty"`
	Reboot                  bool                           `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff                bool                           `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	ExtraDirsRootfs         []string                       `yaml:"extra-dirs-rootfs,omitempty" mapstructure:"extra-dirs-rootfs"`
	Active                  images.Image                   `yaml:"system,omitempty" mapstructure:"system"`
	Recovery                images.Image                   `yaml:"recovery-system,omitempty" mapstructure:"recovery-system"`
	AllowInsecureRegistries bool                           `yaml:"allow-insecure-registries,omitempty" mapstructure:"allow-insecure-registries"`
	Passive                 images.Image
	GrubConf                string
}

// Sanitize checks the consistency of the struct, returns error
// if unsolvable inconsistencies are found
func (i *InstallSpec) Sanitize() error {
	// Check if the target device has mounted partitions, and remember its size
	// so we can later make sure the requested partitions fit in it.
	targetDisk, err := lookupTargetDisk(i.Target)
	if err != nil {
		return err
	}

	if i.Active.Source.IsEmpty() && i.Iso == "" {
		return fmt.Errorf("undefined system source to install")
	}
	if i.Partitions.State == nil || i.Partitions.State.MountPoint == "" {
		return fmt.Errorf("undefined state partition")
	}
	// Set the image file name depending on the filesystem
	recoveryMnt := constants.RecoveryDir
	if i.Partitions.Recovery != nil && i.Partitions.Recovery.MountPoint != "" {
		recoveryMnt = i.Partitions.Recovery.MountPoint
	}
	if i.Recovery.FS == constants.SquashFs {
		i.Recovery.File = filepath.Join(recoveryMnt, "cOS", constants.RecoverySquashFile)
	} else {
		i.Recovery.File = filepath.Join(recoveryMnt, "cOS", constants.RecoveryImgFile)
	}

	if err := checkExtraPartitionsSize(i.ExtraPartitions, i.Partitions.Persistent); err != nil {
		return err
	}

	// Set default labels in case the config from cloud/config overrides this values.
	// we need them to be on fixed values, otherwise we wont know where to find things on boot, on reset, etc...
	i.Partitions.SetDefaultLabels()

	if err := i.Partitions.SetFirmwarePartitions(i.Firmware, i.PartTable); err != nil {
		return err
	}

	// Make sure the requested partitions actually fit in the target disk.
	// Going over the disk size produces an unbootable system that only fails
	// at first boot, so we fail early here instead.
	return checkPartitionsFitTargetDisk(i.Target, targetDisk, i.Partitions.PartitionsByInstallOrder(i.ExtraPartitions))
}

// gptMetadataOverhead is the disk space the partitioner reserves and that the
// partitions cannot use: 1MiB of alignment before the first partition plus 1MiB
// at the end of the disk for the backup GPT header (see
// pkg/partitioner/disk.go). We account for it so we reject a layout that would
// not fit instead of letting the partitioning fail mid-install.
const gptMetadataOverhead = 2 * 1024 * 1024

// lookupTargetDisk resolves the target device path to a ghw disk. It returns a
// nil disk (no error) when the target is empty or cannot be found (for example
// when running unit tests without ghw fixtures). If the target disk has
// mounted partitions the caller cannot safely repartition, so this returns an
// error instead.
func lookupTargetDisk(target string) (*partitions.Disk, error) {
	if target == "" {
		return nil, nil
	}
	for _, disk := range ghw.GetDisks(ghw.NewPaths(""), nil) {
		if fmt.Sprintf("/dev/%s", disk.Name) == target {
			for _, p := range disk.Partitions {
				if p.MountPoint != "" {
					return nil, fmt.Errorf("target device %s has mounted partitions, please unmount them before installing", target)
				}
			}
			return disk, nil
		}
	}
	return nil, nil
}

// checkExtraPartitionsSize enforces that at most one partition (either an
// extra one or the persistent partition) has size 0, which means "take all the
// remaining space on the disk".
func checkExtraPartitionsSize(extras partitions.PartitionList, persistent *partitions.Partition) error {
	extraPartsSizeCheck := 0
	for _, p := range extras {
		if p.Size == 0 {
			extraPartsSizeCheck++
		}
	}
	if extraPartsSizeCheck > 1 {
		return fmt.Errorf("more than one extra partition has its size set to 0. Only one partition can have its size set to 0 which means that it will take all the available disk space in the device")
	}
	if extraPartsSizeCheck == 1 && persistent != nil && persistent.Size == 0 {
		return fmt.Errorf("both persistent partition and extra partitions have size set to 0. Only one partition can have its size set to 0 which means that it will take all the available disk space in the device")
	}
	return nil
}

// checkPartitionsFitTargetDisk returns an error if the sum of the requested
// partition sizes does not fit in the target disk once the GPT metadata
// overhead is reserved. A partition with size 0 means "take whatever space is
// left on the disk", so it does not count towards the requested total. If the
// target disk was not found (for example when the target is not set yet) the
// check is skipped.
func checkPartitionsFitTargetDisk(target string, targetDisk *partitions.Disk, parts partitions.PartitionList) error {
	if targetDisk == nil {
		return nil
	}

	var requestedMiB uint
	for _, p := range parts {
		requestedMiB += p.Size
	}

	// Partition sizes are expressed in MiB while the disk size is in bytes.
	if uint64(requestedMiB)*1024*1024+gptMetadataOverhead > targetDisk.SizeBytes {
		return fmt.Errorf(
			"the requested partitions size (%dMiB) does not fit in the target disk %q (%dMiB), which also needs %dMiB reserved for partition metadata",
			requestedMiB, target, targetDisk.SizeBytes/(1024*1024), gptMetadataOverhead/(1024*1024))
	}

	return nil
}

func (i *InstallSpec) ShouldReboot() bool                            { return i.Reboot }
func (i *InstallSpec) ShouldShutdown() bool                          { return i.PowerOff }
func (i *InstallSpec) GetTarget() string                             { return i.Target }
func (i *InstallSpec) GetPartTable() string                          { return i.PartTable }
func (i *InstallSpec) GetPartitions() partitions.ElementalPartitions { return i.Partitions }
func (i *InstallSpec) GetExtraPartitions() partitions.PartitionList  { return i.ExtraPartitions }

// ResetSpec struct represents all the reset action details
type ResetSpec struct {
	FormatPersistent bool         `yaml:"reset-persistent,omitempty" mapstructure:"reset-persistent"`
	FormatOEM        bool         `yaml:"reset-oem,omitempty" mapstructure:"reset-oem"`
	Reboot           bool         `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff         bool         `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	GrubDefEntry     string       `yaml:"grub-entry-name,omitempty" mapstructure:"grub-entry-name"`
	Tty              string       `yaml:"tty,omitempty" mapstructure:"tty"`
	ExtraDirsRootfs  []string     `yaml:"extra-dirs-rootfs,omitempty" mapstructure:"extra-dirs-rootfs"`
	Active           images.Image `yaml:"system,omitempty" mapstructure:"system"`
	Passive          images.Image
	Partitions       partitions.ElementalPartitions
	Target           string
	Efi              bool
	GrubConf         string
}

// Sanitize checks the consistency of the struct, returns error
// if unsolvable inconsistencies are found
func (r *ResetSpec) Sanitize() error {
	if r.Active.Source.IsEmpty() {
		return fmt.Errorf("undefined system source to reset to")
	}
	if r.Partitions.State == nil || r.Partitions.State.MountPoint == "" {
		return fmt.Errorf("undefined state partition")
	}
	return nil
}

func (r *ResetSpec) ShouldReboot() bool   { return r.Reboot }
func (r *ResetSpec) ShouldShutdown() bool { return r.PowerOff }

type UpgradeSpec struct {
	Entry                   string       `yaml:"entry,omitempty" mapstructure:"entry"`
	Active                  images.Image `yaml:"system,omitempty" mapstructure:"system"`
	Recovery                images.Image `yaml:"recovery-system,omitempty" mapstructure:"recovery-system"`
	GrubDefEntry            string       `yaml:"grub-entry-name,omitempty" mapstructure:"grub-entry-name"`
	Reboot                  bool         `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff                bool         `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	ExtraDirsRootfs         []string     `yaml:"extra-dirs-rootfs,omitempty" mapstructure:"extra-dirs-rootfs"`
	AllowInsecureRegistries bool         `yaml:"allow-insecure-registries,omitempty" mapstructure:"allow-insecure-registries"`
	Passive                 images.Image
	Partitions              partitions.ElementalPartitions
	ExcludedPaths           []string `yaml:"excluded-paths,omitempty" mapstructure:"excluded-paths"`
}

func (u *UpgradeSpec) RecoveryUpgrade() bool {
	return u.Entry == constants.BootEntryRecovery
}

// Sanitize checks the consistency of the struct, returns error
// if unsolvable inconsistencies are found
func (u *UpgradeSpec) Sanitize() error {
	if u.RecoveryUpgrade() {
		if u.Recovery.Source.IsEmpty() {
			return fmt.Errorf(constants.UpgradeRecoveryNoSourceError)
		}
		if u.Partitions.Recovery == nil || u.Partitions.Recovery.MountPoint == "" {
			return fmt.Errorf("undefined recovery partition")
		}
	} else {
		if u.Active.Source.IsEmpty() {
			return fmt.Errorf(constants.UpgradeNoSourceError)
		}
		if u.Partitions.State == nil || u.Partitions.State.MountPoint == "" {
			return fmt.Errorf("undefined state partition")
		}
	}
	return nil
}
func (u *UpgradeSpec) ShouldReboot() bool   { return u.Reboot }
func (u *UpgradeSpec) ShouldShutdown() bool { return u.PowerOff }

// EmptySpec is an empty spec for places that may need to inject a spec but doent really have one associated like firstboot
type EmptySpec struct {
}

func (r *EmptySpec) Sanitize() error {
	return nil
}

func (r *EmptySpec) ShouldReboot() bool   { return false }
func (r *EmptySpec) ShouldShutdown() bool { return false }

// NewElementalPartitionsFromList fills an ElementalPartitions instance from given
// partitions list. First tries to match partitions by partition label, if not,
// it tries to match partitions by default filesystem label
// TODO find a way to map custom labels when partition labels are not available
func NewElementalPartitionsFromList(pl partitions.PartitionList) partitions.ElementalPartitions {
	ep := partitions.ElementalPartitions{}
	ep.BIOS = GetPartitionByNameOrLabel(constants.BiosPartName, "", pl)
	ep.EFI = GetPartitionByNameOrLabel(constants.EfiPartName, constants.EfiLabel, pl)
	ep.OEM = GetPartitionByNameOrLabel(constants.OEMPartName, constants.OEMLabel, pl)
	ep.Recovery = GetPartitionByNameOrLabel(constants.RecoveryPartName, constants.RecoveryLabel, pl)
	ep.State = GetPartitionByNameOrLabel(constants.StatePartName, constants.StateLabel, pl)
	ep.Persistent = GetPartitionByNameOrLabel(constants.PersistentPartName, constants.PersistentLabel, pl)
	return ep
}

// GetPartitionByNameOrLabel will get a types.Partition from a types.PartitionList by name or label
func GetPartitionByNameOrLabel(name string, label string, partitionList partitions.PartitionList) *partitions.Partition {
	var part *partitions.Partition

	for _, p := range partitionList {
		if p.Name == name || p.FilesystemLabel == label {
			part = p
			if part.MountPoint != "" {
				return part
			}
			break
		}
	}
	return part
}

type InstallUkiSpec struct {
	Active                  images.Image                   `yaml:"system,omitempty" mapstructure:"system"`
	Target                  string                         `yaml:"device,omitempty" mapstructure:"device"`
	Reboot                  bool                           `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff                bool                           `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	Partitions              partitions.ElementalPartitions `yaml:"partitions,omitempty" mapstructure:"partitions"`
	ExtraPartitions         partitions.PartitionList       `yaml:"extra-partitions,omitempty" mapstructure:"extra-partitions"`
	NoFormat                bool                           `yaml:"no-format,omitempty" mapstructure:"no-format"`
	CloudInit               []string                       `yaml:"cloud-init,omitempty" mapstructure:"cloud-init"`
	SkipEntries             []string                       `yaml:"skip-entries,omitempty" mapstructure:"skip-entries"`
	AllowInsecureRegistries bool                           `yaml:"allow-insecure-registries,omitempty" mapstructure:"allow-insecure-registries"`
}

func (i *InstallUkiSpec) Sanitize() error {
	targetDisk, err := lookupTargetDisk(i.Target)
	if err != nil {
		return err
	}

	if err := checkExtraPartitionsSize(i.ExtraPartitions, i.Partitions.Persistent); err != nil {
		return err
	}

	return checkPartitionsFitTargetDisk(i.Target, targetDisk, i.Partitions.PartitionsByInstallOrder(i.ExtraPartitions))
}

func (i *InstallUkiSpec) ShouldReboot() bool                            { return i.Reboot }
func (i *InstallUkiSpec) ShouldShutdown() bool                          { return i.PowerOff }
func (i *InstallUkiSpec) GetTarget() string                             { return i.Target }
func (i *InstallUkiSpec) GetPartTable() string                          { return "gpt" }
func (i *InstallUkiSpec) GetPartitions() partitions.ElementalPartitions { return i.Partitions }
func (i *InstallUkiSpec) GetExtraPartitions() partitions.PartitionList  { return i.ExtraPartitions }

type UpgradeUkiSpec struct {
	Entry                   string                `yaml:"entry,omitempty" mapstructure:"entry"`
	Active                  images.Image          `yaml:"system,omitempty" mapstructure:"system"`
	Reboot                  bool                  `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff                bool                  `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	EfiPartition            *partitions.Partition `yaml:"efi-partition,omitempty" mapstructure:"efi-partition"`
	AllowInsecureRegistries bool                  `yaml:"allow-insecure-registries,omitempty" mapstructure:"allow-insecure-registries"`
}

func (i *UpgradeUkiSpec) RecoveryUpgrade() bool {
	return i.Entry == constants.BootEntryRecovery
}

func (i *UpgradeUkiSpec) Sanitize() error {
	var err error
	return err
}

func (i *UpgradeUkiSpec) ShouldReboot() bool   { return i.Reboot }
func (i *UpgradeUkiSpec) ShouldShutdown() bool { return i.PowerOff }

type ResetUkiSpec struct {
	FormatPersistent bool `yaml:"reset-persistent,omitempty" mapstructure:"reset-persistent"`
	FormatOEM        bool `yaml:"reset-oem,omitempty" mapstructure:"reset-oem"`
	Reboot           bool `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff         bool `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	Partitions       partitions.ElementalPartitions
}

func (i *ResetUkiSpec) Sanitize() error {
	var err error
	return err
}

func (i *ResetUkiSpec) ShouldReboot() bool   { return i.Reboot }
func (i *ResetUkiSpec) ShouldShutdown() bool { return i.PowerOff }
