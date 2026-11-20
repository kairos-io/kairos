# RFC 0001: Redfish vendor-support ecosystem — declarative quirk profiles + recorded-evidence CI

| | |
|---|---|
| **Status** | Proposed |
| **Author** | William Rizzo (@wrkode) |
| **Created** | 2026-06-09 |
| **Area** | `pkg/redfish` (virtual-media deployment) |

## Summary

Make AuroraBoot's Redfish vendor support **community-scalable**: replace the compiled-in vendor
quirk profiles with **declarative YAML profiles** that operators can write, share, and contribute —
validated in CI against **recorded evidence** of real hardware (DMTF Redfish mockups) — so vendor
coverage can grow without the maintainers owning or testing every BMC.

This RFC deliberately does **not** introduce a scripting engine (Lua/CEL/WASM). Profiles are inert
data with a bounded blast radius; that property is what makes contributions safe to accept from
people whose hardware we have never seen.

## Motivation

Every BMC vendor implements DMTF Redfish a little differently — and frequently not compliantly.
AuroraBoot's deploy flow (session → discovery → `VirtualMedia.InsertMedia` URL-pull → one-time boot
override → `ComputerSystem.Reset` → Task polling → session teardown) is spec-correct and tested
against emulators, but the *deltas* between BMCs are encoded as compiled-in Go "quirk" hooks
(`pkg/redfish/quirks.go`, `quirks_vendor.go`). Today:

- Adding or fixing a vendor quirk requires a Go change and a release from the maintainers.
- The existing iLO/Supermicro profiles are reasoned from public documentation, not confirmed on
  metal — and the project has no scalable way to confirm them.
- An operator who hits a quirky BMC in the field has no self-service path at all.

The maintainers cannot own a hardware lab covering every vendor × model × firmware. The design must
therefore change *who maintains vendor knowledge* and *how it is validated*.

## The ecosystem contract

| Layer | Maintained by | Tested by | How |
|---|---|---|---|
| Spec-correct core flow + safety boundary | Project | Project CI | Existing fake-BMC unit suite (+ optional live emulator job) |
| Vendor knowledge (quirk profiles) | Community / affected operators | The contributor, on their hardware | Declarative YAML profile + recorded mockup, PR'd together |
| Evidence replay | Project CI | Automatic, every PR | The recorded mockup is replayed through the fake-BMC harness; a per-vendor golden test asserts the request sequence |

The enabling property is **bounded blast radius**: a profile is data, never code. The worst a wrong
or malicious profile can do is cause a failed deploy with a clear error. It structurally cannot:

- set or alter the ISO **image URL** (core-owned, SSRF-validated via the media-URL allowlist);
- force an unsupported **ResetType** (re-validated against the BMC's advertised allowable values);
- touch **sessions, TLS verification, or system selection** (core-owned; profiles hold no client).

That is what lets a maintainer merge a profile for hardware they cannot test.

## Design

### 1. Declarative quirk profiles

The existing quirks seam stays exactly as it is: a `quirks` struct of optional hook functions
(`mediaSearch`, `mediaType`, `resetType`, `tuneInsertParams`), where a nil hook means spec-default
and the `generic` profile is the zero value (guarded by an existing regression test). The only new
thing is a second *producer* of that struct: a loader that compiles a YAML profile into hook
closures.

```yaml
# Example: the equivalent of the current built-in iLO profile
name: ilo
match: { vendor: HPE }
mediaSearch: { order: [manager, system] }   # iLO hosts virtual media under the Manager
mediaType: CD                                # optional; omitted = spec default
resetType:                                   # optional rules, first match wins
  - { when: { powerState: "Off" }, then: "On" }
  - { when: { powerState: "*" },   then: "ForceRestart" }
tuneInsertParams:                            # sparse patch over an allowlist
  clear: [TransferProtocolType]              # e.g. for firmware that rejects it
validatedFirmware: ""                        # filled when evidence is recorded (see tiers)
```

Profiles cross a narrow, safe boundary: the core hands each hook **plain, read-only view structs**
(media members with their location/types, the system's power state and allowable reset types, the
insert parameters minus the image URL) and accepts back **constrained answers** (an ordering of
indices, an enum pick, a sparse patch over an allowlisted field set). Profiles never see the live
Redfish client, the deploy request, or any credential. An omitted section means spec-default; an
empty profile is byte-for-byte the `generic` behaviour.

Validation happens at load: unknown keys, bad enums, attempts to patch non-allowlisted fields
(including `Image`), and duplicate names are rejected. A malformed profile disables only itself.

### 2. Support tiers

Support levels are explicit and derived by the project — a profile cannot assert its own tier:

| Tier | Meaning | Guarantee |
|---|---|---|
| **A — Core-tested** | `generic` + whatever the fake-BMC suite / emulator CI exercises | Project guarantee; breakage blocks merge |
| **B — Community-validated** | In-tree profile **with** a recorded, sanitized mockup, replayed in CI | "Works against the recorded firmware; regressions are caught automatically" |
| **C — Unverified** | Bare profile (in-tree without evidence, or operator-local) | Loaded fine, loudly logged as unverified; no promises |

Promotion C → B = contribute a mockup. The tier is surfaced in a load-time log line (e.g.
`redfish: loaded quirk profile "ilo" [tier B: validated against iLO 5 fw 2.44]`).

### 3. Recorded-evidence CI (the mockup loop)

The contribution unit is **a profile + a recorded mockup of the contributor's BMC**, PR'd together:

1. A deploy fails; AuroraBoot already surfaces the BMC's `@Message.ExtendedInfo` so the operator
   sees what the BMC objected to.
2. The operator runs `auroraboot redfish probe` (below), gets a pre-filled starter profile, tweaks
   it in their local profile directory until the deploy works on their hardware.
3. They record their BMC's resource tree with DMTF's `redfish-mockup-creator` (a read-only walk),
   sanitize it (serials/MACs/IPs/asset tags redacted — documented procedure + PR-template
   attestation), prune it to the deploy-relevant subtree, and open a PR with profile + mockup +
   firmware label.
4. CI loads the mockup tree into the existing fake-BMC test harness (recorded GETs; the harness's
   existing synthesized handlers cover the POST actions a static mockup cannot record) and runs a
   **per-vendor request-sequence golden test**: discovery resolves the expected system and media
   member, the InsertMedia body matches and its `Image` equals the test's served URL (proving the
   profile never rewrote it), boot/reset match the profile rules after allowable-values
   re-validation, and the session DELETE fires on success and error paths.

A regression in a profile, the profile compiler, or the core flow then fails that vendor's test *by
name* — loudly, attributably, with no hardware in the loop. This is also the scalable path for the
long-open real-hardware-validation work: vendor support stops being "reasoned from docs" and becomes
"confirmed on a contributor's metal, frozen as evidence, regression-tested forever."

**Honest limitation:** mockup replay validates AuroraBoot's request sequence against recorded GET
responses; it cannot catch a BMC that would reject a request at runtime (action responses are
synthesized). That residual gap is exactly what the tier-B "validated against firmware X on real
hardware" claim and the optional live-emulator CI job cover. The tiers are honest because they name
this boundary.

### 4. The probe command

`auroraboot redfish probe --endpoint <url> [credentials]` — a read-only diagnostic that prints what
a BMC actually exposes, and emits a **starter profile**. Everything it reports comes from discovery
code that already exists: SessionService presence/auth mode, the Systems members and their IDs
(feeds system selection), where VirtualMedia lives (System vs Manager) and its MediaTypes (feeds
`mediaSearch`/`mediaType`), the allowable ResetTypes, and model/manufacturer/firmware strings. It
performs no writes and is CLI-only in v1. This turns "my BMC doesn't work" into an afternoon of
self-service instead of an upstream issue.

### 5. Future, explicitly gated: constrained OEM actions

Some vendors require OEM actions instead of the spec InsertMedia/Reset. A possible later extension
is a declarative `oemAction` block under hard constraints: it may only invoke an action **present in
the BMC's own `Actions` advertisement** (never a free-form URI); the image URL is only referenceable
through a placeholder the core substitutes **after** SSRF validation (a raw URL literal in a profile
is a load error); body fields and values come from a fixed allowlist; flow points are a fixed enum.

This lands **last, behind a dedicated security review**. If the discovered-actions constraint cannot
be made airtight, OEM actions go to a reviewed in-tree Go hook instead — the declarative surface's
entire value is that it is safe to accept from strangers, and an escape hatch that breaks that
property defeats the design.

## Alternatives considered

- **Embedded scripting (Lua via gopher-lua, Starlark):** pure-Go and feasible, but rejected. An
  inventory of the real quirks shows they are all *selection and field-override*, never computation
  — so a scripting engine adds no reachable capability while adding a permanent, hand-maintained
  sandbox (CPU/memory/time limits, stdlib stripping) on a path that drives BMCs with admin
  credentials, and it breaks the "safe to accept from strangers" property that the ecosystem model
  depends on. If a future quirk genuinely requires computation, it should be a reviewed Go change.
- **CEL expressions:** safer than Lua (non-Turing-complete, bounded), but still solves the wrong
  problem: the quirks need data, not expressions, and CEL cannot express the mutating hook cleanly.
- **WASM (wazero):** strongest sandbox, but the contributor experience ("set up a toolchain and
  compile a module to drop a field") is wrong for the audience, and marshalling across the ABI is
  heavy for what is usually a two-line tweak.
- **Go `plugin` packages / out-of-process driver binaries:** `plugin` is incompatible with the
  project's static, cross-platform (amd64/arm64/riscv64, no cgo) builds. Out-of-process drivers
  (gRPC, Terraform-provider-style) are the honest answer for *radically* divergent vendors, but they
  are a driver SDK with a much larger trust and maintenance footprint — deferred unless real demand
  for tier-2 vendors materialises; the in-tree Go hook path covers those until then.

## Rollout

Each phase is independently shippable; none changes the core deploy flow.

| Phase | Scope |
|---|---|
| **1** | Profile schema + loader + safe view/boundary types, in-tree only. Bundled iLO/Supermicro profiles expressed against it. **Zero behaviour change** (the generic-zero-value regression test proves it). |
| **2** | `auroraboot redfish probe` (read-only report + starter profile). |
| **3** | Operator profile directory (load-at-start), named selection with typo-safe fallback to `generic`, tier surfacing, `CONTRIBUTING` guide + PR template for vendor profiles. |
| **4** | Mockup replay harness in CI + per-vendor golden tests + sanitization/pruning policy. This makes tier B real. |
| **5** | *(conditional)* the constrained `oemAction` block, behind its own security review. |

## Open questions

1. Mockup sanitization: documented redaction script + contributor attestation, or a maintained
   `sanitize-mockup` helper command? What is the exact redaction field list?
2. Mockup location and size policy (proposed: `pkg/redfish/testdata/mockups/<vendor>/<firmware>/`,
   pruned to the deploy-relevant subtree, a few hundred KB cap).
3. Tier surfacing beyond the load-time log line (CLI listing? a badge in the web UI's BMC page?).
4. `oemAction` ship/no-ship criteria and its placeholder/field allowlists.
5. Should `probe` ever get a server/UI endpoint, or stay CLI-only?

## Security considerations

Summarised throughout; the load-bearing invariants are: profiles are inert data; the image URL is
never profile-settable and is always SSRF-validated by the core; ResetType is re-validated against
the BMC's advertised allowable values; sessions/TLS/system-selection are core-owned; operator-dir
profiles are always tier C and logged; tier is derived by the project, never asserted by a profile;
the (future) OEM-action block may only target actions the BMC itself advertises and is gated on a
dedicated security review.
