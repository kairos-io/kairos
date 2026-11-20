import { useState } from "react";
import { setGroup } from "@/api/nodes";
import type { Group } from "@/api/groups";
import { toast } from "@/hooks/useToast";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";

interface MoveToGroupDialogProps {
  open: boolean;
  onOpenChange(open: boolean): void;
  nodeIds: string[];
  groups: Group[];
  onDone(): void;
}

const NO_GROUP = "";

function plural(n: number) {
  return `${n} node${n !== 1 ? "s" : ""}`;
}

// MoveToGroupDialog assigns every given node to one group, one request per
// node, because the API has no bulk group endpoint.
export function MoveToGroupDialog({ open, onOpenChange, nodeIds, groups, onDone }: MoveToGroupDialogProps) {
  const [choice, setChoice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function change(next: boolean) {
    if (!next) setChoice(null);
    onOpenChange(next);
  }

  async function submit() {
    if (choice === null) return;
    setBusy(true);
    const results = await Promise.allSettled(nodeIds.map((id) => setGroup(id, choice === NO_GROUP ? null : choice)));
    setBusy(false);
    const failed = results.filter((r) => r.status === "rejected").length;
    if (failed > 0) toast(`Could not move ${plural(failed)}`, "error");
    else toast(`Moved ${plural(nodeIds.length)}`, "success");
    setChoice(null);
    onDone();
    onOpenChange(false);
  }

  const options = [...groups.map((g) => ({ id: g.id, name: g.name })), { id: NO_GROUP, name: "No group" }];

  return (
    <Dialog open={open} onOpenChange={change}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Move to group</DialogTitle>
          <DialogDescription>Choose the group for {plural(nodeIds.length)}.</DialogDescription>
        </DialogHeader>
        <div role="radiogroup" aria-label="Group" className="flex max-h-72 flex-col gap-1 overflow-y-auto">
          {options.map((o) => (
            <label
              key={o.id || "none"}
              className={cn(
                "flex cursor-pointer items-center gap-2 rounded-md border border-border px-3 py-2 text-sm hover:bg-muted",
                choice === o.id && "border-primary bg-primary-soft",
              )}
            >
              <input
                type="radio"
                name="move-to-group"
                className="accent-primary"
                checked={choice === o.id}
                onChange={() => setChoice(o.id)}
              />
              {o.name}
            </label>
          ))}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => change(false)}>
            Cancel
          </Button>
          <Button disabled={choice === null || nodeIds.length === 0} loading={busy} onClick={submit}>
            Move {plural(nodeIds.length)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
