import { describe, it, expect } from "vitest";

import type { Node } from "@/api/nodes";
import type { Artifact } from "@/api/artifacts";
import type { Deployment } from "@/api/deployments";
import type { Extension } from "@/api/extensions";
import { buildActivity, needsAttention } from "@/lib/activity";

function node(o: Partial<Node> = {}): Node {
  return {
    id: "n1",
    hostname: "edge-1",
    machineID: "m1",
    groupID: "g1",
    labels: {},
    phase: "Online",
    osRelease: null,
    agentVersion: "",
    lastHeartbeat: "2026-09-28T10:00:00Z",
    createdAt: "2026-09-20T10:00:00Z",
    updatedAt: "2026-09-28T10:00:00Z",
    bootState: "active",
    ...o,
  };
}

function artifact(o: Partial<Artifact> = {}): Artifact {
  return {
    id: "a1",
    name: "img",
    phase: "Ready",
    message: "",
    createdAt: "2026-09-21T10:00:00Z",
    updatedAt: "2026-09-21T11:00:00Z",
    ...o,
  } as Artifact;
}

function extension(o: Partial<Extension> = {}): Extension {
  return {
    id: "e1",
    name: "tailscale",
    type: "sysext",
    phase: "Ready",
    message: "",
    arch: "amd64",
    version: "1.0",
    sourceMode: "image",
    createdAt: "2026-09-22T10:00:00Z",
    updatedAt: "2026-09-22T11:00:00Z",
    ...o,
  } as Extension;
}

function deployment(o: Partial<Deployment> = {}): Deployment {
  return {
    id: "d1",
    artifactId: "a1",
    method: "redfish",
    status: "Active",
    message: "",
    bmcTargetId: "bmc-1",
    progress: 0,
    startedAt: "2026-09-23T10:00:00Z",
    ...o,
  };
}

describe("buildActivity", () => {
  it("returns events newest first", () => {
    const events = buildActivity({
      nodes: [node({ createdAt: "2026-09-25T10:00:00Z" })],
      artifacts: [artifact({ phase: "Building", createdAt: "2026-09-27T10:00:00Z", updatedAt: "2026-09-27T10:00:00Z" })],
      deployments: [deployment({ startedAt: "2026-09-26T10:00:00Z" })],
      extensions: [extension({ phase: "Building", createdAt: "2026-09-24T10:00:00Z", updatedAt: "2026-09-24T10:00:00Z" })],
    });
    expect(events.map((e) => e.kind)).toEqual(["build", "deploy", "node", "extension"]);
    const times = events.map((e) => new Date(e.time).getTime());
    expect([...times].sort((a, b) => b - a)).toEqual(times);
  });

  it("keeps at most 10 events", () => {
    const nodes = Array.from({ length: 15 }, (_, i) =>
      node({ id: `n${i}`, createdAt: `2026-09-${String(10 + i).padStart(2, "0")}T10:00:00Z` }),
    );
    const events = buildActivity({ nodes, artifacts: [], deployments: [], extensions: [] });
    expect(events).toHaveLength(10);
    expect(events[0].id).toContain("n14");
  });

  it("adds ready and failed events and a heartbeat event for offline nodes", () => {
    const events = buildActivity({
      nodes: [node({ id: "off", phase: "Offline" })],
      artifacts: [artifact({ id: "ok" }), artifact({ id: "bad", phase: "Error" })],
      deployments: [],
      extensions: [extension({ id: "xbad", phase: "Error" })],
    });
    const tones = Object.fromEntries(events.map((e) => [e.id, e.tone]));
    expect(Object.values(tones)).toContain("success");
    expect(events.some((e) => e.kind === "build" && e.tone === "danger")).toBe(true);
    expect(events.some((e) => e.kind === "extension" && e.tone === "danger")).toBe(true);
    expect(events.some((e) => e.kind === "node" && e.tone === "danger")).toBe(true);
    expect(new Set(events.map((e) => e.id)).size).toBe(events.length);
  });
});

describe("needsAttention", () => {
  it("returns nothing for a healthy fleet", () => {
    expect(
      needsAttention({
        nodes: [node(), node({ id: "n2", hostname: "edge-2" })],
        artifacts: [artifact()],
        extensions: [extension()],
      }),
    ).toEqual([]);
  });

  it("lists offline, recovery, ungrouped nodes and failed builds", () => {
    const rows = needsAttention({
      nodes: [
        node({ id: "off", hostname: "pos-2", phase: "Offline" }),
        node({ id: "rec", hostname: "plc-gw", bootState: "recovery" }),
        node({ id: "new", hostname: "lab-box", groupID: null }),
      ],
      artifacts: [artifact({ id: "abad", name: "kiosk", phase: "Error" })],
      extensions: [extension({ id: "xbad", name: "plc-ext", phase: "Error" })],
    });
    const byLink = Object.fromEntries(rows.map((r) => [r.link, r]));
    expect(byLink["/nodes/off"].title).toContain("pos-2");
    expect(byLink["/nodes/off"].tone).toBe("danger");
    expect(byLink["/nodes/rec"].title).toContain("recovery");
    expect(byLink["/artifacts/abad"].tone).toBe("danger");
    expect(byLink["/extensions/xbad"].title).toContain("plc-ext");
    expect(byLink["/groups"].title).toContain("lab-box");
    expect(byLink["/groups"].actionLabel).toBe("Assign");
    expect(rows).toHaveLength(5);
  });
});
