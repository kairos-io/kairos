package values

import (
	"runtime"

	kairosversion "github.com/kairos-io/kairos/v4/internal/version"
)

// GetVersion returns the version kairos-init reports. It is the monorepo's
// single version string, the same one every other binary in this tree reports.
//
// It is not only printed: pkg/stages/steps_init.go writes it into each image it
// builds as KAIROS_INIT_VERSION, so a value that disagrees with the binaries in
// that image is a lie recorded in the image's kairos-release file.
//
// Before the monorepo this was a hardcoded literal that kairos-init's own
// goreleaser overrode at release time with -X against
// github.com/kairos-io/kairos-init/pkg/values.version. That module path stopped
// existing when the repo was absorbed, so nothing overrode it any more and it
// reported the v0.0.1 placeholder on every build. The injection point is now
// internal/version, which the root Makefile sets for every binary at once.
func GetVersion() string {
	return kairosversion.Version
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

// GetFullVersion returns the full build info.
func GetFullVersion() BuildInfo {
	return BuildInfo{
		Version: GetVersion(),
		// The monorepo version string is `git describe --tags --always --dirty`,
		// which already carries the short sha between tags and a -dirty suffix,
		// so there is no separate commit to report.
		GitCommit: kairosversion.Version,
		GoVersion: runtime.Version(),
	}
}
