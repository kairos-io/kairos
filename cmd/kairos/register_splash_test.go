package main

import "testing"

// The splash reaches a boot only because it rides the multi-call binary that
// is already in every initramfs. If the registration is dropped, nothing fails
// to build: `kairos splash` just starts reporting an unknown sub-tool, and the
// dracut module's ExecStart quietly does nothing.
func TestSplashIsRegistered(t *testing.T) {
	if _, ok := subs["splash"]; !ok {
		t.Fatalf("splash is not a sub-tool; registered: %v", keys(subs))
	}
}

// Both splash units exec /usr/bin/kairos-splash with no sub-tool argument, so
// the only thing that selects the splash is argv[0]. kairos-init installs that
// path as a symlink to this binary. Without the alias the symlink resolves,
// execs, and exits 2 with "unknown sub-tool kairos-splash": a boot with no
// animation and nothing on the console to say why.
func TestKairosSplashArgv0Dispatches(t *testing.T) {
	canonical, ok := aliases["kairos-splash"]
	if !ok {
		t.Fatalf("kairos-splash is not an argv[0] alias; aliases: %v", aliases)
	}
	if canonical != "splash" {
		t.Fatalf("kairos-splash dispatches to %q, want \"splash\"", canonical)
	}
	if _, ok := subs[canonical]; !ok {
		t.Fatalf("alias kairos-splash points at %q, which is not a sub-tool", canonical)
	}
}

func keys(m map[string]subEntrypoint) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
