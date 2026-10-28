# Recorded BMC mockups (tier-B evidence)

This directory holds **recorded, sanitized DMTF mockups** of real BMCs. Each mockup
is the no-hardware fidelity substitute for metal: CI replays a vendor's recorded GET
responses against the Redfish deploy flow on **every PR** (pure Go, blocking), so a
per-vendor quirk profile is golden-tested against that vendor's REAL responses with
no hardware in the project's CI.

A built-in profile that ships a mockup here is promoted to **tier B**
("community-validated against the recorded firmware") — see `../../mockups.go`,
`deriveTier` in `../../registry.go`, and the golden test in
`../../mockup_golden_test.go`. This is the entire C→B promotion incentive (design
§4a/§4b, RFC 0001 P4).

## Layout (DMTF `redfish-mockup-creator` format)

```
testdata/mockups/<vendor>/<model-or-firmware>/
  redfish/v1/index.json                         # ServiceRoot
  redfish/v1/Systems/index.json                 # ComputerSystem collection
  redfish/v1/Systems/<id>/index.json            # the member (model/mfr/Boot/Reset action)
  redfish/v1/Systems/<id>/VirtualMedia/...       # System-hosted media (if any)
  redfish/v1/Managers/index.json
  redfish/v1/Managers/<id>/index.json
  redfish/v1/Managers/<id>/VirtualMedia/...      # Manager-hosted media (iLO lives here)
  redfish/v1/TaskService/index.json
```

The tree mirrors the Redfish URI hierarchy: **each resource is a directory whose
`index.json` is the GET body for that URI** (e.g. a GET of
`/redfish/v1/Systems/1` is served from `.../Systems/1/index.json`). The replay
harness (`../../mockupbmc_test.go`) serves these recorded GETs and keeps the
synthesized POST/PATCH action handlers a static mockup cannot record (InsertMedia →
202 + Task, Reset → 204/Task, Task → Completed, session create/DELETE).

## Prune list (keep mockups small — cap a few hundred KB)

Record only the **deploy-relevant subtree**:

- ServiceRoot (`redfish/v1/index.json`), with `SessionService`/`Links.Sessions`.
- The `Systems` collection and the target member, including:
  - `Manufacturer` / `Model` (used for vendor identification),
  - `MemorySummary.TotalSystemMemoryGiB` and `ProcessorSummary.Count`,
  - the `Boot` object (and `BootSourceOverrideMode@Redfish.AllowableValues` if the
    BMC advertises it),
  - the `#ComputerSystem.Reset` action **with its `ResetType@Redfish.AllowableValues`**.
- `VirtualMedia` collections + the CD/DVD member, on the System and/or the Managers —
  whichever the BMC actually exposes (iLO exposes it under the Manager only).
- The `Managers` collection + the target Manager.
- `TaskService/index.json`.

Drop everything else (Chassis, Sensors, EthernetInterfaces, BIOS attribute
registries, etc.): the golden test never touches them.

## Sanitization (required, by construction)

Recorded trees from real hardware contain serials, MACs, IPs, asset tags, and UUIDs.
**Mockups committed here must be sanitized**: replace any such value with an obvious
placeholder (`PLACEHOLDER-SERIAL`, `00000000-0000-0000-0000-000000000000`, etc.).
The hand-authored iLO mockups here are sanitized by construction. A contributor PRing
a recorded mockup must attest to the sanitization pass (see
`../../../../docs/CONTRIBUTING-vendor-profiles.md` and the vendor-profile PR
template).

## Adding the next vendor (one golden-test row)

1. Record/author `testdata/mockups/<vendor>/<fw>/` per the layout + prune list above,
   sanitized.
2. Add the vendor's name to `../../mockups.manifest` **iff** it is a built-in
   profile (this is what makes the runtime report tier B; an internal test keeps the
   manifest in exact sync with the committed trees).
3. Add a `goldenCase` row in `../../mockup_golden_test.go` selecting that vendor's
   profile against the new tree, asserting: the discovered SystemID + VirtualMedia
   member, the InsertMedia body (`Image` equals the served URL — the profile must
   never rewrite it), the boot PATCH, the Reset `ResetType` (validated against the
   recorded allowable values), and the session DELETE on teardown.

## Current mockups

| Vendor | Tree | Shape | Profile | Tier |
|---|---|---|---|---|
| HPE iLO | `ilo/gen10-ilo5-fw2.44` | Manager-hosted CD only (member 2 CD/DVD, member 1 Floppy/USB); ProLiant DL360 Gen10 / iLO 5 | `ilo` | B |
| HPE iLO | `ilo/system-and-manager-cd` | CD on **both** System and Manager — the negative-guard tree proving `ilo` (manager-first) and `generic` (system-first) pick **different** media | `ilo` / `generic` | (test fixture) |
