import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { listGroups, type Group } from "@/api/groups";
import { listNodes, sendBulkCommand, type Node } from "@/api/nodes";
import { extensionSourceUrl, type Extension } from "@/api/extensions";
import { ExtensionTypeChip } from "@/components/ExtensionTypeChip";

type Action = "install" | "enable" | "disable" | "remove";
type Scope = "active" | "passive" | "recovery" | "common";
type TargetKind = "group" | "node";

interface Props {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  extension: Extension;
  /** Pre-select a single node target (e.g. when opened from NodeDetail). */
  presetNodeID?: string;
}

export function InstallExtensionDialog({
  open,
  onOpenChange,
  extension,
  presetNodeID,
}: Props) {
  const [targetKind, setTargetKind] = useState<TargetKind>(
    presetNodeID ? "node" : "group",
  );
  const [groups, setGroups] = useState<Group[]>([]);
  const [groupID, setGroupID] = useState<string>("");
  const [nodes, setNodes] = useState<Node[]>([]);
  const [nodeID, setNodeID] = useState<string>(presetNodeID ?? "");
  const [action, setAction] = useState<Action>("install");
  const [bootState, setBootState] = useState<Scope>("common");
  const [now, setNow] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Re-sync tab + selection with the current preset each time the dialog opens.
  // Done during render rather than in an effect, matching CommandDialog: an
  // effect that calls setState synchronously triggers a second render pass
  // before paint, which is what react-hooks/set-state-in-effect flags.
  const [prevOpen, setPrevOpen] = useState(open);
  if (open !== prevOpen) {
    setPrevOpen(open);
    if (open && presetNodeID) {
      setTargetKind("node");
      setNodeID(presetNodeID);
    }
  }

  useEffect(() => {
    if (!open) return;
    listGroups()
      .then((gs) => {
        setGroups(gs);
        if (gs.length === 1) setGroupID(gs[0].id);
      })
      .catch(() => {});
    listNodes()
      .then((ns) => setNodes(ns))
      .catch(() => {});
  }, [open, presetNodeID]);

  const args: Record<string, string> = {
    type: extension.type,
    action,
    name: extension.name,
    bootState,
    now: now ? "true" : "false",
  };
  // The source URL travels to every node in the selector and is kept in the
  // commands table, so it carries the extension's own download token rather
  // than the admin password. sourcePath is null when the build minted no
  // token; the install button stays disabled instead of sending a source that
  // would 401 on every node.
  const sourcePath = extensionSourceUrl(extension);
  if (action === "install" && sourcePath) {
    args.source = window.location.origin + sourcePath;
  }

  const canSend =
    !submitting &&
    (action !== "install" || sourcePath !== null) &&
    ((targetKind === "group" && groupID !== "") ||
      (targetKind === "node" && nodeID !== ""));

  async function handleSend() {
    setSubmitting(true);
    setErr(null);
    try {
      // Both paths use the bulk-command endpoint — {groupID} for the group
      // fan-out, {nodeIDs:[…]} for a single-node send. Keeps the server contract
      // uniform and lets tracking write per-node rows either way.
      const selector =
        targetKind === "group" ? { groupID } : { nodeIDs: [nodeID] };
      await sendBulkCommand(selector, "extension", args);
      onOpenChange(false);
    } catch (e) {
      setErr(String(e));
    } finally {
      setSubmitting(false);
    }
  }

  const submitLabel = submitting
    ? "Sending…"
    : targetKind === "group"
      ? "Send to group"
      : "Send to node";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[720px]">
        <DialogHeader>
          <DialogTitle>
            <span className="inline-flex items-center gap-2.5">
              <ExtensionTypeChip type={extension.type} />
              <span className="font-semibold">{extension.name}</span>
              <code className="text-xs opacity-60">
                {extension.version} · {extension.arch}
              </code>
            </span>
          </DialogTitle>
        </DialogHeader>

        <p className="text-sm text-muted-foreground">
          Installing an extension with the same name again upgrades it.
        </p>

        <Section label="Target">
          <div className="flex gap-1.5">
            <TargetTab
              active={targetKind === "group"}
              label="Group"
              onClick={() => setTargetKind("group")}
            />
            <TargetTab
              active={targetKind === "node"}
              label="Node"
              onClick={() => setTargetKind("node")}
            />
          </div>
          {targetKind === "group" ? (
            <Select value={groupID} onValueChange={setGroupID}>
              <SelectTrigger aria-label="Group" className="mt-2">
                <SelectValue placeholder="Select a group…" />
              </SelectTrigger>
              <SelectContent>
                {groups.map((g) => (
                  <SelectItem key={g.id} value={g.id}>
                    {g.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Select value={nodeID} onValueChange={setNodeID}>
              <SelectTrigger aria-label="Node" className="mt-2">
                <SelectValue placeholder="Select a node…" />
              </SelectTrigger>
              <SelectContent>
                {nodes.map((n) => (
                  <SelectItem key={n.id} value={n.id}>
                    {n.hostname || n.id.slice(0, 8)}
                    {n.group?.name ? ` · ${n.group.name}` : ""}
                    {n.phase ? ` · ${n.phase}` : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Section>

        <Section label="Action">
          <div className="grid grid-cols-4 gap-1.5">
            {(["install", "enable", "disable", "remove"] as const).map((a) => (
              <button
                key={a}
                type="button"
                aria-pressed={action === a}
                onClick={() => setAction(a)}
                className={`px-2.5 py-2 border rounded-md text-xs ${
                  action === a
                    ? "border-primary bg-primary-soft ring-1 ring-primary"
                    : "hover:bg-muted/30"
                }`}
              >
                {a[0].toUpperCase() + a.slice(1)}
              </button>
            ))}
          </div>
        </Section>

        <div className="grid grid-cols-2 gap-4">
          <Section label="Boot scope">
            <div className="flex gap-1.5 flex-wrap">
              {(["active", "passive", "recovery", "common"] as const).map(
                (s) => (
                  <button
                    key={s}
                    type="button"
                    aria-pressed={bootState === s}
                    onClick={() => setBootState(s)}
                    className={`px-2.5 py-1 text-xs rounded-md border ${
                      bootState === s
                        ? "bg-primary text-primary-foreground border-primary"
                        : "hover:bg-muted/30"
                    }`}
                  >
                    {s[0].toUpperCase() + s.slice(1)}
                  </button>
                ),
              )}
            </div>
            {bootState === "active" && (
              <p
                role="alert"
                className="text-xs mt-2 px-2.5 py-1.5 rounded-md border border-warning/40 bg-warning/10 text-warning-foreground"
              >
                This extension is only enabled when the node is booted in the
                active partition. If the node is rolled back to passive, it
                won&apos;t be loaded.
              </p>
            )}
          </Section>

          <Section label="When to apply">
            <label className="flex items-start gap-2 text-sm">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={now}
                onChange={(e) => setNow(e.target.checked)}
              />
              <span>
                <span className="font-medium">Activate immediately</span>
                <span className="block text-xs text-muted-foreground">
                  Otherwise applies on next reboot.
                </span>
              </span>
            </label>
          </Section>
        </div>

        <details className="mt-2">
          <summary className="text-xs text-muted-foreground cursor-pointer">
            Show payload
          </summary>
          <pre className="text-[11px] mt-1.5 p-2 rounded-md bg-muted/40 whitespace-pre-wrap">
            {JSON.stringify({ command: "extension", args }, null, 2)}
          </pre>
        </details>

        {action === "install" && sourcePath === null && (
          <p role="alert" className="text-sm text-danger-foreground mt-2">
            This extension has no download token, so nodes could not fetch it.
            Rebuild it to mint one.
          </p>
        )}

        {err && (
          <p role="alert" className="text-sm text-danger-foreground mt-2">
            {err}
          </p>
        )}

        <div className="flex justify-end gap-2 mt-4">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={!canSend} onClick={handleSend}>
            {submitLabel}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function Section({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="mb-3">
      <div className="text-xs text-muted-foreground mb-1.5">{label}</div>
      {children}
    </div>
  );
}

function TargetTab({
  active,
  label,
  onClick,
}: {
  active: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={`px-2.5 py-1 text-xs rounded-md transition-colors ${
        active
          ? "bg-primary text-primary-foreground"
          : "border hover:bg-muted/30"
      }`}
    >
      {label}
    </button>
  );
}
