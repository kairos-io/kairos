package action

import (
	"time"

	"github.com/kairos-io/kairos/v4/sdk/diffupgrade"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
)

// measureRootfsID returns the RootfsID of the release just unpacked at
// img.MountPoint, or an empty string when it cannot be taken. Call it before
// anything on the node changes the unpacked tree, as the ID has to name the
// release and not what the node made of it.
//
// A diff upgrade is only offered onto an image with a recorded ID, so failing
// to take one costs the next upgrade its diff and nothing else.
func measureRootfsID(cfg *sdkConfig.Config, img *sdkImages.Image) string {
	root, err := cfg.Fs.RawPath(img.MountPoint)
	if err != nil {
		cfg.Logger.Warnf("Could not take the rootfs ID of %s: %s", img.File, err)
		return ""
	}
	start := time.Now()
	id, err := diffupgrade.RootfsID(root)
	if err != nil {
		cfg.Logger.Warnf("Could not take the rootfs ID of %s: %s", img.File, err)
		return ""
	}
	cfg.Logger.Infof("Rootfs ID of %s is %s, measured in %s", img.File, id, time.Since(start).Round(time.Millisecond))
	return id
}

// recordRootfsID records id for the image at imgFile, once nothing else is
// going to write it. An empty id removes what was recorded before, so an
// image never keeps the ID of a release it no longer holds.
func recordRootfsID(cfg *sdkConfig.Config, imgFile, id string) {
	if id == "" {
		if err := diffupgrade.ForgetID(cfg.Fs, imgFile); err != nil {
			cfg.Logger.Warnf("Could not remove the rootfs ID of %s: %s", imgFile, err)
		}
		return
	}
	if err := diffupgrade.RecordID(cfg.Fs, imgFile, id); err != nil {
		cfg.Logger.Warnf("Could not record the rootfs ID of %s: %s", imgFile, err)
		_ = diffupgrade.ForgetID(cfg.Fs, imgFile)
	}
}
