package action

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
)

// VerifyImages checks the OS images found in dirs (active, passive and recovery
// .img files) against their sha256 files and prints a result per image. It only
// reads: nothing is mounted, written or removed.
//
// Per image: a match is fine; a mismatch, a checksum file that cannot be read or
// an image that cannot be read is a failure; a missing or stale checksum is a
// failure only when image-checksum.require is on, otherwise it is reported and
// skipped, as immucore does at boot. The error says how many images failed, so
// the command exits non-zero. The action (image-checksum.verify) is not
// consulted: running this command is the request to check.
func VerifyImages(cfg *sdkConfig.Config, w io.Writer, dirs []string) error {
	require := cfg.ImageChecksum.RequireEnabled()
	var checked, failed int

	for _, dir := range dirs {
		for _, name := range []string{constants.ActiveImgFile, constants.PassiveImgFile, constants.RecoveryImgFile} {
			img := filepath.Join(dir, name)
			if exists, _ := cfg.Fs.Stat(img); exists == nil {
				continue
			}
			checked++
			if !printImageResult(cfg, w, img, require) {
				failed++
			}
		}
	}

	if checked == 0 {
		return fmt.Errorf("no OS images found in %v", dirs)
	}
	fmt.Fprintf(w, "\n%d image(s) checked, %d failed\n", checked, failed)
	if failed > 0 {
		return fmt.Errorf("%d of %d OS images failed the checksum check", failed, checked)
	}
	return nil
}

// printImageResult prints the outcome for one image and reports whether it is ok.
func printImageResult(cfg *sdkConfig.Config, w io.Writer, img string, require bool) bool {
	report, err := imagesum.Check(cfg.Fs, img)
	fmt.Fprintf(w, "%s\n", img)

	switch {
	case err != nil:
		fmt.Fprintf(w, "  result:   FAILED, the check could not be done: %s\n", err)
		if report.Expected != "" {
			fmt.Fprintf(w, "  expected: %s\n", report.Expected)
		}
		return false
	case report.Result == imagesum.OK:
		fmt.Fprintf(w, "  result:   OK\n  expected: %s\n  actual:   %s\n", report.Expected, report.Actual)
		return true
	case report.Result == imagesum.Mismatch:
		fmt.Fprintf(w, "  result:   MISMATCH, the image is not what was written\n  expected: %s\n  actual:   %s\n", report.Expected, report.Actual)
		return false
	case report.Result == imagesum.Stale:
		fmt.Fprintf(w, "  result:   %s\n  expected: %s\n  actual:   (not computed)\n",
			skippedOrFailed("STALE, the image changed since its checksum was written (replaced by an agent that does not update checksums)", require), report.Expected)
		return !require
	default: // Missing
		fmt.Fprintf(w, "  result:   %s\n  expected: (no %s)\n  actual:   (not computed)\n",
			skippedOrFailed("NO CHECKSUM", require), filepath.Base(imagesum.SidecarPath(img)))
		return !require
	}
}

func skippedOrFailed(what string, require bool) string {
	if require {
		return what + ", FAILED because image-checksum.require is on"
	}
	return what + ", skipped (image-checksum.require is off)"
}
