package redfish

import (
	"fmt"
	"strings"
)

// This file renders a ProbeReport into a starter quirk profile (tier C: UNVERIFIED).
// The emitted YAML is a VALID profile: it round-trips through ParseProfile (a profile
// with only a name + match is valid). Anything the probe can only *suggest* — notably
// ResetType candidates the operator must confirm — is emitted as YAML comments, which
// ParseProfile ignores, so the document always parses while still guiding the operator.

// StarterProfileHeader is the delimiter line callers print before the emitted
// starter YAML in mixed (text+yaml) output, clearly marking the tier and the path to
// promotion (add a recorded mockup to reach tier B — design §4b). It is a visual
// delimiter only and is NOT part of the profile body — yaml-only output omits it so
// the stream piped to a file parses cleanly via ParseProfile.
const StarterProfileHeader = "--- suggested profile (tier C: UNVERIFIED — add a recorded mockup to reach tier B) ---"

// StarterProfile renders the probe report into a starter quirk-profile YAML document.
// The document is guaranteed to parse via ParseProfile. It pre-fills:
//   - name: a slug of the manufacturer (or "custom").
//   - match.vendor: the manufacturer string.
//   - mediaSearch.order: [manager, system] ONLY when the only CD/DVD media is
//     Manager-hosted (the iLO signal); otherwise omitted (the spec default is correct),
//     with a comment saying so.
//   - mediaType: only when the chosen CD/DVD member advertises DVD-but-not-CD (a
//     non-default); otherwise omitted.
//   - validatedFirmware: the firmware string as a stub the operator confirms.
//
// ResetType candidates are emitted as comments (suggestions the operator confirms),
// keeping the document minimal and the active rules empty.
func (r *ProbeReport) StarterProfile() string {
	var b strings.Builder

	name := slugify(r.System.Manufacturer)
	if name == "" {
		name = "custom"
	}

	b.WriteString("name: " + name + "\n")

	// match.vendor: the manufacturer string, when the BMC reported one.
	if v := strings.TrimSpace(r.System.Manufacturer); v != "" {
		b.WriteString("match:\n")
		b.WriteString("  vendor: " + yamlScalar(v) + "\n")
	} else {
		b.WriteString("# match.vendor: <manufacturer> (the BMC reported none; fill in if you know it)\n")
	}

	// mediaSearch: only when the only CD/DVD media is Manager-hosted.
	if r.ManagerHostedCDOnly {
		b.WriteString("# The only CD/DVD media is Manager-hosted (the HPE iLO signal): the\n")
		b.WriteString("# spec-default System-first search misses it, so search Manager first.\n")
		b.WriteString("mediaSearch:\n")
		b.WriteString("  order: [manager, system]\n")
	} else {
		b.WriteString("# mediaSearch omitted: the spec-default media search order is correct for this BMC.\n")
	}

	// mediaType: only when the chosen member advertises DVD but not CD (non-default).
	if mt := suggestedMediaType(r); mt != "" {
		b.WriteString("# The default CD/DVD member advertises " + mt + " but not CD; pin the MediaType.\n")
		b.WriteString("mediaType: " + mt + "\n")
	} else {
		b.WriteString("# mediaType omitted: the spec default (CD) matches the chosen media.\n")
	}

	// resetType: suggestions only, as comments. The core re-validates any rule, but
	// the operator should confirm the right one for their hardware.
	b.WriteString("# resetType suggestions (uncomment and confirm against your hardware):\n")
	b.WriteString("#   the core default for power state " + nonEmpty(r.PowerState, "<unknown>") +
		" is: " + nonEmpty(r.DefaultResetType, "<none>") + "\n")
	if len(r.AllowableResetTypes) > 0 {
		b.WriteString("#   the BMC advertises these allowable ResetTypes: " +
			strings.Join(r.AllowableResetTypes, ", ") + "\n")
	} else {
		b.WriteString("#   the BMC advertised no allowable ResetTypes.\n")
	}
	b.WriteString("# resetType:\n")
	b.WriteString("#   - { when: { powerState: \"Off\" }, then: \"On\" }\n")
	b.WriteString("#   - { when: { powerState: \"*\" },   then: \"" +
		nonEmpty(r.DefaultResetType, "ForceRestart") + "\" }\n")

	// validatedFirmware: a stub the operator confirms.
	if fw := strings.TrimSpace(r.FirmwareVersion); fw != "" {
		b.WriteString("validatedFirmware: " + yamlScalar(fw) + "\n")
	} else {
		b.WriteString("# validatedFirmware: <firmware version> (the BMC reported none)\n")
	}

	return b.String()
}

// suggestedMediaType returns the MediaType to pin in the starter profile, or "".
// It returns "DVD" only when the spec-default-chosen CD/DVD member advertises DVD
// but NOT CD — the one non-default case worth pinning. Otherwise the spec default
// (CD) is correct and nothing is emitted.
func suggestedMediaType(r *ProbeReport) string {
	if r.DefaultCDIndex < 0 || r.DefaultCDIndex >= len(r.Media) {
		return ""
	}
	view := r.Media[r.DefaultCDIndex]
	hasCD, hasDVD := false, false
	for _, t := range view.MediaTypes {
		switch t {
		case "CD":
			hasCD = true
		case "DVD":
			hasDVD = true
		}
	}
	if hasDVD && !hasCD {
		return "DVD"
	}
	return ""
}

// slugify lowercases s and replaces any run of non-alphanumeric characters with a
// single hyphen, trimming leading/trailing hyphens. It yields a profile name that is
// stable and readable (e.g. "Hewlett Packard Enterprise" → "hewlett-packard-enterprise").
func slugify(s string) string {
	var b strings.Builder
	lastHyphen := true // suppress a leading hyphen
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// yamlScalar quotes a YAML scalar so a value containing characters with YAML meaning
// (colons, leading symbols, etc.) round-trips intact. Plain strings are double-quoted
// with embedded quotes/backslashes escaped — always-valid flow scalar syntax.
func yamlScalar(s string) string {
	return fmt.Sprintf("%q", s)
}

// nonEmpty returns s when it is non-empty, otherwise fallback.
func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
