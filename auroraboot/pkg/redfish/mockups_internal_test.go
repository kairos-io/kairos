package redfish

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMockupManifestMatchesTestdata keeps the embedded mockups.manifest honest: it
// must list EXACTLY the built-in profiles that actually have a recorded mockup tree
// under testdata/mockups/<name>/. This prevents the runtime tier-B determination
// (which reads the embedded manifest, not testdata) from drifting from the evidence
// — claiming a mockup that isn't committed, or omitting one that is.
//
// A "mockup for built-in <name>" is a testdata/mockups/<name>/ directory that holds
// at least one firmware/model subtree with a redfish/v1/index.json (the recorded
// ServiceRoot). The directory name must match a built-in profile name; other dirs
// under testdata/mockups (none today) would be operator/example fixtures and are
// not part of the manifest.
func TestMockupManifestMatchesTestdata(t *testing.T) {
	root := filepath.Join("testdata", "mockups")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}

	found := map[string]bool{}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		name := ent.Name()
		// Only directories that name a built-in profile AND contain a recorded
		// ServiceRoot count as evidence for that built-in.
		if _, ok := builtinQuirks[name]; !ok {
			continue
		}
		if hasRecordedServiceRoot(t, filepath.Join(root, name)) {
			found[name] = true
		}
	}

	// Every manifest entry must have evidence on disk.
	for name := range builtinsWithMockup {
		if !found[name] {
			t.Errorf("mockups.manifest lists %q but no testdata/mockups/%s/<fw>/redfish/v1/index.json exists", name, name)
		}
		if _, ok := builtinQuirks[name]; !ok {
			t.Errorf("mockups.manifest lists %q which is not a built-in profile", name)
		}
	}
	// Every built-in with evidence on disk must be in the manifest.
	for name := range found {
		if !builtinsWithMockup[name] {
			t.Errorf("testdata/mockups/%s/ exists but %q is not listed in mockups.manifest (it would not report tier B at runtime)", name, name)
		}
	}
}

// hasRecordedServiceRoot reports whether dir contains at least one firmware/model
// subtree with a redfish/v1/index.json (a recorded ServiceRoot).
func hasRecordedServiceRoot(t *testing.T, dir string) bool {
	t.Helper()
	subs, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, sub := range subs {
		if !sub.IsDir() {
			continue
		}
		root := filepath.Join(dir, sub.Name(), "redfish", "v1", "index.json")
		if _, err := os.Stat(root); err == nil {
			return true
		}
	}
	return false
}

// TestBuiltinTiersWithMockup locks the P4 tier outcome: the in-tree ilo profile (now
// shipping a recorded mockup) reports tier B; supermicro (no mockup) stays C; the
// spec-default generic stays A. This is the user-visible result of the seam flip.
func TestBuiltinTiersWithMockup(t *testing.T) {
	if !builtinHasMockup("ilo") {
		t.Fatal("ilo must have a recorded mockup (manifest + testdata) after P4")
	}
	if builtinHasMockup("supermicro") {
		t.Fatal("supermicro must NOT have a mockup (stays tier C)")
	}

	r := newRegistry()
	cases := []struct {
		name string
		want Tier
	}{
		{"ilo", TierB},
		{"supermicro", TierC},
		{"generic", TierA},
	}
	for _, c := range cases {
		_, tier, ok := r.quirksForName(c.name)
		if !ok {
			t.Fatalf("%q must resolve as a built-in", c.name)
		}
		if tier != c.want {
			t.Errorf("tier for %q = %q, want %q", c.name, tier, c.want)
		}
	}
}
