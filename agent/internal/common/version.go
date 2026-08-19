package common

import (
	"runtime"

	monoversion "github.com/kairos-io/kairos/internal/version"
)

var (
	// VERSION is a legacy alias for the monorepo-wide version string.
	// Set once at package init time from internal/version.Version, which is
	// itself injected at build time via ldflags. See internal/version.
	VERSION = monoversion.Version
	// gitCommit is the git sha1 + dirty if built from a dirty git. The
	// monorepo-wide Version already carries this suffix via git describe;
	// this variable is kept for backward-compatible BuildInfo output.
	gitCommit = "none"
)

func GetVersion() string {
	return VERSION
}

// BuildInfo describes the compiled time information.
type BuildInfo struct {
	// Version is the current semver.
	Version string `json:"version,omitempty"`
	// GitCommit is the git sha1.
	GitCommit string `json:"git_commit,omitempty"`
	// GoVersion is the version of the Go compiler used.
	GoVersion string `json:"go_version,omitempty"`
}

// Get returns build info.
func Get() BuildInfo {
	v := BuildInfo{
		Version:   GetVersion(),
		GitCommit: gitCommit,
		GoVersion: runtime.Version(),
	}

	return v
}
