// Package lookup holds the partition-label resolution helpers that the boot
// and post-install mount paths depend on to disambiguate a LUKS container
// from its unlocked mapper. Both advertise the same filesystem label on
// encrypted installs (see kairos-io/kairos#4403), so any caller that resolves
// a label via /dev/disk/by-label/<X> or a bare blkid -L is racing with udev.
//
// The helpers live in this leaf package so both sdk/kcrypt (the encryption
// flow) and sdk/machine (the post-install mount helpers) can call them
// without introducing an import cycle through sdk/collector.
package lookup

import (
	"fmt"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// LegacyPartitionName maps a Kairos filesystem label back to its GPT
// PartitionLabel counterpart (COS_PERSISTENT -> persistent, COS_OEM -> oem).
// The reverse mapping is needed to find a partition whose filesystem label
// was never set. That is the pre-kairos-sdk#822 install shape where the LUKS
// container has no FS label at all and only its GPT PARTLABEL identifies it.
func LegacyPartitionName(filesystemLabel string) string {
	switch filesystemLabel {
	case constants.OEMLabel:
		return constants.OEMPartName
	case constants.PersistentLabel:
		return constants.PersistentPartName
	default:
		return ""
	}
}

// MapperPath returns the device-mapper path for a given LUKS partition.
// This is where cryptsetup luksOpen places the unlocked mapper, keyed by the
// underlying LUKS partition's kernel name (e.g. /dev/mapper/vda5).
func MapperPath(p *partitions.Partition) string {
	if p == nil {
		return ""
	}
	return filepath.Join("/dev", "mapper", p.Name)
}

// FindByLabel returns any partition carrying the given filesystem label,
// without regard to filesystem type. Callers who need a specific FS type
// must use FindLUKSContainerByLabel or FindMapperByLabel so the by-label
// ambiguity does not steer them to the wrong device.
func FindByLabel(partitionLabel string) (*partitions.Partition, error) {
	return findByLabelFiltered(partitionLabel, nil)
}

// FindLUKSContainerByLabel returns the crypto_LUKS partition carrying the
// given filesystem label. Used at unlock time and by mount code paths that
// need the LUKS device to derive its mapper path.
func FindLUKSContainerByLabel(partitionLabel string) (*partitions.Partition, error) {
	return findByLabelFiltered(partitionLabel, func(p *partitions.Partition) bool {
		return p.FS == "crypto_LUKS"
	})
}

// FindMapperByLabel returns the plaintext filesystem carrying the given
// filesystem label. That is, the ext4/xfs partition, or the unlocked mapper
// of a crypto_LUKS container. Used to bypass /dev/disk/by-label ambiguity
// when mounting a label that also exists on a LUKS wrapper.
func FindMapperByLabel(partitionLabel string) (*partitions.Partition, error) {
	return findByLabelFiltered(partitionLabel, func(p *partitions.Partition) bool {
		return p.FS != "crypto_LUKS"
	})
}

// MountSourceForLabel returns the concrete device path to hand to mount(2)
// for the filesystem with the given label, resolving the by-label symlink
// ambiguity that kairos-io/kairos#4403 relies on. If the label lives on a
// LUKS container, the returned path is the mapper (/dev/mapper/<name>),
// which is unambiguous even when the outer and inner filesystems share the
// same label. If the label lives on a plain partition, the returned path is
// that partition's device path.
//
// This is the value that boot-time mount code AND the fstab entry Spec must
// use for label-addressed mounts on Kairos systems. Passing
// /dev/disk/by-label/<X> leaves the caller at the mercy of udev's
// last-writer-wins resolution.
func MountSourceForLabel(partitionLabel string) (string, error) {
	if luks, err := FindLUKSContainerByLabel(partitionLabel); err == nil {
		return MapperPath(luks), nil
	}
	p, err := FindMapperByLabel(partitionLabel)
	if err != nil {
		return "", err
	}
	if p.Path != "" {
		return p.Path, nil
	}
	return filepath.Join("/dev", p.Name), nil
}

func findByLabelFiltered(partitionLabel string, filter func(*partitions.Partition) bool) (*partitions.Partition, error) {
	legacyName := LegacyPartitionName(partitionLabel)

	logger := sdkLogger.NewNullLogger()
	disks := ghw.GetDisks(ghw.NewPaths(""), &logger)
	if disks == nil {
		return nil, fmt.Errorf("failed to scan block devices")
	}

	labelMatches := func(p *partitions.Partition) bool {
		return p.FilesystemLabel == partitionLabel ||
			(legacyName != "" && p.PartitionLabel == legacyName)
	}

	for _, disk := range disks {
		for _, p := range disk.Partitions {
			if !labelMatches(p) {
				continue
			}
			if filter != nil && !filter(p) {
				continue
			}
			if p.Path == "" {
				p.Path = filepath.Join("/dev", p.Name)
			}
			if p.Name == "" {
				p.Name = filepath.Base(p.Path)
			}
			return p, nil
		}
	}
	return nil, fmt.Errorf("partition not found")
}
