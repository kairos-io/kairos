import { useEffect, useMemo, useState } from "react";
import { useParams, useNavigate, Link } from "react-router";
import { getGroup, deleteGroup, sendGroupCommand, updateGroup, type Group } from "@/api/groups";
import type { Batch, Rollout } from "@/api/batches";
import { listNodes, sendBulkCommand, type Node } from "@/api/nodes";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { NodeTable } from "@/components/NodeTable";
import { PageHeader } from "@/components/PageHeader";
import { CommandDialog } from "@/components/CommandDialog";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { groupNodeCount, deleteGroupDescription } from "@/lib/groupNodes";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { StatTile } from "@/components/fleet/StatTile";
import { StackBar } from "@/components/fleet/StackBar";
import { SelectionBar } from "@/components/fleet/SelectionBar";
import { RecentBatches } from "@/components/fleet/RecentBatches";
import { Activity, AlertTriangle, Cpu, Download, Layers, MoreHorizontal, ShieldAlert, Terminal } from "lucide-react";
import { isOffline, isOnline } from "@/lib/phase";
import { cpuCount, hasBootIssue, imageVersion, memGiB } from "@/lib/nodeInfo";
import { toast } from "@/hooks/useToast";

const gibPerUnit: Record<string, number> = {
  B: 1 / 1024 ** 3,
  KiB: 1 / 1024 ** 2,
  MiB: 1 / 1024,
  GiB: 1,
  TiB: 1024,
  PiB: 1024 ** 2,
};

// memoryGiB turns the readable size from memGiB back into a number of GiB so
// the group total can be summed.
function memoryGiB(node: Node): number {
  const m = /^(\d+(?:\.\d+)?)\s*(\w+)$/.exec(memGiB(node) ?? "");
  if (!m) return 0;
  return parseFloat(m[1]) * (gibPerUnit[m[2]] ?? 0);
}

export function GroupDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [group, setGroup] = useState<Group | null>(null);
  const [nodes, setNodes] = useState<Node[]>([]);
  // Whether the node list above is the server's answer rather than the empty
  // initial value. See groupNodeCount.
  const [nodesLoaded, setNodesLoaded] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // "group" sends to the whole group, "selection" to the selected rows.
  const [cmdTarget, setCmdTarget] = useState<"group" | "selection" | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState("");
  const [editDescription, setEditDescription] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!id) return;
    getGroup(id).then(setGroup).catch(() => {});
    listNodes({ group_id: id })
      .then((n) => {
        setNodes(n);
        setNodesLoaded(true);
      })
      .catch(() => {});
  }, [id]);

  const selectedIDs = useMemo(() => nodes.filter((n) => selected.has(n.id)).map((n) => n.id), [nodes, selected]);

  const summary = useMemo(() => {
    const online = nodes.filter((n) => isOnline(n.phase)).length;
    const offline = nodes.filter((n) => isOffline(n.phase)).length;
    const versions = new Set(nodes.map(imageVersion).filter(Boolean)).size;
    const bootIssues = nodes.filter(hasBootIssue);
    const vcpu = nodes.reduce((sum, n) => sum + (cpuCount(n) ?? 0), 0);
    const gib = nodes.reduce((sum, n) => sum + memoryGiB(n), 0);
    return { online, offline, other: nodes.length - online - offline, versions, bootIssues, vcpu, gib };
  }, [nodes]);

  async function handleDelete() {
    if (!id || !group) return;
    setConfirmDelete(false);
    try {
      await deleteGroup(id);
      toast(`Deleted group "${group.name}"`, "success");
      navigate("/groups");
    } catch (err) {
      toast(`Failed to delete group: ${(err as Error).message}`, "error");
    }
  }

  function openEdit() {
    if (!group) return;
    setEditName(group.name);
    setEditDescription(group.description ?? "");
    setEditOpen(true);
  }

  async function handleSave() {
    if (!id || !group) return;
    setSaving(true);
    try {
      const updated = await updateGroup(id, { name: editName.trim(), description: editDescription });
      setGroup({ ...group, ...updated });
      setEditOpen(false);
      toast("Group saved", "success");
    } catch (err) {
      toast(`Failed to save group: ${(err as Error).message}`, "error");
    } finally {
      setSaving(false);
    }
  }

  async function handleCommand(
    command: string,
    args: Record<string, unknown>,
    rollout?: Rollout,
  ) {
    if (!id) return;
    let batch: Batch;
    try {
      // The group ID narrows nothing here, since every selected node is in
      // the group, but it records the group so its page lists the batch.
      batch =
        cmdTarget === "selection"
          ? await sendBulkCommand({ groupID: id, nodeIDs: selectedIDs }, command, args, rollout)
          : await sendGroupCommand(id, command, args, rollout);
    } catch (err) {
      toast(`Failed to send command: ${(err as Error).message}`, "error");
      return;
    }
    setCmdTarget(null);
    navigate(`/batches/${batch.id}`);
  }

  if (!group) {
    return <div className="text-muted-foreground">Loading...</div>;
  }

  return (
    <div>
      <PageHeader
        title={group.name}
        description={group.description}
        breadcrumb={[{ label: "Groups", to: "/groups" }, { label: group.name }]}
      >
        <Button variant="outline" asChild>
          <Link to={`/import?group=${id}`}>
            <Download aria-hidden="true" />
            Import nodes
          </Link>
        </Button>
        <Button onClick={() => setCmdTarget("group")}>
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
            <DropdownMenuItem onSelect={openEdit}>Edit name and description</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem className="text-danger focus:text-danger" onSelect={() => setConfirmDelete(true)}>
              Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </PageHeader>

      <section aria-label="Group summary" className="mb-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatTile label="Online" icon={Activity} tone="success" value={`${summary.online} of ${nodes.length} online`}>
          <StackBar
            className="mt-1"
            counts={[
              { tone: "success", count: summary.online, label: "online" },
              { tone: "danger", count: summary.offline, label: "offline" },
              { tone: "neutral", count: summary.other, label: "other" },
            ]}
          />
        </StatTile>
        <StatTile
          label="Image versions"
          icon={Layers}
          value={<span data-testid="stat-versions">{summary.versions}</span>}
          sub={summary.versions > 1 ? "Nodes run different versions" : undefined}
        />
        <StatTile
          label="Boot issues"
          icon={ShieldAlert}
          tone={summary.bootIssues.length > 0 ? "warning" : undefined}
          value={<span data-testid="stat-boot">{summary.bootIssues.length}</span>}
          sub="Recovery or passive boot"
        />
        <StatTile
          label="Capacity"
          icon={Cpu}
          value={<span data-testid="stat-capacity">{summary.vcpu} vCPU</span>}
          sub={`${summary.gib.toFixed(1)} GiB memory`}
        />
      </section>

      {summary.bootIssues.length > 0 && (
        <div className="mb-6 flex flex-col gap-2">
          {summary.bootIssues.map((n) => (
            <div
              key={n.id}
              role="alert"
              className="flex flex-wrap items-center gap-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm"
            >
              <AlertTriangle className="h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
              <Link to={`/nodes/${n.id}`} className="font-medium hover:underline">
                {n.hostname || n.id}
              </Link>
              <span className="text-muted-foreground">booted from the {(n.bootState ?? "").toLowerCase()} image</span>
            </div>
          ))}
        </div>
      )}

      <h2 className="text-xl font-semibold mb-4">Nodes</h2>
      <SelectionBar count={selectedIDs.length} onClear={() => setSelected(new Set())}>
        <Button size="sm" onClick={() => setCmdTarget("selection")}>
          <Terminal aria-hidden="true" />
          Send command
        </Button>
      </SelectionBar>
      <NodeTable nodes={nodes} showGroupColumn={false} selectable selected={selected} onSelectedChange={setSelected} />

      <div className="mt-6">
        <RecentBatches groupID={group.id} />
      </div>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title="Delete group"
        description={deleteGroupDescription(group.name, groupNodeCount(group, nodes, nodesLoaded))}
        confirmLabel="Delete"
        destructive
        onConfirm={handleDelete}
      />

      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit group</DialogTitle>
            <DialogDescription>Change the name or description of this group.</DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="group-edit-name">Name</Label>
              <Input id="group-edit-name" value={editName} onChange={(e) => setEditName(e.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="group-edit-desc">Description</Label>
              <Input id="group-edit-desc" value={editDescription} onChange={(e) => setEditDescription(e.target.value)} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleSave} disabled={!editName.trim()} loading={saving}>
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <CommandDialog
        open={cmdTarget !== null}
        onOpenChange={(open) => {
          if (!open) setCmdTarget(null);
        }}
        onSubmit={handleCommand}
        targetCount={
          cmdTarget === "selection"
            ? selectedIDs.length
            : groupNodeCount(group, nodes, nodesLoaded)
        }
        title={
          cmdTarget === "selection"
            ? `Send command · ${selectedIDs.length} selected`
            : `Send command · ${group.name}`
        }
      />
    </div>
  );
}
