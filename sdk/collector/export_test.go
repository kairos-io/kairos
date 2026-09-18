package collector

import "io"

// SetBuildContextForTest replaces the buildContext seam used by
// RenderConfigURL. It returns a restore function that reverts the swap when
// called (intended for DeferCleanup). Only linked into test binaries because
// this file has the _test.go suffix.
func SetBuildContextForTest(f func() (map[string]interface{}, error)) (restore func()) {
	prev := buildContext
	buildContext = f
	return func() { buildContext = prev }
}

// WalkAndEscape exposes the internal helper for tests that need to escape a
// mocked context to compare against rendered output.
var WalkAndEscape = walkAndEscape

// SetWarnOutForTest redirects the collector's warning output so a test can
// assert on what a node would have printed. It returns a restore function that
// reverts the swap when called (intended for DeferCleanup).
func SetWarnOutForTest(w io.Writer) (restore func()) {
	prev := warnOut
	warnOut = w
	return func() { warnOut = prev }
}
