// Package installer holds the contract for locating the kairos installer binary
// within an image. It is shared across kairos projects: kairos-init bundles the
// default installer (and skips bundling when one is already present, via
// Existing), while kairos-agent resolves and execs the installer at install time
// (via Resolve).
package installer

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-sdk/constants"
)

// Existing reports whether an installer is already present under root, returning
// its path. It checks the override slot first and then the default path,
// mirroring the resolution order. It ignores the KAIROS_INSTALLER env var, which
// is a runtime override rather than an image-build signal.
//
// root is prefixed to the candidate paths so callers can point it at a test
// directory; production callers pass "/".
func Existing(root string) (string, bool) {
	for _, p := range []string{constants.InstallerOverridePath, constants.InstallerDefaultPath} {
		full := filepath.Join(root, p)
		if _, err := os.Stat(full); err == nil {
			return full, true
		}
	}
	return "", false
}

// Resolve returns the path to an installer binary, or "" if none is found.
// Resolution order matches the installer contract:
//
//	$KAIROS_INSTALLER (when set and existing) -> override path -> default path.
//
// root is prefixed to the override and default paths so callers can point it at
// a test directory; production callers pass "/". The KAIROS_INSTALLER value is an
// explicit absolute path and is never prefixed with root.
func Resolve(root string) string {
	if p := os.Getenv(constants.InstallerEnvVar); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if path, found := Existing(root); found {
		return path
	}
	return ""
}
