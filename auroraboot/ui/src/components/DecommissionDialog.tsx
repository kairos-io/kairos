import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { StatusBadge } from "@/components/StatusBadge";
import { decommissionNode, deleteNode, type Node } from "@/api/nodes";
import { useUIWebSocket } from "@/hooks/useUIWebSocket";
import { ansiToHtml } from "@/lib/ansi";
import { Copy, Check } from "lucide-react";
import { PRODUCT_NAME } from "@/lib/branding";

interface DecommissionDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  node: Node | null;
  onDeleted: () => void;
}

// Visible states the dialog progresses through. The backend's
// nodeOnline=false branch skips straight to `offline` after the confirm
// click; the online branch goes idle -> running -> (completed | failed),
// with `timedOut` reached from `running` after 30 s.
type Phase = "idle" | "running" | "completed" | "failed" | "offline" | "timedOut";

// DECOMMISSION_TIMEOUT_MS matches the server-side ExpiresAt (30 s). If no
// terminal status arrives before this elapses, we assume the agent is
// stuck or already gone and let the operator force-delete.
const DECOMMISSION_TIMEOUT_MS = 30_000;

const UNINSTALL_CMD = "kairos-agent phone-home uninstall";

// DecommissionDialog replaces the plain "Are you sure?" delete flow on
// NodeDetail with a two-stage teardown: dispatch the remote `unregister`
// command, stream the agent's output live, then remove the DB record
// once the command reaches a terminal phase. Offline nodes skip straight
// to the CLI-fallback warning so the operator knows what to run manually.
export function DecommissionDialog({ open, onOpenChange, node, onDeleted }: DecommissionDialogProps) {
  const [phase, setPhase] = useState<Phase>("idle");
  const [commandID, setCommandID] = useState<string>("");
  const [output, setOutput] = useState<string>("");
  const [error, setError] = useState<string>("");
  const [elapsedS, setElapsedS] = useState<number>(0);
  const [copied, setCopied] = useState(false);
  const startedAt = useRef<number | null>(null);

  // Reset state on every open transition so stale output from a previous
  // decommission doesn't flash on re-open for another node. Uses React's
  // "adjust state during rendering" pattern — the conditional setState
  // converges in one extra render and avoids setState-in-effect.
  const [prevOpen, setPrevOpen] = useState(open);
  if (open !== prevOpen) {
    setPrevOpen(open);
    if (open) {
      setPhase("idle");
      setCommandID("");
      setOutput("");
      setError("");
      setElapsedS(0);
      setCopied(false);
      // startedAt.current is overwritten on the next phase="running" transition,
      // so no need to reset it here (updating a ref during render is disallowed).
    }
  }

  // Elapsed-seconds ticker while running; used to reveal "Force delete
  // anyway" after the 30 s timeout.
  useEffect(() => {
    if (phase !== "running") return;
    startedAt.current = Date.now();
    const id = setInterval(() => {
      if (startedAt.current !== null) {
        const e = Math.floor((Date.now() - startedAt.current) / 1000);
        setElapsedS(e);
        if (e * 1000 >= DECOMMISSION_TIMEOUT_MS) {
          setPhase("timedOut");
        }
      }
    }, 250);
    return () => clearInterval(id);
  }, [phase]);

  // Subscribe to the UI WebSocket command_update events for our command.
  // The hook is unconditionally mounted (it auto-reconnects internally) so
  // it stays subscribed across state transitions while the dialog is open.
  useUIWebSocket((msg) => {
    if (msg.type !== "command_update" || !commandID) return;
    const d = msg.data as { id?: string; phase?: string; result?: string } | null | undefined;
    if (!d || d.id !== commandID) return;

    if (typeof d.result === "string" && d.result) {
      setOutput(d.result);
    }

    if (d.phase === "Completed") {
      setPhase("completed");
    } else if (d.phase === "Failed") {
      setPhase("failed");
      setError(typeof d.result === "string" ? d.result : "teardown failed on the node");
    }
  });

  // Once the agent reports Completed, drop the DB record and close.
  useEffect(() => {
    if (phase !== "completed" || !node) return;
    (async () => {
      try {
        await deleteNode(node.id);
        onOpenChange(false);
        onDeleted();
      } catch (e) {
        setError(`teardown succeeded but delete failed: ${(e as Error).message}`);
        setPhase("failed");
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase]);

  async function handleDecommission() {
    if (!node) return;
    try {
      const res = await decommissionNode(node.id);
      if (!res.nodeOnline) {
        setPhase("offline");
        return;
      }
      setCommandID(res.commandID);
      setPhase("running");
    } catch (e) {
      setError((e as Error).message);
      setPhase("failed");
    }
  }

  async function handleForceDelete() {
    if (!node) return;
    try {
      await deleteNode(node.id);
      onOpenChange(false);
      onDeleted();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  function copyCmd() {
    navigator.clipboard.writeText(UNINSTALL_CMD).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }

  if (!node) return null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Decommission {node.hostname || node.id.slice(0, 8)}</DialogTitle>
          <DialogDescription>
            Runs the phone-home teardown on the node, then removes its record
            from {PRODUCT_NAME}.
          </DialogDescription>
        </DialogHeader>

        {phase === "idle" && (
          <div className="space-y-3 text-sm">
            <p>
              The node will receive an <code className="font-mono">unregister</code>{" "}
              command: its phone-home service will stop and be removed, along
              with the saved credentials and cloud-config files.
              The {PRODUCT_NAME} record is deleted as soon as the teardown completes.
            </p>
            <p className="text-xs text-muted-foreground">
              If the node is offline you'll see instructions to finish the
              teardown manually.
            </p>
          </div>
        )}

        {phase === "running" && (
          <div className="space-y-3">
            <div className="flex items-center gap-2 text-sm">
              <StatusBadge status="Running" />
              <span className="text-muted-foreground">Teardown in progress · {elapsedS}s</span>
            </div>
            <div className="terminal-output font-mono text-xs p-3 rounded max-h-48 overflow-y-auto whitespace-pre-wrap">
              {output ? (
                <span dangerouslySetInnerHTML={{ __html: ansiToHtml(output) }} />
              ) : (
                "Waiting for the agent to start the teardown..."
              )}
            </div>
          </div>
        )}

        {phase === "timedOut" && (
          <div className="space-y-3 text-sm">
            <div className="flex items-center gap-2">
              <StatusBadge status="Pending" />
              <span className="text-muted-foreground">No response after {Math.floor(DECOMMISSION_TIMEOUT_MS / 1000)}s</span>
            </div>
            <div className="rounded-md border border-amber-400/60 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-400/40 dark:bg-amber-950/60 dark:text-amber-100">
              The agent hasn't reported back. It may be offline or stuck. You
              can force-delete the record now; to finish cleanup on the node
              itself, SSH in and run:
              <div className="mt-2 flex items-center gap-2">
                <code className="flex-1 font-mono text-xs bg-background/60 px-2 py-1 rounded">{UNINSTALL_CMD}</code>
                <Button variant="outline" size="sm" onClick={copyCmd} aria-label="Copy command">
                  {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                </Button>
              </div>
            </div>
            {output && (
              <div className="terminal-output font-mono text-xs p-3 rounded max-h-32 overflow-y-auto whitespace-pre-wrap">
                <span dangerouslySetInnerHTML={{ __html: ansiToHtml(output) }} />
              </div>
            )}
          </div>
        )}

        {phase === "offline" && (
          <div className="space-y-3 text-sm">
            <div className="flex items-center gap-2">
              <StatusBadge status="Offline" />
            </div>
            <div className="rounded-md border border-amber-400/60 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-400/40 dark:bg-amber-950/60 dark:text-amber-100">
              <p>
                The node is offline, so we can't run the teardown remotely.
                The {PRODUCT_NAME} record will be removed, but the node itself
                still has the phone-home service running. SSH into the node
                and run:
              </p>
              <div className="mt-2 flex items-center gap-2">
                <code className="flex-1 font-mono text-xs bg-background/60 px-2 py-1 rounded">{UNINSTALL_CMD}</code>
                <Button variant="outline" size="sm" onClick={copyCmd} aria-label="Copy command">
                  {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                </Button>
              </div>
              <p className="mt-2">…before reimaging or retiring it.</p>
            </div>
          </div>
        )}

        {phase === "failed" && (
          <div className="space-y-3 text-sm">
            <div className="flex items-center gap-2">
              <StatusBadge status="Failed" />
            </div>
            <div className="rounded-md border border-red-400/60 bg-red-50 px-3 py-2 text-xs text-red-900 dark:border-red-400/40 dark:bg-red-950/60 dark:text-red-100 whitespace-pre-wrap">
              {error || "Teardown failed on the node."}
            </div>
            <div className="rounded-md border border-amber-400/60 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-400/40 dark:bg-amber-950/60 dark:text-amber-100">
              Force-deleting now removes the record from {PRODUCT_NAME}. You
              should also SSH into the node and run{" "}
              <code className="font-mono">{UNINSTALL_CMD}</code> to finish
              cleanup.
            </div>
          </div>
        )}

        {phase === "completed" && (
          <div className="text-sm flex items-center gap-2">
            <StatusBadge status="Completed" />
            <span>Teardown done. Removing record…</span>
          </div>
        )}

        <DialogFooter>
          {phase === "idle" && (
            <>
              <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
              <Button variant="destructive" onClick={handleDecommission}>Decommission</Button>
            </>
          )}
          {phase === "running" && (
            <Button variant="outline" onClick={() => onOpenChange(false)}>Close</Button>
          )}
          {(phase === "offline" || phase === "timedOut" || phase === "failed") && (
            <>
              <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
              <Button variant="destructive" onClick={handleForceDelete}>
                {phase === "offline" ? "Remove record" : "Force delete anyway"}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
