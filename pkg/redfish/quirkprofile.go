package redfish

import (
	"fmt"
	"strings"

	"github.com/stmcginnis/gofish/schemas"
	"gopkg.in/yaml.v3"
)

// This file is the second PRODUCER of a quirks value (the first being the in-tree
// Go profiles in quirks_vendor.go): a compiler from a declarative YAML profile to
// the same hook closures, working ONLY through the safe Views (quirkviews.go).
//
// A profile is inert data with a bounded blast radius (design §0/§7): it can
// reorder/filter media, pick a MediaType, prefer a ResetType (re-validated by the
// core), and sparse-patch an allowlisted set of InsertMedia fields. It can NEVER
// set the image URL, force an unsupported ResetType, or touch sessions/TLS/system
// selection. That is what makes a community-authored profile safe to run against a
// BMC we hold admin credentials for.
//
// Validation is strict and happens at load (ParseProfile): unknown keys, bad
// enums, and non-allowlisted insert-param fields (notably Image, any casing) are
// rejected with a clear error before any hook is compiled.

// QuirkProfile is the parsed, validated representation of a YAML quirk profile. It
// is compiled into a quirks value by Compile. The struct mirrors the schema in the
// RFC/design §4.1.
type QuirkProfile struct {
	// Name identifies the profile (required). Compiled into quirks.name.
	Name string `yaml:"name"`
	// Match is an optional selection hint (e.g. which vendor this profile targets).
	// It is informational in P1; P3 wires it to selection.
	Match *ProfileMatch `yaml:"match,omitempty"`
	// MediaSearch optionally reorders the media candidates by location.
	MediaSearch *ProfileMediaSearch `yaml:"mediaSearch,omitempty"`
	// MediaType optionally overrides the InsertMedia MediaType (CD|DVD|USBStick|Floppy).
	MediaType string `yaml:"mediaType,omitempty"`
	// ResetType is an optional ordered rule set; the first matching power-state rule
	// wins. The chosen type is still re-validated by the core.
	ResetType []ProfileResetRule `yaml:"resetType,omitempty"`
	// TuneInsertParams is an optional sparse patch over an allowlisted field set.
	TuneInsertParams *ProfileTuneInsertParams `yaml:"tuneInsertParams,omitempty"`
	// ValidatedFirmware is informational (tier labelling, P3/P4): the firmware the
	// profile+mockup pair was validated against. Not interpreted in P1.
	ValidatedFirmware string `yaml:"validatedFirmware,omitempty"`
}

// ProfileMatch is the optional selection hint.
type ProfileMatch struct {
	Vendor string `yaml:"vendor,omitempty"`
}

// ProfileMediaSearch declares a media-candidate ordering. Each Order entry is one
// of "system", "manager", or "manager:<id>"; candidates whose Location matches an
// entry are emitted in that order (earlier entries first), and unmatched
// candidates are dropped — exactly the index-ordering closure over []MediaView.
type ProfileMediaSearch struct {
	Order []string `yaml:"order"`
}

// ProfileResetRule is one first-match-wins reset rule. When.PowerState is one of
// "On", "Off", or "*"; Then is the ResetType to prefer (re-validated by the core).
type ProfileResetRule struct {
	When ProfileResetWhen `yaml:"when"`
	Then string           `yaml:"then"`
}

// ProfileResetWhen is the condition of a reset rule.
type ProfileResetWhen struct {
	PowerState string `yaml:"powerState"`
}

// ProfileTuneInsertParams is the sparse InsertMedia patch. Clear names fields to
// drop; Set names fields to set. Keys are restricted to the allowlist
// {MediaType, TransferProtocolType, Inserted, WriteProtected}; Image (any casing)
// is explicitly rejected.
type ProfileTuneInsertParams struct {
	Clear []string       `yaml:"clear,omitempty"`
	Set   map[string]any `yaml:"set,omitempty"`
}

// Allowed enum/allowlist sets, defined once so validation and documentation stay
// in sync.
var (
	allowedMediaTypes = map[string]schemas.VirtualMediaType{
		"CD":       schemas.CDVirtualMediaType,
		"DVD":      schemas.DVDVirtualMediaType,
		"USBStick": schemas.USBStickVirtualMediaType,
		"Floppy":   schemas.FloppyVirtualMediaType,
	}
	// allowedTransferProtocols is the Redfish VirtualMedia TransferProtocolType
	// enum, the values InsertMedia accepts.
	allowedTransferProtocols = map[string]bool{
		"CIFS": true, "FTP": true, "SFTP": true, "HTTP": true, "HTTPS": true,
		"NFS": true, "SCP": true, "TFTP": true, "OEM": true,
	}
	allowedPowerStates = map[string]bool{"On": true, "Off": true, "*": true}
	// insertParamAllowlist is the ONLY set of InsertMedia fields a profile may
	// patch. Image is deliberately absent and is rejected with a specific error.
	insertParamAllowlist = map[string]bool{
		"MediaType":            true,
		"TransferProtocolType": true,
		"Inserted":             true,
		"WriteProtected":       true,
	}
)

// ParseProfile decodes and strictly validates a YAML quirk profile. Unknown keys
// are rejected (KnownFields), as are bad enums and non-allowlisted insert-param
// fields. A malformed profile is a load-time error; it disables only itself.
func ParseProfile(data []byte) (*QuirkProfile, error) {
	var p QuirkProfile
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("parsing quirk profile: %w", err)
	}
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("validating quirk profile: %w", err)
	}
	return &p, nil
}

// LoadProfile parses and compiles a YAML quirk profile in one step.
func LoadProfile(data []byte) (quirks, error) {
	p, err := ParseProfile(data)
	if err != nil {
		return quirks{}, err
	}
	return p.Compile()
}

// validate checks the parsed profile against the schema's enums and allowlists.
func (p *QuirkProfile) validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}

	if p.MediaType != "" {
		if _, ok := allowedMediaTypes[p.MediaType]; !ok {
			return fmt.Errorf("mediaType %q is not one of CD, DVD, USBStick, Floppy", p.MediaType)
		}
	}

	if p.MediaSearch != nil {
		for _, entry := range p.MediaSearch.Order {
			if err := validateMediaSearchEntry(entry); err != nil {
				return err
			}
		}
	}

	for i, rule := range p.ResetType {
		if !allowedPowerStates[rule.When.PowerState] {
			return fmt.Errorf("resetType[%d].when.powerState %q is not one of On, Off, *", i, rule.When.PowerState)
		}
		if strings.TrimSpace(rule.Then) == "" {
			return fmt.Errorf("resetType[%d].then is required", i)
		}
		if !knownResetType(rule.Then) {
			return fmt.Errorf("resetType[%d].then %q is not a known Redfish ResetType", i, rule.Then)
		}
	}

	if p.TuneInsertParams != nil {
		if err := validateTuneInsertParams(p.TuneInsertParams); err != nil {
			return err
		}
	}

	return nil
}

// validateMediaSearchEntry checks one mediaSearch.order entry: "system",
// "manager", or "manager:<id>".
func validateMediaSearchEntry(entry string) error {
	if entry == "system" || entry == "manager" {
		return nil
	}
	if id, ok := strings.CutPrefix(entry, "manager:"); ok && id != "" {
		return nil
	}
	return fmt.Errorf("mediaSearch.order entry %q is not one of system, manager, manager:<id>", entry)
}

// validateTuneInsertParams enforces the field allowlist on both clear and set,
// rejecting Image (any casing) with a specific, actionable error, and checks each
// set value against the field's type and enum so nothing invalid reaches the BMC
// and nothing is silently dropped at compile time.
func validateTuneInsertParams(t *ProfileTuneInsertParams) error {
	for _, field := range t.Clear {
		if err := checkInsertParamField(field); err != nil {
			return fmt.Errorf("tuneInsertParams.clear: %w", err)
		}
	}
	for field, val := range t.Set {
		if err := checkInsertParamField(field); err != nil {
			return fmt.Errorf("tuneInsertParams.set: %w", err)
		}
		if err := checkInsertParamValue(field, val); err != nil {
			return fmt.Errorf("tuneInsertParams.set: %w", err)
		}
	}
	return nil
}

// checkInsertParamValue checks a set value for an allowlisted field: MediaType and
// TransferProtocolType must be one of their enums, Inserted and WriteProtected
// must be booleans.
func checkInsertParamValue(field string, val any) error {
	switch field {
	case "MediaType":
		s, ok := val.(string)
		if _, known := allowedMediaTypes[s]; !ok || !known {
			return fmt.Errorf("MediaType %v is not one of CD, DVD, USBStick, Floppy", val)
		}
	case "TransferProtocolType":
		s, ok := val.(string)
		if !ok || !allowedTransferProtocols[s] {
			return fmt.Errorf("TransferProtocolType %v is not one of CIFS, FTP, SFTP, HTTP, HTTPS, NFS, SCP, TFTP, OEM", val)
		}
	case "Inserted", "WriteProtected":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("%s %v is not a boolean (true or false)", field, val)
		}
	}
	return nil
}

// checkInsertParamField rejects any field outside the InsertMedia allowlist, with
// a dedicated message for Image (the SSRF-sensitive, core-owned URL) regardless of
// casing.
func checkInsertParamField(field string) error {
	if strings.EqualFold(field, "Image") {
		return fmt.Errorf("the Image field is core-owned and SSRF-validated; a profile must never set or clear it")
	}
	if !insertParamAllowlist[field] {
		return fmt.Errorf("field %q is not in the allowlist (MediaType, TransferProtocolType, Inserted, WriteProtected)", field)
	}
	return nil
}

// knownResetType reports whether s names a Redfish ResetType the core understands.
func knownResetType(s string) bool {
	switch schemas.ResetType(s) {
	case schemas.OnResetType,
		schemas.ForceOnResetType,
		schemas.ForceOffResetType,
		schemas.GracefulShutdownResetType,
		schemas.ForceRestartResetType,
		schemas.GracefulRestartResetType,
		schemas.NmiResetType,
		schemas.PauseResetType,
		schemas.ResumeResetType,
		schemas.SuspendResetType,
		schemas.PowerCycleResetType:
		return true
	default:
		return false
	}
}

// Compile turns the validated profile into a quirks value. Each omitted section
// compiles to a nil hook, so an empty profile (just a name) yields the zero-value
// quirks (plus name) — byte-for-byte the generic/default behaviour.
func (p *QuirkProfile) Compile() (quirks, error) {
	if err := p.validate(); err != nil {
		return quirks{}, fmt.Errorf("validating quirk profile: %w", err)
	}

	q := quirks{name: p.Name}

	if p.MediaSearch != nil && len(p.MediaSearch.Order) > 0 {
		q.mediaSearch = compileMediaSearch(p.MediaSearch.Order)
	}
	if p.MediaType != "" {
		mt := allowedMediaTypes[p.MediaType]
		q.mediaType = func(MediaView) schemas.VirtualMediaType { return mt }
	}
	if len(p.ResetType) > 0 {
		q.resetType = compileResetType(p.ResetType)
	}
	if p.TuneInsertParams != nil {
		q.tuneInsertParams = compileTuneInsertParams(p.TuneInsertParams)
	}

	return q, nil
}

// compileMediaSearch builds the index-ordering closure: for each Location selector
// in order, emit the indexes of the matching MediaViews; drop unmatched. An entry
// "system" matches Location "system"; "manager" matches any "manager:<id>";
// "manager:<id>" matches that exact manager.
func compileMediaSearch(order []string) func([]MediaView) []int {
	return func(media []MediaView) []int {
		var out []int
		for _, sel := range order {
			for _, m := range media {
				if locationMatches(sel, m.Location) {
					out = append(out, m.Index)
				}
			}
		}
		return out
	}
}

// locationMatches reports whether a MediaView Location satisfies a mediaSearch
// selector.
func locationMatches(sel, location string) bool {
	switch sel {
	case "system":
		return location == "system"
	case "manager":
		return strings.HasPrefix(location, "manager:")
	default:
		// "manager:<id>" — exact match.
		return location == sel
	}
}

// compileResetType builds the first-match-wins reset closure. PowerState matching
// is case-insensitive; "*" matches all. The returned type is still re-validated by
// the core's firstSupported, so a rule can prefer but not force an unsupported type.
func compileResetType(rules []ProfileResetRule) func(ResetView) schemas.ResetType {
	return func(view ResetView) schemas.ResetType {
		for _, rule := range rules {
			if rule.When.PowerState == "*" || strings.EqualFold(rule.When.PowerState, view.PowerState) {
				return schemas.ResetType(rule.Then)
			}
		}
		// No rule matched: keep the core default.
		return schemas.ResetType(view.Default)
	}
}

// compileTuneInsertParams builds the sparse-patch closure from the validated
// clear/set maps. The allowlist, value types and enums were enforced at
// validation, so this only needs to translate field names to the InsertParamsPatch.
func compileTuneInsertParams(t *ProfileTuneInsertParams) func(InsertParamsView) InsertParamsPatch {
	return func(InsertParamsView) InsertParamsPatch {
		var patch InsertParamsPatch
		for _, field := range t.Clear {
			switch field {
			case "MediaType":
				patch.ClearMediaType = true
			case "TransferProtocolType":
				patch.ClearTransferProtocolType = true
			case "Inserted":
				patch.ClearInserted = true
			case "WriteProtected":
				patch.ClearWriteProtected = true
			}
		}
		for field, val := range t.Set {
			switch field {
			case "MediaType":
				if s, ok := val.(string); ok {
					patch.SetMediaType = s
				}
			case "TransferProtocolType":
				if s, ok := val.(string); ok {
					patch.SetTransferProtocolType = s
				}
			case "Inserted":
				if b, ok := val.(bool); ok {
					patch.SetInserted, patch.SetInsertedSet = b, true
				}
			case "WriteProtected":
				if b, ok := val.(bool); ok {
					patch.SetWriteProtected, patch.SetWriteProtectedSet = b, true
				}
			}
		}
		return patch
	}
}
