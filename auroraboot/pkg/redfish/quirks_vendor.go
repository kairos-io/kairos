package redfish

import "strings"

// This file holds the conservative, documented per-vendor quirk profiles. Every
// quirk here is derived from public Redfish/vendor documentation, NOT from
// real-hardware testing. Until catalogue item #7 (real-HW validation) is closed
// these must stay defensive: prefer the spec-default flow and only diverge where
// the divergence is clearly safe and reasoned. Where a quirk genuinely needs
// hardware confirmation, leave a `// TODO(#7): verify on real <vendor> hardware`
// marker rather than guessing at an OEM action we cannot verify.

// iloQuirks is the HPE iLO profile.
//
// The one well-documented, safe divergence is virtual-media location: on HPE iLO,
// virtual media is exposed under the Manager (the iLO BMC), not under the
// ComputerSystem. Spec-compliant clients that only look at System.VirtualMedia
// find nothing on iLO. The core already searches Managers as a fallback, but the
// generic order tries the System first; this profile prefers the Manager
// collections so iLO's media is found directly and we don't depend on an empty
// System collection being present.
//
// iLO 5 vs iLO 6 notes (informational; both are handled by the same Manager-first
// search):
//   - iLO 5 (Gen10): VirtualMedia lives at /redfish/v1/Managers/1/VirtualMedia,
//     with members typically Id "1" (Floppy/USB) and "2" (CD/DVD). The CD device
//     is the one advertising MediaTypes including "CD"/"DVD"; mediaSupportsCD
//     already selects it, so we do not hardcode the index.
//   - iLO 6 (Gen11): same Manager-hosted layout; URL-pull InsertMedia with an HTTP
//     Image works the same way. No separate code path is needed.
//
// We deliberately do NOT add HPE OEM actions (e.g. the legacy iLO
// "BootOnNextServerReset" OEM virtual-media flow) — the standard
// VirtualMedia.InsertMedia + one-time Boot override is supported on iLO 5/6 and is
// what we can reason about safely.
//
// TODO(#7): verify on real HPE iLO 5/6 hardware that Manager-hosted InsertMedia +
// one-time Cd boot override drives the install boot as expected.
func iloQuirks() quirks {
	return quirks{
		name:        "ilo",
		mediaSearch: managerFirstMediaSearch,
		// iLO accepts the spec-default MediaType (CD) and the URL-pull params, so we
		// leave mediaType/tuneInsertParams/resetType at spec default.
	}
}

// managerFirstMediaSearch is the iLO media-ordering hook: prefer Manager-hosted
// media, then fall back to whatever the System exposed (defensive: some iLO
// firmware also surfaces it on the System). It works purely over the read-only
// []MediaView the core supplies — it has no *Deployer and issues no requests.
//
// It preserves the exact semantics of the original hook: when there is no
// Manager-hosted media it returns nil, which the core reads as "use the default
// order" — i.e. behaviour identical to the generic/default path. Otherwise it
// emits the manager-located member indexes first, then the system-located ones,
// dropping nothing.
func managerFirstMediaSearch(media []MediaView) []int {
	var managerIdx, systemIdx []int
	for _, m := range media {
		if strings.HasPrefix(m.Location, "manager:") {
			managerIdx = append(managerIdx, m.Index)
		} else {
			systemIdx = append(systemIdx, m.Index)
		}
	}
	if len(managerIdx) == 0 {
		// Nothing under the Manager — behave exactly like the default.
		return nil
	}
	return append(managerIdx, systemIdx...)
}

// supermicroQuirks is the Supermicro (X11/X12/H12 BMC) profile.
//
// Supermicro BMCs are known to be picky about virtual media and session handling,
// but the specifics are firmware-revision-dependent and we have no hardware to
// confirm them. Rather than guess at an OEM action or a parameter the firmware
// might reject, we keep the spec-default flow and only document the known
// sensitivities so a future change has the context:
//
//   - Older Supermicro firmware exposed virtual media via a separate, stateful
//     "SD5"/"CD-ROM image" upload service (a non-Redfish proprietary applet), not
//     the Redfish VirtualMedia.InsertMedia action. Newer BMC firmware (X12/H12 and
//     recent X11) implements the standard URL-pull InsertMedia, which is what this
//     flow uses. If a target runs old firmware, deployment will fail at
//     InsertMedia with a clear error rather than silently doing the wrong thing.
//   - Supermicro has historically been sensitive to the media URL form (e.g.
//     requiring a trailing filename and rejecting query strings on the ISO URL).
//     Our tokenized serve URL already ends in a real path; we do not append query
//     parameters to it. No code change is needed today, but keep this in mind if
//     the serve URL shape changes.
//   - Some Supermicro firmware rejects an explicit TransferProtocolType on
//     InsertMedia. We do NOT drop it pre-emptively (the spec-default is correct and
//     most firmware accepts it); if real hardware proves otherwise, the safe fix is
//     a tuneInsertParams hook clearing params.TransferProtocolType for HTTP.
//
// TODO(#7): verify on real Supermicro X11/X12 hardware which firmware revisions
// accept URL-pull InsertMedia and whether TransferProtocolType must be omitted.
// Until then this profile is intentionally identical to generic.
func supermicroQuirks() quirks {
	return quirks{
		name: "supermicro",
		// No safe, verifiable divergence yet — see the doc comment above. Behaves as
		// the spec-default (generic) profile.
	}
}
