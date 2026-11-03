import { useRef, useState, type DragEvent, type KeyboardEvent } from "react";
import { useNavigate } from "react-router";
import { FolderTree, GripVertical, Inbox, MoreHorizontal, Plus } from "lucide-react";
import type { Group } from "@/api/groups";
import type { Node } from "@/api/nodes";
import type { NodeMetrics } from "@/api/metrics";
import { cpuPercent, maxDiskPercent, memPercent, usageTone } from "@/lib/metrics";
import { isOnline, phaseTone } from "@/lib/phase";
import { imageVersion, nodeAddress } from "@/lib/nodeInfo";
import { phaseCounts } from "@/lib/nodeFilter";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { StackBar } from "./StackBar";
import { StatusDot } from "./StatusDot";
import { toneBg } from "./tones";

interface GroupBoardProps {
  groups: Group[];
  nodes: Node[];
  onMove(nodeId: string, groupId: string): Promise<void>;
  onCreate(): void;
  onRename(g: Group): void;
  onDelete(g: Group): void;
  // Latest agent metrics by node ID. Cards of nodes with an entry show small
  // CPU, memory and disk bars.
  metrics?: Record<string, NodeMetrics>;
}

// The "Not in a group" column. setGroup takes "" to clear a node's group.
const NO_GROUP = "";
const DRAG_TYPE = "text/plain";

interface Target {
  id: string;
  name: string;
}

// MiniMeters shows CPU, memory and disk as three short vertical bars.
function MiniMeters({ metrics }: { metrics: NodeMetrics }) {
  const items: [string, number | null][] = [
    ["CPU", cpuPercent(metrics)],
    ["Memory", memPercent(metrics)],
    ["Disk", maxDiskPercent(metrics)],
  ];
  return (
    <span className="ml-auto flex h-5 shrink-0 items-end gap-0.5">
      {items.map(([label, v]) => (
        <span
          key={label}
          role="meter"
          aria-label={label}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={v ?? 0}
          aria-valuetext={v === null ? "no data" : `${v}%`}
          title={`${label} ${v === null ? "—" : `${v}%`}`}
          className="flex h-full w-1.5 items-end overflow-hidden rounded-sm bg-muted"
        >
          {v !== null && (
            <span className={cn("block w-full", toneBg[usageTone(v)])} style={{ height: `${Math.max(15, v)}%` }} />
          )}
        </span>
      ))}
    </span>
  );
}

function NodeCard({
  node,
  metrics,
  targets,
  onMove,
  onDragStart,
  onDragEnd,
}: {
  node: Node;
  metrics?: NodeMetrics;
  targets: Target[];
  onMove(nodeId: string, groupId: string): void;
  onDragStart(nodeId: string): void;
  onDragEnd(): void;
}) {
  const navigate = useNavigate();
  const [menuOpen, setMenuOpen] = useState(false);
  const name = node.hostname || node.id;
  const sub = imageVersion(node) || nodeAddress(node) || "—";

  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.target !== e.currentTarget) return;
    if (e.key === "Enter") {
      e.preventDefault();
      navigate(`/nodes/${node.id}`);
    } else if (e.key === "m" || e.key === "M") {
      e.preventDefault();
      setMenuOpen(true);
    }
  }

  function handleDragStart(e: DragEvent<HTMLDivElement>) {
    e.dataTransfer?.setData(DRAG_TYPE, node.id);
    if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
    onDragStart(node.id);
  }

  return (
    <li className="group/card relative">
      <div
        role="button"
        tabIndex={0}
        draggable
        aria-label={name}
        aria-keyshortcuts="m"
        onClick={() => navigate(`/nodes/${node.id}`)}
        onKeyDown={handleKeyDown}
        onDragStart={handleDragStart}
        onDragEnd={onDragEnd}
        className="flex min-w-0 cursor-grab items-center gap-2 rounded-md border border-border bg-card py-2 pl-1.5 pr-9 text-sm text-card-foreground transition-colors hover:border-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing"
      >
        <GripVertical className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <StatusDot tone={phaseTone(node.phase)} />
        <span className="flex min-w-0 flex-col">
          <span className="truncate font-medium">{name}</span>
          <span className="truncate font-mono text-xs text-muted-foreground">{sub}</span>
        </span>
        {metrics && <MiniMeters metrics={metrics} />}
      </div>
      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Move ${name} to…`}
            className="absolute right-1 top-1/2 -translate-y-1/2"
          >
            <MoreHorizontal aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>Move to…</DropdownMenuLabel>
          <DropdownMenuSeparator />
          {targets.length === 0 ? (
            <DropdownMenuItem disabled>No other groups</DropdownMenuItem>
          ) : (
            targets.map((t) => (
              <DropdownMenuItem key={t.id || "none"} onSelect={() => onMove(node.id, t.id)}>
                {t.name}
              </DropdownMenuItem>
            ))
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </li>
  );
}

function GroupActions({ group, onRename, onDelete }: { group: Group; onRename(g: Group): void; onDelete(g: Group): void }) {
  const navigate = useNavigate();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={`Actions for ${group.name}`}>
          <MoreHorizontal aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={() => navigate(`/groups/${group.id}`)}>Open</DropdownMenuItem>
        <DropdownMenuItem onSelect={() => navigate(`/groups/${group.id}`)}>Send command</DropdownMenuItem>
        <DropdownMenuItem onSelect={() => onRename(group)}>Rename</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem className="text-danger focus:text-danger" onSelect={() => onDelete(group)}>
          Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// GroupBoard shows one column per group, with "Not in a group" first. Cards
// are dragged between columns, or moved with the m key or the card's ⋯ menu.
// The parent owns the move: it updates the nodes at once and rolls back on
// failure.
export function GroupBoard({ groups, nodes, onMove, onCreate, onRename, onDelete, metrics }: GroupBoardProps) {
  const dragging = useRef<string | null>(null);
  const [over, setOver] = useState<string | null>(null);

  const known = new Set(groups.map((g) => g.id));
  const columns: { id: string; group: Group | null; name: string; nodes: Node[] }[] = [
    {
      id: NO_GROUP,
      group: null,
      name: "Not in a group",
      nodes: nodes.filter((n) => !n.groupID || !known.has(n.groupID)),
    },
    ...groups.map((g) => ({ id: g.id, group: g, name: g.name, nodes: nodes.filter((n) => n.groupID === g.id) })),
  ];
  const allTargets: Target[] = columns.map((c) => ({ id: c.id, name: c.name }));

  function move(nodeId: string, groupId: string) {
    const node = nodes.find((n) => n.id === nodeId);
    if (!node) return;
    const current = node.groupID && known.has(node.groupID) ? node.groupID : NO_GROUP;
    if (current === groupId) return;
    // The parent reports failures with a toast; nothing more to do here.
    onMove(nodeId, groupId).catch(() => {});
  }

  function handleDrop(e: DragEvent<HTMLElement>, groupId: string) {
    e.preventDefault();
    const nodeId = e.dataTransfer?.getData(DRAG_TYPE) || dragging.current;
    dragging.current = null;
    setOver(null);
    if (nodeId) move(nodeId, groupId);
  }

  function handleDragLeave(e: DragEvent<HTMLElement>, groupId: string) {
    const next = e.relatedTarget as globalThis.Node | null;
    if (next && e.currentTarget.contains(next)) return;
    setOver((cur) => (cur === groupId ? null : cur));
  }

  return (
    <div className="flex min-w-0 gap-3 overflow-x-auto pb-2">
      {columns.map((col) => {
        const online = col.nodes.filter((n) => isOnline(n.phase)).length;
        const description = col.group ? col.group.description : "New machines land here";
        return (
          <section
            key={col.id || "none"}
            aria-label={col.name}
            onDragOver={(e) => {
              e.preventDefault();
              if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
              if (over !== col.id) setOver(col.id);
            }}
            onDragLeave={(e) => handleDragLeave(e, col.id)}
            onDrop={(e) => handleDrop(e, col.id)}
            className={cn(
              "flex w-64 shrink-0 flex-col gap-2 rounded-lg border border-border bg-muted/40 p-2 transition-colors",
              !col.group && "border-dashed",
              over === col.id && "border-primary bg-primary-soft outline outline-2 outline-primary",
            )}
          >
            <div className="flex items-center gap-2 px-1">
              {col.group ? (
                <FolderTree className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              ) : (
                <Inbox className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              )}
              <h3 className="min-w-0 truncate text-sm font-semibold">{col.name}</h3>
              <span className="text-xs text-muted-foreground" title={`${online} of ${col.nodes.length} online`}>
                {col.group ? `${online}/${col.nodes.length}` : col.nodes.length}
              </span>
              {col.group && (
                <span className="ml-auto">
                  <GroupActions group={col.group} onRename={onRename} onDelete={onDelete} />
                </span>
              )}
            </div>
            {col.group && <StackBar counts={phaseCounts(col.nodes)} className="mx-1 w-auto" />}
            {description && <p className="px-1 text-xs text-muted-foreground">{description}</p>}
            <ul className="flex flex-col gap-1.5">
              {col.nodes.map((n) => (
                <NodeCard
                  key={n.id}
                  node={n}
                  metrics={metrics?.[n.id]}
                  targets={allTargets.filter((t) => t.id !== col.id)}
                  onMove={move}
                  onDragStart={(id) => {
                    dragging.current = id;
                  }}
                  onDragEnd={() => {
                    dragging.current = null;
                    setOver(null);
                  }}
                />
              ))}
            </ul>
            {col.nodes.length === 0 && (
              <p className="rounded-md border border-dashed border-border px-2 py-4 text-center text-xs text-muted-foreground">
                Drop a node here
              </p>
            )}
          </section>
        );
      })}
      <button
        type="button"
        onClick={onCreate}
        className="flex w-64 shrink-0 items-center justify-center gap-2 rounded-lg border border-dashed border-border p-4 text-sm font-medium text-muted-foreground transition-colors hover:border-primary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Plus className="h-4 w-4" aria-hidden="true" />
        New group
      </button>
    </div>
  );
}
