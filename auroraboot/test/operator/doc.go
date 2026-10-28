// Package operator hosts the kind-based e2e suite that exercises the
// operator-backed ArtifactBuilder against a real kairos-operator install.
// Every test file is gated by the operator_e2e build tag so it only runs
// via `make test-operator-e2e`. This untagged doc file exists so ginkgo
// -r and go test ./... can walk the package without hitting "build
// constraints exclude all Go files".
package operator
