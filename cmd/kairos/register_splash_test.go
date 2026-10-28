package main

import "testing"

// The splash reaches a boot only because it rides the multi-call binary that
// is already in every initramfs. If the registration is dropped, nothing fails
// to build: `kairos splash` just starts reporting an unknown sub-tool, and the
// dracut module's ExecStart quietly does nothing.
func TestSplashIsRegistered(t *testing.T) {
	if got := subToolFor(t, "splash"); got != "splash" {
		t.Fatalf("splash is not a sub-tool; it resolves to %q", got)
	}
}

// Both splash units exec /usr/bin/kairos-splash with no sub-tool argument, so
// the only thing that selects the splash is argv[0]. kairos-init installs that
// path as a symlink to this binary. Without the alias the symlink resolves,
// execs, and exits 2 with "unknown sub-tool kairos-splash": a boot with no
// animation and nothing on the console to say why.
func TestKairosSplashArgv0Dispatches(t *testing.T) {
	if got := subToolFor(t, "/usr/bin/kairos-splash"); got != "splash" {
		t.Fatalf("kairos-splash dispatches to %q, want \"splash\"", got)
	}
}
