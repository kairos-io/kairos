package uki

import (
	"fmt"
	"os"
	"strings"

	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"golang.org/x/sys/unix"
)

// artifactSetSize adds up the size of every file in dir that belongs to the
// artifact set with the given role.
//
// It selects the same files that copyArtifactSetRole copies and that
// removeArtifactSetWithRole deletes, so the number it returns is what one copy
// of the set costs, and what dropping the set gives back.
func artifactSetSize(fs sdkFs.KairosFS, dir, role string) (int64, error) {
	var size int64

	err := fsutils.WalkDirFs(fs, dir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasPrefix(info.Name(), role) {
			return nil
		}

		stat, err := fs.Stat(path)
		if err != nil {
			return err
		}
		size += stat.Size()

		return nil
	})

	return size, err
}

// freeSpaceOn returns the bytes still writable on the filesystem that holds
// path. It reports Bavail rather than Bfree, so it does not count blocks that
// only root may use.
func freeSpaceOn(fs sdkFs.KairosFS, path string) (int64, error) {
	realPath, err := fs.RawPath(path)
	if err != nil {
		return 0, fmt.Errorf("resolving %s: %w", path, err)
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(realPath, &stat); err != nil {
		return 0, fmt.Errorf("reading free space on %s: %w", path, err)
	}

	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// checkSpaceForInstall fails before the artifact set is copied into its roles,
// rather than letting a copy run out of room part way and leave the EFI
// partition holding an unbootable half of an install.
//
// The source is already on disk as the UnassignedArtifactRole set, so all that
// is left to pay for is one copy of it per role.
func checkSpaceForInstall(fs sdkFs.KairosFS, efiDir string, copies int, logger sdkLogger.KairosLogger) error {
	setSize, err := artifactSetSize(fs, efiDir, UnassignedArtifactRole)
	if err != nil {
		return fmt.Errorf("measuring the %s artifact set: %w", UnassignedArtifactRole, err)
	}

	free, err := freeSpaceOn(fs, efiDir)
	if err != nil {
		return err
	}

	needed := setSize * int64(copies)
	logger.Debugf("EFI partition %s: %d bytes free, %d needed for %d copies of a %d byte artifact set",
		efiDir, free, needed, copies, setSize)

	if needed > free {
		return fmt.Errorf("not enough space on the EFI partition %s: %d copies of a %d byte artifact set need %d bytes, %d free. Install with a larger EFI partition or a smaller source",
			efiDir, copies, setSize, needed, free)
	}

	return nil
}

// checkSpaceForUpgradeRotation fails before the rotation starts. Once it has
// started, the passive set and then the active set are already gone, so a copy
// that runs out of room leaves the machine with no entry to boot.
//
// The rotation drops passive, copies active over it, drops active, then copies
// the new set over that. It never holds more than one extra copy at a time, so
// the larger of the two copies has to fit in the free space plus what dropping
// passive gives back.
func checkSpaceForUpgradeRotation(fs sdkFs.KairosFS, efiDir string, logger sdkLogger.KairosLogger) error {
	newSize, err := artifactSetSize(fs, efiDir, UnassignedArtifactRole)
	if err != nil {
		return fmt.Errorf("measuring the %s artifact set: %w", UnassignedArtifactRole, err)
	}

	activeSize, err := artifactSetSize(fs, efiDir, "active")
	if err != nil {
		return fmt.Errorf("measuring the active artifact set: %w", err)
	}

	passiveSize, err := artifactSetSize(fs, efiDir, "passive")
	if err != nil {
		return fmt.Errorf("measuring the passive artifact set: %w", err)
	}

	free, err := freeSpaceOn(fs, efiDir)
	if err != nil {
		return err
	}

	biggestCopy := max(activeSize, newSize)
	room := free + passiveSize
	logger.Debugf("EFI partition %s: %d bytes free plus %d from passive, biggest copy to make is %d (new %d, active %d)",
		efiDir, free, passiveSize, biggestCopy, newSize, activeSize)

	if biggestCopy > room {
		return fmt.Errorf("not enough space on the EFI partition %s: rotating a %d byte upgrade over a %d byte active set needs %d bytes, %d free plus %d from the passive set it replaces. Free space on the EFI partition or upgrade to a smaller source",
			efiDir, newSize, activeSize, biggestCopy, free, passiveSize)
	}

	return nil
}
