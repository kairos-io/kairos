// Search, filter and grouping for the node list. The query lives in the URL
// (q, phase, group, label, groupBy) so a filtered view can be shared as a link.

import type { Node } from "@/api/nodes";
import type { Group } from "@/api/groups";
import { imageVersion } from "@/lib/nodeInfo";
import { phaseTone, type Tone } from "@/lib/phase";

export type NodeQuery = {
  q: string;
  phase: string;
  group: string;
  label: string;
  // "none", "group", or a label key such as "site".
  groupBy: "none" | "group" | string;
};

// The group filter value for nodes that are in no group.
export const UNGROUPED = "ungrouped";

export const queryKeys = ["q", "phase", "group", "label", "groupBy"] as const;

export function parseQuery(sp: URLSearchParams): NodeQuery {
  return {
    q: sp.get("q") ?? "",
    phase: sp.get("phase") ?? "",
    group: sp.get("group") ?? "",
    label: sp.get("label") ?? "",
    groupBy: sp.get("groupBy") || "none",
  };
}

export function toSearchParams(q: NodeQuery): URLSearchParams {
  const sp = new URLSearchParams();
  if (q.q) sp.set("q", q.q);
  if (q.phase) sp.set("phase", q.phase);
  if (q.group) sp.set("group", q.group);
  if (q.label) sp.set("label", q.label);
  if (q.groupBy && q.groupBy !== "none") sp.set("groupBy", q.groupBy);
  return sp;
}

// hasActiveFilter is true when the query hides any node. Grouping hides none.
export function hasActiveFilter(q: NodeQuery): boolean {
  return !!(q.q || q.phase || q.group || q.label);
}

export function matchesSearch(node: Node, q: string): boolean {
  const needle = q.trim().toLowerCase();
  if (!needle) return true;
  const fields = [
    node.hostname,
    node.remoteIP ?? "",
    imageVersion(node),
    ...(node.addresses ?? []).map((a) => a.address),
    ...Object.entries(node.labels ?? {}).flat(),
  ];
  return fields.some((f) => (f ?? "").toLowerCase().includes(needle));
}

// splitLabel reads "key=value" or a bare "key".
export function splitLabel(label: string): { key: string; value?: string } {
  const i = label.indexOf("=");
  if (i < 0) return { key: label.trim() };
  return { key: label.slice(0, i).trim(), value: label.slice(i + 1).trim() };
}

function matchesLabel(node: Node, label: string): boolean {
  const { key, value } = splitLabel(label);
  if (!key) return true;
  const labels = node.labels ?? {};
  if (!(key in labels)) return false;
  return value === undefined || labels[key] === value;
}

export function filterNodes(nodes: Node[], q: NodeQuery): Node[] {
  const phase = q.phase.toLowerCase();
  return nodes.filter((n) => {
    if (phase && (n.phase ?? "").toLowerCase() !== phase) return false;
    if (q.group === UNGROUPED ? !!n.groupID : q.group && n.groupID !== q.group) return false;
    if (q.label && !matchesLabel(n, q.label)) return false;
    return matchesSearch(n, q.q);
  });
}

export type NodeBucket = { key: string; label: string; nodes: Node[] };

export function groupNodes(nodes: Node[], by: string, groups: Group[]): NodeBucket[] {
  if (!by || by === "none") return [{ key: "all", label: "All nodes", nodes }];

  if (by === "group") {
    const buckets = new Map<string, NodeBucket>();
    for (const g of groups) buckets.set(g.id, { key: g.id, label: g.name, nodes: [] });
    const ungrouped: NodeBucket = { key: UNGROUPED, label: "Not in a group", nodes: [] };
    for (const n of nodes) {
      if (!n.groupID) {
        ungrouped.nodes.push(n);
        continue;
      }
      let b = buckets.get(n.groupID);
      if (!b) {
        b = { key: n.groupID, label: n.group?.name || n.groupID, nodes: [] };
        buckets.set(n.groupID, b);
      }
      b.nodes.push(n);
    }
    return [...buckets.values(), ungrouped].filter((b) => b.nodes.length > 0);
  }

  const buckets = new Map<string, NodeBucket>();
  const missing: NodeBucket = { key: `no-${by}`, label: `No ${by}`, nodes: [] };
  for (const n of nodes) {
    const v = n.labels?.[by];
    if (v === undefined || v === "") {
      missing.nodes.push(n);
      continue;
    }
    let b = buckets.get(v);
    if (!b) {
      b = { key: `${by}=${v}`, label: v, nodes: [] };
      buckets.set(v, b);
    }
    b.nodes.push(n);
  }
  const sorted = [...buckets.values()].sort((a, b) => a.label.localeCompare(b.label));
  return [...sorted, missing].filter((b) => b.nodes.length > 0);
}

const phaseOrder = ["online", "offline", "pending"];

// phaseCounts counts nodes per phase, ignoring case, in a stable order:
// online, offline, pending, then the rest by name. `label` is lower case for
// StackBar ("2 online, 1 offline"); `name` is for a legend.
export function phaseCounts(nodes: Node[]): { tone: Tone; count: number; label: string; name: string }[] {
  const byPhase = new Map<string, { name: string; count: number }>();
  for (const n of nodes) {
    const name = n.phase || "Unknown";
    const key = name.toLowerCase();
    const cur = byPhase.get(key);
    if (cur) cur.count++;
    else byPhase.set(key, { name: name.charAt(0).toUpperCase() + name.slice(1), count: 1 });
  }
  const rank = (k: string) => {
    const i = phaseOrder.indexOf(k);
    return i < 0 ? phaseOrder.length : i;
  };
  return [...byPhase.entries()]
    .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
    .map(([key, v]) => ({ tone: phaseTone(key), count: v.count, label: key, name: v.name }));
}
