import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

interface RepeatBatchDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // The label of the batch's command, as the batch pages show it.
  commandLabel: string;
  // The batch's rollout settings, as text.
  settings: string;
  nodeChoices: { id: string; label: string }[];
  // Receives the checked node IDs. The caller sends the batch's command,
  // arguments and rollout settings unchanged.
  onSubmit: (nodeIDs: string[]) => void;
}

// RepeatBatchDialog starts a new batch for a command that CommandDialog has no
// form for, such as an extension install. The operator only picks the nodes,
// so the new batch cannot lose arguments a form would have to rebuild.
export function RepeatBatchDialog({
  open,
  onOpenChange,
  commandLabel,
  settings,
  nodeChoices,
  onSubmit,
}: RepeatBatchDialogProps) {
  const [unchecked, setUnchecked] = useState<Set<string>>(new Set());
  const checkedIDs = nodeChoices.filter((n) => !unchecked.has(n.id)).map((n) => n.id);

  function handleOpenChange(next: boolean) {
    if (!next) setUnchecked(new Set());
    onOpenChange(next);
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{`Repeat ${commandLabel}`}</DialogTitle>
          <DialogDescription>
            The new batch sends the same command with the same arguments to the nodes
            checked below.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-2">
          <div className="grid gap-2">
            <h3 className="text-sm font-semibold">Nodes</h3>
            <div className="grid max-h-48 gap-1 overflow-y-auto">
              {nodeChoices.map((n) => (
                <label key={n.id} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={!unchecked.has(n.id)}
                    onChange={(e) =>
                      setUnchecked((prev) => {
                        const next = new Set(prev);
                        if (e.target.checked) next.delete(n.id);
                        else next.add(n.id);
                        return next;
                      })
                    }
                  />
                  {n.label}
                </label>
              ))}
            </div>
          </div>
          <div className="grid gap-1 border-t pt-4">
            <h3 className="text-sm font-semibold">Rollout</h3>
            <p className="text-sm text-muted-foreground">{settings}</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => handleOpenChange(false)}>
            Cancel
          </Button>
          <Button
            disabled={checkedIDs.length === 0}
            onClick={() => {
              onSubmit(checkedIDs);
              handleOpenChange(false);
            }}
          >
            Start batch
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
