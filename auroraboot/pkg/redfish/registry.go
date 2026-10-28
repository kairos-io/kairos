package redfish

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// This file is P3's profile registry: it loads operator-supplied YAML quirk
// profiles from a directory and folds them into the built-in (in-tree) profiles,
// with an explicit, project-derived support tier and a name-first selection seam
// the CLI and fleet server share.
//
// The boundary guarantees from P1/P2 are unchanged: a profile is still inert data
// compiled through the safe Views (it can never set the Image URL, force an
// unsupported ResetType, or touch sessions/TLS/system selection). P3 only adds
// WHERE a profile comes from and HOW it is selected and labelled.

// Tier is the project-derived support level of a quirk profile. A profile never
// asserts its own tier; the registry derives it from where the profile came from
// and whether it carries recorded hardware evidence (a sanitized mockup, added in
// P4). See design §4b.
type Tier string

const (
	// TierA — core-tested. The spec-default generic path and whatever the
	// fake-BMC/sushy CI exercises. Project guarantee; breakage blocks merge.
	TierA Tier = "A"
	// TierB — community-validated. An in-tree profile WITH a recorded sanitized
	// mockup replayed by the golden test. P4 adds the mockups; until then no
	// profile reaches B (deriveTier has the rule but no mockup evidence yet).
	TierB Tier = "B"
	// TierC — unverified. A bare profile with no recorded mockup, whether in-tree
	// or operator-supplied. Loaded fine, but logged as UNVERIFIED.
	TierC Tier = "C"
)

// describe returns the human-readable tier annotation used in load/override log
// lines, e.g. "tier C: UNVERIFIED — no recorded mockup".
func (t Tier) describe() string {
	switch t {
	case TierA:
		return "tier A: core-tested"
	case TierB:
		return "tier B: community-validated"
	default:
		return "tier C: UNVERIFIED — no recorded mockup"
	}
}

// origin records where a registered profile came from, which drives tier
// derivation and the override-precedence log line.
type origin int

const (
	originBuiltin  origin = iota // compiled in-tree (quirks_vendor.go)
	originOperator               // loaded from --quirks-dir / --redfish-quirks-dir
)

// deriveTier is the single place the project assigns a support tier. Keeping it in
// one small function is deliberate: the in-tree+mockup case (the C->B promotion) is
// driven entirely by the `hasMockup` argument, which builtinHasMockup supplies.
//
// Rules (design §4b):
//   - operator-supplied profile        => always C (we have no evidence for it).
//   - in-tree profile WITH a mockup     => B  (P4: a recorded, sanitized mockup is
//     replayed by the §4a golden test in every-PR CI — builtinHasMockup reports it).
//   - in-tree profile WITHOUT a mockup  => C, EXCEPT the spec-default "generic"
//     profile, which is the core-tested path and is therefore A.
func deriveTier(name string, src origin, hasMockup bool) Tier {
	if src == originOperator {
		return TierC
	}
	// In-tree from here on.
	if hasMockup {
		// A recorded, sanitized mockup exists for this built-in and is replayed by
		// the golden test, so it is community-validated against that firmware.
		return TierB
	}
	if name == "generic" {
		return TierA
	}
	return TierC
}

// registryEntry is one resolvable profile: its compiled quirks, declared name,
// derived tier, origin, and the informational firmware label a profile may carry.
type registryEntry struct {
	quirks            quirks
	name              string
	tier              Tier
	origin            origin
	validatedFirmware string
}

// ProfileInfo is a read-only description of one registered quirk profile, for
// listing (e.g. the fleet server's quirk-profiles endpoint that populates the UI
// vendor selector with tier badges). It carries no quirks/closures — purely the
// labelling an operator needs to pick a profile.
type ProfileInfo struct {
	// Name is the profile's selection name (what a BMCTarget.Vendor matches).
	Name string
	// Tier is the project-derived support tier (A/B/C); a profile never asserts it.
	Tier Tier
	// TierDescription is the human-readable annotation, e.g.
	// "tier C: UNVERIFIED — no recorded mockup".
	TierDescription string
	// Origin is "builtin" for in-tree profiles or "operator" for ones loaded from
	// the quirks directory.
	Origin string
	// ValidatedFirmware is the informational firmware label the profile declared,
	// when any (empty for built-ins without recorded evidence).
	ValidatedFirmware string
}

// Profiles returns every registered profile as a read-only ProfileInfo, sorted by
// name. It is the listing seam the fleet server exposes so the UI can populate the
// vendor selector from the profiles actually loaded (built-in + operator) instead
// of a hardcoded list, and surface each profile's support tier.
func (r *Registry) Profiles() []ProfileInfo {
	infos := make([]ProfileInfo, 0, len(r.byName))
	for _, e := range r.byName {
		o := "builtin"
		if e.origin == originOperator {
			o = "operator"
		}
		infos = append(infos, ProfileInfo{
			Name:              e.name,
			Tier:              e.tier,
			TierDescription:   e.tier.describe(),
			Origin:            o,
			ValidatedFirmware: e.validatedFirmware,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// ProfileLoadResult records the outcome of loading one operator profile file. A
// malformed profile disables only itself: Err is set, Name/Tier are best-effort,
// and the load of the rest of the directory continues. Callers (the CLI/server)
// log these; the registry has already logged a line per profile too.
type ProfileLoadResult struct {
	// Path is the file the result is for.
	Path string
	// Name is the profile's declared name (empty when parsing failed before the
	// name was read).
	Name string
	// Tier is the derived support tier (always TierC for operator profiles).
	Tier Tier
	// Overrode is true when this operator profile replaced a built-in of the same
	// name in the registry.
	Overrode bool
	// Err is non-nil when the file could not be parsed/compiled; that single
	// profile is skipped and every other profile in the directory still loads.
	Err error
}

// Registry holds the resolvable quirk profiles: the built-in (in-tree) profiles
// plus any operator-supplied profiles loaded from a directory. It is built once at
// process/server start (load-at-start, NOT hot-reloaded: an in-flight deploy must
// never have its profile swapped — design §5) and is then read-only.
type Registry struct {
	byName map[string]registryEntry
}

// newRegistry builds a Registry seeded with the built-in profiles, each at its
// project-derived tier.
func newRegistry() *Registry {
	r := &Registry{byName: make(map[string]registryEntry)}
	for name, produce := range builtinQuirks {
		r.byName[name] = registryEntry{
			quirks: produce(),
			name:   name,
			tier:   deriveTier(name, originBuiltin, builtinHasMockup(name)),
			origin: originBuiltin,
		}
	}
	return r
}

// add inserts (or overrides) a profile, logging a load line and, on a name
// collision with a built-in, a loud override line. Mirrors the audit-log-the-
// override pattern used by the eject fallback / settings precedence.
func (r *Registry) add(e registryEntry) (overrode bool) {
	if prev, ok := r.byName[e.name]; ok && prev.origin == originBuiltin && e.origin == originOperator {
		overrode = true
		log.Printf("redfish: operator quirk profile %q overrides the built-in [%s]", e.name, e.tier.describe())
	}
	r.byName[e.name] = e
	log.Printf("redfish: loaded quirk profile %q [%s]", e.name, e.tier.describe())
	return overrode
}

// quirksForName resolves a profile by name first, then falls back to the
// VendorType mapping, then to generic. This is the selection seam the CLI and
// server share. A typo / unknown name MUST resolve to generic so a mistake can
// never silently enable the wrong vendor workaround (the P1 invariant). The bool
// reports whether the lookup matched a registered profile by name (false means it
// fell through to the VendorType/generic path).
func (r *Registry) quirksForName(name string) (quirks, Tier, bool) {
	if e, ok := r.byName[strings.TrimSpace(name)]; ok {
		return e.quirks, e.tier, true
	}
	// Not a registered profile name: fall back to the VendorType mapping (which
	// itself resolves unknown vendors to generic), and report the built-in's tier.
	q := quirksFor(VendorType(name))
	if e, ok := r.byName[q.name]; ok {
		return q, e.tier, false
	}
	return q, deriveTier(q.name, originBuiltin, builtinHasMockup(q.name)), false
}

// LoadProfileDir reads every *.yaml / *.yml file in dir, parses and compiles each
// into the registry, and returns one ProfileLoadResult per file. A single bad
// profile is a logged error that disables ONLY that profile (design "malformed
// profile disables only itself"): it never fails the whole load. An empty or
// missing dir is not an error (the registry keeps only its built-ins). The
// returned error is reserved for a directory-read failure.
//
// The returned Registry is ready for selection via quirksForName and must be
// treated as read-only thereafter (load-at-start, no hot-reload).
func LoadProfileDir(dir string) (*Registry, []ProfileLoadResult, error) {
	r := newRegistry()

	if strings.TrimSpace(dir) == "" {
		return r, nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("redfish: quirks dir %q does not exist; using built-in profiles only", dir)
			return r, nil, nil
		}
		return r, nil, fmt.Errorf("reading quirks dir %q: %w", dir, err)
	}

	// Deterministic order so override logging and results are stable.
	files := make([]string, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		if !isProfileFile(ent.Name()) {
			continue
		}
		files = append(files, ent.Name())
	}
	sort.Strings(files)

	var results []ProfileLoadResult
	for _, fname := range files {
		path := filepath.Join(dir, fname)
		res := ProfileLoadResult{Path: path, Tier: TierC}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			res.Err = fmt.Errorf("reading quirk profile %q: %w", path, readErr)
			log.Printf("redfish: skipping quirk profile %q: %v", path, res.Err)
			results = append(results, res)
			continue
		}

		profile, parseErr := ParseProfile(data)
		if parseErr != nil {
			res.Err = fmt.Errorf("loading quirk profile %q: %w", path, parseErr)
			log.Printf("redfish: skipping quirk profile %q: %v", path, res.Err)
			results = append(results, res)
			continue
		}

		q, compileErr := profile.Compile()
		if compileErr != nil {
			res.Err = fmt.Errorf("compiling quirk profile %q: %w", path, compileErr)
			log.Printf("redfish: skipping quirk profile %q: %v", path, res.Err)
			results = append(results, res)
			continue
		}

		// Operator profiles are ALWAYS tier C (design §4b): we have no recorded
		// evidence for them, regardless of any tier the file might imply.
		res.Name = profile.Name
		res.Tier = deriveTier(profile.Name, originOperator, false)
		res.Overrode = r.add(registryEntry{
			quirks:            q,
			name:              profile.Name,
			tier:              res.Tier,
			origin:            originOperator,
			validatedFirmware: profile.ValidatedFirmware,
		})
		results = append(results, res)
	}

	return r, results, nil
}

// isProfileFile reports whether a filename is a YAML quirk profile by extension.
func isProfileFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

// defaultRegistry is the process-wide registry consulted by NewDeployer when no
// per-Deployer registry is supplied. It holds only the built-in profiles until
// SetDefaultRegistry installs an operator-loaded one at start. It is guarded by a
// mutex purely for the load-at-start write; reads after start are effectively
// read-only.
var (
	defaultRegistryMu sync.RWMutex
	defaultRegistry   = newRegistry()
)

// SetDefaultRegistry installs the process-wide registry (load-at-start). The CLI
// and fleet server call it once, after LoadProfileDir, so every subsequent
// NewDeployer resolves names/vendors against the loaded operator profiles. A nil
// argument is ignored (keeps the built-in-only registry).
func SetDefaultRegistry(r *Registry) {
	if r == nil {
		return
	}
	defaultRegistryMu.Lock()
	defaultRegistry = r
	defaultRegistryMu.Unlock()
}

// DefaultRegistry returns the process-wide registry installed at start (built-in
// profiles, plus any operator profiles loaded via SetDefaultRegistry). The fleet
// server uses it to list the loaded profiles for the UI. The returned Registry is
// read-only after start; callers must only read it (e.g. via Profiles).
func DefaultRegistry() *Registry {
	defaultRegistryMu.RLock()
	defer defaultRegistryMu.RUnlock()
	return defaultRegistry
}

// resolveQuirks selects the quirks for a profile name / vendor string using the
// process-wide registry: name-first, then the VendorType mapping, then generic.
// NewDeployer uses it so the operator dir is honoured everywhere a Deployer is
// built (CLI and fleet server) without threading a registry through every call
// site.
func resolveQuirks(name string) quirks {
	defaultRegistryMu.RLock()
	r := defaultRegistry
	defaultRegistryMu.RUnlock()
	q, _, _ := r.quirksForName(name)
	return q
}
