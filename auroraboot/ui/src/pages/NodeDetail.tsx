import { useEffect, useState, useCallback, type ReactNode } from "react";
import { useParams, useNavigate } from "react-router";
import { getNode, sendCommand, setLabels, setGroup, type Node } from "@/api/nodes";
import { DecommissionDialog } from "@/components/DecommissionDialog";
import { listNodeCommands, deleteCommand, clearCommandHistory, type Command } from "@/api/commands";
import { listExtensionsForNode, listExtensions, type NodeExtensionRow, type Extension } from "@/api/extensions";
import { ExtensionTypeChip } from "@/components/ExtensionTypeChip";
import { InstallExtensionDialog } from "@/components/InstallExtensionDialog";
import { listGroups, type Group } from "@/api/groups";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { StatusBadge } from "@/components/StatusBadge";
import { PageHeader, type BreadcrumbItem } from "@/components/PageHeader";
import { CommandDialog } from "@/components/CommandDialog";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { StatTile } from "@/components/fleet/StatTile";
import { CommandTimeline } from "@/components/fleet/CommandTimeline";
import { LabelChips } from "@/components/fleet/LabelChips";
import { RecentBatches } from "@/components/fleet/RecentBatches";
import { toneText } from "@/components/fleet/tones";
import { ResourcesCard } from "@/components/fleet/ResourcesCard";
import { useUIWebSocket } from "@/hooks/useUIWebSocket";
import { useNodeMetrics } from "@/hooks/useMetrics";
import {
  Activity,
  ArrowUpCircle,
  Copy,
  Cpu,
  Layers,
  MemoryStick,
  MoreHorizontal,
  Plus,
  Power,
  RotateCcw,
  ShieldCheck,
  Tag,
  Terminal,
  Zap,
  Puzzle,
  CircuitBoard,
  AlertTriangle,
} from "lucide-react";
import { toast } from "@/hooks/useToast";
import { timeAgo } from "@/lib/time";
import { cpuCount, imageVersion, memGiB, ociImage } from "@/lib/nodeInfo";
import { phaseTone, type Tone } from "@/lib/phase";
import { cn } from "@/lib/utils";
import { sustainedHighCpu } from "@/lib/metrics";

// The node record also carries its claim, which the Node type does not model.
type NodeWithClaim = Node & { claimKey?: string | null; claimedAt?: string | null };

type QuickCommand = "reboot" | "upgrade" | null;

function capitalize(s: string): string {
  return s ? s[0].toUpperCase() + s.slice(1) : s;
}

// bootTone: booting the active image is the normal case; recovery or passive
// means the node fell back and wants attention. An agent that does not report
// a boot state gets no tone rather than a false alarm.
function bootTone(state: string): Tone {
  if (!state) return "neutral";
  return state === "active" ? "success" : "warning";
}

// lifecycleTone flags the upgrade or reset that failed: the node did not come
// back on the new image, or the command failed before it could reboot.
function lifecycleTone(state: string | undefined): Tone | undefined {
  return state === "failed" ? "danger" : undefined;
}

function LifecycleState({ state, idle }: { state: string | undefined; idle: string }) {
  if (!state) return <>{idle}</>;
  const tone = lifecycleTone(state);
  return <span className={cn(tone && toneText[tone])}>{capitalize(state)}</span>;
}

const bootText: Record<string, string> = {
  active: "Booted from the active image",
  passive: "Booted from the fallback (passive) image",
  recovery: "Booted into recovery",
};

function shortID(id: string): string {
  return id.length > 14 ? `${id.slice(0, 5)}…${id.slice(-5)}` : id;
}

function formatDate(d: string | null | undefined): string {
  if (!d) return "—";
  const t = new Date(d);
  if (Number.isNaN(t.getTime())) return "—";
  return t.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

function SmallTag({ children }: { children: ReactNode }) {
  return (
    <span className="rounded border bg-muted px-1.5 py-px text-[11px] text-muted-foreground">{children}</span>
  );
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[6.5rem_1fr] items-start gap-2 py-1.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

export function NodeDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [node, setNode] = useState<Node | null>(null);
  const [cmdOpen, setCmdOpen] = useState(false);
  const [cmdDefault, setCmdDefault] = useState<QuickCommand>(null);
  const [commands, setCommands] = useState<Command[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [confirmState, setConfirmState] = useState<{ open: boolean; action: () => void; title: string; description: string }>({ open: false, action: () => {}, title: "", description: "" });
  const [nodeExtensions, setNodeExtensions] = useState<NodeExtensionRow[]>([]);
  const [readyExtensions, setReadyExtensions] = useState<Extension[]>([]);
  const [pickedForInstall, setPickedForInstall] = useState<Extension | null>(null);
  const [extPickerOpen, setExtPickerOpen] = useState(false);
  const [decommissionOpen, setDecommissionOpen] = useState(false);
  // A failed or missing metrics endpoint leaves latest null, and the page
  // keeps the Hardware card.
  const metrics = useNodeMetrics(id ?? "", 10000);

  const fetchCommands = useCallback(() => {
    if (!id) return;
    listNodeCommands(id).then(setCommands).catch(() => {});
  }, [id]);

  // The label editors in LabelChips keep their own text, so a poll replacing
  // `node` cannot reseed what someone is typing.
  const fetchNode = useCallback(() => {
    if (!id) return;
    getNode(id).then(setNode).catch(() => {});
  }, [id]);

  useEffect(() => {
    fetchNode();
    fetchCommands();
    listGroups().then(setGroups).catch(() => {});
    if (id) {
      listExtensionsForNode(id).then(setNodeExtensions).catch(() => {});
    }
    // Populate the Install-extension picker with everything that's Ready.
    listExtensions()
      .then((es) => setReadyExtensions(es.filter((e) => e.phase === "Ready")))
      .catch(() => {});
  }, [fetchNode, fetchCommands, id]);

  // Fallback polling every 10s. The node is re-read alongside its commands
  // because fields the agent reports keep changing after the page is open:
  // phase, last heartbeat, and the hostname, which a node that registered
  // before cloud-init applied a templated one only corrects on a later
  // heartbeat (kairos-io/kairos#4196). Without this the detail page showed the
  // registration-time value until a manual reload, while the Nodes list, which
  // polls, showed the new one.
  useEffect(() => {
    const interval = setInterval(() => {
      fetchCommands();
      fetchNode();
    }, 10000);
    return () => clearInterval(interval);
  }, [fetchCommands, fetchNode]);

  // Live updates via WebSocket
  useUIWebSocket((msg) => {
    if (msg.type !== "command_update") return;
    const d = msg.data as { id?: string; phase?: string; result?: string } | null | undefined;
    if (!d?.id) return;
    setCommands((prev) =>
      prev.map((cmd) =>
        cmd.id === d.id
          ? { ...cmd, phase: d.phase ?? cmd.phase, result: d.result ?? cmd.result }
          : cmd
      )
    );
  });

  function openCommand(which: QuickCommand) {
    setCmdDefault(which);
    setCmdOpen(true);
  }

  async function handleCommand(command: string, args: Record<string, unknown>) {
    if (!id) return;
    await sendCommand(id, command, args);
    setCmdOpen(false);
    fetchCommands();
  }

  async function handleDeleteCommand(commandID: string) {
    if (!id) return;
    await deleteCommand(id, commandID);
    setCommands((prev) => prev.filter((c) => c.id !== commandID));
  }

  function handleClearHistory() {
    if (!id) return;
    setConfirmState({
      open: true,
      title: "Clear command history",
      description: "Clear all completed and failed commands? This cannot be undone.",
      action: async () => {
        await clearCommandHistory(id);
        fetchCommands();
      },
    });
  }

  async function handleLabels(next: Record<string, string>) {
    if (!id) return;
    try {
      await setLabels(id, next);
    } catch (err) {
      toast(`Failed to save labels: ${(err as Error).message}`, "error");
      throw err;
    }
    // The PUT does not return the node, so read it back rather than guess at
    // the new state. This also picks up anything the server normalised.
    fetchNode();
  }

  async function copyMachineID(machineID: string) {
    try {
      await navigator.clipboard.writeText(machineID);
      toast("Machine ID copied");
    } catch {
      toast("Could not copy the machine ID", "error");
    }
  }

  if (!node) {
    return <div className="text-muted-foreground">Loading...</div>;
  }

  const n = node as NodeWithClaim;
  const groupName = n.group?.name || groups.find((g) => g.id === n.groupID)?.name || "";
  const breadcrumb: BreadcrumbItem[] = [{ label: "Nodes", to: "/nodes" }];
  if (n.groupID && groupName) breadcrumb.push({ label: groupName, to: `/groups/${n.groupID}` });
  breadcrumb.push({ label: n.hostname });

  const image = imageVersion(n);
  const oci = ociImage(n);
  const boot = (n.bootState ?? "").toLowerCase();
  const bTone = bootTone(boot);
  const cpus = cpuCount(n);
  const mem = memGiB(n);
  const arch = n.osRelease?.ARCH ?? "";
  const kernel = n.osRelease?.KERNEL ?? "";
  const osLine = [n.osRelease?.PRETTY_NAME, n.osRelease?.KAIROS_FLAVOR].filter(Boolean).join(" · ");
  const hardware: { icon: typeof Cpu; value: string; label: string; mono?: boolean }[] = [
    { icon: Cpu, value: cpus !== null ? `${cpus} vCPU` : "", label: "Processors" },
    { icon: MemoryStick, value: mem ?? "", label: "Memory" },
    { icon: CircuitBoard, value: arch, label: "Architecture" },
    { icon: Terminal, value: kernel, label: "Kernel", mono: true },
  ];
  const hasHardware = hardware.some((h) => h.value);
  const highCpu = metrics.latest !== null && sustainedHighCpu(metrics.samples);
  const load1 = metrics.latest?.load?.[0];

  return (
    <div>
      <PageHeader
        title={n.hostname}
        breadcrumb={breadcrumb}
        status={<StatusBadge status={n.phase} />}
        meta={
          <>
            {n.remoteIP && <span className="font-mono">{n.remoteIP}</span>}
            {groupName && <span>{groupName}</span>}
            {image && <span>Image {image}</span>}
            {n.agentVersion && <span>Kairos {n.agentVersion}</span>}
          </>
        }
      >
        <Button variant="outline" onClick={() => openCommand("reboot")}>
          <Power aria-hidden="true" />
          Reboot
        </Button>
        <Button variant="outline" onClick={() => openCommand("upgrade")}>
          <ArrowUpCircle aria-hidden="true" />
          Upgrade
        </Button>
        <Button onClick={() => openCommand(null)}>
          <Terminal aria-hidden="true" />
          Send command
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="icon" aria-label="More actions">
              <MoreHorizontal aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={handleClearHistory}>Clear command history</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem className="text-danger focus:text-danger" onSelect={() => setDecommissionOpen(true)}>
              Decommission
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </PageHeader>

      {highCpu && (
        <div
          role="alert"
          className="mb-6 flex items-start gap-2 rounded-md border border-warning bg-warning/10 px-4 py-3 text-sm"
        >
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
          <span>
            <b>CPU above 85%</b> for the last 10 samples
            {load1 !== undefined && cpus !== null ? `. Load ${load1.toFixed(1)} on ${cpus} vCPU.` : "."}
          </span>
        </div>
      )}

      {/* Health strip */}
      <section aria-label="Health" className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <StatTile
          label="Connection"
          icon={Activity}
          tone={phaseTone(n.phase)}
          value={<StatusBadge status={n.phase} className="text-sm" />}
          sub={n.lastHeartbeat ? `Heartbeat ${timeAgo(n.lastHeartbeat)}` : "No heartbeat yet"}
        />
        <StatTile
          label="Boot"
          icon={ShieldCheck}
          tone={bTone}
          value={<span className={cn("text-lg", toneText[bTone])}>{boot ? capitalize(boot) : "Unknown"}</span>}
          sub={bootText[boot] ?? (boot ? `Booted from ${boot}` : "Not reported by the agent")}
        />
        <StatTile
          label="Image Version"
          icon={Layers}
          value={<span className="block truncate font-mono text-lg">{image || "—"}</span>}
          sub={
            oci ? (
              <span className="block truncate font-mono" title={oci}>
                {oci}
              </span>
            ) : image ? undefined : (
              "Not reported"
            )
          }
        />
        <StatTile
          label="Kairos Version"
          icon={Zap}
          value={<span className="block truncate font-mono text-lg">{n.agentVersion || "—"}</span>}
          sub={osLine || undefined}
        />
        <StatTile
          label="Upgrade"
          icon={ArrowUpCircle}
          tone={lifecycleTone(n.upgradeState)}
          value={<span className="text-lg">{n.lastUpgrade ? timeAgo(n.lastUpgrade) : "Never"}</span>}
          sub={<LifecycleState state={n.upgradeState} idle="No upgrade pending" />}
        />
        <StatTile
          label="Reset"
          icon={RotateCcw}
          tone={lifecycleTone(n.resetState)}
          value={<span className="text-lg">{n.lastReset ? timeAgo(n.lastReset) : "Never"}</span>}
          sub={<LifecycleState state={n.resetState} idle="No reset pending" />}
        />
      </section>

      {metrics.latest ? (
        <ResourcesCard latest={metrics.latest} samples={metrics.samples} />
      ) : (
        <Card role="region" aria-labelledby="hardware-title" className="mb-6">
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <CardTitle id="hardware-title" className="flex items-center gap-2 text-sm font-medium">
              <Cpu className="h-4 w-4" aria-hidden="true" />
              Hardware
            </CardTitle>
            <span className="text-xs text-muted-foreground">Reported by the agent</span>
          </CardHeader>
          <CardContent>
            {hasHardware ? (
              <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                {hardware.map((h) => (
                  <div key={h.label} className="flex items-center gap-3 rounded-md border p-3">
                    <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                      <h.icon className="h-4 w-4" aria-hidden="true" />
                    </span>
                    <div className="min-w-0">
                      <p className={cn("truncate text-sm font-semibold", h.mono && "font-mono text-xs")} title={h.value}>
                        {h.value || "—"}
                      </p>
                      <p className="text-xs text-muted-foreground">{h.label}</p>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">The agent has not reported hardware details yet.</p>
            )}
          </CardContent>
        </Card>
      )}

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <Card className="min-w-0">
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <CardTitle className="flex items-center gap-2 text-sm font-medium">
              <Terminal className="h-4 w-4" aria-hidden="true" />
              Commands
            </CardTitle>
            <Button variant="outline" size="sm" onClick={() => openCommand(null)}>
              <Plus aria-hidden="true" />
              New
            </Button>
          </CardHeader>
          <CardContent>
            <CommandTimeline commands={commands} onDelete={handleDeleteCommand} />
          </CardContent>
        </Card>

        <div className="grid content-start gap-6">
          <Card>
            <CardHeader>
              <CardTitle className="text-sm font-medium">Details</CardTitle>
            </CardHeader>
            <CardContent>
              <dl className="divide-y text-sm">
                <DetailRow label="Machine ID">
                  <span className="flex items-center gap-1">
                    <span className="font-mono text-xs" title={n.machineID}>{shortID(n.machineID)}</span>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label="Copy machine ID"
                      onClick={() => copyMachineID(n.machineID)}
                    >
                      <Copy className="h-3.5 w-3.5" />
                    </Button>
                  </span>
                </DetailRow>
                <DetailRow label="Seen from">
                  {n.remoteIP ? (
                    <span className="flex flex-wrap items-center gap-1.5">
                      <span className="font-mono text-xs">{n.remoteIP}</span>
                      <SmallTag>remote IP</SmallTag>
                    </span>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </DetailRow>
                <DetailRow label="Reported">
                  {n.addresses && n.addresses.length > 0 ? (
                    <ul className="grid gap-1">
                      {n.addresses.map((a) => (
                        <li key={`${a.type}:${a.address}`} className="flex flex-wrap items-center gap-1.5">
                          <span className="font-mono text-xs">{a.address}</span>
                          {a.type && <SmallTag>{a.type}</SmallTag>}
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <span className="text-muted-foreground">No addresses</span>
                  )}
                </DetailRow>
                <DetailRow label="Group">
                  <Select
                    value={n.groupID || "__none__"}
                    onValueChange={async (v) => {
                      await setGroup(id!, v === "__none__" ? null : v);
                      fetchNode();
                    }}
                  >
                    <SelectTrigger className="h-7 w-full max-w-48 text-xs" aria-label="Group">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="__none__">None</SelectItem>
                      {groups.map((g) => (
                        <SelectItem key={g.id} value={g.id}>
                          {g.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </DetailRow>
                <DetailRow label="Claimed">
                  {n.claimKey ? (
                    <span>
                      <span className="font-mono text-xs">{n.claimKey}</span>
                      {n.claimedAt && (
                        <span className="text-muted-foreground"> · {timeAgo(n.claimedAt)}</span>
                      )}
                    </span>
                  ) : (
                    <span className="text-muted-foreground">Not claimed</span>
                  )}
                </DetailRow>
                <DetailRow label="Registered">{formatDate(n.createdAt)}</DetailRow>
              </dl>
            </CardContent>
          </Card>

          {/* Installed extensions — populated by the status callback when the
              agent reports a successful install/upgrade. The card is always
              shown so the "Install extension…" affordance is discoverable even
              on a node with no extensions yet. */}
          <Card>
            <CardHeader className="flex-row items-center justify-between space-y-0">
              <CardTitle className="flex items-center gap-2 text-sm font-medium">
                <Puzzle className="h-4 w-4" aria-hidden="true" />
                Installed extensions
              </CardTitle>
              <Button
                size="sm"
                variant="outline"
                disabled={readyExtensions.length === 0}
                onClick={() => setExtPickerOpen(true)}
              >
                Install extension…
              </Button>
            </CardHeader>
            <CardContent>
              {nodeExtensions.length === 0 ? (
                <p className="text-xs text-muted-foreground">
                  No extensions installed on this node.{" "}
                  {readyExtensions.length > 0
                    ? "Click Install extension… above to push one."
                    : "Build a sysext or confext first, then install."}
                </p>
              ) : (
                <ul className="divide-y">
                  {nodeExtensions.map((row) => (
                    <li
                      key={`${row.type}:${row.name}:${row.bootState}`}
                      className="flex items-center justify-between gap-2 py-2 text-sm first:pt-0 last:pb-0"
                    >
                      <div className="flex min-w-0 items-center gap-2">
                        <ExtensionTypeChip type={row.type} />
                        <span className="truncate font-medium">{row.name}</span>
                        <code className="text-[11px] text-muted-foreground">{row.version}</code>
                      </div>
                      <SmallTag>{row.bootState}</SmallTag>
                    </li>
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-sm font-medium">
                <Tag className="h-4 w-4" aria-hidden="true" />
                Labels
              </CardTitle>
            </CardHeader>
            <CardContent>
              <LabelChips labels={n.labels || {}} onChange={handleLabels} />
            </CardContent>
          </Card>

          <RecentBatches nodeID={n.id} />
        </div>
      </div>

      {/* Simple picker: choose a Ready extension, then hand off to the shared
          InstallExtensionDialog with this node preset as the target. */}
      {extPickerOpen && (
        <div
          role="dialog"
          aria-modal="true"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
          onClick={() => setExtPickerOpen(false)}
        >
          <div
            className="bg-background rounded-lg shadow-xl border max-w-[480px] w-[92%] p-5"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="text-base font-semibold">
              Install extension on{" "}
              <code className="text-sm font-mono">{n.hostname || n.id.slice(0, 8)}</code>
            </h2>
            <p className="text-sm text-muted-foreground mt-1">
              Pick a Ready extension. The next step lets you choose the boot scope.
            </p>
            <ul className="mt-3 border rounded-md divide-y max-h-72 overflow-y-auto">
              {readyExtensions.map((e) => (
                <li key={e.id}>
                  <button
                    type="button"
                    className="w-full text-left px-3 py-2 text-sm flex items-center justify-between gap-3 hover:bg-muted/60"
                    onClick={() => {
                      setPickedForInstall(e);
                      setExtPickerOpen(false);
                    }}
                  >
                    <span className="flex items-center gap-2 min-w-0">
                      <ExtensionTypeChip type={e.type} />
                      <span className="font-medium truncate">{e.name}</span>
                    </span>
                    <code className="text-[11px] opacity-60 shrink-0">
                      {e.version} · {e.arch}
                    </code>
                  </button>
                </li>
              ))}
            </ul>
            <div className="mt-4 flex justify-end">
              <Button variant="outline" onClick={() => setExtPickerOpen(false)}>
                Cancel
              </Button>
            </div>
          </div>
        </div>
      )}

      {pickedForInstall && (
        <InstallExtensionDialog
          open={pickedForInstall !== null}
          onOpenChange={(o) => !o && setPickedForInstall(null)}
          extension={pickedForInstall}
          presetNodeID={n.id}
        />
      )}

      <CommandDialog
        open={cmdOpen}
        onOpenChange={setCmdOpen}
        onSubmit={handleCommand}
        title={`Send command · ${n.hostname}`}
        defaultCommand={cmdDefault}
      />

      <ConfirmDialog
        open={confirmState.open}
        onOpenChange={(open) => setConfirmState(prev => ({ ...prev, open }))}
        title={confirmState.title}
        description={confirmState.description}
        confirmLabel="Clear history"
        destructive
        onConfirm={confirmState.action}
      />

      <DecommissionDialog
        open={decommissionOpen}
        onOpenChange={setDecommissionOpen}
        node={n}
        onDeleted={() => navigate("/nodes")}
      />
    </div>
  );
}
