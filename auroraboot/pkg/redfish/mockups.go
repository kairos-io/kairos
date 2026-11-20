package redfish

import (
	"bufio"
	"embed"
	"strings"
)

// This file makes the P3 tier seam real (design §4a/§4b): an in-tree quirk profile
// that ships with a recorded, sanitized DMTF mockup of real hardware is promoted to
// tier B, because the §4a golden test replays that mockup against the deploy flow in
// every-PR CI. builtinHasMockup is the lookup deriveTier consults; before P4 it was
// a stub returning false, so no profile reached B.
//
// Runtime vs test-only — why an embedded manifest:
// The recorded mockup trees live under pkg/redfish/testdata/mockups/<name>/, and Go
// does NOT compile testdata/ into the binary. But the tier a profile reports is a
// RUNTIME fact (it appears in the load-time log line and, later, a UI badge), so
// builtinHasMockup must work in the shipped binary too — not only under `go test`.
//
// We therefore embed a tiny, hand-kept manifest (mockups.manifest: one built-in
// profile name per line) listing the in-tree profiles that have a recorded mockup.
// The manifest — not the multi-hundred-KB mockup trees — is what ships in the
// binary, keeping it small while letting the runtime know which built-ins are
// tier B. A test (TestMockupManifestMatchesTestdata) keeps the manifest honest:
// it must list EXACTLY the built-in profiles that actually have a
// testdata/mockups/<name>/ tree, so the manifest can never drift from the evidence
// or claim a mockup that isn't there.

//go:embed mockups.manifest
var mockupManifest embed.FS

// builtinsWithMockup is the set of in-tree profile names that ship with a recorded
// mockup (read once from the embedded manifest). Membership ⇒ the built-in profile
// is tier B via deriveTier.
var builtinsWithMockup = loadMockupManifest()

// loadMockupManifest parses the embedded manifest into a set of profile names. It
// ignores blank lines and "#"-prefixed comments. A missing/unreadable manifest
// yields an empty set (every built-in stays at its no-mockup tier), failing safe.
func loadMockupManifest() map[string]bool {
	set := map[string]bool{}
	data, err := mockupManifest.ReadFile("mockups.manifest")
	if err != nil {
		return set
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = true
	}
	return set
}

// builtinHasMockup reports whether an in-tree profile carries a recorded mockup and
// is therefore tier B. It is the single seam deriveTier consults; see the file
// comment for why the determination is driven by an embedded manifest rather than
// the (non-embedded) testdata tree.
func builtinHasMockup(name string) bool {
	return builtinsWithMockup[name]
}
