package action

import (
	"path/filepath"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
)

// invalidateImageSum removes the checksum of an image that is about to be
// replaced or modified, so a crash half way never leaves a stale checksum
// behind. A missing checksum only means the image is not verified at boot.
func invalidateImageSum(cfg *sdkConfig.Config, img string) {
	if !imagesum.Applies(img) {
		return
	}
	if err := imagesum.Remove(cfg.Fs, img); err != nil {
		cfg.Logger.Warnf("could not remove stale checksum of %s: %s", img, err)
	}
}

// hashImage computes the sha256 of a finished image, if image-checksum.create is
// on. Failures are not fatal: the checksum is a safety net and must never break
// an install or an upgrade. An empty result means nothing to store.
func hashImage(cfg *sdkConfig.Config, img string) string {
	if !cfg.ImageChecksum.CreateEnabled() || !imagesum.Applies(img) {
		return ""
	}
	start := time.Now()
	sum, err := imagesum.Compute(cfg.Fs, img)
	if err != nil {
		cfg.Logger.Warnf("could not compute sha256 of %s (continuing): %s", img, err)
		return ""
	}
	cfg.Logger.Debugf("sha256 of %s computed in %s", img, time.Since(start).Round(time.Millisecond))
	return sum
}

// storeImageSum writes the checksum sidecar of img. An empty sum is a no-op.
func storeImageSum(cfg *sdkConfig.Config, img, sum string) {
	if sum == "" {
		return
	}
	if err := imagesum.Write(cfg.Fs, img, sum); err != nil {
		cfg.Logger.Warnf("could not write sha256 of %s (continuing): %s", img, err)
		return
	}
	cfg.Logger.Infof("Recorded sha256 of %s: %s", img, sum)
}

// recordImageSum hashes img and stores the result next to it.
func recordImageSum(cfg *sdkConfig.Config, img string) {
	storeImageSum(cfg, img, hashImage(cfg, img))
}

// recordChecksumPolicy stores the boot time checksum policy (image-checksum.verify
// and image-checksum.require) next to the images in each dir. Immucore runs before
// the cloud config is readable, so this file is how the config reaches it. It
// mirrors the config of the last install, upgrade or reset: a config that asks for
// nothing removes the file.
func recordChecksumPolicy(cfg *sdkConfig.Config, imageFiles ...string) {
	policy := cfg.ImageChecksum.Policy()
	seen := map[string]bool{}
	for _, img := range imageFiles {
		dir := filepath.Dir(img)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if err := imagesum.WritePolicy(cfg.Fs, dir, policy); err != nil {
			cfg.Logger.Warnf("could not store the image checksum policy in %s (continuing): %s", dir, err)
		}
	}
}
