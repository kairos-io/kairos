package ghw

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
)

const (
	sectorSize = 512
	UNKNOWN    = "unknown"
)

type Paths struct {
	SysBlock    string
	RunUdevData string
	ProcMounts  string
}

func NewPaths(withOptionalPrefix string) *Paths {
	p := &Paths{
		SysBlock:    "/sys/block/",
		RunUdevData: "/run/udev/data",
		ProcMounts:  "/proc/mounts",
	}

	// Allow overriding the paths via env var. It has precedence over anything
	val, exists := os.LookupEnv("GHW_CHROOT")
	if exists {
		val = strings.TrimSuffix(val, "/")
		p.SysBlock = fmt.Sprintf("%s%s", val, p.SysBlock)
		p.RunUdevData = fmt.Sprintf("%s%s", val, p.RunUdevData)
		p.ProcMounts = fmt.Sprintf("%s%s", val, p.ProcMounts)
		return p
	}

	if withOptionalPrefix != "" {
		withOptionalPrefix = strings.TrimSuffix(withOptionalPrefix, "/")
		p.SysBlock = fmt.Sprintf("%s%s", withOptionalPrefix, p.SysBlock)
		p.RunUdevData = fmt.Sprintf("%s%s", withOptionalPrefix, p.RunUdevData)
		p.ProcMounts = fmt.Sprintf("%s%s", withOptionalPrefix, p.ProcMounts)
	}
	return p
}

func isMultipathDevice(paths *Paths, entry os.DirEntry, logger *sdkLogger.KairosLogger) bool {
	hasPrefix := strings.HasPrefix(entry.Name(), "dm-")
	if !hasPrefix {
		return false
	}

	// If the device is prefixed with "dm-", it is a device mapper device
	// We can check if it has the DM_UUID attribute to confirm it's a multipath device
	udevInfo, err := udevInfoPartition(paths, entry.Name(), logger)
	if err != nil {
		logger.Logger.Error().Err(err).Str("devNo", entry.Name()).Msg("Failed to get udev info")
		return false
	}

	// Check if the udev info contains DM_UUID indicating it's a device mapper device
	uuid, ok := udevInfo["DM_UUID"]
	if !ok {
		logger.Logger.Trace().Str("devNo", entry.Name()).Msg("Not a multipath device. DM_UUID not found")
		return false
	}

	// DM_UUID should contain mpath-<uuid> for multipath devices
	// We can check if it starts with "mpath" to confirm it's a multipath device
	// for partitions they are normally named part<number>-mpath-<uuid>
	// for disks they are normally named mpath-<uuid>
	if !strings.Contains(uuid, "mpath") {
		logger.Logger.Trace().Str("devNo", entry.Name()).Msg("Not a multipath device. DM_UUID does not contain 'mpath'")
		return false
	}

	logger.Logger.Trace().Str("devNo", entry.Name()).Msg("Is a multipath device")

	// If we have a DM_UUID, prefixed with "mpath", it's a multipath device
	return true
}

func GetDisks(paths *Paths, logger *sdkLogger.KairosLogger) []*partitions.Disk {
	if logger == nil {
		newLogger := sdkLogger.NewKairosLogger("ghw", "info", true)
		logger = &newLogger
		defer newLogger.Close()
	}
	disks := make([]*partitions.Disk, 0)
	logger.Logger.Trace().Str("path", paths.SysBlock).Msg("Scanning for disks")
	files, err := os.ReadDir(paths.SysBlock)
	if err != nil {
		return nil
	}
	for _, file := range files {
		var partitionHandler PartitionHandler
		logger.Logger.Trace().Str("file", file.Name()).Msg("Reading file")
		dname := file.Name()
		size := diskSizeBytes(paths, dname, logger)

		// Skip entries that are multipath partitions
		// we will handle them when we parse this disks partitions
		if isMultipathPartition(file, paths, logger) {
			logger.Logger.Trace().Str("file", dname).Msg("Skipping multipath partition")
			continue
		}

		if strings.HasPrefix(dname, "loop") && size == 0 {
			// We don't care about unused loop devices...
			continue
		}

		// Skip kernel-hidden block devices (/sys/block/<dev>/hidden == 1).
		// These include the NVMe per-controller path device (nvmeXcYnZ)
		// exposed by native NVMe multipath, which shadows the real namespace
		// (nvmeXnY) with the same size but has no usable /dev node. Returning
		// it makes callers pick an unusable install target that fails with
		// "Disk /dev/nvmeXcYnZ does not exist".
		if isHiddenDevice(paths, dname) {
			logger.Logger.Trace().Str("file", dname).Msg("Skipping hidden device")
			continue
		}
		d := &partitions.Disk{
			Name:      dname,
			SizeBytes: size,
			UUID:      diskUUID(paths, dname, logger),
		}

		if isMultipathDevice(paths, file, logger) {
			partitionHandler = NewMultipathPartitionHandler(dname)
		} else {
			partitionHandler = NewDiskPartitionHandler(dname)
		}

		parts := partitionHandler.GetPartitions(paths, logger)
		d.Partitions = parts

		disks = append(disks, d)
	}

	return disks
}

func isMultipathPartition(entry os.DirEntry, paths *Paths, logger *sdkLogger.KairosLogger) bool {
	// Must be a dm device to be a multipath partition
	if !isMultipathDevice(paths, entry, logger) {
		return false
	}

	deviceName := entry.Name()
	udevInfo, err := udevInfoPartition(paths, deviceName, logger)
	if err != nil {
		logger.Logger.Error().Err(err).Str("devNo", deviceName).Msg("Failed to get udev info")
		return false
	}

	// Check if the udev info contains DM_PART indicating it's a partition
	// this is the primary check for multipath partitions and should be safe.
	_, ok := udevInfo["DM_PART"]
	return ok
}

// isHiddenDevice reports whether the block device is marked hidden by the
// kernel via /sys/block/<dev>/hidden == 1. A missing attribute is treated as
// not hidden, matching devices/kernels that do not expose it.
func isHiddenDevice(paths *Paths, disk string) bool {
	contents, err := os.ReadFile(filepath.Join(paths.SysBlock, disk, "hidden"))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(contents)) == "1"
}

func diskSizeBytes(paths *Paths, disk string, logger *sdkLogger.KairosLogger) uint64 {
	// We can find the number of 512-byte sectors by examining the contents of
	// /sys/block/$DEVICE/size and calculate the physical bytes accordingly.
	path := filepath.Join(paths.SysBlock, disk, "size")
	logger.Logger.Trace().Str("path", path).Msg("Reading disk size")
	contents, err := os.ReadFile(path)
	if err != nil {
		logger.Logger.Error().Str("path", path).Err(err).Msg("Failed to read file")
		return 0
	}
	size, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil {
		logger.Logger.Error().Str("path", path).Err(err).Str("content", string(contents)).Msg("Failed to parse size")
		return 0
	}
	logger.Logger.Trace().Uint64("size", size*sectorSize).Msg("Got disk size")
	return size * sectorSize
}
