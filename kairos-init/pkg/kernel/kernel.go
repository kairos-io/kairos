package kernel

import (
	"fmt"
	"os"
	"sort"
	"strings"

	semver "github.com/hashicorp/go-version"
	"github.com/kairos-io/kairos-init/pkg/values"
	"github.com/kairos-io/kairos-sdk/types/logger"
)

// GetLatest returns the latest kernel version installed under /lib/modules for
// the given model. It is the standard production entry-point; use
// GetLatestFromPath when you need to inject a custom path (e.g. in tests).
func GetLatest(model string, l logger.KairosLogger) (string, error) {
	return GetLatestFromPath("/lib/modules", model, l)
}

// GetLatestFromPath returns the latest kernel version found under modulesPath
// for the given model name.
//
// General selection rules:
//  1. The highest semver directory is returned.
//  2. If no directory name parses as semver, the first directory entry is used
//     as a fallback.
//  3. If no directories exist at all, an error is returned.
//
// RPi3/RPi4 models apply an extra preference step before the general rules:
// directories ending in "-raspi" are tried first.  The highest semver raspi
// directory wins; if none parse as semver the lexicographically last raspi
// directory is used.  Only when no raspi directory is present at all does
// selection fall through to the general rules above.
func GetLatestFromPath(modulesPath, model string, l logger.KairosLogger) (string, error) {
	var kernelVersion string

	dirs, err := os.ReadDir(modulesPath)
	if err != nil {
		l.Logger.Error().Msgf("Failed to read the directory %s: %s", modulesPath, err)
		return kernelVersion, err
	}

	// Ubuntu RPi images must boot the raspi kernel: the generic HWE kernel lacks
	// the Pi SD/MMC drivers needed under UEFI (see kairos-io/kairos#4222).
	if model == values.Rpi3.String() || model == values.Rpi4.String() {
		var raspiVersions []*semver.Version
		var raspiFallback []string
		for _, dir := range dirs {
			if !dir.IsDir() || !strings.HasSuffix(dir.Name(), "-raspi") {
				continue
			}
			raspiFallback = append(raspiFallback, dir.Name())
			v, parseErr := semver.NewVersion(dir.Name())
			if parseErr != nil {
				continue
			}
			raspiVersions = append(raspiVersions, v)
		}
		if len(raspiVersions) > 0 {
			sort.Sort(semver.Collection(raspiVersions))
			return raspiVersions[len(raspiVersions)-1].String(), nil
		}
		if len(raspiFallback) > 0 {
			sort.Strings(raspiFallback)
			return raspiFallback[len(raspiFallback)-1], nil
		}
	}

	var versions []*semver.Version
	var version *semver.Version
	for _, dir := range dirs {
		if dir.IsDir() {
			// Parse the directory name as a semver version
			version, err = semver.NewVersion(dir.Name())
			if err != nil {
				l.Logger.Debug().Err(err).Str("version", dir.Name()).Msg("Failed to parse the version as semver, will use the full name instead")
				continue
			}
			versions = append(versions, version)
		}
	}

	// We could have no semver version but custom versions like 5.4.0-101-generic.fc32.x86_64
	// In that case we need to just use the full name
	if len(versions) == 0 {
		if len(dirs) >= 1 {
			kernelVersion = dirs[0].Name()
		} else {
			return kernelVersion, fmt.Errorf("no kernel versions found")
		}
	} else {
		sort.Sort(semver.Collection(versions))
		kernelVersion = versions[len(versions)-1].String()
		if kernelVersion == "" {
			l.Logger.Error().Msgf("Failed to find the latest kernel version")
			return kernelVersion, fmt.Errorf("failed to find the latest kernel")
		}
	}

	return kernelVersion, nil
}
