import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import type { Rollout } from "@/api/batches";
import { listNodes, sendBulkCommand, type Node } from "@/api/nodes";
import { listGroups, type Group } from "@/api/groups";
import { NodeTable } from "@/components/NodeTable";
import { PageHeader } from "@/components/PageHeader";
import { CommandDialog } from "@/components/CommandDialog";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Activity, Folder, FolderInput, LayoutGrid, List, Search, SearchX, Tag, Terminal } from "lucide-react";
import { FilterChip } from "@/components/fleet/FilterChip";
import { NodeSummary } from "@/components/fleet/NodeSummary";
import { toast } from "@/hooks/useToast";
import { EmptyState } from "@/components/fleet/EmptyState";
import { SelectionBar } from "@/components/fleet/SelectionBar";
import { NodeTiles, type TileColorBy } from "@/components/fleet/NodeTiles";
import { useLatestMetrics } from "@/hooks/useMetrics";
import { MoveToGroupDialog } from "@/components/fleet/MoveToGroupDialog";
import { AddLabelDialog } from "@/components/fleet/AddLabelDialog";
import { cn } from "@/lib/utils";
import {
  filterNodes,
  groupNodes,
  hasActiveFilter,
  parseQuery,
  phaseCounts,
  queryKeys,
  toSearchParams,
  UNGROUPED,
  type NodeQuery,
} from "@/lib/nodeFilter";

const defaultPhases = ["Online", "Offline", "Pending"];
const defaultGroupKey = "site";

const views = [
  { value: "list", label: "List", icon: List },
  { value: "tiles", label: "Tiles", icon: LayoutGrid },
] as const;

const colorOptions: { value: TileColorBy; label: string }[] = [
  { value: "status", label: "Status" },
  { value: "cpu", label: "CPU" },
  { value: "memory", label: "Memory" },
  { value: "disk", label: "Disk" },
];

function parseColorBy(v: string | null): TileColorBy {
  return colorOptions.find((o) => o.value === v)?.value ?? "status";
}

function ColorByToggle({ value, onChange }: { value: TileColorBy; onChange(v: TileColorBy): void }) {
  return (
    <div className="mb-3 flex items-center gap-2">
      <span className="text-xs text-muted-foreground">Color by</span>
      <div role="radiogroup" aria-label="Color by" className="inline-flex rounded-md border border-input p-0.5">
        {colorOptions.map((o) => (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={value === o.value}
            onClick={() => onChange(o.value)}
            className={cn(
              "rounded px-2.5 py-1 text-xs font-medium text-muted-foreground hover:text-foreground",
              value === o.value && "bg-muted text-foreground",
            )}
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function ViewToggle({ value, onChange }: { value: "list" | "tiles"; onChange(v: "list" | "tiles"): void }) {
  return (
    <div role="radiogroup" aria-label="View" className="inline-flex rounded-md border border-input p-0.5">
      {views.map(({ value: v, label, icon: Icon }) => (
        <button
          key={v}
          type="button"
          role="radio"
          aria-checked={value === v}
          aria-label={label}
          title={label}
          onClick={() => onChange(v)}
          className={cn(
            "inline-flex h-7 w-7 items-center justify-center rounded text-muted-foreground hover:text-foreground",
            value === v && "bg-muted text-foreground",
          )}
        >
          <Icon className="h-4 w-4" aria-hidden="true" />
        </button>
      ))}
    </div>
  );
}

export function Nodes() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [bulkCmdOpen, setBulkCmdOpen] = useState(false);
  const [confirmState, setConfirmState] = useState<{ open: boolean; action: () => void }>({ open: false, action: () => {} });
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [moveOpen, setMoveOpen] = useState(false);
  const [labelOpen, setLabelOpen] = useState(false);

  const query = parseQuery(searchParams);
  const view = searchParams.get("view") === "tiles" ? "tiles" : "list";
  const colorBy = parseColorBy(searchParams.get("color"));
  const { byNode } = useLatestMetrics();
  // Usage columns and tile colors only appear once a node reports metrics, so
  // a fleet on older agents sees the page as before.
  const metrics = nodes.some((n) => byNode[n.id]) ? byNode : undefined;

  function setColorBy(next: TileColorBy) {
    setSearchParams(
      (prev) => {
        const sp = new URLSearchParams(prev);
        if (next === "status") sp.delete("color");
        else sp.set("color", next);
        return sp;
      },
      { replace: true },
    );
  }

  function setView(next: "list" | "tiles") {
    setSearchParams(
      (prev) => {
        const sp = new URLSearchParams(prev);
        if (next === "tiles") sp.set("view", "tiles");
        else sp.delete("view");
        return sp;
      },
      { replace: true },
    );
  }

  // Write the query to the URL, keeping keys this page does not own (such as
  // `view`) so a shared link keeps them.
  function setQuery(patch: Partial<NodeQuery>) {
    const next = toSearchParams({ ...query, ...patch });
    for (const [k, v] of searchParams) {
      if (!(queryKeys as readonly string[]).includes(k)) next.append(k, v);
    }
    setSearchParams(next, { replace: true });
  }

  // The whole fleet is loaded once per tick and filtered here, so the summary
  // strip always describes every node while the table shows the matches.
  const load = useCallback(() => {
    listNodes().then(setNodes).catch(() => {});
  }, []);

  useEffect(() => {
    listGroups().then(setGroups).catch(() => {});
  }, []);

  // Poll the node list so newly-registered machines appear without a
  // manual refresh. Five seconds matches the kairos-agent's default
  // reconnect backoff, so a freshly-booted node typically shows up on
  // the next tick after it phones home.
  useEffect(() => {
    load();
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
  }, [load]);

  const filteredNodes = filterNodes(nodes, query);
  const buckets = query.groupBy === "none" ? undefined : groupNodes(filteredNodes, query.groupBy, groups);

  const phaseOptions = useMemo(() => {
    const names = new Map(defaultPhases.map((p) => [p.toLowerCase(), p]));
    for (const p of phaseCounts(nodes)) if (!names.has(p.label)) names.set(p.label, p.name);
    return [...names.values()].map((p) => ({ value: p, label: p }));
  }, [nodes]);

  const groupOptions = useMemo(
    () => [...groups.map((g) => ({ value: g.id, label: g.name })), { value: UNGROUPED, label: "Not in a group" }],
    [groups],
  );

  const labelOptions = useMemo(() => {
    const pairs = new Set<string>();
    for (const n of nodes) for (const [k, v] of Object.entries(n.labels ?? {})) pairs.add(`${k}=${v}`);
    return [...pairs].sort().map((p) => ({ value: p, label: p }));
  }, [nodes]);

  const labelKeys = useMemo(() => {
    const keys = new Set<string>([defaultGroupKey]);
    for (const n of nodes) for (const k of Object.keys(n.labels ?? {})) keys.add(k);
    if (query.groupBy !== "none" && query.groupBy !== "group") keys.add(query.groupBy);
    return [...keys].sort();
  }, [nodes, query.groupBy]);

  // Only visible rows count as selected: a node that a filter hides, or the
  // tile view (which has no checkboxes), never receives a bulk action.
  const selectedNodes = useMemo(
    () => (view === "list" ? filteredNodes.filter((n) => selected.has(n.id)) : []),
    [view, filteredNodes, selected],
  );
  const hasSelection = selectedNodes.length > 0;

  // The bulk command targets the selected rows, or else exactly the nodes on
  // screen: every filter is applied to filteredNodes, so we always send their
  // IDs instead of a selector the server would resolve differently.
  const targetNodes = hasSelection ? selectedNodes : filteredNodes;
  const targetCount = targetNodes.length;
  const anyFilterActive = hasActiveFilter(query);
  const [confirmCommand, setConfirmCommand] = useState("");

  function afterBulkEdit() {
    setSelected(new Set());
    load();
  }

  function handleBulkSubmit(
    command: string,
    args: Record<string, unknown>,
    rollout?: Rollout,
  ) {
    const nodeIDs = targetNodes.map((n) => n.id);
    const send = () => {
      sendBulkCommand({ nodeIDs }, command, args, rollout)
        .then((batch) => navigate(`/batches/${batch.id}`))
        .catch((err) => {
          toast(`Failed to send command: ${(err as Error).message}`, "error");
        });
      setBulkCmdOpen(false);
    };

    // With no filter and no selection the command reaches the whole fleet, so
    // ask first.
    if (!anyFilterActive && !hasSelection) {
      setConfirmCommand(command);
      setConfirmState({ open: true, action: send });
      return;
    }

    send();
  }

  return (
    <div>
      <PageHeader title="Nodes" description="Manage your registered machines">
        <Button
          disabled={targetCount === 0}
          onClick={() => setBulkCmdOpen(true)}
        >
          <Terminal className="h-4 w-4 mr-2" />
          Send Command{targetCount > 0 ? ` to ${targetCount} node${targetCount !== 1 ? "s" : ""}` : ""}
        </Button>
      </PageHeader>

      {nodes.length > 0 && <NodeSummary nodes={nodes} />}

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="relative w-full sm:w-72">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
          <Input
            aria-label="Search hostname, IP, label or image version"
            placeholder="Search by hostname..."
            className="pl-8"
            value={query.q}
            onChange={(e) => setQuery({ q: e.target.value })}
          />
        </div>
        <FilterChip
          label="Status"
          icon={Activity}
          value={query.phase}
          options={phaseOptions}
          onSelect={(phase) => setQuery({ phase })}
          onClear={() => setQuery({ phase: "" })}
        />
        <FilterChip
          label="Group"
          icon={Folder}
          value={query.group}
          options={groupOptions}
          onSelect={(group) => setQuery({ group })}
          onClear={() => setQuery({ group: "" })}
        />
        <FilterChip
          label="Label"
          icon={Tag}
          value={query.label}
          options={labelOptions}
          emptyText="No labels in the fleet"
          onSelect={(label) => setQuery({ label })}
          onClear={() => setQuery({ label: "" })}
        />
        <div className="flex items-center gap-2 sm:ml-auto">
          <span className="text-xs text-muted-foreground">Group by</span>
          <Select value={query.groupBy} onValueChange={(groupBy) => setQuery({ groupBy })}>
            <SelectTrigger className="h-8 w-36" aria-label="Group by">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">None</SelectItem>
              <SelectItem value="group">Group</SelectItem>
              {labelKeys.map((k) => (
                <SelectItem key={k} value={k}>
                  Label: {k}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <ViewToggle value={view} onChange={setView} />
        </div>
      </div>

      {view === "list" && (
        <SelectionBar count={selectedNodes.length} onClear={() => setSelected(new Set())}>
          <Button size="sm" onClick={() => setBulkCmdOpen(true)}>
            <Terminal className="h-4 w-4" aria-hidden="true" />
            Send command
          </Button>
          <Button size="sm" variant="secondary" onClick={() => setMoveOpen(true)}>
            <FolderInput className="h-4 w-4" aria-hidden="true" />
            Move to group
          </Button>
          <Button size="sm" variant="secondary" onClick={() => setLabelOpen(true)}>
            <Tag className="h-4 w-4" aria-hidden="true" />
            Add label
          </Button>
        </SelectionBar>
      )}

      {nodes.length > 0 && filteredNodes.length === 0 ? (
        <EmptyState
          icon={SearchX}
          title="No nodes match"
          text="Change the search or remove a filter to see more nodes."
          action={
            <Button variant="outline" onClick={() => setQuery({ q: "", phase: "", group: "", label: "" })}>
              Clear filters
            </Button>
          }
        />
      ) : view === "tiles" && filteredNodes.length > 0 ? (
        <>
          {metrics && <ColorByToggle value={colorBy} onChange={setColorBy} />}
          <NodeTiles
            groups={buckets ?? [{ key: "", label: "", nodes: filteredNodes }]}
            colorBy={metrics ? colorBy : "status"}
            metrics={metrics}
          />
        </>
      ) : (
        <NodeTable
          nodes={filteredNodes}
          groups={buckets}
          showGroupColumn={query.groupBy !== "group"}
          selectable
          selected={selected}
          onSelectedChange={setSelected}
          emptyAction={() => navigate("/import")}
          metrics={metrics}
        />
      )}

      <MoveToGroupDialog
        open={moveOpen}
        onOpenChange={setMoveOpen}
        nodeIds={selectedNodes.map((n) => n.id)}
        groups={groups}
        onDone={afterBulkEdit}
      />

      <AddLabelDialog open={labelOpen} onOpenChange={setLabelOpen} nodes={selectedNodes} onDone={afterBulkEdit} />

      <CommandDialog
        open={bulkCmdOpen}
        onOpenChange={setBulkCmdOpen}
        onSubmit={handleBulkSubmit}
        targetCount={targetCount}
        title={`Send Command to ${targetCount} node${targetCount !== 1 ? "s" : ""}`}
      />

      <ConfirmDialog
        open={confirmState.open}
        onOpenChange={(open) => setConfirmState(prev => ({ ...prev, open }))}
        title="Send to all nodes"
        description={`Send ${confirmCommand} to all ${targetCount} nodes?`}
        confirmLabel="Send to all"
        onConfirm={confirmState.action}
      />
    </div>
  );
}
