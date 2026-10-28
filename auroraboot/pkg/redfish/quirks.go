package redfish

import "github.com/stmcginnis/gofish/schemas"

// quirks is a per-vendor profile of optional hook functions that influence the
// genuine deltas between BMC implementations. The shared auth/discovery/insert/
// boot/reset/task machinery lives in the core Deployer; a quirk only customises
// the small set of decisions where vendors actually diverge.
//
// Every field is optional. A nil hook means "use the spec-default behaviour", so
// the generic profile (genericQuirks) is the zero value and the default path is
// byte-for-byte identical to having no quirk seam at all. This is the contract
// the regression tests assert: VendorGeneric must never change behaviour.
type quirks struct {
	// name identifies the profile (for diagnostics only).
	name string

	// mediaSearch reorders/filters the candidate VirtualMedia members. The core
	// flattens the spec-default collections (System-hosted first, then each
	// Manager's) into a read-only []MediaView and passes it in; the hook returns
	// the Indexes it prefers, in order. Out-of-range or duplicate indexes are
	// dropped by the core; a nil/empty return means "use the core default order".
	// The hook never sees the *Deployer or any gofish object (design §2 boundary):
	// e.g. HPE iLO hosts virtual media under the Manager, so the iLO profile emits
	// the manager-located indexes first.
	mediaSearch func(media []MediaView) []int

	// mediaType selects the VirtualMedia MediaType sent on InsertMedia, given the
	// chosen media's read-only MediaView. nil uses the spec default (CD).
	mediaType func(media MediaView) schemas.VirtualMediaType

	// resetType lets a profile prefer a ResetType from the read-only ResetView
	// (current power state, allowable values, and the core default). Whatever it
	// returns is still re-validated by the core against the allowable values, so a
	// profile can prefer but never force an unsupported type. nil keeps the
	// default.
	resetType func(view ResetView) schemas.ResetType

	// tuneInsertParams returns a sparse patch over an allowlisted set of InsertMedia
	// fields, given a read-only InsertParamsView (which never carries the image
	// URL). The core applies the patch to the spec-default parameters. A profile
	// may, for example, drop TransferProtocolType for BMCs that reject it. nil
	// leaves the params untouched.
	tuneInsertParams func(view InsertParamsView) InsertParamsPatch

	// pushMedia is an optional hook point for a future multipart media push (some
	// modern BMCs accept a multipart HTTP POST of the ISO bytes instead of a
	// URL-pull InsertMedia). It is a STUB seam only: no profile implements it yet
	// and the core never calls it. It exists so the multipart work (a later phase)
	// has a defined extension point rather than re-plumbing the flow. When a
	// profile returns (handled=true), it has performed the media insertion itself.
	pushMedia func(d *Deployer, media *schemas.VirtualMedia, req DeployRequest) (handled bool, info *schemas.TaskMonitorInfo, err error)
}

// mediaCollections is an ordered list of VirtualMedia collections (each itself a
// slice of media members). Search order is significant: the first CD/DVD-capable
// member wins.
type mediaCollections [][]*schemas.VirtualMedia

// quirksFor returns the quirk profile for a VendorType. Unknown/empty vendors map
// to the generic (spec-default) profile so a typo can never silently enable a
// vendor workaround.
func quirksFor(v VendorType) quirks {
	switch v {
	case VendorHPE:
		return iloQuirks()
	case VendorSuperMicro:
		return supermicroQuirks()
	case VendorGeneric, VendorDMTF, "":
		return genericQuirks()
	default:
		return genericQuirks()
	}
}

// builtinQuirks is the registry of in-tree profiles keyed by their declared name.
// It is the seam a later phase (P3) extends with operator-supplied/YAML profiles;
// in P1 it only mirrors the named Go profiles. quirksFor's VendorType-switch
// selection is intentionally NOT routed through this yet, so current selection
// behaviour (typo ⇒ generic) is unchanged.
var builtinQuirks = map[string]func() quirks{
	"generic":    genericQuirks,
	"ilo":        iloQuirks,
	"supermicro": supermicroQuirks,
}

// quirksByName looks up a quirk profile by its declared name. It returns (profile,
// true) for a known name and (zero, false) otherwise. It does NOT fall back to
// generic itself — callers decide how an unknown name is handled — so it can be
// composed with operator/YAML profiles in a later phase without changing the
// VendorType-driven selection in quirksFor.
func quirksByName(name string) (quirks, bool) {
	if producer, ok := builtinQuirks[name]; ok {
		return producer(), true
	}
	return quirks{}, false
}

// genericQuirks is the spec-default profile: all hooks nil. DMTF/sushy-tools/
// PiKVM use this. Keeping it the zero value (plus a name) guarantees the default
// path is unchanged.
func genericQuirks() quirks {
	return quirks{name: "generic"}
}
