import { Fragment, useState } from "react";
import { useNavigate } from "react-router";
import type { Node } from "@/api/nodes";
import type { NodeMetrics } from "@/api/metrics";
import { StatusBadge } from "@/components/StatusBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ChevronDown, ChevronRight, Server, Plus } from "lucide-react";
import { timeAgo } from "@/lib/time";
import { isOnline, phaseTone } from "@/lib/phase";
import { cpuCount, hasBootIssue, imageVersion, memGiB, nodeAddress } from "@/lib/nodeInfo";
import { phaseCounts } from "@/lib/nodeFilter";
import { StatusDot } from "@/components/fleet/StatusDot";
import { StackBar } from "@/components/fleet/StackBar";
import { EmptyState } from "@/components/fleet/EmptyState";
import { MeterBar } from "@/components/fleet/MeterBar";
import { cpuPercent, maxDiskPercent, memPercent } from "@/lib/metrics";

export interface NodeTableGroup {
  key: string;
  label: string;
  nodes: Node[];
}

interface NodeTableProps {
  nodes: Node[];
  // When set, the table shows a header row per group and the rows under it.
  groups?: NodeTableGroup[];
  showGroupColumn?: boolean;
  selectable?: boolean;
  selected?: Set<string>;
  onSelectedChange?(next: Set<string>): void;
  emptyAction?: () => void;
  // Latest agent metrics by node ID. When set, the CPU, Memory and Disk
  // columns replace Kairos and Hardware.
  metrics?: Record<string, NodeMetrics>;
}

function hardware(node: Node): string {
  const cpu = cpuCount(node);
  const mem = memGiB(node);
  const parts = [cpu !== null ? `${cpu} vCPU` : null, mem].filter(Boolean);
  return parts.length ? parts.join(" · ") : "—";
}

function SelectBox({
  checked,
  indeterminate,
  label,
  onChange,
}: {
  checked: boolean;
  indeterminate?: boolean;
  label: string;
  onChange(checked: boolean): void;
}) {
  return (
    <input
      type="checkbox"
      aria-label={label}
      className="h-4 w-4 cursor-pointer accent-primary"
      checked={checked}
      ref={(el) => {
        if (el) el.indeterminate = !!indeterminate && !checked;
      }}
      onClick={(e) => e.stopPropagation()}
      onChange={(e) => onChange(e.target.checked)}
    />
  );
}

export function NodeTable({
  nodes,
  groups,
  showGroupColumn = false,
  selectable = false,
  selected,
  onSelectedChange,
  emptyAction,
  metrics,
}: NodeTableProps) {
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const sel = selected ?? new Set<string>();
  const columns = (metrics ? 8 : 7) + (selectable ? 1 : 0) + (showGroupColumn ? 1 : 0);
  const allNodes = groups ? groups.flatMap((g) => g.nodes) : nodes;

  function setMany(ids: string[], on: boolean) {
    const next = new Set(sel);
    for (const id of ids) {
      if (on) next.add(id);
      else next.delete(id);
    }
    onSelectedChange?.(next);
  }

  function selectState(ids: string[]) {
    const n = ids.filter((id) => sel.has(id)).length;
    return { checked: ids.length > 0 && n === ids.length, indeterminate: n > 0 };
  }

  function toggleCollapsed(key: string) {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  function row(node: Node) {
    const tone = phaseTone(node.phase);
    const address = nodeAddress(node);
    const image = imageVersion(node);
    const labels = Object.entries(node.labels || {});
    const online = isOnline(node.phase);
    const boot = hasBootIssue(node) ? (node.bootState ?? "").toLowerCase() : "";
    return (
      <TableRow
        key={node.id}
        className="cursor-pointer hover:bg-primary-soft"
        data-state={sel.has(node.id) ? "selected" : undefined}
        onClick={() => navigate(`/nodes/${node.id}`)}
      >
        {selectable && (
          // A click near the checkbox must not open the node either.
          <TableCell className="w-8" onClick={(e) => e.stopPropagation()}>
            <SelectBox
              label={`Select ${node.hostname || node.id}`}
              checked={sel.has(node.id)}
              onChange={(on) => setMany([node.id], on)}
            />
          </TableCell>
        )}
        <TableCell>
          <div className="flex min-w-0 items-center gap-2">
            <StatusDot tone={tone} />
            <div className="min-w-0">
              <div className="truncate font-medium">{node.hostname || node.id}</div>
              <div className="truncate font-mono text-xs text-muted-foreground">{address || "no address yet"}</div>
            </div>
          </div>
        </TableCell>
        <TableCell>
          <div className="flex flex-wrap items-center gap-1">
            {!online && <StatusBadge status={node.phase} />}
            {boot && (
              <Badge variant="outline" className="border-warning/25 bg-warning/15 text-warning-foreground">
                {boot}
              </Badge>
            )}
          </div>
        </TableCell>
        <TableCell className="font-mono text-xs">
          {image || <span className="text-muted-foreground">—</span>}
        </TableCell>
        {metrics ? (
          <>
            <TableCell className="text-xs">
              <MeterBar value={cpuPercent(metrics[node.id])} />
            </TableCell>
            <TableCell className="text-xs">
              <MeterBar value={memPercent(metrics[node.id])} />
            </TableCell>
            <TableCell className="text-xs">
              <MeterBar value={maxDiskPercent(metrics[node.id])} />
            </TableCell>
          </>
        ) : (
          <>
            <TableCell className="font-mono text-xs text-muted-foreground">{node.agentVersion || "—"}</TableCell>
            <TableCell className="whitespace-nowrap text-xs text-muted-foreground">{hardware(node)}</TableCell>
          </>
        )}
        <TableCell className="whitespace-nowrap text-xs">{timeAgo(node.lastHeartbeat)}</TableCell>
        <TableCell>
          {labels.length === 0 ? (
            <span className="text-xs text-muted-foreground">—</span>
          ) : (
            <div className="flex flex-nowrap items-center gap-1">
              <Badge variant="secondary" className="text-xs">
                {labels[0][0]}={labels[0][1]}
              </Badge>
              {labels.length > 1 && (
                <span className="text-xs text-muted-foreground" title={labels.slice(1).map(([k, v]) => `${k}=${v}`).join(", ")}>
                  +{labels.length - 1}
                </span>
              )}
            </div>
          )}
        </TableCell>
        {showGroupColumn && (
          <TableCell className="text-xs">{node.group?.name || (node.groupID ? node.groupID : "—")}</TableCell>
        )}
      </TableRow>
    );
  }

  function groupHeader(g: NodeTableGroup) {
    const ids = g.nodes.map((n) => n.id);
    const open = !collapsed.has(g.key);
    const online = g.nodes.filter((n) => isOnline(n.phase)).length;
    const st = selectState(ids);
    return (
      <TableRow key={`group-${g.key}`} data-testid="group-header" className="bg-muted/50 hover:bg-muted/50">
        {selectable && (
          <TableCell className="w-8">
            <SelectBox
              label={`Select all in ${g.label}`}
              checked={st.checked}
              indeterminate={st.indeterminate}
              onChange={(on) => setMany(ids, on)}
            />
          </TableCell>
        )}
        <TableCell colSpan={columns - (selectable ? 1 : 0)}>
          <div className="flex flex-wrap items-center gap-3">
            <button
              type="button"
              className="inline-flex items-center gap-1 font-medium"
              aria-expanded={open}
              aria-label={`${open ? "Collapse" : "Expand"} ${g.label}`}
              onClick={() => toggleCollapsed(g.key)}
            >
              {open ? <ChevronDown className="h-4 w-4" aria-hidden="true" /> : <ChevronRight className="h-4 w-4" aria-hidden="true" />}
            </button>
            <span className="font-medium" data-testid="group-label">
              {g.label}
            </span>
            <span className="text-xs text-muted-foreground">
              {g.nodes.length} node{g.nodes.length !== 1 ? "s" : ""}
            </span>
            <StackBar counts={phaseCounts(g.nodes)} className="w-20" />
            <span className="text-xs text-muted-foreground">
              {online}/{g.nodes.length} online
            </span>
          </div>
        </TableCell>
      </TableRow>
    );
  }

  const headerState = selectState(allNodes.map((n) => n.id));

  return (
    <Table>
      <TableHeader>
        <TableRow>
          {selectable && (
            <TableHead className="w-8">
              <SelectBox
                label="Select all nodes"
                checked={headerState.checked}
                indeterminate={headerState.indeterminate}
                onChange={(on) => setMany(allNodes.map((n) => n.id), on)}
              />
            </TableHead>
          )}
          <TableHead>Node</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Image</TableHead>
          {metrics ? (
            <>
              <TableHead>CPU</TableHead>
              <TableHead>Memory</TableHead>
              <TableHead>Disk</TableHead>
            </>
          ) : (
            <>
              <TableHead>Kairos</TableHead>
              <TableHead>Hardware</TableHead>
            </>
          )}
          <TableHead>Last heartbeat</TableHead>
          <TableHead>Labels</TableHead>
          {showGroupColumn && <TableHead>Group</TableHead>}
        </TableRow>
      </TableHeader>
      <TableBody>
        {allNodes.length === 0 ? (
          <TableRow>
            <TableCell colSpan={columns}>
              <EmptyState
                icon={Server}
                title="No nodes registered"
                text="Import your first node to start managing your fleet."
                action={
                  emptyAction && (
                    <Button onClick={emptyAction}>
                      <Plus className="h-4 w-4" /> Import first node
                    </Button>
                  )
                }
              />
            </TableCell>
          </TableRow>
        ) : groups ? (
          groups.map((g) => (
            <Fragment key={g.key}>
              {groupHeader(g)}
              {!collapsed.has(g.key) && g.nodes.map(row)}
            </Fragment>
          ))
        ) : (
          nodes.map(row)
        )}
      </TableBody>
    </Table>
  );
}
