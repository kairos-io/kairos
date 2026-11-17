<!--
Use this template for a Redfish vendor quirk-profile contribution.
See docs/CONTRIBUTING-vendor-profiles.md for the full guide.
-->

## Vendor quirk profile

**Vendor / model:**
**Profile name (`name:` field):**
**Firmware validated against (`validatedFirmware:`):**

### What this profile changes vs. the spec-default (`generic`) flow

<!-- e.g. "prefers Manager-hosted virtual media (iLO hosts media under the Manager)". -->

### How it was validated

<!-- e.g. "deployed successfully against <model> running <firmware> in my lab". -->

### Checklist

- [ ] Profile is **schema-valid** — parses with `auroraboot redfish probe --quirks-dir`
      (or `LoadProfileDir`); no unknown keys, valid enums, no non-allowlisted
      `tuneInsertParams` fields (the `Image` field is rejected by design).
- [ ] `validatedFirmware` is **labeled** with the exact firmware revision tested.
- [ ] The profile **does not** attempt to set the `Image` URL, force an unsupported
      `ResetType`, or otherwise reach past the documented allowlist.
- [ ] **(once P4 lands)** A sanitized, recorded DMTF **mockup** of the BMC resource
      tree is included under `pkg/redfish/testdata/mockups/<vendor>/<firmware>/`,
      and I attest it has been **sanitized** (serials, MACs, IPs, asset tags redacted).
- [ ] **(once P4 lands)** A **golden test** replays the mockup against the deploy
      flow and is green, so this profile reaches **tier B**.

> New operator-supplied / in-tree-without-mockup profiles ship as **tier C
> (UNVERIFIED)**. Adding a sanitized mockup + golden test (P4) promotes the profile
> to **tier B (community-validated)**.
