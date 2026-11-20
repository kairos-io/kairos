// Builds the dashboard's activity feed and "needs attention" list out of the
// records the UI already loads. Nothing here talks to the server.

import type { Node } from "@/api/nodes";
import type { Artifact } from "@/api/artifacts";
import type { Deployment } from "@/api/deployments";
import type { Extension } from "@/api/extensions";
import type { NodeMetrics } from "@/api/metrics";
import { hasBootIssue } from "@/lib/nodeInfo";
import { isBuilding, isFailed, isOffline, isReady, type Tone } from "@/lib/phase";
import { timeAgo } from "@/lib/time";
import { cpuPercent, diskPercent, DANGER_PERCENT } from "@/lib/metrics";

export type ActivityKind = "build" | "node" | "deploy" | "extension";

export type ActivityEvent = {
  id: string;
  kind: ActivityKind;
  tone: Tone;
  title: string;
  detail: string;
  time: string;
  link: string;
};

export type AttentionKind = "boot" | "offline" | "cpu" | "disk" | "build" | "extension" | "ungrouped";

export type AttentionItem = {
  id: string;
  kind: AttentionKind;
  tone: Tone;
  title: string;
  detail: string;
  actionLabel: string;
  link: string;
};

const MAX_EVENTS = 10;

function nodeName(n: Node): string {
  return n.hostname || n.machineID || n.id.slice(0, 8);
}

function artifactName(a: Artifact): string {
  return a.name || a.id.slice(0, 8);
}

function ms(time: string | null | undefined): number {
  const t = time ? new Date(time).getTime() : NaN;
  return Number.isNaN(t) ? 0 : t;
}

// Artifacts and extensions share a lifecycle: one event when the build was
// created, and one more when it finished ready or failed.
function buildEvents(
  kind: "build" | "extension",
  id: string,
  name: string,
  phase: string,
  message: string,
  createdAt: string,
  updatedAt: string,
  link: string,
): ActivityEvent[] {
  const noun = kind === "build" ? "Build" : "Extension build";
  const events: ActivityEvent[] = [
    {
      id: `${kind}-${id}-created`,
      kind,
      tone: isBuilding(phase) ? "info" : "neutral",
      title: `${noun} started: ${name}`,
      detail: isBuilding(phase) ? "Building" : "",
      time: createdAt,
      link,
    },
  ];
  if (isReady(phase)) {
    events.push({
      id: `${kind}-${id}-ready`,
      kind,
      tone: "success",
      title: `${noun} ready: ${name}`,
      detail: "",
      time: updatedAt || createdAt,
      link,
    });
  } else if (isFailed(phase)) {
    events.push({
      id: `${kind}-${id}-failed`,
      kind,
      tone: "danger",
      title: `${noun} failed: ${name}`,
      detail: message || "",
      time: updatedAt || createdAt,
      link,
    });
  }
  return events;
}

function deploymentTone(status: string): Tone {
  const s = status.toLowerCase();
  if (s === "active" || s === "running") return "info";
  if (s === "completed") return "success";
  if (s === "failed" || s === "error") return "danger";
  return "neutral";
}

// buildActivity merges builds, extensions, deployments and nodes into one
// feed, newest first, at most 10 events.
export function buildActivity(input: {
  nodes: Node[];
  artifacts: Artifact[];
  deployments: Deployment[];
  extensions: Extension[];
}): ActivityEvent[] {
  const events: ActivityEvent[] = [];

  for (const a of input.artifacts) {
    events.push(
      ...buildEvents("build", a.id, artifactName(a), a.phase, a.message, a.createdAt, a.updatedAt, `/artifacts/${a.id}`),
    );
  }
  for (const e of input.extensions) {
    events.push(
      ...buildEvents("extension", e.id, e.name || e.id.slice(0, 8), e.phase, e.message, e.createdAt, e.updatedAt, `/extensions/${e.id}`),
    );
  }
  for (const d of input.deployments) {
    events.push({
      id: `deploy-${d.id}`,
      kind: "deploy",
      tone: deploymentTone(d.status),
      title: `Deployment started: ${d.method || "deploy"}`,
      detail: d.bmcTargetId ? `Target ${d.bmcTargetId}` : d.message || "",
      time: d.startedAt,
      link: "/deployments",
    });
  }
  for (const n of input.nodes) {
    events.push({
      id: `node-${n.id}-registered`,
      kind: "node",
      tone: "neutral",
      title: `Node registered: ${nodeName(n)}`,
      detail: n.group?.name ? `Group ${n.group.name}` : "",
      time: n.createdAt,
      link: `/nodes/${n.id}`,
    });
    if (isOffline(n.phase) && n.lastHeartbeat) {
      events.push({
        id: `node-${n.id}-offline`,
        kind: "node",
        tone: "danger",
        title: `Last heartbeat from ${nodeName(n)}`,
        detail: "The node is offline",
        time: n.lastHeartbeat,
        link: `/nodes/${n.id}`,
      });
    }
  }

  return events
    .filter((e) => ms(e.time) > 0)
    .sort((a, b) => ms(b.time) - ms(a.time))
    .slice(0, MAX_EVENTS);
}

// needsAttention lists the problems an operator should act on. It is empty
// for a healthy fleet.
export function needsAttention(input: {
  nodes: Node[];
  artifacts: Artifact[];
  extensions: Extension[];
  // Latest agent metrics by node ID. Nodes without an entry add no rows.
  metrics?: Record<string, NodeMetrics>;
}): AttentionItem[] {
  const rows: AttentionItem[] = [];

  for (const n of input.nodes) {
    if (hasBootIssue(n)) {
      const state = (n.bootState ?? "").toLowerCase();
      rows.push({
        id: `boot-${n.id}`,
        kind: "boot",
        tone: "warning",
        title: `${nodeName(n)} booted the ${state} image`,
        detail: "The node did not boot its active image",
        actionLabel: "View node",
        link: `/nodes/${n.id}`,
      });
    }
  }
  for (const n of input.nodes) {
    if (isOffline(n.phase)) {
      rows.push({
        id: `offline-${n.id}`,
        kind: "offline",
        tone: "danger",
        title: `${nodeName(n)} is offline`,
        detail: n.lastHeartbeat ? `Last heartbeat ${timeAgo(n.lastHeartbeat)}` : "No heartbeat yet",
        actionLabel: "View node",
        link: `/nodes/${n.id}`,
      });
    }
  }
  if (input.metrics) {
    for (const n of input.nodes) {
      const m = input.metrics[n.id];
      if (!m) continue;
      const cpu = cpuPercent(m);
      if (cpu !== null && cpu >= DANGER_PERCENT) {
        rows.push({
          id: `cpu-${n.id}`,
          kind: "cpu",
          tone: "danger",
          title: `${nodeName(n)} CPU at ${cpu}%`,
          detail: m.load?.length ? `Load ${m.load[0].toFixed(1)}` : "High CPU usage",
          actionLabel: "View node",
          link: `/nodes/${n.id}`,
        });
      }
      const fullest = [...(m.disks ?? [])].sort((a, b) => diskPercent(b) - diskPercent(a))[0];
      if (fullest && diskPercent(fullest) >= DANGER_PERCENT) {
        const gib = (b: number) => (b / 1024 ** 3).toFixed(1);
        rows.push({
          id: `disk-${n.id}`,
          kind: "disk",
          tone: "danger",
          title: `${nodeName(n)} disk at ${diskPercent(fullest)}%`,
          detail: `${fullest.label || fullest.mount} · ${gib(fullest.usedBytes)} of ${gib(fullest.totalBytes)} GiB`,
          actionLabel: "View node",
          link: `/nodes/${n.id}`,
        });
      }
    }
  }
  for (const a of input.artifacts) {
    if (isFailed(a.phase)) {
      rows.push({
        id: `build-${a.id}`,
        kind: "build",
        tone: "danger",
        title: `Build failed: ${artifactName(a)}`,
        detail: a.message || `Failed ${timeAgo(a.updatedAt || a.createdAt)}`,
        actionLabel: "View build",
        link: `/artifacts/${a.id}`,
      });
    }
  }
  for (const e of input.extensions) {
    if (isFailed(e.phase)) {
      rows.push({
        id: `extension-${e.id}`,
        kind: "extension",
        tone: "danger",
        title: `Extension build failed: ${e.name || e.id.slice(0, 8)}`,
        detail: e.message || `Failed ${timeAgo(e.updatedAt || e.createdAt)}`,
        actionLabel: "View extension",
        link: `/extensions/${e.id}`,
      });
    }
  }
  for (const n of input.nodes) {
    if (!n.groupID && !n.group?.id) {
      rows.push({
        id: `ungrouped-${n.id}`,
        kind: "ungrouped",
        tone: "neutral",
        title: `${nodeName(n)} is not in a group`,
        detail: `Registered ${timeAgo(n.createdAt)}`,
        actionLabel: "Assign",
        link: "/groups",
      });
    }
  }
  return rows;
}
