// Self-contained Go module for the ghcr-cleanup tool -- the binary
// the ghcr-prune scheduled workflow invokes to sweep aged GHCR
// container versions. Directory kept under .github/actions/ for
// historical reasons (it used to expose a composite action); the
// action.yml wrapper is gone and the module is now just a Go program.
// Runtime code is stdlib only; test-only deps (ginkgo/gomega) match
// the root module's versions -- keep them in sync when bumping.
module github.com/kairos-io/kairos/.github/actions/ghcr-cleanup

go 1.25.0

require (
	github.com/onsi/ginkgo/v2 v2.32.2
	github.com/onsi/gomega v1.43.0
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.36.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/tools v0.45.0 // indirect
)
