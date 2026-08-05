package ghw

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
)

type PartitionHandler interface {
	// GetPartitions returns a list of partitions on the system.
	GetPartitions(paths *Paths, logger *logger.KairosLogger) partitions.PartitionList
}

func partitionSizeBytes(paths *Paths, partitionPath string, logger *logger.KairosLogger) uint64 {
	path := filepath.Join(paths.SysBlock, partitionPath, "size")
	logger.Logger.Trace().Str("file", path).Msg("Reading size file")
	contents, err := os.ReadFile(path)
	if err != nil {
		logger.Logger.Error().Str("file", path).Err(err).Msg("failed to read disk partition size")
		return 0
	}
	size, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil {
		logger.Logger.Error().Str("contents", string(contents)).Err(err).Msg("failed to parse disk partition size")
		return 0
	}
	logger.Logger.Trace().Str("partition", partitionPath).Uint64("size", size*sectorSize).Msg("Got partition size")
	return size * sectorSize
}

func partitionInfo(paths *Paths, part string, logger *logger.KairosLogger) (string, string) {
	// Allow calling PartitionInfo with either the full partition name
	// "/dev/sda1" or just "sda1"
	if !strings.HasPrefix(part, "/dev") {
		part = "/dev/" + part
	}

	// mount entries for mounted partitions look like this:
	// /dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0
	var r io.ReadCloser
	logger.Logger.Trace().Str("file", paths.ProcMounts).Msg("Reading mounts file")
	r, err := os.Open(paths.ProcMounts)
	if err != nil {
		logger.Logger.Error().Str("file", paths.ProcMounts).Err(err).Msg("failed to open mounts")
		return "", ""
	}
	defer r.Close()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		logger.Logger.Trace().Str("line", line).Msg("Parsing mount info")
		entry := parseMountEntry(line, logger)
		if entry == nil || entry.Partition != part {
			continue
		}

		return entry.Mountpoint, entry.FilesystemType
	}
	return "", ""
}

type mountEntry struct {
	Partition      string
	Mountpoint     string
	FilesystemType string
}

func parseMountEntry(line string, logger *logger.KairosLogger) *mountEntry {
	// mount entries for mounted partitions look like this:
	// /dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0
	if line[0] != '/' {
		return nil
	}
	fields := strings.Fields(line)

	if len(fields) < 4 {
		logger.Logger.Trace().Interface("fields", fields).Msg("Mount line has less than 4 fields")
		return nil
	}

	// We do some special parsing of the mountpoint, which may contain space,
	// tab and newline characters, encoded into the mount entry line using their
	// octal-to-string representations. From the GNU mtab man pages:
	//
	//   "Therefore these characters are encoded in the files and the getmntent
	//   function takes care of the decoding while reading the entries back in.
	//   '\040' is used to encode a space character, '\011' to encode a tab
	//   character, '\012' to encode a newline character, and '\\' to encode a
	//   backslash."
	mp := fields[1]
	r := strings.NewReplacer(
		"\\011", "\t", "\\012", "\n", "\\040", " ", "\\\\", "\\",
	)
	mp = r.Replace(mp)

	res := &mountEntry{
		Partition:      fields[0],
		Mountpoint:     mp,
		FilesystemType: fields[2],
	}
	return res
}

func diskUUID(paths *Paths, partitionPath string, logger *logger.KairosLogger) string {
	info, err := udevInfoPartition(paths, partitionPath, logger)
	logger.Logger.Trace().Interface("info", info).Msg("Disk UUID")
	if err != nil {
		logger.Logger.Error().Str("partition", partitionPath).Interface("info", info).Err(err).Msg("failed to read disk UUID")
		return UNKNOWN
	}

	if pType, ok := info["ID_PART_TABLE_UUID"]; ok {
		logger.Logger.Trace().Str("disk", partitionPath).Str("partition", partitionPath).Str("uuid", pType).Msg("Got disk uuid")
		return pType
	}

	return UNKNOWN
}

func diskPartUUID(paths *Paths, partitionPath string, logger *logger.KairosLogger) string {
	info, err := udevInfoPartition(paths, partitionPath, logger)
	logger.Logger.Trace().Interface("info", info).Msg("Disk Part UUID")
	if err != nil {
		logger.Logger.Error().Str("partition", partitionPath).Interface("info", info).Err(err).Msg("Disk Part UUID")
		return UNKNOWN
	}

	if pType, ok := info["ID_PART_ENTRY_UUID"]; ok {
		logger.Logger.Trace().Str("partition", partitionPath).Str("uuid", pType).Msg("Got partition uuid")
		return pType
	}
	return UNKNOWN
}

// diskPartTypeUdev gets the partition type from the udev database directly and its only used as fallback when
// the partition is not mounted, so we cannot get the type from paths.ProcMounts from the partitionInfo function.
func diskPartTypeUdev(paths *Paths, partitionPath string, logger *logger.KairosLogger) string {
	info, err := udevInfoPartition(paths, partitionPath, logger)
	logger.Logger.Trace().Interface("info", info).Msg("Disk Part Type")
	if err != nil {
		logger.Logger.Error().Str("partition", partitionPath).Interface("info", info).Err(err).Msg("Disk Part Type")
		return UNKNOWN
	}

	if pType, ok := info["ID_FS_TYPE"]; ok {
		logger.Logger.Trace().Str("partition", partitionPath).Str("FS", pType).Msg("Got partition fs type")
		return pType
	}
	return UNKNOWN
}

func diskFSLabel(paths *Paths, partitionPath string, logger *logger.KairosLogger) string {
	info, err := udevInfoPartition(paths, partitionPath, logger)
	logger.Logger.Trace().Interface("info", info).Msg("Disk FS label")
	if err != nil {
		logger.Logger.Error().Str("partition", partitionPath).Interface("info", info).Err(err).Msg("Disk FS label")
		return UNKNOWN
	}

	if label, ok := info["ID_FS_LABEL"]; ok {
		logger.Logger.Trace().Str("partition", partitionPath).Str("uuid", label).Msg("Got partition label")
		return label
	}
	return UNKNOWN
}

func diskPartitionLabel(paths *Paths, partitionPath string, logger *logger.KairosLogger) string {
	info, err := udevInfoPartition(paths, partitionPath, logger)
	logger.Logger.Trace().Interface("info", info).Msg("Disk partition label")
	if err != nil {
		logger.Logger.Error().Str("partition", partitionPath).Interface("info", info).Err(err).Msg("Disk partition label")
		return UNKNOWN
	}

	if label, ok := info["ID_PART_ENTRY_NAME"]; ok {
		logger.Logger.Trace().Str("partition", partitionPath).Str("label", label).Msg("Got partition label")
		return label
	}
	return UNKNOWN
}

func udevInfoPartition(paths *Paths, partitionPath string, logger *logger.KairosLogger) (map[string]string, error) {
	// Get device major:minor numbers
	devNo, err := os.ReadFile(filepath.Join(paths.SysBlock, partitionPath, "dev"))
	if err != nil {
		logger.Logger.Error().Err(err).Str("path", filepath.Join(paths.SysBlock, partitionPath, "dev")).Msg("failed to read udev info")
		return nil, err
	}
	return UdevInfo(paths, string(devNo), logger)
}

// UdevInfo will return information on udev database about a device number.
func UdevInfo(paths *Paths, devNo string, logger *logger.KairosLogger) (map[string]string, error) {
	// Look up block device in udev runtime database
	udevID := "b" + strings.TrimSpace(devNo)
	udevBytes, err := os.ReadFile(filepath.Join(paths.RunUdevData, udevID))
	if err != nil {
		logger.Logger.Error().Err(err).Str("path", filepath.Join(paths.RunUdevData, udevID)).Msg("failed to read udev info for device")
		return nil, err
	}

	udevInfo := make(map[string]string)
	for _, udevLine := range strings.Split(string(udevBytes), "\n") {
		if strings.HasPrefix(udevLine, "E:") {
			if s := strings.SplitN(udevLine[2:], "=", 2); len(s) == 2 {
				udevInfo[s[0]] = s[1]
			}
		}
	}
	return udevInfo, nil
}
