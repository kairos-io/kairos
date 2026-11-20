import { useState } from "react";
import { setLabels, type Node } from "@/api/nodes";
import { toast } from "@/hooks/useToast";
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

interface AddLabelDialogProps {
  open: boolean;
  onOpenChange(open: boolean): void;
  nodes: Node[];
  onDone(): void;
}

function plural(n: number) {
  return `${n} node${n !== 1 ? "s" : ""}`;
}

// parseLabel reads "key=value". The key is required; the value may be empty.
function parseLabel(text: string): { key: string; value: string } | null {
  const i = text.indexOf("=");
  const key = (i < 0 ? text : text.slice(0, i)).trim();
  if (!key) return null;
  return { key, value: i < 0 ? "" : text.slice(i + 1).trim() };
}

// AddLabelDialog merges one key=value label into each node's labels. The API
// replaces the whole label map, so each node's existing labels are sent too.
export function AddLabelDialog({ open, onOpenChange, nodes, onDone }: AddLabelDialogProps) {
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const label = parseLabel(text);

  function change(next: boolean) {
    if (!next) setText("");
    onOpenChange(next);
  }

  async function submit() {
    if (!label) return;
    setBusy(true);
    const results = await Promise.allSettled(
      nodes.map((n) => setLabels(n.id, { ...(n.labels ?? {}), [label.key]: label.value })),
    );
    setBusy(false);
    const failed = results.filter((r) => r.status === "rejected").length;
    if (failed > 0) toast(`Could not label ${plural(failed)}`, "error");
    else toast(`Added ${label.key}=${label.value} to ${plural(nodes.length)}`, "success");
    setText("");
    onDone();
    onOpenChange(false);
  }

  return (
    <Dialog open={open} onOpenChange={change}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add label</DialogTitle>
          <DialogDescription>
            Add a label to {plural(nodes.length)}. A node that already has the key gets the new value.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Label htmlFor="add-label-input">Label</Label>
          <Input
            id="add-label-input"
            placeholder="key=value"
            className="font-mono"
            value={text}
            onChange={(e) => setText(e.target.value)}
            autoFocus
          />
          <DialogFooter className="mt-2">
            <Button type="button" variant="outline" onClick={() => change(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!label || nodes.length === 0} loading={busy}>
              Add to {plural(nodes.length)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
