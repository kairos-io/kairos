package redfish

// This file defines the safe boundary between the spec-correct core flow and a
// quirk profile. A quirk hook NEVER sees a live gofish object, the *Deployer, the
// Redfish client, credentials, or the image URL. Instead the core projects the
// gofish resources it needs into these plain, read-only value structs ("Views"),
// passes them to the hook, and accepts back a constrained answer (an ordering of
// indices, an enum pick, a sparse field patch) which the core re-validates before
// it ever touches the BMC.
//
// This is the load-bearing decision of the quirk system (design §2): it is what
// makes a declaratively-compiled, possibly community-authored profile safe to run
// against a BMC we hold admin credentials for. The same Views are used by the
// in-tree Go profiles (quirks_vendor.go) and by the YAML profile compiler
// (quirkprofile.go), so there is exactly one boundary shape to reason about.
//
// All Views are value copies built at the call sites; mutating an input changes
// nothing in the core. None of them carries the ISO image URL — that is
// core-owned and SSRF-validated, and a profile must never be able to set it.

// MediaView is the read-only projection of one VirtualMedia member handed to the
// mediaSearch hook. The hook receives the flattened, spec-default-ordered list of
// candidate media (System-hosted first, then each Manager's) and returns an
// ordering/filter over their Index values.
type MediaView struct {
	// Index is the member's position in the flat candidate list the hook received.
	// A mediaSearch hook returns a slice of these to express its preferred order.
	Index int
	// ID is the VirtualMedia Redfish Id (e.g. "Cd"), for diagnostics/matching.
	ID string
	// Location records where the media is hosted: "system" for ComputerSystem-
	// hosted media, or "manager:<managerId>" for Manager-hosted media (e.g. HPE
	// iLO exposes virtual media under the Manager). A profile selects by this.
	Location string
	// MediaTypes is the advertised media-type set (e.g. ["CD","DVD"]). May be empty
	// when the BMC omits the field.
	MediaTypes []string
	// ConnectedVia is the VirtualMedia ConnectedVia value when advertised (e.g.
	// "NotConnected", "URI"), best-effort and informational.
	ConnectedVia string
	// Inserted reports whether media is currently inserted in this device.
	Inserted bool
}

// ResetView is the read-only projection handed to the resetType hook. The hook may
// prefer a ResetType from AllowableResetTypes; whatever it returns is still
// re-validated by the core (firstSupported) against the system's advertised
// allowable values, so a profile can prefer but never force an unsupported type.
type ResetView struct {
	// PowerState is the system's current Redfish PowerState (e.g. "On", "Off").
	PowerState string
	// AllowableResetTypes is the system's advertised ResetType allowable values.
	AllowableResetTypes []string
	// Default is the ResetType the core chose by default (chooseResetType), offered
	// so a hook can fall back to it.
	Default string
}

// InsertParamsView is the read-only projection of the InsertMedia parameters
// handed to the tuneInsertParams hook. It deliberately carries NO image URL: the
// URL is core-owned and SSRF-validated, and a profile must never be able to read
// or rewrite it. HasImage reports merely that the core set an image, without
// exposing its value.
type InsertParamsView struct {
	// MediaType is the chosen VirtualMedia MediaType (e.g. "CD").
	MediaType string
	// TransferProtocolType is the chosen transfer protocol (e.g. "HTTP", "HTTPS"),
	// empty when the core did not set one.
	TransferProtocolType string
	// Inserted / WriteProtected are the boolean params the core set, with their
	// "was it set" flags so a hook can distinguish false from unset.
	Inserted          bool
	InsertedSet       bool
	WriteProtected    bool
	WriteProtectedSet bool
	// HasImage reports that the core set an image URL — never the URL itself.
	HasImage bool
}

// InsertParamsPatch is the constrained answer a tuneInsertParams hook returns: a
// sparse patch over an allowlisted set of InsertMedia fields. The core applies it
// to the spec-default parameters. Fields NOT named are left untouched; the image
// URL is structurally absent and can never be patched.
type InsertParamsPatch struct {
	// ClearMediaType drops the MediaType so it is omitted from the request.
	ClearMediaType bool
	// SetMediaType, when non-empty, overrides the MediaType.
	SetMediaType string
	// ClearTransferProtocolType drops the TransferProtocolType (some firmware
	// rejects an explicit one).
	ClearTransferProtocolType bool
	// SetTransferProtocolType, when non-empty, overrides the TransferProtocolType.
	SetTransferProtocolType string
	// ClearInserted / ClearWriteProtected drop the respective booleans so they are
	// omitted from the request.
	ClearInserted       bool
	ClearWriteProtected bool
	// SetInserted / SetWriteProtected override the respective booleans when their
	// "...Set" flag is true (so a patch can set false explicitly).
	SetInserted          bool
	SetInsertedSet       bool
	SetWriteProtected    bool
	SetWriteProtectedSet bool
}
