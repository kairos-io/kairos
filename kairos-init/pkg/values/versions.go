package values

import (
	"runtime"

	kairosversion "github.com/kairos-io/kairos/v4/internal/version"
)

// GetVersion returns the single version string the root Makefile stamps into
// every binary in this tree. pkg/stages/steps_init.go writes it into each image
// it builds as KAIROS_INIT_VERSION, so it must not diverge from what the
// binaries in that image report.
func GetVersion() string {
	return kairosversion.Version
}

// BuildInfo describes the compiled time information.
type BuildInfo struct {
	// Version is `git describe --tags --always --dirty`, so it already carries
	// the short sha between tags. There is no separate commit field.
	Version string `json:"version,omitempty"`
	// GoVersion is the version of the Go compiler used.
	GoVersion string `json:"go_version,omitempty"`
}

// GetFullVersion returns the full build info.
func GetFullVersion() BuildInfo {
	return BuildInfo{
		Version:   GetVersion(),
		GoVersion: runtime.Version(),
	}
}
