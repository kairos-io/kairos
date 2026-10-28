package action

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/diffupgrade"
	"github.com/kairos-io/kairos/v4/sdk/state"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
)

// tryDiffDeploy builds the transition image from a diff instead of the full
// image, when a diff upgrade was asked for. It reports whether it did: on true
// the transition image holds the target and is mounted at
// upgradeImg.MountPoint, exactly as DeployImage leaves it, so the rest of the
// upgrade does not know which way it was built.
//
// On false nothing is left behind and the caller deploys the full image. Every
// failure here happens before the active or passive image is touched, so
// falling back is always safe, and it is what the diff upgrade promises: a
// missing diff, a diff for another release or one that does not fit in the
// image costs the bandwidth the diff would have saved and nothing else.
func (u *UpgradeAction) tryDiffDeploy(e *elemental.Elemental, upgradeImg *sdkImages.Image, bootedFrom state.Boot) bool {
	if !u.spec.DiffUpgrade() {
		return false
	}
	err := u.diffDeploy(e, upgradeImg, bootedFrom)
	if err == nil {
		return true
	}
	u.config.Logger.Warnf("Diff upgrade not possible, falling back to the full image: %s", err)
	if umountErr := e.UnmountImage(upgradeImg); umountErr != nil {
		u.Debug("could not unmount the diff transition image: %s", umountErr)
	}
	if rmErr := u.remove(upgradeImg.File); rmErr != nil {
		u.Debug("could not remove the diff transition image: %s", rmErr)
	}
	return false
}

func (u *UpgradeAction) diffDeploy(e *elemental.Elemental, upgradeImg *sdkImages.Image, bootedFrom state.Boot) error {
	if u.spec.RecoveryUpgrade() {
		return errors.New("diff upgrades are not supported for the recovery system")
	}
	if upgradeImg.FS == constants.SquashFs {
		return errors.New("diff upgrades are not supported for squashfs images")
	}
	diffSrc, err := u.spec.ResolveDiffSource()
	if err != nil {
		return err
	}
	if diffSrc.IsFile() {
		return errors.New("file: is not a diff source, use an oci, ocifile or dir source")
	}

	// The diff applies onto the release that is running. Booted from
	// passive, that is passive.img, which a regular upgrade leaves alone.
	baseFile := filepath.Join(u.spec.Partitions.State.MountPoint, "cOS", constants.ActiveImgFile)
	if bootedFrom == state.Passive {
		baseFile = u.spec.Passive.File
	}
	if exists, _ := fsutils.Exists(u.config.Fs, baseFile); !exists {
		return fmt.Errorf("base image %s not found", baseFile)
	}

	u.Info("Copying %s to %s to apply the diff onto", baseFile, upgradeImg.File)
	// cp rather than a copy through the Fs, so the copy stays as sparse as
	// the image it is taken from.
	if out, err := u.config.Runner.Run("cp", "--sparse=always", baseFile, upgradeImg.File); err != nil {
		return fmt.Errorf("copying %s: %s: %w", baseFile, string(out), err)
	}
	if upgradeImg.Label != "" {
		if out, err := u.config.Runner.Run("tune2fs", "-L", upgradeImg.Label, upgradeImg.File); err != nil {
			return fmt.Errorf("labeling %s: %s: %w", upgradeImg.File, string(out), err)
		}
	}
	if err := e.MountImage(upgradeImg, "rw"); err != nil {
		return err
	}

	// Taken before the diff is unpacked, as the diff overwrites the file it
	// is computed from.
	base, err := diffupgrade.Fingerprint(u.config.Fs, upgradeImg.MountPoint)
	if err != nil {
		return err
	}

	u.Info("Applying diff %s onto %s", diffSrc.Value(), upgradeImg.File)
	if _, err := e.DumpSource(upgradeImg.MountPoint, diffSrc, u.spec.ExcludedPaths...); err != nil {
		return err
	}
	if err := diffupgrade.Apply(u.config.Fs, upgradeImg.MountPoint, base); err != nil {
		return err
	}
	return utils.CreateDirStructure(u.config.Fs, upgradeImg.MountPoint)
}
