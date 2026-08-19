package partitioner

import (
	"fmt"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/partition"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/gofrs/uuid"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/sanity-io/litter"
)

type Disk struct {
	*disk.Disk
	logger logger.KairosLogger
}

func (d *Disk) NewPartitionTable(partType string, parts partitions.PartitionList) error {
	d.logger.Infof("Creating partition table for partition type %s", partType)
	var table partition.Table
	switch partType {
	case sdkConstants.GPT:
		gptParts := kairosPartsToDiskfsGPTParts(parts, d.Size, d.LogicalBlocksize)
		if err := validateGPTPartitionsFit(gptParts, d.Size, d.LogicalBlocksize); err != nil {
			return err
		}
		table = &gpt.Table{
			ProtectiveMBR:      true,
			GUID:               cnst.DiskUUID, // Set know predictable UUID
			Partitions:         gptParts,
			LogicalSectorSize:  int(d.LogicalBlocksize),
			PhysicalSectorSize: int(d.PhysicalBlocksize),
		}
	default:
		return fmt.Errorf("invalid partition type: %s", partType)
	}
	err := d.Partition(table)
	if err != nil {
		return err
	}
	d.logger.Infof("Created partition table for partition type %s", partType)
	return nil
}

// gptBackupTailSectors returns the number of sectors at the tail of the disk
// occupied by the backup GPT structures: one sector for the backup header plus
// 128 partition entries * 128 bytes for the backup partition array (32 sectors
// on a 512-byte disk, 4 sectors on a 4K-native disk).
func gptBackupTailSectors(sectorSize int64) uint64 {
	partArraySectors := uint64(128*128) / uint64(sectorSize)
	return partArraySectors + 1
}

// validateGPTPartitionsFit refuses layouts whose last partition would land on
// or past the sectors go-diskfs reserves for the backup GPT structures.
// Without this the partitioner happily writes a table where the last partition
// overflows the disk, sgdisk reports a "secondary partition table overlaps"
// warning, and udev never creates the corresponding /dev/disk/by-partlabel
// symlink (kairos-io/kairos#4257).
func validateGPTPartitionsFit(parts []*gpt.Partition, diskSize, sectorSize int64) error {
	if diskSize <= 0 || sectorSize <= 0 {
		return nil
	}
	diskSectors := uint64(diskSize / sectorSize)
	tail := gptBackupTailSectors(sectorSize)
	if diskSectors <= tail+1 {
		return fmt.Errorf("target disk is too small to hold a GPT partition table")
	}
	lastDataSector := diskSectors - tail - 1
	for _, p := range parts {
		if p == nil {
			continue
		}
		if p.End > lastDataSector {
			return fmt.Errorf(
				"partition %q (index %d) would end at sector %d, past the last usable sector %d on a %d-byte disk; requested partition sizes do not fit the target device",
				p.Name, p.Index, p.End, lastDataSector, diskSize,
			)
		}
	}
	return nil
}

func getSectorEndFromSize(start, size uint64, sectorSize int64) uint64 {
	return (size / uint64(sectorSize)) + start - 1
}

func kairosPartsToDiskfsGPTParts(parts partitions.PartitionList, diskSize int64, sectorSize int64) []*gpt.Partition {
	var partitions []*gpt.Partition
	for index, part := range parts {
		var start uint64
		var end uint64
		var size uint64
		if len(partitions) == 0 {
			// first partition, align to 1Mb
			start = 1024 * 1024 / uint64(sectorSize)
		} else {
			// get latest partition end, sum 1
			start = partitions[len(partitions)-1].End + 1
		}

		// Reserve the exact tail space go-diskfs uses for the backup GPT
		// header + backup partition array; anything more is wasted disk.
		tailReserveBytes := gptBackupTailSectors(sectorSize) * uint64(sectorSize)

		// part.Size 0 means take over whats left on the disk
		if part.Size == 0 {
			// Remember to add the 1Mb alignment to total size
			// This will be on bytes already no need to transform it
			var sizeUsed = uint64(1024 * 1024)
			for _, p := range partitions {
				sizeUsed = sizeUsed + p.Size
			}
			size = uint64(diskSize) - sizeUsed - tailReserveBytes
		} else {
			// Change it to bytes. If it is the last partition, trim the
			// backup-GPT tail off its requested size so the write stays
			// inside lastDataSector.
			if index == len(parts)-1 {
				size = uint64(part.Size*1024*1024) - tailReserveBytes
			} else {
				size = uint64(part.Size * 1024 * 1024)
			}

		}

		end = getSectorEndFromSize(start, size, sectorSize)

		if part.Name == sdkConstants.EfiPartName && part.FS == sdkConstants.EfiFs {
			// EFI boot partition
			partitions = append(partitions, &gpt.Partition{
				Start:      start,
				End:        end,
				Type:       gpt.EFISystemPartition,
				Size:       size,                                                         // partition size in bytes
				GUID:       uuid.NewV5(uuid.NamespaceURL, part.FilesystemLabel).String(), // set know predictable UUID
				Name:       part.Name,
				Index:      index + 1, // GPT partition indices are 1-based
				Attributes: 0x1,       // system partition flag
			})
		} else if part.Name == sdkConstants.BiosPartName {
			// Non-EFI boot partition
			partitions = append(partitions, &gpt.Partition{
				Start:      start,
				End:        end,
				Type:       gpt.BIOSBoot,
				Size:       size,                                                         // partition size in bytes
				GUID:       uuid.NewV5(uuid.NamespaceURL, part.FilesystemLabel).String(), // set know predictable UUID
				Name:       part.Name,
				Index:      index + 1, // GPT partition indices are 1-based
				Attributes: 0x4,       // legacy bios bootable flag
			})
		} else {
			// Other partitions
			partitions = append(partitions, &gpt.Partition{
				Start: start,
				End:   end,
				Type:  gpt.LinuxFilesystem,
				Size:  size,
				GUID:  uuid.NewV5(uuid.NamespaceURL, part.FilesystemLabel).String(),
				Name:  part.Name,
				Index: index + 1, // GPT partition indices are 1-based
			})
		}
	}
	return partitions
}

type DiskOptions func(d *Disk) error

func WithLogger(logger logger.KairosLogger) func(d *Disk) error {
	return func(d *Disk) error {
		d.logger = logger
		return nil
	}
}

func NewDisk(device string, opts ...DiskOptions) (*Disk, error) {
	d, err := diskfs.Open(device)
	if err != nil {
		return nil, err
	}
	dev := &Disk{d, logger.NewKairosLogger("partitioner", "info", false)}

	for _, opt := range opts {
		if err := opt(dev); err != nil {
			return nil, err
		}
	}

	dev.logger.Debugf("Initialized new disk from device %s", litter.Sdump(dev))
	return dev, nil
}
