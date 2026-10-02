package blockdev

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// SizeSectors returns the size of a block device in 512-byte sectors, read from
// sysfs by the device's major:minor so it works for partitions, mappers and
// by-label symlinks alike.
func SizeSectors(device string) (uint64, error) {
	rdev, err := rdevOf(device)
	if err != nil {
		return 0, err
	}
	raw, err := os.ReadFile(filepath.Join(sysDevBlock, rdev, "size"))
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
}

// SnapshotStatus is what the kernel reports for a dm snapshot target.
type SnapshotStatus struct {
	// UsedSectors and TotalSectors describe the copy-on-write store: how much
	// of it holds changed chunks, out of how much there is. MetadataSectors is
	// the part of the store spent on the exception table.
	UsedSectors     uint64 `yaml:"used_sectors" json:"used_sectors"`
	TotalSectors    uint64 `yaml:"total_sectors" json:"total_sectors"`
	MetadataSectors uint64 `yaml:"metadata_sectors" json:"metadata_sectors"`
	// State is "active" while the store has room, "overflow" once a PO store
	// has filled and refuses writes but still serves reads, and "invalid" once
	// a store without overflow support has filled and the whole device fails.
	State string `yaml:"state" json:"state"`
}

// UsedPercent is how full the store is, rounded down.
func (s SnapshotStatus) UsedPercent() int {
	if s.TotalSectors == 0 {
		return 0
	}
	return int(s.UsedSectors * 100 / s.TotalSectors)
}

// ParseSnapshotStatus reads one line of `dmsetup status` for a snapshot
// target. The kernel prints "<used>/<total> <metadata>" while the store has
// room, and the single word Overflow or Invalid once it has not.
func ParseSnapshotStatus(line string) (SnapshotStatus, error) {
	fields := strings.Fields(line)
	// "0 <length> snapshot ..." when the target name is included, or just the
	// status part on its own.
	for i, f := range fields {
		if f == "snapshot" {
			fields = fields[i+1:]
			break
		}
	}
	if len(fields) == 0 {
		return SnapshotStatus{}, fmt.Errorf("not a snapshot status line: %q", line)
	}
	switch strings.ToLower(fields[0]) {
	case "overflow":
		return SnapshotStatus{State: "overflow"}, nil
	case "invalid":
		return SnapshotStatus{State: "invalid"}, nil
	}
	used, total, ok := strings.Cut(fields[0], "/")
	if !ok {
		return SnapshotStatus{}, fmt.Errorf("not a snapshot status line: %q", line)
	}
	st := SnapshotStatus{State: "active"}
	var err error
	if st.UsedSectors, err = strconv.ParseUint(used, 10, 64); err != nil {
		return SnapshotStatus{}, fmt.Errorf("parsing snapshot status %q: %w", line, err)
	}
	if st.TotalSectors, err = strconv.ParseUint(total, 10, 64); err != nil {
		return SnapshotStatus{}, fmt.Errorf("parsing snapshot status %q: %w", line, err)
	}
	if len(fields) > 1 {
		if st.MetadataSectors, err = strconv.ParseUint(fields[1], 10, 64); err != nil {
			return SnapshotStatus{}, fmt.Errorf("parsing snapshot status %q: %w", line, err)
		}
	}
	return st, nil
}

// QuerySnapshotStatus asks device-mapper about the named snapshot.
func QuerySnapshotStatus(name string) (SnapshotStatus, error) {
	out, err := utils.SH("dmsetup status " + name)
	if err != nil {
		return SnapshotStatus{}, fmt.Errorf("dmsetup status %s: %s: %w", name, strings.TrimSpace(out), err)
	}
	return ParseSnapshotStatus(out)
}
