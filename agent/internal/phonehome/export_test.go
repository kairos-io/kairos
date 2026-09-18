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
	glob func(pattern string) ([]string, error),
) func() {
	prevRun, prevRm := runCommand, removeFile
	prevRead, prevWrite, prevGlob := readFile, writeFile, globFiles
	runCommand = run
	removeFile = rm
	readFile = read
	writeFile = write
	globFiles = glob
	return func() {
		runCommand = prevRun
		removeFile = prevRm
		readFile = prevRead
		writeFile = prevWrite
		globFiles = prevGlob
	}
}

// SetHostnameFunc pins what gatherHostname reports and returns a restorer that
// puts os.Hostname back. Specs use it so a heartbeat's hostname is an assertion
// about the agent rather than about the machine running the suite.
func SetHostnameFunc(fn func() (string, error)) func() {
	prev := hostnameFn
	hostnameFn = fn
	return func() { hostnameFn = prev }
}
