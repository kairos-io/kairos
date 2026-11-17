import { apiFetch } from "./client";

// QuirkProfile mirrors one entry of the server's GET
// /api/v1/redfish/quirk-profiles response: a Redfish vendor quirk profile loaded
// at server start (built-in or operator-supplied via --redfish-quirks-dir), with
// its project-derived support tier. The UI uses these to populate the BMC vendor
// selector — so an operator's own profile is selectable — instead of a hardcoded
// list, and to show each profile's support tier.
export interface QuirkProfile {
  // name is the selection name a BMCTarget's vendor matches.
  name: string;
  // tier is the support tier letter: "A" (core-tested), "B" (community-validated),
  // or "C" (unverified). A profile never asserts its own tier; the server derives
  // it.
  tier: string;
  // tierDescription is the human-readable annotation, e.g.
  // "tier C: UNVERIFIED — no recorded mockup".
  tierDescription: string;
  // origin is "builtin" or "operator".
  origin: string;
  // validatedFirmware is the informational firmware label the profile declared, if
  // any (operator profiles typically; empty for built-ins without evidence).
  validatedFirmware?: string;
}

// listQuirkProfiles returns the Redfish quirk profiles loaded into the server,
// sorted by name.
export function listQuirkProfiles(): Promise<QuirkProfile[]> {
  return apiFetch<QuirkProfile[]>("/api/v1/redfish/quirk-profiles");
}
