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

// spaceNeededForInstall returns the bytes an install still has to write after
// the source is on disk.
//
// The install dumps the source once as the UnassignedArtifactRole set, then
// copies that set once per role, so the copies are all that is left to pay for.
func spaceNeededForInstall(unassignedSize int64, roles int) int64 {
	return unassignedSize * int64(roles)
}

// spaceNeededForUpgradeRotation returns the bytes an upgrade needs free on the
// EFI partition to rotate, with the new set already dumped as
// UnassignedArtifactRole.
//
// The rotation runs in four steps: drop passive, copy active over it, drop
// active, copy the new set over that. Dropping passive is what pays for both
// copies, so the partition has to hold whichever of the two copied sets is
// larger, less what passive gives back. A set that already fits needs nothing,
// hence the floor at zero.
func spaceNeededForUpgradeRotation(newSize, activeSize, passiveSize int64) int64 {
	needed := max(activeSize, newSize) - passiveSize
	if needed < 0 {
		return 0
	}

	return needed
}

// checkSpaceForInstall fails before the artifact set is copied into its roles,
// rather than letting a copy run out of room part way and leave the EFI
// partition holding an unbootable half of an install.
func checkSpaceForInstall(fs sdkFs.KairosFS, efiDir string, roles []string, logger sdkLogger.KairosLogger) error {
	unassignedSize, err := artifactSetSize(fs, efiDir, UnassignedArtifactRole)
	if err != nil {
		return fmt.Errorf("measuring the %s artifact set: %w", UnassignedArtifactRole, err)
	}

	free, err := freeSpaceOn(fs, efiDir)
	if err != nil {
		return err
	}

	needed := spaceNeededForInstall(unassignedSize, len(roles))
	logger.Debugf("EFI partition %s: %d bytes free, %d needed for %d copies of a %d byte artifact set",
		efiDir, free, needed, len(roles), unassignedSize)

	if needed > free {
		return fmt.Errorf(
			"not enough space on the EFI partition %s: installing %d artifact sets of %d bytes each needs %d bytes, %d free. Install with a larger EFI partition or a smaller source",
			efiDir, len(roles), unassignedSize, needed, free)
	}

	return nil
}

// checkSpaceForUpgradeRotation fails before the rotation starts. Once it has
// started, the passive set and then the active set are already gone, so a copy
// that runs out of room leaves the machine with no entry to boot.
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

	needed := spaceNeededForUpgradeRotation(newSize, activeSize, passiveSize)
	logger.Debugf("EFI partition %s: %d bytes free, %d needed to rotate (new %d, active %d, passive %d)",
		efiDir, free, needed, newSize, activeSize, passiveSize)

	if needed > free {
		return fmt.Errorf(
			"not enough space on the EFI partition %s: rotating a %d byte upgrade over a %d byte active set needs %d bytes free, %d available. Free space on the EFI partition or upgrade to a smaller source",
			efiDir, newSize, activeSize, needed, free)
	}

	return nil
}
