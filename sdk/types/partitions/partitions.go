package partitions

import (
	"fmt"
	"sort"

	"github.com/kairos-io/kairos-sdk/constants"
)

type Partition struct {
	Name            string   `yaml:"name,omitempty" mapstructure:"name" json:"name,omitempty"`
	PartitionLabel  string   `yaml:"partition_label,omitempty" mapstructure:"partition_label" json:"partition_label,omitempty"`
	FilesystemLabel string   `yaml:"label,omitempty" mapstructure:"label" json:"label,omitempty"`
	Size            uint     `yaml:"size,omitempty" mapstructure:"size" json:"size,omitempty"`
	FS              string   `yaml:"fs,omitempty" mapstrcuture:"fs" json:"fs,omitempty"`
	Flags           []string `yaml:"flags,omitempty" mapstrcuture:"flags" json:"flags,omitempty"`
	UUID            string   `yaml:"uuid,omitempty" mapstructure:"uuid" json:"uuid,omitempty"`
	MountPoint      string   `yaml:"-" json:"-"` // MountPoint is not serialized
	LoopDevice      string   `yaml:"-" json:"-"` // LoopDevice is not serialized
	Path            string   `yaml:"-" json:"-"` // Path is not serialized
	Disk            string   `yaml:"-" json:"-"` // Disk is not serialized
}

type PartitionList []*Partition

type Disk struct {
	Name       string        `json:"name,omitempty" mapstructure:"name" yaml:"name,omitempty"`
	SizeBytes  uint64        `json:"size_bytes,omitempty" mapstructure:"size_bytes" yaml:"size_bytes,omitempty"`
	UUID       string        `json:"uuid,omitempty" mapstructure:"uuid" yaml:"uuid,omitempty"`
	Partitions PartitionList `json:"partitions,omitempty" mapstructure:"partitions" yaml:"partitions,omitempty"`
}

type ElementalPartitions struct {
	BIOS *Partition `yaml:"-" json:"-"` // We dont want marshal/unmarshal this field
	// EFI is not serialized (yaml/json "-"), but its size may be set from
	// config (e.g. install.partitions.efi.size) via mapstructure, so platforms
	// that need a larger ESP (e.g. Jetson Thor) can request one.
	EFI        *Partition `yaml:"-" json:"-" mapstructure:"efi"`
	OEM        *Partition `yaml:"oem,omitempty" mapstructure:"oem" json:"oem,omitempty"`
	Recovery   *Partition `yaml:"recovery,omitempty" mapstructure:"recovery" json:"recovery,omitempty"`
	State      *Partition `yaml:"state,omitempty" mapstructure:"state" json:"state,omitempty"`
	Persistent *Partition `yaml:"persistent,omitempty" mapstructure:"persistent" json:"persistent,omitempty"`
}

// PartitionsByInstallOrder sorts partitions according to the default layout
// nil partitions are ignored and partition with 0 size is set last.
func (ep *ElementalPartitions) PartitionsByInstallOrder(extraPartitions PartitionList, excludes ...*Partition) PartitionList {
	partitions := PartitionList{}
	var lastPartition *Partition

	inExcludes := func(part *Partition, list ...*Partition) bool {
		for _, p := range list {
			if part == p {
				return true
			}
		}
		return false
	}

	if ep.BIOS != nil && !inExcludes(ep.BIOS, excludes...) {
		partitions = append(partitions, ep.BIOS)
	}
	if ep.EFI != nil && !inExcludes(ep.EFI, excludes...) {
		partitions = append(partitions, ep.EFI)
	}
	if ep.OEM != nil && !inExcludes(ep.OEM, excludes...) {
		partitions = append(partitions, ep.OEM)
	}
	if ep.Recovery != nil && !inExcludes(ep.Recovery, excludes...) {
		partitions = append(partitions, ep.Recovery)
	}
	if ep.State != nil && !inExcludes(ep.State, excludes...) {
		partitions = append(partitions, ep.State)
	}
	if ep.Persistent != nil && !inExcludes(ep.Persistent, excludes...) {
		// Check if we have to set this partition the latest due size == 0
		if ep.Persistent.Size == 0 {
			lastPartition = ep.Persistent
		} else {
			partitions = append(partitions, ep.Persistent)
		}
	}
	for _, p := range extraPartitions {
		// Check if we have to set this partition the latest due size == 0
		// Also check that we didn't set already the persistent to last in which case ignore this
		// InstallConfig.Sanitize should have already taken care of failing if this is the case, so this is extra protection
		if p.Size == 0 {
			if lastPartition != nil {
				// Ignore this part, we are not setting 2 parts to have 0 size!
				continue
			}
			lastPartition = p
		} else {
			partitions = append(partitions, p)
		}
	}

	// Set the last partition in the list the partition which has 0 size, so it grows to use the rest of free space
	if lastPartition != nil {
		partitions = append(partitions, lastPartition)
	}

	return partitions
}

// PartitionsByMountPoint sorts partitions according to its mountpoint, ignores nil.
// partitions or partitions with an empty mountpoint.
func (ep *ElementalPartitions) PartitionsByMountPoint(descending bool, excludes ...*Partition) PartitionList {
	mountPointKeys := map[string]*Partition{}
	mountPoints := []string{}
	partitions := PartitionList{}

	for _, p := range ep.PartitionsByInstallOrder([]*Partition{}, excludes...) {
		if p.MountPoint != "" {
			mountPointKeys[p.MountPoint] = p
			mountPoints = append(mountPoints, p.MountPoint)
		}
	}

	if descending {
		sort.Sort(sort.Reverse(sort.StringSlice(mountPoints)))
	} else {
		sort.Strings(mountPoints)
	}

	for _, mnt := range mountPoints {
		partitions = append(partitions, mountPointKeys[mnt])
	}
	return partitions
}

// SetFirmwarePartitions sets firmware partitions for a given firmware and partition table type.
func (ep *ElementalPartitions) SetFirmwarePartitions(firmware string, partTable string) error {
	if firmware == constants.EFI && partTable == constants.GPT {
		// Preserve a size the caller already set (e.g. from
		// install.partitions.efi.size in the config) and only fall back to the
		// default when it is unset. Some platforms (e.g. Jetson Thor, which
		// stages a ~29MB UEFI firmware capsule on the ESP) need a larger ESP
		// than the 64MiB default.
		efiSize := constants.EfiSize
		if ep.EFI != nil && ep.EFI.Size != 0 {
			efiSize = ep.EFI.Size
		}
		ep.EFI = &Partition{
			FilesystemLabel: constants.EfiLabel,
			Size:            efiSize,
			Name:            constants.EfiPartName,
			FS:              constants.EfiFs,
			MountPoint:      constants.EfiDirTransient,
			Flags:           []string{constants.ESPFLAG},
		}
		ep.BIOS = nil
	} else if firmware == constants.BIOS && partTable == constants.GPT {
		ep.BIOS = &Partition{
			FilesystemLabel: constants.EfiLabel,
			Size:            constants.BiosSize,
			Name:            constants.BiosPartName,
			FS:              "",
			MountPoint:      "",
			Flags:           []string{constants.BIOSFLAG},
		}
		ep.EFI = nil
	} else {
		// This should return an error. We support only EFI+GPT or BIOS+GPT
		return fmt.Errorf("unsupported firmware %s and partition table %s combination", firmware, partTable)
	}

	return nil
}

// SetDefaultLabels sets the default labels for oem, state, persistent and recovery partitions.
func (ep *ElementalPartitions) SetDefaultLabels() {
	ep.OEM.FilesystemLabel = constants.OEMLabel
	ep.OEM.Name = constants.OEMPartName
	ep.State.FilesystemLabel = constants.StateLabel
	ep.State.Name = constants.StatePartName
	ep.Persistent.FilesystemLabel = constants.PersistentLabel
	ep.Persistent.Name = constants.PersistentPartName
	ep.Recovery.FilesystemLabel = constants.RecoveryLabel
	ep.Recovery.Name = constants.RecoveryPartName
}
