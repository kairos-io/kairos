package action

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	sdkFS "github.com/kairos-io/kairos/v4/sdk/types/fs"
	"golang.org/x/sys/unix"
)

// freeBytesOn is a variable so tests can put the state partition at any fill level.
var freeBytesOn = utils.FreeBytesOn

// noRoomSingleImageReason is added to the free space error on a node without
// a recovery partition, where the missing room is by design.
const noRoomSingleImageReason = "This node was built with a single system image and no recovery partition, so there is no room for the upgrade image. Replace the node with a fresh disk."

const bytesPerMiB = 1 << 20

// imageFootprint returns the space a file takes on disk, which for the sparse
// system images is what they hold rather than the size they were created with.
func imageFootprint(fsys sdkFS.KairosFS, path string) (int64, error) {
	realPath, err := fsys.RawPath(path)
	if err != nil {
		return 0, err
	}
	var st unix.Stat_t
	if err := unix.Stat(realPath, &st); err != nil {
		return 0, err
	}
	return st.Blocks * 512, nil
}

// checkStateSpace fails before the upgrade image is downloaded when COS_STATE
// cannot hold the transition image next to the active one.
//
// The size of the new image is not known before it is downloaded, so the
// current active image stands in for it: a disk without room for another
// image of that size cannot take the upgrade. A transition image left by an
// earlier failed upgrade is recreated, so it counts as free. When the space
// cannot be measured the upgrade goes on, as it did before the check existed.
func (u *UpgradeAction) checkStateSpace() error {
	stateDir := u.spec.Partitions.State.MountPoint
	cosDir := filepath.Join(stateDir, "cOS")

	needed, err := imageFootprint(u.config.Fs, filepath.Join(cosDir, constants.ActiveImgFile))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			u.Debug("Not checking free space, could not measure the active image: %s", err)
		}
		return nil
	}

	free, err := freeBytesOn(u.config.Fs, stateDir)
	if err != nil {
		u.Debug("Not checking free space: %s", err)
		return nil
	}
	if leftover, err := imageFootprint(u.config.Fs, filepath.Join(cosDir, constants.TransitionImgFile)); err == nil {
		free += leftover
	}

	if free >= needed {
		return nil
	}

	msg := fmt.Sprintf("not enough free space on the state partition (%s) for the upgrade image: %d MiB free, about %d MiB needed. Free some space and run the upgrade again.",
		stateDir, free/bytesPerMiB, needed/bytesPerMiB)
	if IsSingleImageNode(u.spec.Partitions.Recovery) {
		msg = fmt.Sprintf("not enough free space on the state partition (%s) for the upgrade image: %d MiB free, about %d MiB needed. %s",
			stateDir, free/bytesPerMiB, needed/bytesPerMiB, noRoomSingleImageReason)
	}
	return errors.New(msg)
}
