import { Link } from "react-router";
import type { Node } from "@/api/nodes";
import { isOffline, type Tone } from "@/lib/phase";
import { phaseCounts } from "@/lib/nodeFilter";
import { hasBootIssue, imageVersion } from "@/lib/nodeInfo";
import { timeAgo } from "@/lib/time";
import { StackBar } from "./StackBar";
import { StatusDot } from "./StatusDot";
import { toneBg } from "./tones";

const maxVersions = 4;

function versionCounts(nodes: Node[]): { version: string; count: number }[] {
  const counts = new Map<string, number>();
  for (const n of nodes) {
    const v = imageVersion(n) || "Unknown";
    counts.set(v, (counts.get(v) ?? 0) + 1);
  }
  const sorted = [...counts.entries()]
    .map(([version, count]) => ({ version, count }))
    .sort((a, b) => b.count - a.count || a.version.localeCompare(b.version));
  if (sorted.length <= maxVersions) return sorted;
  const head = sorted.slice(0, maxVersions - 1);
  const rest = sorted.slice(maxVersions - 1).reduce((s, v) => s + v.count, 0);
  return [...head, { version: "Other", count: rest }];
}

interface AttentionRow {
  node: Node;
  tone: Tone;
  reason: string;
}

function attention(nodes: Node[]): AttentionRow[] {
  const rows: AttentionRow[] = [];
  for (const n of nodes) {
    if (isOffline(n.phase)) {
      rows.push({
        node: n,
        tone: "danger",
        reason: n.lastHeartbeat ? `offline, last seen ${timeAgo(n.lastHeartbeat)}` : "offline",
      });
    } else if (hasBootIssue(n)) {
      rows.push({ node: n, tone: "warning", reason: `booted ${(n.bootState ?? "").toLowerCase()}` });
    }
  }
  return rows;
}

// NodeSummary describes the whole fleet: status, image versions and the nodes
// that need attention (offline or not on their active image).
export function NodeSummary({ nodes }: { nodes: Node[] }) {
  const phases = phaseCounts(nodes);
  const versions = versionCounts(nodes);
  const attn = attention(nodes);
  const maxCount = Math.max(1, ...versions.map((v) => v.count));

  return (
    <div className="mb-6 grid gap-4 rounded-lg border bg-card p-4 md:grid-cols-3">
      <section aria-label="Status" className="min-w-0">
        <div className="mb-2 flex items-center justify-between text-sm text-muted-foreground">
          <span>Status</span>
          <span className="font-medium text-foreground">{nodes.length}</span>
        </div>
        <StackBar counts={phases} />
        <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          {phases.map((p) => (
            <li key={p.label} className="flex items-center gap-1.5">
              <StatusDot tone={p.tone} />
              {p.name} <span className="font-medium text-foreground">{p.count}</span>
            </li>
          ))}
        </ul>
      </section>

      <section aria-label="Image version" className="min-w-0">
        <div className="mb-2 text-sm text-muted-foreground">Image version</div>
        {versions.length === 0 ? (
          <p className="text-xs text-muted-foreground">No nodes yet</p>
        ) : (
          <ul className="grid gap-1.5">
            {versions.map((v) => (
              <li key={v.version} className="grid grid-cols-[minmax(0,7rem)_minmax(0,1fr)_1.5rem] items-center gap-2 text-xs">
                <span className="truncate font-mono" title={v.version}>
                  {v.version}
                </span>
                <span className="h-1.5 overflow-hidden rounded-full bg-muted">
                  <span
                    className={`block h-full rounded-full ${v.version === "Unknown" ? toneBg.neutral : toneBg.info}`}
                    style={{ width: `${(v.count / maxCount) * 100}%` }}
                  />
                </span>
                <span className="text-right font-medium">{v.count}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-label="Needs attention" className="min-w-0">
        <div className="mb-2 flex items-center justify-between text-sm text-muted-foreground">
          <span>Needs attention</span>
          {attn.length > 0 && <span className="font-medium text-foreground">{attn.length}</span>}
        </div>
        {attn.length === 0 ? (
          <p className="text-xs text-muted-foreground">Nothing needs attention</p>
        ) : (
          <ul className="grid gap-1.5">
            {attn.slice(0, 4).map((r) => (
              <li key={r.node.id} className="flex min-w-0 items-center gap-2 text-xs">
                <StatusDot tone={r.tone} />
                <Link to={`/nodes/${r.node.id}`} className="truncate font-medium hover:underline">
                  {r.node.hostname || r.node.id}
                </Link>
                <span className="ml-auto shrink-0 text-muted-foreground">{r.reason}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
