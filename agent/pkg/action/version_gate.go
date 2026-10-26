package action

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/Masterminds/semver/v3"
	"github.com/joho/godotenv"
	sdkFS "github.com/kairos-io/kairos/v4/sdk/types/fs"
)

// RefuseIfTargetIsDowngrade compares two KAIROS_INIT_VERSION strings and
// returns an error when the target is older than the current. A newer host
// has the format-writing code the target owns; going backwards hands the
// wheel to an older kairos-agent that predates the finalize contract this
// PR introduced. That was the exact regression class the change was meant
// to close (see kairos-io/kairos#4456 and kairos-io/kairos#4917), so a
// downgrade attempt is a hard failure rather than a silent fallback.
//
// The gate is on KAIROS_INIT_VERSION rather than KAIROS_VERSION because
// KAIROS_VERSION is whatever the image author passed as `--version` to
// kairos-init (a free-form string, commonly a product tag on derivative
// images), while KAIROS_INIT_VERSION is stamped by the kairos-init
// binary itself and is the field that actually tracks build tooling.
// Downgrading a base image or a product version on top of it stays
// allowed, per kairos-io/kairos#4953.
//
// Refusal is also the correct answer when either version cannot be
// parsed: an image that carries no readable KAIROS_INIT_VERSION is not
// one we can prove is safe to upgrade to. That matches the acceptance
// criteria of #4917 ("no permissive default"). Callers get the version
// strings via ReadKairosInitVersionFromFs (running system, or a mounted
// target rootfs) or ReadKairosInitVersionFromDisk (files extracted from
// a UKI .initrd into a host temp dir).
//
// KAIROS_INIT_VERSION comes from `git describe --tags --dirty`, so a build
// made between tags reads like v4.4.0-2-g8c24a223. Semver would read the
// "-2-g8c24a223" as a pre-release and rank it below v4.4.0, while it is two
// commits past that tag. See parseKairosInitVersionForCompare.
func RefuseIfTargetIsDowngrade(currentStr, targetStr string) error {
	current, currentAhead, err := parseKairosInitVersionForCompare(currentStr)
	if err != nil {
		return fmt.Errorf("parsing current KAIROS_INIT_VERSION %q: %w", currentStr, err)
	}
	target, targetAhead, err := parseKairosInitVersionForCompare(targetStr)
	if err != nil {
		return fmt.Errorf("parsing target KAIROS_INIT_VERSION %q: %w", targetStr, err)
	}
	if target.LessThan(current) || (target.Equal(current) && targetAhead < currentAhead) {
		return fmt.Errorf(
			"refusing to upgrade: target KAIROS_INIT_VERSION %q is older than current %q "+
				"(this tracks build tooling, not the image's KAIROS_VERSION or base image, "+
				"so downgrading either of those is fine); "+
				"downgrading to a kairos-agent that predates the target-side upgrade-finalize step "+
				"is not supported (see kairos-io/kairos#4917), rebuild the target image with a "+
				"newer kairos-init or pick a newer source",
			targetStr, currentStr)
	}
	return nil
}

// gitDescribeSuffix matches the "-<commits>-g<sha>" that `git describe`
// appends after the tag, and the optional "-dirty" after that. It also
// matches the "+<commits>.g<sha>" form the root Makefile stamps since
// (BINARY_VERSION), so builds past the same tag stay ordered.
var gitDescribeSuffix = regexp.MustCompile(`^(.+)(?:-([0-9]+)-|\+([0-9]+)\.)g[0-9a-f]+(-dirty)?$`)

// parseKairosInitVersionForCompare splits a `git describe` version into
// the tag it was built from and how many commits past that tag it is, so
// v4.4.0-2-g8c24a223 compares as newer than v4.4.0 and older than v4.4.1.
// A plain tag is zero commits past itself.
func parseKairosInitVersionForCompare(s string) (*semver.Version, int, error) {
	tag, ahead := s, 0
	if m := gitDescribeSuffix.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[2] + m[3])
		if err != nil {
			return nil, 0, err
		}
		tag, ahead = m[1], n
	}
	v, err := semver.NewVersion(tag)
	if err != nil {
		return nil, 0, err
	}
	return v, ahead, nil
}

// ReadKairosInitVersionFromFs loads the given os-release-style file via
// the passed fs (so vfs-backed tests work), then returns the
// KAIROS_INIT_VERSION value. Use for the running system's
// /etc/kairos-release (goes through cfg.Fs) and for files inside a
// mounted target rootfs (also through cfg.Fs, since the mount lives in
// the same fs abstraction).
func ReadKairosInitVersionFromFs(fs sdkFS.KairosFS, path string) (string, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return "", err
	}
	return parseKairosInitVersion(path, data)
}

// ReadKairosInitVersionFromDisk loads the given os-release-style file
// with os.ReadFile directly. Use for files that were written to a host
// temp dir by extractFromInitrd: those live on real disk, not on cfg.Fs,
// so a vfs-backed cfg.Fs would misroute the read through its own backing
// dir.
func ReadKairosInitVersionFromDisk(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return parseKairosInitVersion(path, data)
}

func parseKairosInitVersion(path string, data []byte) (string, error) {
	env, err := godotenv.Parse(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}
	v, ok := env["KAIROS_INIT_VERSION"]
	if !ok || v == "" {
		return "", fmt.Errorf("KAIROS_INIT_VERSION missing from %s", path)
	}
	return v, nil
}
