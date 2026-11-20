import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/PageHeader";
import { GetStartedHero } from "@/components/GetStartedHero";
import { StatTile } from "@/components/fleet/StatTile";
import { StackBar, type StackBarCount } from "@/components/fleet/StackBar";
import { toneText } from "@/components/fleet/tones";
import { Gauge } from "@/components/fleet/Gauge";
import { MeterBar } from "@/components/fleet/MeterBar";
import { useLatestMetrics } from "@/hooks/useMetrics";
import { cpuPercent, maxDiskPercent, memPercent } from "@/lib/metrics";
import type { NodeMetrics } from "@/api/metrics";
import { isBuilding, isFailed, isOffline, isOnline, isReady, phaseTone, type Tone } from "@/lib/phase";
import { timeAgo } from "@/lib/time";
import { imageVersion } from "@/lib/nodeInfo";
import {
  buildActivity,
  needsAttention,
  type ActivityEvent,
  type ActivityKind,
  type AttentionKind,
} from "@/lib/activity";
import { listNodes, type Node } from "@/api/nodes";
import { listGroups, type Group } from "@/api/groups";
import { listArtifacts, type Artifact } from "@/api/artifacts";
import { listDeployments, type Deployment } from "@/api/deployments";
import { listExtensions, type Extension } from "@/api/extensions";
import {
  Loader2,
  Plus,
  Download,
  Server,
  Package,
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  Clock,
  Folder,
  Layers,
  Puzzle,
  Rocket,
  ShieldAlert,
  Move,
  Activity,
  Cpu,
  HardDrive,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";

// Written out in full so Tailwind can find every class in the source.
const toneSoft: Record<Tone, string> = {
  success: "bg-success/15",
  warning: "bg-warning/15",
  danger: "bg-danger/15",
  info: "bg-info/15",
  neutral: "bg-neutral/15",
};

const activityIcons: Record<ActivityKind, LucideIcon> = {
  build: Package,
  extension: Puzzle,
  deploy: Rocket,
  node: Server,
};

const attentionIcons: Record<AttentionKind, LucideIcon> = {
  boot: ShieldAlert,
  offline: Server,
  cpu: Cpu,
  disk: HardDrive,
  build: Package,
  extension: Puzzle,
  ungrouped: Move,
};

type ActivityFilter = "all" | "builds" | "nodes";

const filters: { value: ActivityFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "builds", label: "Builds" },
  { value: "nodes", label: "Nodes" },
];

function matchesFilter(e: ActivityEvent, f: ActivityFilter): boolean {
  if (f === "builds") return e.kind === "build" || e.kind === "extension";
  if (f === "nodes") return e.kind === "node";
  return true;
}

const DAY_MS = 24 * 60 * 60 * 1000;

function ms(time: string | null | undefined): number {
  const t = time ? new Date(time).getTime() : NaN;
  return Number.isNaN(t) ? 0 : t;
}

// fleetCounts splits nodes into the StackBar segments by status tone.
function fleetCounts(nodes: Node[]): StackBarCount[] {
  const by = (tone: Tone) => nodes.filter((n) => phaseTone(n.phase) === tone).length;
  return [
    { tone: "success", count: by("success"), label: "online" },
    { tone: "danger", count: by("danger"), label: "offline" },
    { tone: "warning", count: by("warning"), label: "pending" },
    { tone: "info", count: by("info"), label: "updating" },
    { tone: "neutral", count: by("neutral"), label: "registered" },
  ];
}

function isDeploymentRunning(d: Deployment): boolean {
  const s = d.status.toLowerCase();
  return s === "active" || s === "running";
}

function average(values: (number | null)[]): number | null {
  const known = values.filter((v): v is number => v !== null);
  if (known.length === 0) return null;
  return Math.round(known.reduce((a, b) => a + b, 0) / known.length);
}

type Busy = { node: Node; kind: "CPU" | "memory" | "disk"; value: number };

// busiestNodes ranks nodes by their highest usage of CPU, memory or disk.
function busiestNodes(nodes: Node[], metrics: Record<string, NodeMetrics>, limit = 3): Busy[] {
  const rows: Busy[] = [];
  for (const node of nodes) {
    const m = metrics[node.id];
    if (!m) continue;
    const options: [Busy["kind"], number | null][] = [
      ["CPU", cpuPercent(m)],
      ["memory", memPercent(m)],
      ["disk", maxDiskPercent(m)],
    ];
    let best: Busy | null = null;
    for (const [kind, value] of options) {
      if (value !== null && (!best || value > best.value)) best = { node, kind, value };
    }
    if (best) rows.push(best);
  }
  return rows.sort((a, b) => b.value - a.value).slice(0, limit);
}

function FleetResources({ nodes, metrics }: { nodes: Node[]; metrics: Record<string, NodeMetrics> }) {
  const reporting = nodes.filter((n) => metrics[n.id]);
  const avgCpu = average(reporting.map((n) => cpuPercent(metrics[n.id])));
  const avgMem = average(reporting.map((n) => memPercent(metrics[n.id])));
  const busy = busiestNodes(reporting, metrics);
  return (
    <Card className="mb-6">
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold">
            <Activity className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
            Fleet resources
          </CardTitle>
          <span className="text-xs text-muted-foreground">
            {reporting.length} node{reporting.length === 1 ? "" : "s"} reporting metrics
          </span>
        </div>
      </CardHeader>
      <CardContent>
        <div className="flex flex-wrap items-center gap-6">
          {avgCpu !== null && <Gauge value={avgCpu} label="Average CPU" sub="avg CPU" />}
          {avgMem !== null && <Gauge value={avgMem} label="Average memory" sub="avg memory" />}
          {busy.length > 0 && (
            <div className="min-w-0 flex-1 basis-56">
              <p className="mb-2 text-xs font-semibold text-muted-foreground">Busiest nodes</p>
              <ul className="space-y-1.5">
                {busy.map((b) => (
                  <li key={b.node.id} className="flex items-center justify-between gap-3 text-sm">
                    <Link to={`/nodes/${b.node.id}`} className="min-w-0 truncate hover:underline">
                      {b.node.hostname || b.node.id}
                      {b.kind !== "CPU" && <span className="ml-1 text-xs text-muted-foreground">{b.kind}</span>}
                    </Link>
                    <MeterBar value={b.value} />
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function IconBubble({ icon: Icon, tone }: { icon: LucideIcon; tone: Tone }) {
  return (
    <span
      className={cn(
        "flex h-8 w-8 shrink-0 items-center justify-center rounded-full",
        toneSoft[tone],
        toneText[tone],
      )}
    >
      <Icon className="h-4 w-4" aria-hidden="true" />
    </span>
  );
}

export function Dashboard() {
  const navigate = useNavigate();
  const [nodes, setNodes] = useState<Node[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [extensions, setExtensions] = useState<Extension[]>([]);
  const [filter, setFilter] = useState<ActivityFilter>("all");
  const { byNode } = useLatestMetrics();
  // The reference time for "in the last 24 h", taken once per mount.
  const [now] = useState(() => Date.now());
  // We must not render anything until BOTH nodes and artifacts have been
  // fetched at least once. Otherwise the initial empty arrays cause a
  // split-second zero-state wizard flash right before the real dashboard
  // paints, which looks glitchy on every page load.
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let nodesDone = false;
    let artifactsDone = false;
    const markLoaded = () => {
      if (nodesDone && artifactsDone) setLoaded(true);
    };
    listNodes()
      .then(setNodes)
      .catch(() => {})
      .finally(() => {
        nodesDone = true;
        markLoaded();
      });
    listArtifacts()
      .then(setArtifacts)
      .catch(() => {})
      .finally(() => {
        artifactsDone = true;
        markLoaded();
      });
    // Non-gating background loads — wizard decision doesn't depend on them.
    listGroups().then(setGroups).catch(() => {});
    listDeployments().then(setDeployments).catch(() => {});
    listExtensions().then(setExtensions).catch(() => {});
  }, []);

  // Hold the whole page until we actually know what the state is — prevents
  // the zero-state wizard from flashing on every dashboard paint.
  if (!loaded) {
    return (
      <div className="flex items-center justify-center py-24 text-muted-foreground">
        <Loader2 className="h-5 w-5 animate-spin" />
      </div>
    );
  }

  // First-run and partial-run states. We only show the full welcome wizard
  // when the instance is completely empty — no nodes, no artifacts, no
  // history at all. Once the user has built something OR registered a node,
  // the normal dashboard takes over (but a partial-state banner nudges them
  // to the next step if only half the journey is done).
  const isZeroState = nodes.length === 0 && artifacts.length === 0;
  if (isZeroState) {
    return <GetStartedHero />;
  }
  const hasArtifactsButNoNodes = nodes.length === 0 && artifacts.length > 0;
  const readyArtifact = artifacts.find((a) => isReady(a.phase));

  // Summary band.
  const onlineCount = nodes.filter((n) => isOnline(n.phase)).length;
  const offlineCount = nodes.filter((n) => isOffline(n.phase)).length;
  // Pending and Registered nodes have not come online yet; they are not offline.
  const waitingCount = nodes.length - onlineCount - offlineCount;

  const activeBuilds = artifacts
    .filter((a) => isBuilding(a.phase))
    .sort((a, b) => ms(b.createdAt) - ms(a.createdAt));
  const newestBuild = activeBuilds[0];
  const failedRecently = artifacts.filter(
    (a) => isFailed(a.phase) && now - ms(a.updatedAt || a.createdAt) < DAY_MS,
  ).length;

  const runningDeployments = deployments.filter(isDeploymentRunning).length;
  const newestDeployment = [...deployments].sort((a, b) => ms(b.startedAt) - ms(a.startedAt))[0];

  const extReady = extensions.filter((e) => isReady(e.phase)).length;
  const extBuilding = extensions.filter((e) => isBuilding(e.phase)).length;
  const extError = extensions.filter((e) => isFailed(e.phase)).length;

  // Metrics add to the page only once a node reports them.
  const metrics = nodes.some((n) => byNode[n.id]) ? byNode : undefined;
  const attention = needsAttention({ nodes, artifacts, extensions, metrics });
  const activity = buildActivity({ nodes, artifacts, deployments, extensions }).filter((e) =>
    matchesFilter(e, filter),
  );

  // Groups card.
  const ungrouped = nodes.filter((n) => !n.groupID && !n.group?.id);

  // Image versions card.
  const versionCounts = new Map<string, number>();
  for (const n of nodes) {
    const v = imageVersion(n);
    if (v) versionCounts.set(v, (versionCounts.get(v) ?? 0) + 1);
  }
  const versions = [...versionCounts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  const maxVersionCount = versions[0]?.[1] ?? 0;

  const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

  return (
    <div>
      <PageHeader
        title="Overview"
        description={`${plural(nodes.length, "node")} in ${plural(groups.length, "group")}`}
      >
        <Button variant="outline" onClick={() => navigate("/import")}>
          <Download className="h-4 w-4" /> Import nodes
        </Button>
        <Button onClick={() => navigate("/artifacts/new")}>
          <Plus className="h-4 w-4" /> Build artifact
        </Button>
      </PageHeader>

      {/* Partial-state banner: closes the loop for users who've built an
          artifact but haven't deployed it or imported any existing nodes
          yet. Only rendered when there's at least one artifact and zero
          nodes. */}
      {hasArtifactsButNoNodes && (
        <div className="mb-8 rounded-xl border border-primary/30 bg-primary-soft p-5 animate-fade-up">
          <div className="flex items-start gap-4">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-primary/15 text-primary">
              <Rocket className="h-5 w-5" />
            </div>
            <div className="flex-1 min-w-0">
              <h3 className="font-semibold text-sm">
                {artifacts.length === 1
                  ? "You've built your first artifact."
                  : `You've built ${artifacts.length} artifacts.`}{" "}
                Ready to put {artifacts.length === 1 ? "it" : "one"} on a machine?
              </h3>
              <p className="text-sm text-muted-foreground mt-1">
                Flash the ISO to a USB stick, serve it over netboot, or point a
                Redfish BMC at the image — nodes auto-register on first boot.
              </p>
              <div className="mt-4 flex flex-wrap gap-2">
                <Button size="sm" variant="outline" onClick={() => navigate("/import")}>
                  <Download className="h-4 w-4" />
                  Or import existing nodes
                </Button>
                {readyArtifact ? (
                  <Button size="sm" onClick={() => navigate(`/artifacts/${readyArtifact.id}`)}>
                    Deploy "{readyArtifact.name || readyArtifact.id.slice(0, 8)}"
                    <ArrowRight className="h-4 w-4" />
                  </Button>
                ) : (
                  <Button size="sm" onClick={() => navigate("/artifacts")}>
                    View artifacts
                    <ArrowRight className="h-4 w-4" />
                  </Button>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Summary band */}
      <div className="mb-6 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatTile
          label="Fleet"
          icon={Server}
          value={
            <>
              {onlineCount}
              <span className="text-sm font-normal text-muted-foreground"> / {nodes.length} online</span>
            </>
          }
          sub={`${offlineCount} offline · ${waitingCount} waiting`}
        >
          <StackBar counts={fleetCounts(nodes)} className="mt-2" />
        </StatTile>
        <StatTile
          label="Builds"
          icon={Package}
          tone={activeBuilds.length > 0 ? "info" : undefined}
          value={
            <>
              {activeBuilds.length}
              <span className="text-sm font-normal text-muted-foreground"> building</span>
            </>
          }
          sub={
            <div className="flex flex-col gap-0.5">
              {newestBuild && (
                <span className="truncate">
                  <span className="font-medium text-foreground">
                    {newestBuild.name || newestBuild.id.slice(0, 8)}
                  </span>{" "}
                  · started {timeAgo(newestBuild.createdAt)}
                </span>
              )}
              <span className={cn(failedRecently > 0 && "text-danger")}>
                {failedRecently} failed in the last 24 h
              </span>
            </div>
          }
        />
        <StatTile
          label="Deployments"
          icon={Rocket}
          tone={runningDeployments > 0 ? "info" : undefined}
          value={
            <>
              {runningDeployments}
              <span className="text-sm font-normal text-muted-foreground"> running</span>
            </>
          }
          sub={
            newestDeployment ? (
              <span className="truncate">
                Latest: {newestDeployment.method} →{" "}
                <span className="font-medium text-foreground">
                  {newestDeployment.bmcTargetId || newestDeployment.artifactId.slice(0, 8)}
                </span>
              </span>
            ) : (
              "No deployments yet"
            )
          }
        />
        <StatTile
          label="Extensions"
          icon={Puzzle}
          value={extensions.length}
          sub={
            <span className="flex flex-wrap gap-x-3">
              <span>{extReady} ready</span>
              <span>{extBuilding} building</span>
              <span className={cn(extError > 0 && "text-danger")}>{extError} error</span>
            </span>
          }
        />
      </div>

      {metrics && <FleetResources nodes={nodes} metrics={metrics} />}

      <div className="grid gap-6 lg:grid-cols-5">
        <div className="space-y-6 lg:col-span-3 min-w-0">
          {/* Needs attention */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <AlertTriangle className="h-4 w-4 text-warning" aria-hidden="true" />
                Needs attention
                {attention.length > 0 && (
                  <span className="rounded-full bg-warning/15 px-2 py-0.5 text-xs text-warning-foreground">
                    {attention.length}
                  </span>
                )}
              </CardTitle>
            </CardHeader>
            <CardContent>
              {attention.length === 0 ? (
                <p className="flex items-center gap-2 text-sm text-muted-foreground">
                  <CheckCircle2 className="h-4 w-4 text-success" aria-hidden="true" />
                  Everything looks healthy
                </p>
              ) : (
                <ul className="divide-y">
                  {attention.map((row) => (
                    <li key={row.id} className="flex items-center gap-3 py-2.5">
                      <IconBubble icon={attentionIcons[row.kind]} tone={row.tone} />
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">{row.title}</p>
                        <p className="truncate text-xs text-muted-foreground">{row.detail}</p>
                      </div>
                      <Button size="sm" variant="outline" onClick={() => navigate(row.link)}>
                        {row.actionLabel}
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>

          {/* Activity */}
          <Card>
            <CardHeader className="pb-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                  <Clock className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
                  Activity
                </CardTitle>
                <div role="group" aria-label="Filter activity" className="inline-flex rounded-md border bg-muted p-0.5">
                  {filters.map((f) => (
                    <button
                      key={f.value}
                      type="button"
                      aria-pressed={filter === f.value}
                      onClick={() => setFilter(f.value)}
                      className={cn(
                        "rounded px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors",
                        filter === f.value && "bg-background text-foreground shadow-sm",
                      )}
                    >
                      {f.label}
                    </button>
                  ))}
                </div>
              </div>
            </CardHeader>
            <CardContent>
              {activity.length === 0 ? (
                <p className="text-sm text-muted-foreground">No recent activity.</p>
              ) : (
                <ul className="space-y-1">
                  {activity.map((e) => (
                    <li key={e.id}>
                      <Link
                        to={e.link}
                        className="flex items-center gap-3 rounded-lg px-2 py-2 text-sm transition-colors hover:bg-muted/50"
                      >
                        <IconBubble icon={activityIcons[e.kind]} tone={e.tone} />
                        <div className="min-w-0 flex-1">
                          <p className="truncate">{e.title}</p>
                          {e.detail && <p className="truncate text-xs text-muted-foreground">{e.detail}</p>}
                        </div>
                        <span className="whitespace-nowrap text-xs text-muted-foreground">{timeAgo(e.time)}</span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        </div>

        <div className="space-y-6 lg:col-span-2 min-w-0">
          {/* Groups */}
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between gap-2">
                <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                  <Folder className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
                  Groups
                </CardTitle>
                <Button size="sm" variant="ghost" onClick={() => navigate("/groups")}>
                  All groups <ArrowRight className="h-4 w-4" />
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              {groups.length === 0 && ungrouped.length === 0 ? (
                <p className="text-sm text-muted-foreground">No groups yet.</p>
              ) : (
                <ul className="divide-y">
                  {groups.map((g) => {
                    const members = nodes.filter((n) => n.groupID === g.id || n.group?.id === g.id);
                    const online = members.filter((n) => isOnline(n.phase)).length;
                    return (
                      <li key={g.id} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-3 py-2.5">
                        <div className="min-w-0">
                          <Link to={`/groups/${g.id}`} className="block truncate text-sm font-medium hover:underline">
                            {g.name}
                          </Link>
                          <p className="text-xs text-muted-foreground">{plural(members.length, "node")}</p>
                        </div>
                        <StackBar counts={fleetCounts(members)} />
                        <span className="text-xs text-muted-foreground">
                          {online}/{members.length}
                        </span>
                      </li>
                    );
                  })}
                  {ungrouped.length > 0 && (
                    <li className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-3 py-2.5">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">Not in a group</p>
                        <p className="text-xs text-muted-foreground">{plural(ungrouped.length, "node")}</p>
                      </div>
                      <StackBar counts={fleetCounts(ungrouped)} />
                      <Button size="sm" variant="outline" onClick={() => navigate("/groups")}>
                        Assign
                      </Button>
                    </li>
                  )}
                </ul>
              )}
            </CardContent>
          </Card>

          {/* Image versions */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <Layers className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
                Image versions
              </CardTitle>
            </CardHeader>
            <CardContent>
              {versions.length === 0 ? (
                <p className="text-sm text-muted-foreground">No node reports an image version yet.</p>
              ) : (
                <ul className="space-y-2">
                  {versions.map(([version, count]) => (
                    <li key={version} className="grid grid-cols-[minmax(0,8rem)_minmax(0,1fr)_2rem] items-center gap-3 text-sm">
                      <span className="truncate font-mono text-xs" title={version}>
                        {version}
                      </span>
                      <div className="h-2 overflow-hidden rounded-full bg-muted">
                        <div
                          className="h-full rounded-full bg-info"
                          style={{ width: `${(count / maxVersionCount) * 100}%` }}
                        />
                      </div>
                      <span className="text-right font-medium">{count}</span>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
