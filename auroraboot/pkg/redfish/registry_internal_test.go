package redfish

import (
	"os"
	"path/filepath"
	"testing"
)

// writeProfile is a tiny helper: write a profile file into dir and return nothing.
func writeProfile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// TestDeriveTier locks the project-derived tier rules (design §4b). Operator
// profiles are always C; in-tree generic is A; other in-tree without a mockup is
// C; in-tree WITH a mockup is B (the P4 promotion seam).
func TestDeriveTier(t *testing.T) {
	cases := []struct {
		name      string
		src       origin
		hasMockup bool
		want      Tier
	}{
		{"generic", originBuiltin, false, TierA},
		{"ilo", originBuiltin, false, TierC},
		{"supermicro", originBuiltin, false, TierC},
		{"ilo", originBuiltin, true, TierB},       // P4: in-tree + mockup => B
		{"generic", originOperator, false, TierC}, // operator dir is ALWAYS C
		{"anything", originOperator, true, TierC}, // even a (hypothetical) operator mockup stays C
	}
	for _, c := range cases {
		if got := deriveTier(c.name, c.src, c.hasMockup); got != c.want {
			t.Fatalf("deriveTier(%q, %v, %v) = %q, want %q", c.name, c.src, c.hasMockup, got, c.want)
		}
	}
}

// TestLoadProfileDirEmptyAndMissing confirms an empty/missing/blank dir is not an
// error and yields a built-in-only registry.
func TestLoadProfileDirEmptyAndMissing(t *testing.T) {
	r, results, err := LoadProfileDir("")
	if err != nil || results != nil {
		t.Fatalf("blank dir: err=%v results=%v", err, results)
	}
	if _, _, ok := r.quirksForName("ilo"); !ok {
		t.Fatal("built-in ilo must resolve in a blank-dir registry")
	}

	if _, _, err := LoadProfileDir(filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Fatalf("missing dir must not error: %v", err)
	}

	empty := t.TempDir()
	r, results, err = LoadProfileDir(empty)
	if err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("empty dir should yield no results, got %v", results)
	}
	if got, tier, ok := r.quirksForName("generic"); !ok || got.name != "generic" || tier != TierA {
		t.Fatalf("generic in empty-dir registry: name=%q tier=%q ok=%v", got.name, tier, ok)
	}
}

// TestLoadProfileDirLoadsAndSkips loads a directory with one valid and one
// malformed profile: the valid one loads, the bad one is skipped+reported, and the
// rest of the load is unaffected.
func TestLoadProfileDirLoadsAndSkips(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, dir, "good.yaml", "name: good\nmediaType: DVD\n")
	// Bad: Image is a rejected (SSRF-owned) field.
	writeProfile(t, dir, "bad.yaml", "name: bad\ntuneInsertParams:\n  set:\n    Image: http://evil\n")
	// Non-profile extension must be ignored entirely.
	writeProfile(t, dir, "notes.txt", "this is not a profile")

	r, results, err := LoadProfileDir(dir)
	if err != nil {
		t.Fatalf("LoadProfileDir: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (good+bad, .txt ignored), got %d: %v", len(results), results)
	}

	var good, bad *ProfileLoadResult
	for i := range results {
		switch filepath.Base(results[i].Path) {
		case "good.yaml":
			good = &results[i]
		case "bad.yaml":
			bad = &results[i]
		}
	}
	if good == nil || good.Err != nil || good.Name != "good" || good.Tier != TierC {
		t.Fatalf("good.yaml result wrong: %+v", good)
	}
	if bad == nil || bad.Err == nil {
		t.Fatalf("bad.yaml must report an error: %+v", bad)
	}

	// The valid profile resolves; the malformed one did NOT register.
	if q, tier, ok := r.quirksForName("good"); !ok || q.name != "good" || tier != TierC {
		t.Fatalf("good not resolvable: name=%q tier=%q ok=%v", q.name, tier, ok)
	}
	if _, _, ok := r.quirksForName("bad"); ok {
		t.Fatal("malformed profile must not register; a skip must disable only itself")
	}
}

// TestLoadProfileDirOverridesBuiltin is the precedence test: an operator profile
// named the same as a built-in overrides it, the result reports Overrode, and the
// resolved quirks are the operator's (proved by a hook the built-in lacks).
func TestLoadProfileDirOverridesBuiltin(t *testing.T) {
	// Sanity: the built-in supermicro profile has NO mediaType hook.
	if quirksFor(VendorSuperMicro).mediaType != nil {
		t.Fatal("precondition: built-in supermicro must have no mediaType hook")
	}

	dir := t.TempDir()
	// Operator "supermicro" that sets a mediaType — a hook the built-in lacks.
	writeProfile(t, dir, "supermicro.yaml", "name: supermicro\nmediaType: DVD\n")

	r, results, err := LoadProfileDir(dir)
	if err != nil {
		t.Fatalf("LoadProfileDir: %v", err)
	}
	if len(results) != 1 || !results[0].Overrode {
		t.Fatalf("expected the operator supermicro to report Overrode, got %+v", results)
	}
	if results[0].Tier != TierC {
		t.Fatalf("operator override must be tier C, got %q", results[0].Tier)
	}

	q, tier, ok := r.quirksForName("supermicro")
	if !ok || tier != TierC {
		t.Fatalf("override resolution: ok=%v tier=%q", ok, tier)
	}
	if q.mediaType == nil {
		t.Fatal("override must return the OPERATOR profile (with its mediaType hook), not the built-in")
	}
}

// TestQuirksForNameFallback confirms the selection invariants: a known built-in
// name resolves with its tier; an unknown name falls back to generic (never a
// silent wrong workaround); a VendorType string still resolves through quirksFor.
func TestQuirksForNameFallback(t *testing.T) {
	r := newRegistry()

	// ilo now ships a recorded mockup (P4), so the built-in resolves at tier B.
	q, tier, ok := r.quirksForName("ilo")
	if !ok || q.name != "ilo" || tier != TierB {
		t.Fatalf("ilo: name=%q tier=%q ok=%v", q.name, tier, ok)
	}

	// supermicro has no mockup, so it stays tier C.
	if _, tier, ok := r.quirksForName("supermicro"); !ok || tier != TierC {
		t.Fatalf("supermicro must stay tier C: tier=%q ok=%v", tier, ok)
	}

	// Unknown name MUST resolve to generic, reported as not a name match.
	q, tier, ok = r.quirksForName("totally-unknown-vendor")
	if ok {
		t.Fatal("unknown name must report ok=false (fell through to vendor/generic)")
	}
	if q.name != "generic" || tier != TierA {
		t.Fatalf("unknown name must resolve to generic [A]: name=%q tier=%q", q.name, tier)
	}

	// "dmtf" is a VendorType that maps to generic — name miss, generic result.
	q, _, ok = r.quirksForName("dmtf")
	if ok || q.name != "generic" {
		t.Fatalf("dmtf must fall through to generic: name=%q ok=%v", q.name, ok)
	}
}

// TestSetDefaultRegistryRoutesDeployer confirms the process-wide registry is what
// NewDeployer resolves against: after installing an operator override, a Deployer
// built for that vendor gets the operator quirks. It also confirms nil is ignored.
func TestSetDefaultRegistryRoutesDeployer(t *testing.T) {
	// Restore the default registry after the test so other tests see built-ins.
	defaultRegistryMu.RLock()
	saved := defaultRegistry
	defaultRegistryMu.RUnlock()
	t.Cleanup(func() { SetDefaultRegistry(saved) })

	SetDefaultRegistry(nil) // ignored: must not nil out the registry
	if resolveQuirks("generic").name != "generic" {
		t.Fatal("SetDefaultRegistry(nil) must be a no-op")
	}

	dir := t.TempDir()
	writeProfile(t, dir, "ilo.yaml", "name: ilo\nmediaType: DVD\n")
	r, _, err := LoadProfileDir(dir)
	if err != nil {
		t.Fatalf("LoadProfileDir: %v", err)
	}
	SetDefaultRegistry(r)

	d := NewDeployer(Config{Endpoint: "https://bmc.example", Vendor: VendorHPE})
	if d.quirks.mediaType == nil {
		t.Fatal("NewDeployer(VendorHPE) must pick up the operator ilo override via the default registry")
	}

	// An unknown vendor still resolves to generic (zero-value) through the registry.
	d = NewDeployer(Config{Endpoint: "https://bmc.example", Vendor: "nope"})
	if d.quirks.name != "generic" || d.quirks.mediaType != nil {
		t.Fatalf("unknown vendor must resolve to generic zero-value, got name=%q", d.quirks.name)
	}
}

// TestExampleProfilesParse walks examples/redfish/quirks and asserts every shipped
// example profile parses cleanly (they double as load tests and copy-paste seeds).
func TestExampleProfilesParse(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "redfish", "quirks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading examples dir: %v", err)
	}
	var seen int
	for _, ent := range entries {
		if ent.IsDir() || !isProfileFile(ent.Name()) {
			continue
		}
		seen++
		data, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", ent.Name(), err)
		}
		if _, err := ParseProfile(data); err != nil {
			t.Fatalf("example profile %s must parse cleanly: %v", ent.Name(), err)
		}
	}
	if seen == 0 {
		t.Fatal("expected at least one example profile in examples/redfish/quirks")
	}
}

// TestRegistryProfiles locks the listing seam the fleet server exposes for the UI
// vendor selector: every registered profile is returned, sorted by name, with its
// project-derived tier, origin, and (for operator profiles) the declared firmware
// label. Built-ins carry no validatedFirmware.
func TestRegistryProfiles(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, dir, "acme.yaml",
		"name: acme\nmatch: { vendor: Acme }\nmediaSearch: { order: [manager, system] }\nvalidatedFirmware: \"FW 1.23\"\n")

	r, _, err := LoadProfileDir(dir)
	if err != nil {
		t.Fatalf("LoadProfileDir: %v", err)
	}

	infos := r.Profiles()

	// Sorted by name.
	for i := 1; i < len(infos); i++ {
		if infos[i-1].Name > infos[i].Name {
			t.Fatalf("Profiles() not sorted by name: %q before %q", infos[i-1].Name, infos[i].Name)
		}
	}

	byName := make(map[string]ProfileInfo, len(infos))
	for _, p := range infos {
		byName[p.Name] = p
	}

	// generic is the core-tested built-in (tier A, no firmware label).
	if g, ok := byName["generic"]; !ok || g.Tier != TierA || g.Origin != "builtin" || g.ValidatedFirmware != "" {
		t.Fatalf("generic: %+v (ok=%v)", byName["generic"], ok)
	}

	// The operator profile is tier C, origin operator, and surfaces its firmware.
	c, ok := byName["acme"]
	if !ok {
		t.Fatal("acme operator profile missing from Profiles()")
	}
	if c.Tier != TierC || c.Origin != "operator" {
		t.Fatalf("acme tier/origin: %+v", c)
	}
	if c.ValidatedFirmware != "FW 1.23" {
		t.Fatalf("acme validatedFirmware not surfaced: %q", c.ValidatedFirmware)
	}
	if c.TierDescription == "" || c.TierDescription != TierC.describe() {
		t.Fatalf("acme tierDescription: %q", c.TierDescription)
	}
}
