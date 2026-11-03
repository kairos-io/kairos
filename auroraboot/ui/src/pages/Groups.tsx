import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { listGroups, createGroup, deleteGroup, updateGroup, type Group } from "@/api/groups";
import { listNodes, setGroup, type Node } from "@/api/nodes";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { PageHeader } from "@/components/PageHeader";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { GroupBoard } from "@/components/fleet/GroupBoard";
import { StackBar } from "@/components/fleet/StackBar";
import { phaseCounts } from "@/lib/nodeFilter";
import { groupNodeCount, deleteGroupDescription } from "@/lib/groupNodes";
import { cn } from "@/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Plus, FolderTree, Trash2 } from "lucide-react";
import { toast } from "@/hooks/useToast";
import { useLatestMetrics } from "@/hooks/useMetrics";

type View = "board" | "table";

const views: { value: View; label: string }[] = [
  { value: "board", label: "Board" },
  { value: "table", label: "Table" },
];

function ViewSwitch({ value, onChange }: { value: View; onChange(v: View): void }) {
  return (
    <div role="radiogroup" aria-label="View" className="inline-flex h-8 rounded-md border border-input p-0.5">
      {views.map((v) => (
        <button
          key={v.value}
          type="button"
          role="radio"
          aria-checked={value === v.value}
          onClick={() => onChange(v.value)}
          className={cn(
            "rounded px-3 text-sm text-muted-foreground hover:text-foreground",
            value === v.value && "bg-muted font-medium text-foreground",
          )}
        >
          {v.label}
        </button>
      ))}
    </div>
  );
}

export function Groups() {
  const [groups, setGroups] = useState<Group[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  // Whether the node list above is the server's answer rather than the empty
  // initial value. It decides which of the two node counts this page can
  // believe; see nodeCountOf.
  const [nodesLoaded, setNodesLoaded] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  // The group being renamed; null when the dialog creates a new group.
  const [editing, setEditing] = useState<Group | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [confirmTarget, setConfirmTarget] = useState<Group | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const view: View = searchParams.get("view") === "table" ? "table" : "board";
  const { byNode } = useLatestMetrics();
  const metrics = nodes.some((n) => byNode[n.id]) ? byNode : undefined;

  function load() {
    listGroups().then(setGroups).catch(() => {});
    listNodes()
      .then((n) => {
        setNodes(n);
        setNodesLoaded(true);
      })
      .catch(() => {});
  }

  // A move patches `nodes` the moment the card is dropped, and takes it back
  // out if the server refuses it, so counting from there follows a move without
  // a reload.
  const nodeCountOf = (group: Group) => groupNodeCount(group, nodes, nodesLoaded);

  useEffect(() => {
    load();
  }, []);

  function setView(next: View) {
    setSearchParams(
      (prev) => {
        const sp = new URLSearchParams(prev);
        if (next === "table") sp.set("view", "table");
        else sp.delete("view");
        return sp;
      },
      { replace: true },
    );
  }

  function openCreate() {
    setEditing(null);
    setName("");
    setDescription("");
    setDialogOpen(true);
  }

  function openRename(g: Group) {
    setEditing(g);
    setName(g.name);
    setDescription(g.description);
    setDialogOpen(true);
  }

  async function handleSave() {
    if (!name.trim()) return;
    try {
      if (editing) await updateGroup(editing.id, { name, description });
      else await createGroup({ name, description });
    } catch (err) {
      toast(`Failed to save group: ${(err as Error).message}`, "error");
      return;
    }
    setName("");
    setDescription("");
    setEditing(null);
    setDialogOpen(false);
    load();
  }

  // handleMove shows the move at once and puts the node back if the server
  // refuses it.
  async function handleMove(nodeId: string, groupId: string) {
    const node = nodes.find((n) => n.id === nodeId);
    if (!node) return;
    const previous = node.groupID;
    const patch = (groupID: string | null) =>
      setNodes((cur) => cur.map((n) => (n.id === nodeId ? { ...n, groupID } : n)));
    // The board names "not in a group" with an empty id.
    const target = groupId === "" ? null : groupId;
    patch(target);
    try {
      await setGroup(nodeId, target);
    } catch (err) {
      patch(previous);
      toast(`Could not move ${node.hostname || node.id}: ${(err as Error).message}`, "error");
      throw err;
    }
  }

  async function handleConfirmDelete() {
    if (!confirmTarget) return;
    const target = confirmTarget;
    setConfirmTarget(null);
    try {
      await deleteGroup(target.id);
      toast(`Deleted group "${target.name}"`, "success");
      load();
    } catch (err) {
      toast(`Failed to delete group: ${(err as Error).message}`, "error");
    }
  }

  return (
    <div className="min-w-0">
      <PageHeader title="Groups" description="Drag a node onto a group to move it. The node picks up the group's config the next time it checks in.">
        <ViewSwitch value={view} onChange={setView} />
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4 mr-2" />
          New group
        </Button>
      </PageHeader>

      {view === "board" ? (
        <>
          <GroupBoard
            groups={groups}
            nodes={nodes}
            onMove={handleMove}
            onCreate={openCreate}
            onRename={openRename}
            onDelete={setConfirmTarget}
            metrics={metrics}
          />
          <p className="mt-3 text-xs text-muted-foreground">
            Keyboard: focus a card, press <kbd className="rounded border border-border bg-muted px-1 font-mono">m</kbd> and
            choose the target group. Enter opens the node.
            {metrics && " Bars on each card: CPU, memory, disk."}
          </p>
        </>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Nodes</TableHead>
              <TableHead>Health</TableHead>
              <TableHead className="w-12" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center py-12">
                  <div className="flex flex-col items-center gap-3 py-16">
                    <FolderTree className="h-12 w-12 text-muted-foreground/30" />
                    <div className="text-center">
                      <p className="font-medium">No groups</p>
                      <p className="text-sm text-muted-foreground mt-1">
                        Create a group to organize your nodes by environment or role.
                      </p>
                    </div>
                    <Button className="mt-2" onClick={openCreate}>
                      <Plus className="h-4 w-4 mr-2" /> New group
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ) : (
              groups.map((group) => (
                <TableRow
                  key={group.id}
                  className="cursor-pointer hover:bg-primary-soft"
                  onClick={() => navigate(`/groups/${group.id}`)}
                >
                  <TableCell className="font-medium">{group.name}</TableCell>
                  <TableCell>{group.description || "-"}</TableCell>
                  <TableCell>{nodeCountOf(group) ?? 0}</TableCell>
                  <TableCell className="w-40">
                    <StackBar counts={phaseCounts(nodes.filter((n) => n.groupID === group.id))} />
                  </TableCell>
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-red-500 hover:text-red-700"
                      aria-label={`Delete ${group.name}`}
                      title="Delete group"
                      onClick={() => setConfirmTarget(group)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      )}

      <ConfirmDialog
        open={!!confirmTarget}
        onOpenChange={(open) => !open && setConfirmTarget(null)}
        title="Delete group"
        description={confirmTarget ? deleteGroupDescription(confirmTarget.name, nodeCountOf(confirmTarget)) : ""}
        confirmLabel="Delete"
        destructive
        onConfirm={handleConfirmDelete}
      />

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "Rename group" : "New group"}</DialogTitle>
            <DialogDescription>
              {editing ? "Change the name or description of this group." : "Add a new node group to organize your machines."}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="group-name">Name</Label>
              <Input
                id="group-name"
                placeholder="production-cluster"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="group-desc">Description</Label>
              <Input
                id="group-desc"
                placeholder="Production Kubernetes cluster nodes"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleSave} disabled={!name.trim()}>
              {editing ? "Save" : "Create group"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
