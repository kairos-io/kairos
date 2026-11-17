# Contributing Redfish vendor quirk profiles

AuroraBoot deploys an OS image to a server over Redfish virtual media: it discovers
the system, inserts the media by URL (`VirtualMedia.InsertMedia`), sets a one-time
boot override, resets with an explicit `ResetType`, and polls the returned Task to
completion. The flow is spec-compliant, but BMC vendors implement Redfish
differently — sometimes non-compliantly. A **quirk profile** is a small piece of
declarative YAML that nudges the genuine per-vendor deltas (mostly "where is the
virtual media" and "which `ResetType`") without changing the core flow.

A profile is **inert data, never code.** The worst a broken or malicious profile
can do is cause a failed deploy with a clear error. That safety boundary is exactly
what lets us accept a profile from someone whose hardware we will never see.

This guide covers: getting a starter profile from your BMC, the schema, where
profiles live and how they are selected, the precedence rules, support tiers, and
how to contribute a profile back.

---

## 1. Get a starter profile from your BMC: `redfish probe`

`auroraboot redfish probe` is read-only — it issues GETs only (no InsertMedia, no
boot change, no reset) — so it is safe to run against any BMC. It reports what the
BMC actually exposes and emits a **tier-C starter profile** you can tweak.

```bash
auroraboot redfish probe \
  --endpoint https://bmc.example.com \
  --username admin \
  --password-file ./bmc-pass \
  --output yaml > my-vendor.yaml
```

`--output text` prints the human report only; `--output yaml` prints only the
starter profile (pipe it to a file); `--output both` (default) prints both. The
probe stamps `match.vendor` from the BMC manufacturer, suggests a
`mediaSearch.order` when the only CD/DVD media is Manager-hosted (the HPE iLO
signal), and lists the allowable `ResetType`s as comments.

Then iterate: drop the file in a directory, point `--quirks-dir` at it, and run a
real `redfish deploy --vendor <name>` until it works on your hardware.

---

## 2. Write / tweak a profile: the schema

Start from `examples/redfish/quirks/template.yaml` (every field documented) or the
worked `examples/redfish/quirks/ilo.yaml`. Every section is **optional except
`name`**; an omitted section means "use the spec-default behaviour", so a profile
with only a name behaves exactly like the built-in `generic` (spec-default) profile.

```yaml
name: my-vendor                  # REQUIRED — the selection key (see §3)
match:                           # optional, informational hint only
  vendor: ExampleVendor          #   does NOT drive selection (selection is by name)
mediaSearch:                     # optional — reorder/filter VirtualMedia members
  order: [manager, system]       #   entries: "system" | "manager" | "manager:<id>"
mediaType: CD                    # optional — CD | DVD | USBStick | Floppy (default CD)
resetType:                       # optional — ordered, first-match-wins rules
  - when: { powerState: "Off" }  #   powerState: "On" | "Off" | "*"
    then: "On"                   #   then: a Redfish ResetType, re-validated by the core
  - when: { powerState: "*" }
    then: "ForceRestart"
tuneInsertParams:                # optional — sparse patch over an ALLOWLISTED field set
  clear: [TransferProtocolType]  #   allowlist: MediaType, TransferProtocolType,
  set:                           #              Inserted, WriteProtected
    WriteProtected: true
validatedFirmware: "fw 1.23"     # optional — see §6 (firmware labeling)
```

Field reference:

- **`name`** (string, required) — the profile's identity and selection key.
- **`match.vendor`** (string, optional) — an informational hint. Selection is by
  `name`, not by `match`; `match` is documentation the probe fills in for you.
- **`mediaSearch.order`** (list, optional) — reorders/filters candidate
  `VirtualMedia` members. Each entry is one of `system`, `manager`, or
  `manager:<id>`. Matching members are emitted in the listed order; members
  matching no entry are **dropped**. Omit to keep the core's default order. (HPE
  iLO hosts media under the Manager, so its profile uses `[manager, system]`.)
- **`mediaType`** (enum, optional) — the `MediaType` sent on InsertMedia. One of
  `CD`, `DVD`, `USBStick`, `Floppy`. Omit for the spec default (`CD`).
- **`resetType`** (list of rules, optional) — first-match-wins. `when.powerState`
  is `On`, `Off`, or `*` (any); `then` is a Redfish `ResetType` (`On`,
  `ForceRestart`, `GracefulRestart`, `ForceOff`, `PowerCycle`, …). The chosen type
  is **re-validated by the core** against the BMC's advertised allowable values — a
  rule can prefer, but never force, an unsupported type. Omit for the core default.
- **`tuneInsertParams`** (optional) — a sparse patch over the InsertMedia
  parameters. `clear` drops fields from the request, `set` sets them; a field in
  both is dropped. **The allowlist is exactly `MediaType`, `TransferProtocolType`,
  `Inserted`, `WriteProtected`.** `set` values are checked at load: `MediaType` is
  one of `CD`, `DVD`, `USBStick`, `Floppy`; `TransferProtocolType` is one of `CIFS`,
  `FTP`, `SFTP`, `HTTP`, `HTTPS`, `NFS`, `SCP`, `TFTP`, `OEM`; `Inserted` and
  `WriteProtected` are booleans.
- **`validatedFirmware`** (string, optional) — see §6.

### The safety boundary (what a profile can never do)

Validation is strict and happens **at load** (unknown keys, bad enums, and
non-allowlisted fields are rejected before any hook runs). A profile can **never**:

- **Set or clear the `Image` URL.** `Image` is rejected anywhere it appears
  (`tuneInsertParams.set`/`clear`, any casing) with a dedicated error. The image
  URL is core-owned and SSRF-validated; letting a profile set it would reopen a
  confused-deputy / SSRF hole.
- **Force an unsupported `ResetType`.** Every chosen type flows back through the
  core's allowable-values check.
- **Touch sessions, TLS verification, or system selection** — a profile has no
  client handle and sees only read-only value copies of the relevant data.

A malformed profile is a **load-time error that disables only that profile**; it
never fails the rest of the load or the deploy.

---

## 3. Where profiles live, and how `--quirks-dir` works

Built-in profiles (`generic`, `ilo`, `supermicro`) ship in the binary. Operator
profiles are loaded from a directory of `*.yaml` / `*.yml` files at **start**
(load-at-start, never hot-reloaded — an in-flight deploy must not have its profile
swapped):

- **CLI** — `auroraboot redfish deploy --quirks-dir ./quirks --vendor my-vendor …`
  (and `auroraboot redfish probe --quirks-dir ./quirks` to load+validate them).
  Env: `AURORABOOT_REDFISH_QUIRKS_DIR`.
- **Fleet server** — `auroraboot web --redfish-quirks-dir ./quirks …`. A
  `BMCTarget`'s `vendor` selects a profile by name. Env:
  `AURORABOOT_REDFISH_QUIRKS_DIR`.

Each loaded profile and each skipped (malformed) file is logged at start.

---

## 4. Selection & precedence

- **Selection is by name.** `--vendor <name>` (CLI) or a `BMCTarget.vendor`
  (server) resolves, in order: an operator/built-in profile **by name**, then the
  built-in **vendor mapping**, then **`generic`**. A typo or unknown name resolves
  to `generic` — never a silent wrong workaround.
- **Precedence: operator overrides built-in.** An operator profile whose `name`
  collides with a built-in (e.g. `ilo`) **overrides** the built-in. Operator intent
  wins, and the override is logged loudly, e.g.:

  ```
  redfish: operator quirk profile "ilo" overrides the built-in [tier C: UNVERIFIED — no recorded mockup]
  ```

---

## 5. Support tiers (and how to reach tier B)

The **project derives** a profile's support tier; a profile can never assert its
own. The tier is surfaced in a load-time log line, e.g.
`redfish: loaded quirk profile "ilo" [tier C: UNVERIFIED — no recorded mockup]`.

| Tier | What it means | Where it comes from |
|---|---|---|
| **A — core-tested** | The spec-default path the project exercises in CI. | The built-in `generic` profile. |
| **B — community-validated** | A profile **with a recorded, sanitized hardware mockup** that CI replays on every PR, so regressions are caught. | An in-tree profile + its mockup under `pkg/redfish/testdata/mockups/<vendor>/` (the built-in `ilo` is tier B today). |
| **C — unverified** | A bare profile with no recorded mockup, in-tree or operator-supplied. Loaded fine, logged as UNVERIFIED. | Operator-dir profiles (always C) and in-tree profiles without a mockup. |

**Operator-supplied profiles are always tier C.** To reach **tier B**, contribute
the profile in-tree together with a recorded, sanitized DMTF mockup of your BMC's
resource tree, which CI replays against the deploy flow on every PR. The mockup
format, location, prune list, sanitization expectation, and the golden-test harness
are documented in
[`pkg/redfish/testdata/mockups/README.md`](../pkg/redfish/testdata/mockups/README.md).
Promotion is one step: add the sanitized mockup tree, list the built-in in
`pkg/redfish/mockups.manifest`, and add a golden-test row — add evidence, get a
guarantee.

---

## 6. Firmware labeling (`validatedFirmware`)

A profile is validated against a **specific firmware revision** — Redfish behaviour
changes across firmware. Record the revision you tested against in
`validatedFirmware`, e.g. `"iLO 5 2.44"` or `"Supermicro X12 1.04"`. A different
firmware revision is a different evidence pair; label honestly so operators know
what your profile was proven against.

---

## 7. Contributing back

Open a PR using the **vendor-profile PR template**
(`.github/PULL_REQUEST_TEMPLATE/vendor-profile.md`). The checklist covers:
schema-valid profile, `validatedFirmware` labeled, and — for tier B — a sanitized
mockup tree under `pkg/redfish/testdata/mockups/<vendor>/` plus a golden-test row
that replays it (see
[`pkg/redfish/testdata/mockups/README.md`](../pkg/redfish/testdata/mockups/README.md)).

Put the profile under `examples/redfish/quirks/` if it is an example/template, or
in-tree alongside the built-ins if you are submitting it as a supported vendor
profile (it ships as tier C until a recorded mockup + golden test promote it to B).
