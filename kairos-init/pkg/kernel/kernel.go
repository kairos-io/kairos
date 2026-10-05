package kernel

import (
	"fmt"
	"os"
	"sort"
	"strings"

	semver "github.com/hashicorp/go-version"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// GetLatest returns the latest kernel version installed under /lib/modules for
// the given model. It is the standard production entry-point; use
// GetLatestFromPath when you need to inject a custom path (e.g. in tests).
func GetLatest(model string, l logger.KairosLogger) (string, error) {
	return GetLatestFromPath("/lib/modules", model, l)
}

// GetLatestFromPath returns the latest kernel version found under modulesPath
// for the given model name. Only directories are considered: a plain file
// under modulesPath is not a kernel.
//
// General selection rules:
//  1. The highest semver directory is returned.
//  2. If no directory name parses as semver, the greatest name in natural
//     order is used as a fallback.
//  3. If no directories exist at all, an error is returned.
//
// Rule 2 is not a rare path. go-version rejects any name holding an
// underscore, so no Red Hat family x86_64 kernel parses, nor does any release
// spelled el9_5 or el10_0 on any architecture. See kairos-io/kairos#5213.
//
// RPi3/RPi4 models apply an extra preference step before the general rules:
// directories ending in "-raspi" are tried first.  The highest semver raspi
// directory wins; if none parse as semver the greatest raspi directory in
// natural order is used.  Only when no raspi directory is present at all does
// selection fall through to the general rules above.
func GetLatestFromPath(modulesPath, model string, l logger.KairosLogger) (string, error) {
	var kernelVersion string

	entries, err := os.ReadDir(modulesPath)
	if err != nil {
		l.Logger.Error().Msgf("Failed to read the directory %s: %s", modulesPath, err)
		return kernelVersion, err
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}

	// Ubuntu RPi images must boot the raspi kernel: the generic HWE kernel lacks
	// the Pi SD/MMC drivers needed under UEFI (see kairos-io/kairos#4222).
	if model == values.Rpi3.String() || model == values.Rpi4.String() {
		var raspiVersions []*semver.Version
		var raspiFallback []string
		for _, dir := range dirs {
			if !strings.HasSuffix(dir, "-raspi") {
				continue
			}
			raspiFallback = append(raspiFallback, dir)
			v, parseErr := semver.NewVersion(dir)
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
			return greatestNatural(raspiFallback), nil
		}
	}

	var versions []*semver.Version
	var version *semver.Version
	for _, dir := range dirs {
		// Parse the directory name as a semver version
		version, err = semver.NewVersion(dir)
		if err != nil {
			l.Logger.Debug().Err(err).Str("version", dir).Msg("Failed to parse the version as semver, will use the full name instead")
			continue
		}
		versions = append(versions, version)
	}

	// We could have no semver version but custom versions like 5.4.0-101-generic.fc32.x86_64
	// In that case we need to just use the full name
	if len(versions) == 0 {
		if len(dirs) == 0 {
			return kernelVersion, fmt.Errorf("no kernel versions found")
		}
		kernelVersion = greatestNatural(dirs)
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

// greatestNatural returns the greatest of names in natural order. names must
// not be empty.
func greatestNatural(names []string) string {
	greatest := names[0]
	for _, name := range names[1:] {
		if naturalLess(greatest, name) {
			greatest = name
		}
	}
	return greatest
}

// naturalLess reports whether a sorts before b in the order rpm compares
// version strings: runs of digits are compared as numbers rather than as
// text, and a run of digits ranks above a run of letters at the same position.
//
// Plain string order gets kernel names wrong in both directions.
// "5.14.0-503.el9.x86_64" sorts before "5.14.0-70.el9.x86_64" because "5"
// precedes "7", and "5.14.0-503.el9.x86_64" sorts after
// "5.14.0-503.35.1.el9_5.x86_64" because "e" follows "3", even though the
// z-stream kernel is the newer one.
func naturalLess(a, b string) bool {
	// Difference in leading-zero padding of the first digit run that is
	// otherwise equal. Only used if the two names are equal in every other
	// respect, so that the order is total.
	tie := 0
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		aDigit, bDigit := isDigit(a[i]), isDigit(b[j])
		switch {
		case aDigit && bDigit:
			iStart, jStart := i, j
			for i < len(a) && a[i] == '0' {
				i++
			}
			for j < len(b) && b[j] == '0' {
				j++
			}
			iEnd, jEnd := i, j
			for iEnd < len(a) && isDigit(a[iEnd]) {
				iEnd++
			}
			for jEnd < len(b) && isDigit(b[jEnd]) {
				jEnd++
			}
			// With the zeros gone the longer run is the larger number, and
			// runs of equal length compare as text.
			if iEnd-i != jEnd-j {
				return iEnd-i < jEnd-j
			}
			if runA, runB := a[i:iEnd], b[j:jEnd]; runA != runB {
				return runA < runB
			}
			if tie == 0 {
				switch {
				case iEnd-iStart < jEnd-jStart:
					tie = -1
				case iEnd-iStart > jEnd-jStart:
					tie = 1
				}
			}
			i, j = iEnd, jEnd
		case aDigit != bDigit:
			// rpm ranks a numeric segment above an alphabetic one, which is
			// what makes 5.14.0-503.35.1.el9_5 newer than 5.14.0-503.el9.
			return bDigit
		default:
			if a[i] != b[j] {
				return a[i] < b[j]
			}
			i++
			j++
		}
	}
	if rest := (len(a) - i) - (len(b) - j); rest != 0 {
		return rest < 0
	}
	return tie < 0
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
