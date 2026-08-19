package version

import "runtime"

var (
	version = "v0.0.1"
	// gitCommit is the git sha1 + dirty if build from a dirty git.
	gitCommit = "none"
)

func GetVersion() string {
	return version
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
