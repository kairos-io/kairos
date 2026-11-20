import { describe, it, expect } from "vitest";

import type { Node } from "@/api/nodes";
import type { Group } from "@/api/groups";
import {
  parseQuery,
  toSearchParams,
  matchesSearch,
  filterNodes,
  groupNodes,
  hasActiveFilter,
} from "@/lib/nodeFilter";

function node(overrides: Partial<Node> = {}): Node {
  return {
    id: "n1",
    hostname: "edge-1",
    machineID: "m1",
    groupID: null,
    labels: {},
    phase: "Online",
    osRelease: null,
    agentVersion: "",
    lastHeartbeat: null,
    createdAt: "",
    updatedAt: "",
    ...overrides,
  };
}

const groups: Group[] = [
  { id: "g1", name: "edge-retail", description: "", createdAt: "", updatedAt: "" },
  { id: "g2", name: "lab", description: "", createdAt: "", updatedAt: "" },
];

describe("parseQuery and toSearchParams", () => {
  it("reads every key and defaults the rest", () => {
    const q = parseQuery(new URLSearchParams("q=pos&phase=Offline&groupBy=site"));
    expect(q).toEqual({ q: "pos", phase: "Offline", group: "", label: "", groupBy: "site" });
    expect(parseQuery(new URLSearchParams("")).groupBy).toBe("none");
  });

  it("writes only the keys that are set", () => {
    const sp = toSearchParams({ q: "pos", phase: "", group: "g1", label: "", groupBy: "none" });
    expect(sp.toString()).toBe("q=pos&group=g1");
    expect(parseQuery(toSearchParams(parseQuery(new URLSearchParams("label=site%3Dmilan&groupBy=group"))))).toEqual({
      q: "",
      phase: "",
      group: "",
      label: "site=milan",
      groupBy: "group",
    });
  });
});

describe("matchesSearch", () => {
  it("matches the remote IP and the reported addresses", () => {
    const n = node({ remoteIP: "10.0.4.21", addresses: [{ type: "internal", address: "172.16.4.9" }] });
    expect(matchesSearch(n, "10.0.4")).toBe(true);
    expect(matchesSearch(n, "172.16")).toBe(true);
    expect(matchesSearch(n, "192.168")).toBe(false);
  });

  it("matches label keys and values, hostname and image version, ignoring case", () => {
    const n = node({
      hostname: "POS-Milan-01",
      labels: { site: "Milan" },
      osRelease: { KAIROS_VERSION: "edge-2026.09" },
    });
    expect(matchesSearch(n, "milan")).toBe(true);
    expect(matchesSearch(n, "SITE")).toBe(true);
    expect(matchesSearch(n, "pos-milan")).toBe(true);
    expect(matchesSearch(n, "2026.09")).toBe(true);
    expect(matchesSearch(n, "rome")).toBe(false);
    expect(matchesSearch(n, "")).toBe(true);
  });
});

describe("filterNodes", () => {
  const fleet = [
    node({ id: "a", phase: "Online", groupID: "g1", labels: { site: "milan" } }),
    node({ id: "b", phase: "offline", groupID: null, labels: { role: "worker" } }),
    node({ id: "c", phase: "Offline", groupID: "g2", labels: { site: "rome" } }),
  ];
  const base = { q: "", phase: "", group: "", label: "", groupBy: "none" };

  it("filters by phase without caring about case", () => {
    expect(filterNodes(fleet, { ...base, phase: "Offline" }).map((n) => n.id)).toEqual(["b", "c"]);
  });

  it("filters by group, with ungrouped for nodes in no group", () => {
    expect(filterNodes(fleet, { ...base, group: "g1" }).map((n) => n.id)).toEqual(["a"]);
    expect(filterNodes(fleet, { ...base, group: "ungrouped" }).map((n) => n.id)).toEqual(["b"]);
  });

  it("filters by key=value or by a bare key", () => {
    expect(filterNodes(fleet, { ...base, label: "site=rome" }).map((n) => n.id)).toEqual(["c"]);
    expect(filterNodes(fleet, { ...base, label: "site" }).map((n) => n.id)).toEqual(["a", "c"]);
  });

  it("reports whether any filter is active", () => {
    expect(hasActiveFilter(base)).toBe(false);
    expect(hasActiveFilter({ ...base, groupBy: "group" })).toBe(false);
    expect(hasActiveFilter({ ...base, q: "x" })).toBe(true);
    expect(hasActiveFilter({ ...base, label: "site" })).toBe(true);
  });
});

describe("groupNodes", () => {
  it("groups by a label key and puts nodes without it last", () => {
    const fleet = [
      node({ id: "a", labels: { site: "rome" } }),
      node({ id: "b", labels: {} }),
      node({ id: "c", labels: { site: "milan" } }),
      node({ id: "d", labels: { site: "milan" } }),
    ];
    const out = groupNodes(fleet, "site", groups);
    expect(out.map((g) => g.label)).toEqual(["milan", "rome", "No site"]);
    expect(out[0].nodes.map((n) => n.id)).toEqual(["c", "d"]);
    expect(out[2].nodes.map((n) => n.id)).toEqual(["b"]);
  });

  it("groups by group, in group order, with Not in a group last", () => {
    const fleet = [
      node({ id: "a", groupID: null }),
      node({ id: "b", groupID: "g2" }),
      node({ id: "c", groupID: "g1" }),
    ];
    const out = groupNodes(fleet, "group", groups);
    expect(out.map((g) => g.label)).toEqual(["edge-retail", "lab", "Not in a group"]);
    expect(out.map((g) => g.key)).toEqual(["g1", "g2", "ungrouped"]);
  });

  it("returns one bucket when not grouping", () => {
    const fleet = [node({ id: "a" }), node({ id: "b" })];
    const out = groupNodes(fleet, "none", groups);
    expect(out).toHaveLength(1);
    expect(out[0].nodes).toHaveLength(2);
  });
});
