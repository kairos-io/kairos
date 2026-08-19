package system

import (
	"os"
	"runtime"
	"strings"

	"github.com/joho/godotenv"
	"github.com/kairos-io/kairos-init/pkg/values"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/sanity-io/litter"
)

// DetectSystem detects the system based on the os-release file
// and returns a values.System struct
// This could probably be implemented in a different way, or use a lib but its helpful
// in conjunction with the values packagemaps to determine the packages to install
func DetectSystem(l logger.KairosLogger) values.System {
	// Detects the system
	s := values.System{
		Distro: values.Unknown,
		Family: values.UnknownFamily,
	}

	file, err := os.Open("/etc/os-release")
	if err != nil {
		return s
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)
	val, err := godotenv.Parse(file)
	if err != nil {
		return s
	}
	l.Logger.Trace().Interface("values", val).Msg("Read values from os-release")

	// Resolve distro/family according to os-release precedence:
	//  1. ID (exact distro identifier)
	//  2. ID_LIKE fallback for derivatives
	s.Distro, s.Family = detectFromReleaseIDs(val["ID"], val["ID_LIKE"])

	// Match architecture
	switch values.Architecture(runtime.GOARCH) {
	case values.ArchAMD64:
		s.Arch = values.ArchAMD64
	case values.ArchARM64:
		s.Arch = values.ArchARM64
	case values.ArchRiscV64:
		s.Arch = values.ArchRiscV64
	}

	// Store the version
	s.Version = val["VERSION_ID"]
	if s.Distro == values.Alpine {
		// We currently only do major.minor for alpine, even if os-release reports also the patch
		// So for backwards compatibility we will only store the major.minor
		splittedVersion := strings.Split(s.Version, ".")
		if len(splittedVersion) == 3 {
			s.Version = splittedVersion[0] + "." + splittedVersion[1]
		} else {
			l.Debugf("Could not split version for alpine, using default as is: %s", s.Version)
		}
	}

	// Store the name
	s.Name = val["PRETTY_NAME"]
	// Fallback to normal name
	if s.Name == "" {
		s.Name = val["NAME"]
	}

	l.Debugf("Detected system: %s", litter.Sdump(s))
	return s
}

// detectFromReleaseIDs resolves distro/family using ID first and ID_LIKE as fallback.
// ID is authoritative; ID_LIKE is only consulted when ID is not recognized.
func detectFromReleaseIDs(id, idLike string) (values.Distro, values.Family) {
	distro, family := detectFromID(id)
	if distro != values.Unknown {
		return distro, family
	}

	return detectFromIDLike(idLike)
}

// detectFromID maps a known os-release ID value to our distro/family types.
func detectFromID(id string) (values.Distro, values.Family) {
	switch values.Distro(id) {
	case values.Debian:
		return values.Debian, values.DebianFamily
	case values.Ubuntu:
		return values.Ubuntu, values.DebianFamily
	case values.Fedora:
		return values.Fedora, values.RedHatFamily
	case values.RockyLinux:
		return values.RockyLinux, values.RedHatFamily
	case values.AlmaLinux:
		return values.AlmaLinux, values.RedHatFamily
	case values.OracleLinux:
		return values.OracleLinux, values.RedHatFamily
	case values.RedHat:
		return values.RedHat, values.RedHatFamily
	case values.Arch:
		return values.Arch, values.ArchFamily
	case values.Alpine:
		return values.Alpine, values.AlpineFamily
	case values.OpenSUSELeap:
		return values.OpenSUSELeap, values.SUSEFamily
	case values.OpenSUSETumbleweed:
		return values.OpenSUSETumbleweed, values.SUSEFamily
	case values.SLES:
		return values.SLES, values.SUSEFamily
	case values.Hadron:
		return values.Hadron, values.HadronFamily
	case values.SLEMicroRancher:
		return values.SLEMicroRancher, values.SUSEFamily
	default:
		return values.Unknown, values.UnknownFamily
	}
}

// detectFromIDLike resolves distro/family from ordered ID_LIKE tokens.
// Per the os-release spec, ID_LIKE is a space-separated list of related distro
// IDs, ordered from closest to least related. We iterate left-to-right and:
//  1. Try each token as a distro ID via detectFromID.
//  2. Fall back to family-only markers (e.g. "suse", "redhat") that are not
//     valid distro IDs but indicate family membership.
func detectFromIDLike(idLike string) (values.Distro, values.Family) {
	for _, token := range strings.Fields(idLike) {
		distro, family := detectFromID(token)
		if distro != values.Unknown {
			return distro, family
		}

		switch values.Family(token) {
		case values.RedHatFamily:
			return values.Fedora, values.RedHatFamily
		case values.SUSEFamily:
			return values.OpenSUSELeap, values.SUSEFamily
		}
	}

	return values.Unknown, values.UnknownFamily
}
