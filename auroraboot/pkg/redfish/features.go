package redfish

import (
	"encoding/json"
	"strings"

	"github.com/stmcginnis/gofish/schemas"
)

// Feature names AuroraBoot can genuinely determine from a Redfish ComputerSystem.
// These are the ONLY feature strings the hardware gate understands; any other
// required feature fails closed (see pkg/hardware.hasFeature). Keep this list
// honest: a feature only belongs here if we can back its detection from the
// Redfish data, not from a guess.
const (
	// FeatureUEFI means the system can boot in UEFI mode. Derived from the Boot
	// resource (see detectFeatures).
	FeatureUEFI = "UEFI"
	// FeatureSecureBoot means the system exposes a UEFI SecureBoot resource.
	// Derived from the presence of the ComputerSystem SecureBoot link.
	FeatureSecureBoot = "SecureBoot"
)

// detectFeatures inspects a gofish ComputerSystem and returns the set of
// capabilities AuroraBoot was able to positively determine. A capability is only
// included when there is a concrete Redfish signal for it; anything we cannot
// determine is deliberately omitted so the hardware gate fails closed on it.
//
// Detection is intentionally conservative and each signal is documented inline.
func detectFeatures(system *schemas.ComputerSystem) map[string]bool {
	features := map[string]bool{}

	if detectUEFI(system) {
		features[FeatureUEFI] = true
	}
	if detectSecureBoot(system) {
		features[FeatureSecureBoot] = true
	}

	return features
}

// detectUEFI reports whether the system can boot in UEFI mode.
//
// Primary signal (spec-accurate): the Redfish
// "BootSourceOverrideMode@Redfish.AllowableValues" annotation on the Boot
// resource lists the boot modes the system supports for a one-time override. If
// it includes "UEFI" the system is UEFI-capable. gofish v0.22.0 does not unmarshal
// this annotation into a typed field, so we read it from the ComputerSystem's
// preserved raw JSON (ComputerSystem.RawData).
//
// Secondary signals (used only when the annotation is absent, as many BMCs omit
// it): the current BootSourceOverrideMode is already "UEFI", or the system
// advertises UEFI-only boot targets (UefiTarget/UefiHttp/UefiShell/UefiBootNext)
// in its BootSourceOverrideTarget allowable values — those targets only exist on
// UEFI-capable firmware.
//
// If none of these are present we return false: we did not see a positive UEFI
// signal and the gate should treat UEFI as unverified rather than assume it.
func detectUEFI(system *schemas.ComputerSystem) bool {
	// Primary: parse the boot-mode allowable-values annotation from raw JSON.
	if modes := bootModeAllowableValues(system.RawData); len(modes) > 0 {
		for _, m := range modes {
			if strings.EqualFold(m, string(schemas.UEFIBootSourceOverrideMode)) {
				return true
			}
		}
		// The annotation was present and did NOT list UEFI: the system genuinely
		// does not support a UEFI boot override. Honour that and report false.
		return false
	}

	// Secondary: the system is currently configured for a UEFI override.
	if strings.EqualFold(string(system.Boot.BootSourceOverrideMode), string(schemas.UEFIBootSourceOverrideMode)) {
		return true
	}

	// Secondary: UEFI-only boot targets imply UEFI firmware.
	for _, target := range system.Boot.AllowableBootSourceOverrideTargetValues {
		switch target {
		case schemas.UefiTargetBootSource,
			schemas.UefiHTTPBootSource,
			schemas.UefiShellBootSource,
			schemas.UefiBootNextBootSource:
			return true
		}
	}

	return false
}

// bootModeAllowableValues extracts
// Boot.BootSourceOverrideMode@Redfish.AllowableValues from a ComputerSystem's raw
// JSON. It returns nil when the annotation (or the Boot object) is absent, so the
// caller can distinguish "absent" from "present but UEFI-less".
func bootModeAllowableValues(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var doc struct {
		Boot struct {
			AllowableBootModes []string `json:"BootSourceOverrideMode@Redfish.AllowableValues"`
		} `json:"Boot"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	return doc.Boot.AllowableBootModes
}

// detectSecureBoot reports whether the system exposes a UEFI SecureBoot resource.
// A ComputerSystem links its SecureBoot resource via the "SecureBoot" property;
// its mere presence means the firmware models SecureBoot. We read the link from
// the raw JSON (gofish keeps the parsed link private) rather than issuing an extra
// GET, which keeps the detection cheap and side-effect free. Absence is reported
// as "not supported" (fail closed) — we do not probe further or assume.
func detectSecureBoot(system *schemas.ComputerSystem) bool {
	if len(system.RawData) == 0 {
		return false
	}
	var doc struct {
		SecureBoot struct {
			ODataID string `json:"@odata.id"`
		} `json:"SecureBoot"`
	}
	if err := json.Unmarshal(system.RawData, &doc); err != nil {
		return false
	}
	return strings.TrimSpace(doc.SecureBoot.ODataID) != ""
}
