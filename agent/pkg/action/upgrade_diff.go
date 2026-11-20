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
// image, when a diff upgrade was asked for. It reports whether it did, and
// the RootfsID of the release it built: on true the transition image holds
// the target and is mounted at upgradeImg.MountPoint, exactly as DeployImage
// leaves it, so the rest of the upgrade does not know which way it was built.
//
// On false nothing is left behind and the caller deploys the full image. Every
// failure here happens before the active or passive image is touched, so
// falling back is always safe, and it is what the diff upgrade promises: a
// missing diff, a diff for another release or one that does not fit in the
// image costs the bandwidth the diff would have saved and nothing else.
func (u *UpgradeAction) tryDiffDeploy(e *elemental.Elemental, upgradeImg *sdkImages.Image, bootedFrom state.Boot) (string, bool) {
	if !u.spec.DiffUpgrade() {
		return "", false
	}
	id, err := u.diffDeploy(e, upgradeImg, bootedFrom)
	if err == nil {
		return id, true
	}
	u.config.Logger.Warnf("Diff upgrade not possible, falling back to the full image: %s", err)
	if umountErr := e.UnmountImage(upgradeImg); umountErr != nil {
		u.Debug("could not unmount the diff transition image: %s", umountErr)
	}
	if rmErr := u.remove(upgradeImg.File); rmErr != nil {
		u.Debug("could not remove the diff transition image: %s", rmErr)
	}
	return "", false
}

func (u *UpgradeAction) diffDeploy(e *elemental.Elemental, upgradeImg *sdkImages.Image, bootedFrom state.Boot) (string, error) {
	if u.spec.RecoveryUpgrade() {
		return "", errors.New("diff upgrades are not supported for the recovery system")
	}
	if upgradeImg.FS == constants.SquashFs {
		return "", errors.New("diff upgrades are not supported for squashfs images")
	}
	diffSrc, err := u.spec.ResolveDiffSource()
	if err != nil {
		return "", err
	}
	if diffSrc.IsFile() {
		return "", errors.New("file: is not a diff source, use an oci, ocifile or dir source")
	}

	// The diff applies onto the release that is running. Booted from
	// passive, that is passive.img, which a regular upgrade leaves alone.
	baseFile := filepath.Join(u.spec.Partitions.State.MountPoint, "cOS", constants.ActiveImgFile)
	if bootedFrom == state.Passive {
		baseFile = u.spec.Passive.File
	}
	if exists, _ := fsutils.Exists(u.config.Fs, baseFile); !exists {
		return "", fmt.Errorf("base image %s not found", baseFile)
	}
	// Checked before anything is copied or pulled: without a recorded ID
	// there is nothing a diff could be matched against.
	baseID, err := diffupgrade.RecordedID(u.config.Fs, baseFile)
	if err != nil {
		return "", err
	}
	u.Info("Base release %s is %s", baseFile, baseID)
	if diffSrc, err = u.diffForBase(diffSrc, baseID); err != nil {
		return "", err
	}

	u.Info("Copying %s to %s to apply the diff onto", baseFile, upgradeImg.File)
	if err := u.copyImage(baseFile, upgradeImg.File); err != nil {
		return "", err
	}
	if upgradeImg.Label != "" {
		if out, err := u.config.Runner.Run("tune2fs", "-L", upgradeImg.Label, upgradeImg.File); err != nil {
			return "", fmt.Errorf("labeling %s: %s: %w", upgradeImg.File, string(out), err)
		}
	}
	u.newFilesystemUUID(upgradeImg.File)
	if err := e.MountImage(upgradeImg, "rw"); err != nil {
		return "", err
	}

	u.Info("Applying diff %s onto %s", diffSrc.Value(), upgradeImg.File)
	if _, err := e.DumpSource(upgradeImg.MountPoint, diffSrc, u.spec.ExcludedPaths...); err != nil {
		return "", err
	}
	m, err := diffupgrade.Apply(u.config.Fs, upgradeImg.MountPoint, baseID)
	if err != nil {
		return "", err
	}
	return m.Target, utils.CreateDirStructure(u.config.Fs, upgradeImg.MountPoint)
}

// copyImage copies the image at src to dst with cp rather than through the
// Fs, so the copy stays as sparse as the image it is taken from. The cp of
// BusyBox has no --sparse, so without it the copy is made in full.
func (u *UpgradeAction) copyImage(src, dst string) error {
	out, err := u.config.Runner.Run("cp", "--sparse=always", src, dst)
	if err == nil {
		return nil
	}
	u.Debug("cp --sparse=always failed, copying %s in full: %s", src, string(out))
	if out, err = u.config.Runner.Run("cp", src, dst); err != nil {
		return fmt.Errorf("copying %s: %s: %w", src, string(out), err)
	}
	return nil
}

// newFilesystemUUID gives the copied image a filesystem UUID of its own.
// Otherwise active and passive would share one after the swap, which a full
// upgrade, whose image is a new filesystem, never leaves. The image boots
// either way, as state images are found by label, so a failure only warns.
//
// tune2fs refuses until the filesystem has been checked when the image has
// metadata checksums but not metadata_csum_seed, as images made by an older
// mkfs do, so a refusal is retried once after a check.
func (u *UpgradeAction) newFilesystemUUID(img string) {
	out, err := u.config.Runner.Run("tune2fs", "-U", "random", img)
	if err == nil {
		return
	}
	u.Debug("tune2fs -U random failed, checking %s and trying again: %s", img, string(out))
	_, _ = u.config.Runner.Run("e2fsck", "-fy", img)
	if out, err = u.config.Runner.Run("tune2fs", "-U", "random", img); err != nil {
		u.config.Logger.Warnf("%s keeps the filesystem UUID of the image it was copied from: %s: %s", img, err, string(out))
	}
}

// annotatedResolver is implemented by an image extractor that can look an
// image index up in its registry and pick an entry by its annotations.
type annotatedResolver interface {
	ResolveAnnotated(imageRef string, match func(annotations map[string]string) bool) (string, error)
}

// diffForBase narrows a registry diff source down to the diff for baseID.
// The diffs of a release are published together in an image index, one
// entry per base (see diffupgrade.PushToIndex), so the entry for this node
// is picked from the index, which is a few KB, before anything is pulled.
// A source that is a single image is returned as it is, and its base is
// checked once it is unpacked.
//
// When the index has no entry for baseID, or cannot be read, the diff is
// looked for at its own tag (diffupgrade.BaseRefFor), where it stays when a
// concurrent publish dropped it from the index. If it is not there either,
// pulling it fails, having pulled nothing, and the upgrade falls back.
func (u *UpgradeAction) diffForBase(src *sdkImages.ImageSource, baseID string) (*sdkImages.ImageSource, error) {
	if !src.IsDocker() {
		return src, nil
	}
	r, ok := u.config.ImageExtractor.(annotatedResolver)
	if !ok {
		return src, nil
	}
	ref, err := r.ResolveAnnotated(src.Value(), func(a map[string]string) bool { return a[diffupgrade.BaseLabel] == baseID })
	if err != nil {
		baseRef, refErr := diffupgrade.BaseRefFor(src.Value(), baseID)
		if refErr != nil {
			return nil, fmt.Errorf("finding the diff for %s in %s: %w", baseID, src.Value(), err)
		}
		u.Info("No diff for this release in %s (%s), trying %s", src.Value(), err, baseRef)
		return sdkImages.NewDockerSrc(baseRef), nil
	}
	if ref == src.Value() {
		return src, nil
	}
	u.Info("Diff for this release in %s is %s", src.Value(), ref)
	return sdkImages.NewDockerSrc(ref), nil
}
