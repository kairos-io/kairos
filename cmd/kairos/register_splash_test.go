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

func keys(m map[string]subEntrypoint) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
