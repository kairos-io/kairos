package phonehome

import "os"

// Test-only exports. Files ending in `_test.go` are compiled into the test
// binary only, so the helpers below are invisible to production code.

// SetUninstallRunners swaps the teardown's package-local helpers and
// returns a restorer that puts the production defaults back. Use it in a
// defer (or BeforeEach/AfterEach) so specs don't leak fakes into each other.
func SetUninstallRunners(
	run func(name string, args ...string) ([]byte, error),
	rm func(path string) error,
	read func(path string) ([]byte, error),
	write func(path string, data []byte, perm os.FileMode) error,
	walk func(root string) ([]string, error),
) func() {
	prevRun, prevRm := runCommand, removeFile
	prevRead, prevWrite, prevWalk := readFile, writeFile, walkFiles
	runCommand = run
	removeFile = rm
	readFile = read
	writeFile = write
	walkFiles = walk
	return func() {
		runCommand = prevRun
		removeFile = prevRm
		readFile = prevRead
		writeFile = prevWrite
		walkFiles = prevWalk
	}
}

// ListConfigFiles exposes the on-disk walk the teardown uses by default.
var ListConfigFiles = listConfigFiles

// SetHostnameFunc pins what gatherHostname reports and returns a restorer that
// puts os.Hostname back. Specs use it so a heartbeat's hostname is an assertion
// about the agent rather than about the machine running the suite.
func SetHostnameFunc(fn func() (string, error)) func() {
	prev := hostnameFn
	hostnameFn = fn
	return func() { hostnameFn = prev }
}

// setMetricsSources points gatherMetrics at fake /proc and /sys trees and a fake
// statfs, and returns a restorer that puts the host defaults back.
func setMetricsSources(proc, sys string, statfs func(path string) (uint64, uint64, error)) func() {
	prevProc, prevSys, prevStatfs := procRoot, sysRoot, statfsFn
	procRoot, sysRoot, statfsFn = proc, sys, statfs
	return func() { procRoot, sysRoot, statfsFn = prevProc, prevSys, prevStatfs }
}
