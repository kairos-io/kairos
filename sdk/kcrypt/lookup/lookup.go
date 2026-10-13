// Package lookup holds the partition-label resolution helpers that the boot
// and post-install mount paths depend on to disambiguate a LUKS container
// from its unlocked mapper. Both sides advertise the same filesystem label
// on pre-fix installs (see kairos-io/kairos#4403), so any caller that resolves
// a label to a device path via /dev/disk/by-label/<X> or a bare blkid -L
// is racing with udev.
//
// The helpers live in this leaf package so both sdk/kcrypt (the encryption
// flow) and sdk/machine (the post-install mount helpers) can call them
// without introducing an import cycle through sdk/collector.
package lookup

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// OuterLUKSLabel returns the label to place on the LUKS container for a given
// inner filesystem label. For the two Kairos-owned inner labels
// (COS_PERSISTENT / COS_OEM) the mapping is fixed to the constants declared
// alongside them. Any other label (a user-defined encrypted partition) gets
// the same _LUKS suffix appended so the outer/inner split still holds. The
// function is idempotent: passing an already-suffixed label returns it
// unchanged, and an empty label returns empty.
func OuterLUKSLabel(innerLabel string) string {
	switch innerLabel {
	case "":
		return ""
	case constants.PersistentLabel:
		return constants.PersistentLUKSLabel
	case constants.OEMLabel:
		return constants.OEMLUKSLabel
	}
	if strings.HasSuffix(innerLabel, "_LUKS") {
		return innerLabel
	}
	return innerLabel + "_LUKS"
}

// IsOuterLUKSLabel reports whether the given label identifies a LUKS
// container (as opposed to the plaintext filesystem inside it). Kairos-owned
// labels are checked against their dedicated constants; other labels are
// recognised by the shared _LUKS suffix convention.
func IsOuterLUKSLabel(label string) bool {
	switch label {
	case constants.PersistentLUKSLabel, constants.OEMLUKSLabel:
		return true
	}
	return label != "" && strings.HasSuffix(label, "_LUKS")
}

// LegacyPartitionName maps a Kairos filesystem label back to its GPT
// PartitionLabel counterpart (COS_PERSISTENT -> persistent, COS_OEM -> oem).
// The reverse mapping is needed to find a partition whose filesystem label
// was never set. That is the pre-#822 install shape where the LUKS container has no
// FS label at all and only its GPT PARTLABEL identifies it
// (kairos-sdk#822 / kairos-io/kairos#4276).
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
// without regard to filesystem type. Used at encrypt time when the partition
// has not yet been LUKS-formatted and any FS is acceptable. Callers who need
// a specific FS type must use FindLUKSContainerByLabel or FindMapperByLabel
// so the by-label ambiguity does not steer them to the wrong device.
//
// Lookup order:
//  1. ghw walk matching FilesystemLabel, OuterLUKSLabel(label), or the GPT
//     PartitionLabel counterpart.
//  2. blkid -L <label>, blkid -L <label>_LUKS, blkid -t PARTLABEL=<gpt-name>
//     as a last-resort for installs whose ghw view omits the target.
func FindByLabel(partitionLabel string) (*partitions.Partition, error) {
	return findByLabelFiltered(partitionLabel, nil)
}

// FindLUKSContainerByLabel returns the crypto_LUKS partition carrying the
// given filesystem label. Used at unlock time to unambiguously locate the
// container even when the outer LUKS and its inner ext4 share the same label
// (pre-fix installs, kairos-io/kairos#4403).
func FindLUKSContainerByLabel(partitionLabel string) (*partitions.Partition, error) {
	return findByLabelFiltered(partitionLabel, luksContainerFilter)
}

// FindLUKSContainerOnDisks is FindLUKSContainerByLabel against an
// already-scanned disk list, for callers resolving many labels on one
// ScanBlockDevices walk.
func FindLUKSContainerOnDisks(disks []*partitions.Disk, partitionLabel string) (*partitions.Partition, error) {
	return findOnDisks(disks, partitionLabel, luksContainerFilter)
}

// FindMapperByLabel returns the plaintext filesystem carrying the given
// filesystem label. That is, the ext4/xfs partition, or the unlocked mapper of a
// crypto_LUKS container. Used to bypass /dev/disk/by-label ambiguity when
// mounting a label that also exists on the LUKS wrapper.
//
// If ghw does not enumerate the mapper directly (single-filesystem-on-mapper
// setups typically appear as a whole-disk device), the caller can compose
// the mapper path from FindLUKSContainerByLabel's Name via MountSourceForLabel.
func FindMapperByLabel(partitionLabel string) (*partitions.Partition, error) {
	return findByLabelFiltered(partitionLabel, mapperFilter)
}

// FindMapperOnDisks is FindMapperByLabel against an already-scanned disk
// list, for callers resolving many labels on one ScanBlockDevices walk.
func FindMapperOnDisks(disks []*partitions.Disk, partitionLabel string) (*partitions.Partition, error) {
	return findOnDisks(disks, partitionLabel, mapperFilter)
}

func luksContainerFilter(p *partitions.Partition) bool {
	return p.FS == constants.LUKSFs
}

func mapperFilter(p *partitions.Partition) bool {
	return p.FS != constants.LUKSFs
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

// ScanBlockDevices performs the one ghw walk the by-label helpers share. The
// Find*OnDisks variants match against its result, so a caller classifying
// many labels pays for one scan instead of one per lookup.
func ScanBlockDevices() ([]*partitions.Disk, error) {
	logger := sdkLogger.NewNullLogger()
	disks := ghw.GetDisks(ghw.NewPaths(""), &logger)
	if disks == nil {
		return nil, fmt.Errorf("failed to scan block devices")
	}
	return disks, nil
}

func findByLabelFiltered(partitionLabel string, filter func(*partitions.Partition) bool) (*partitions.Partition, error) {
	disks, err := ScanBlockDevices()
	if err != nil {
		return nil, err
	}

	if p, err := findOnDisks(disks, partitionLabel, filter); err == nil {
		return p, nil
	}

	// blkid fallbacks for installs where ghw doesn't surface the partition,
	// primarily older installs with no FS label at all
	// (kairos-sdk#822 / kairos-io/kairos#4276) whose LUKS container is
	// discoverable only via GPT PARTLABEL. blkid can't filter by FS type, so
	// this branch is intentionally reached only when the ghw scan already
	// failed to satisfy the (typed) match.
	if filter == nil {
		return FindByBlkid(partitionLabel)
	}
	return nil, fmt.Errorf("partition not found")
}

func findOnDisks(disks []*partitions.Disk, partitionLabel string, filter func(*partitions.Partition) bool) (*partitions.Partition, error) {
	outerLabel := OuterLUKSLabel(partitionLabel)
	legacyName := LegacyPartitionName(partitionLabel)

	labelMatches := func(p *partitions.Partition) bool {
		return p.FilesystemLabel == partitionLabel ||
			p.FilesystemLabel == outerLabel ||
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

// FindByBlkid resolves a label straight through blkid (-L for the label and
// its _LUKS outer form, PARTLABEL for the pre kairos-sdk#822 GPT name), for
// installs whose partition ghw does not surface at all. The result carries
// only Path and Name: blkid's by-label output says nothing about the
// filesystem type, so a caller that needs it must probe with FilesystemType.
func FindByBlkid(partitionLabel string) (*partitions.Partition, error) {
	return findByBlkid(partitionLabel, OuterLUKSLabel(partitionLabel), LegacyPartitionName(partitionLabel))
}

// LabelIsEncrypted reports whether the partition carrying the given
// filesystem label is already a LUKS container, against an already-scanned
// disk list. The answer typically feeds a luksFormat decision, so it never
// guesses: a label found nowhere is an error, and so is a filesystem that
// cannot be determined, because "probably plaintext" is not an acceptable
// answer ahead of a destructive write. ghw's filesystem type mirrors the
// udev database, which can lag or omit the type; when it does, the device
// itself is asked.
//
// blkidLookup and fsProbe are the device probes (FindByBlkid and
// FilesystemType in production); they are parameters so tests can stub them
// on hosts without blkid or real devices, and so every consumer (immucore's
// encrypt-pending step, the agent's kcrypt encrypt subcommand and reset
// path) shares one classification instead of each drifting on the edge
// cases (pre kairos-sdk#822 installs whose container only blkid's PARTLABEL
// view finds, udev views with no filesystem type).
func LabelIsEncrypted(
	disks []*partitions.Disk,
	label string,
	blkidLookup func(string) (*partitions.Partition, error),
	fsProbe func(string) (string, error),
) (bool, error) {
	if _, err := FindLUKSContainerOnDisks(disks, label); err == nil {
		return true, nil
	}

	part, err := FindMapperOnDisks(disks, label)
	if err != nil {
		// Pre kairos-sdk#822 installs carry no filesystem label at all and
		// only blkid's PARTLABEL view finds their LUKS container.
		if part, err = blkidLookup(label); err != nil {
			return false, fmt.Errorf("partition %s is configured for encryption but was not found", label)
		}
	}

	fs := part.FS
	if fs == "" || fs == ghw.UNKNOWN {
		fs, _ = fsProbe(part.Path)
	}
	switch fs {
	case constants.LUKSFs:
		return true, nil
	case "", ghw.UNKNOWN:
		return false, fmt.Errorf("the filesystem on partition %s (%s) could not be determined; refusing to treat it as plaintext", label, part.Path)
	}
	return false, nil
}

// FilesystemType probes the filesystem type on a device with blkid
// ("crypto_LUKS" for a LUKS container, "" when blkid cannot tell). ghw's FS
// field mirrors the udev database, which can lag behind the device or omit
// the type entirely; blkid reads the device itself, so this is the answer a
// destructive decision must consult before treating a partition as
// plaintext.
func FilesystemType(devicePath string) (string, error) {
	return blkid("-s", "TYPE", "-o", "value", devicePath)
}

func findByBlkid(partitionLabel, outerLabel, legacyName string) (*partitions.Partition, error) {
	devicePath, err := blkid("-L", partitionLabel)

	if (err != nil || devicePath == "") && outerLabel != "" && outerLabel != partitionLabel {
		devicePath, err = blkid("-L", outerLabel)
	}

	if (err != nil || devicePath == "") && legacyName != "" {
		devicePath, err = blkid("-t", "PARTLABEL="+legacyName, "-o", "device")
	}

	if err != nil || devicePath == "" {
		return nil, fmt.Errorf("partition not found")
	}

	return &partitions.Partition{
		Path: devicePath,
		Name: filepath.Base(devicePath),
	}, nil
}

// blkid runs blkid with the given arguments and returns its trimmed stdout.
// It execs the binary directly instead of going through a shell: labels reach
// this package from user config (install.encrypted_partitions, cmdline
// overrides), so one carrying whitespace would break the lookup and one
// carrying shell metacharacters would run as root at install time.
func blkid(args ...string) (string, error) {
	out, err := exec.Command("blkid", args...).Output()
	return strings.TrimSpace(string(out)), err
}
