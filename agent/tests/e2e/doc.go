// Package e2e holds the QEMU-based end-to-end tests for kairos-agent. The test
// files are guarded by the "e2e" build tag so they are excluded from normal
// `go test ./...` and `ginkgo -r` runs (which lack QEMU and an ISO). This file
// carries no tag so the directory remains a buildable package by default.
package e2e
