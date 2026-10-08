// Package disks lists the block devices an installation can target.
//
// The rules for what counts as a candidate are the installer's, not the
// kernel's: virtual devices are not installation targets, and neither is
// anything too small to hold a Kairos layout. Every installer frontend has to
// apply the same rules, or an agent driving an install would offer a disk the
// interactive installer hides.
package disks

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/jaypipes/ghw/pkg/option"
)

// MinSizeBytes is the smallest disk offered as an installation target.
const MinSizeBytes = 1 * 1024 * 1024 * 1024 // 1 GiB

// excludedPrefixes are kernel device names that are never installation
// targets: loopback mounts, ramdisks, optical drives and compressed swap.
var excludedPrefixes = []string{"loop", "ram", "sr", "zram"}

// Disk is one candidate installation target.
type Disk struct {
	// Path is the device path, e.g. /dev/sda.
	Path string `json:"path" jsonschema:"device path of the disk, e.g. /dev/sda"`
	// SizeBytes is the disk's size as the kernel reports it.
	SizeBytes uint64 `json:"size_bytes" jsonschema:"size of the disk in bytes"`
	// Size is SizeBytes rendered for a human, e.g. "20.00 GiB".
	Size string `json:"size" jsonschema:"size of the disk, rendered for a human"`
	// Model is the disk's model string, when the kernel reports one.
	Model string `json:"model,omitempty" jsonschema:"model reported by the device, when it reports one"`
}

// Scan returns the disks an installation can target.
//
// It re-queries the kernel on every call, so it picks up disks that appeared
// or disappeared since the last one. A prerequisites plugin that wipes an LVM
// changes this answer, which is why nothing caches it (#4260).
func Scan() ([]Disk, error) {
	bl, err := block.New(option.WithDisableTools(), option.WithNullAlerter())
	if err != nil {
		return nil, err
	}

	var disks []Disk
	for _, disk := range bl.Disks {
		if excluded(disk.Name) || disk.SizeBytes < MinSizeBytes {
			continue
		}

		disks = append(disks, Disk{
			Path:      filepath.Join("/dev", disk.Name),
			SizeBytes: disk.SizeBytes,
			Size:      HumanSize(disk.SizeBytes),
			Model:     model(disk.Model),
		})
	}

	return disks, nil
}

// HumanSize renders a size in bytes the way the installer shows it.
func HumanSize(b uint64) string {
	return fmt.Sprintf("%.2f GiB", float64(b)/float64(1024*1024*1024))
}

func excluded(name string) bool {
	for _, prefix := range excludedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

// model normalises a device's model string. ghw reports "unknown" rather than
// nothing when the kernel does not say, which reads as a model called
// "unknown" once it reaches a caller.
func model(m string) string {
	m = strings.TrimSpace(m)
	if strings.EqualFold(m, "unknown") {
		return ""
	}

	return m
}
