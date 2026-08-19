package ghw

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
)

type MultipathPartitionHandler struct {
	DiskName string
}

func NewMultipathPartitionHandler(diskName string) *MultipathPartitionHandler {
	return &MultipathPartitionHandler{DiskName: diskName}
}

var _ PartitionHandler = &MultipathPartitionHandler{}

func (m *MultipathPartitionHandler) GetPartitions(paths *Paths, logger *logger.KairosLogger) partitions.PartitionList {
	out := make(partitions.PartitionList, 0)

	// For multipath devices, partitions appear as holders of the parent device
	// in /sys/block/<disk>/holders/<holder>
	holdersPath := filepath.Join(paths.SysBlock, m.DiskName, "holders")
	logger.Logger.Trace().Str("path", holdersPath).Msg("Reading multipath holders")

	holders, err := os.ReadDir(holdersPath)
	if err != nil {
		logger.Logger.Error().Err(err).Msg("failed to read holders directory")
		return out
	}

	// Find all multipath partitions by checking each holder
	for _, holder := range holders {
		partName := holder.Name()

		// Only consider dm- devices as potential multipath partitions
		if !isMultipathDevice(paths, holder, logger) {
			logger.Logger.Trace().Str("path", holder.Name()).Msg("Is not a multipath device")
			continue
		}

		// Verify this holder is actually a multipath partition
		// We can use the holder DirEntry directly - no need to search for it!
		if !isMultipathPartition(holder, paths, logger) {
			logger.Logger.Trace().Str("partition", partName).Msg("Holder is not a multipath partition")
			continue
		}

		logger.Logger.Trace().Str("partition", partName).Msg("Found multipath partition")

		udevInfo, err := udevInfoPartition(paths, partName, logger)
		if err != nil {
			logger.Logger.Error().Err(err).Str("devNo", partName).Msg("Failed to get udev info")
			return out
		}

		mapperName, ok := udevInfo["DM_NAME"]
		if !ok {
			logger.Logger.Error().Str("devNo", partName).Msg("DM_NAME not found in udev info")
			continue
		}

		// For multipath partitions, we need to get size directly from the partition device
		// since it's a top-level entry in /sys/block, not nested under the parent
		size := partitionSizeBytes(paths, partName, logger)
		du := diskPartUUID(paths, partName, logger)

		// The mount point is usually the same as the mapper name
		// however you can also mount it as /dev/dm-<n> or /dev/mapper/<mapperName>
		// so we need to check both
		potentialMountNames := []string{
			filepath.Join("/dev/mapper", mapperName),
			filepath.Join("/dev", partName),
		}

		// Search for the mount point in the system
		var mp, pt string
		for _, mountName := range potentialMountNames {
			mp, pt = partitionInfo(paths, mountName, logger)
			if mp != "" {
				logger.Logger.Trace().Str("mountPoint", mp).Msg("Found mount point for partition")
				break
			}
		}

		if pt == "" {
			pt = diskPartTypeUdev(paths, partName, logger)
		}
		fsLabel := diskFSLabel(paths, partName, logger)
		partitionLabel := diskPartitionLabel(paths, partName, logger)

		p := &partitions.Partition{
			Name:            partName,
			PartitionLabel:  partitionLabel,
			Size:            uint(size / (1024 * 1024)),
			MountPoint:      mp,
			UUID:            du,
			FilesystemLabel: fsLabel,
			FS:              pt,
			Path:            filepath.Join("/dev", partName),
			Disk:            filepath.Join("/dev", m.DiskName),
		}
		out = append(out, p)
	}

	return out
}
