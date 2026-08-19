// Package version exposes the monorepo's single version string. It is the
// only place a version literal lives; every sub-tool reads from here so that
// one binary reports one version regardless of which sub-tool is invoked.
//
// Set at build time via:
//
//	-ldflags "-X github.com/kairos-io/kairos/internal/version.Version=<value>"
//
// The Makefile at the repo root injects `git describe --tags --always --dirty`
// by default.
package version

// Version is the human-readable version string, e.g. "v1.2.3" on a tagged
// commit or "v1.2.3-5-gabc1234" between tags. Defaults to "dev" if the linker
// does not override it (plain `go build` without ldflags).
var Version = "dev"
