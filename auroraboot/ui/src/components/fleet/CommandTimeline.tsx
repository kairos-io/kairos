import { useState } from "react";
import type { LucideIcon } from "lucide-react";
import { CheckCircle2, Circle, Clock, Loader2, Terminal, Trash2, XCircle } from "lucide-react";
import type { Command } from "@/api/commands";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/StatusBadge";
import { EmptyState } from "@/components/fleet/EmptyState";
import { ansiToHtml } from "@/lib/ansi";
import { isFailed, isRunning, phaseTone, type Tone } from "@/lib/phase";
import { timeAgo } from "@/lib/time";
import { cn } from "@/lib/utils";
import { toneText } from "./tones";

type Filter = "all" | "running" | "failed";

interface CommandTimelineProps {
  commands: Command[];
  onDelete(id: string): void;
}

const toneIcon: Record<Tone, LucideIcon> = {
  success: CheckCircle2,
  danger: XCircle,
  info: Loader2,
  warning: Clock,
  neutral: Circle,
};

// A command is in progress from the moment it is queued until the agent
// reports a result. Only those can not be deleted.
function inProgress(cmd: Command): boolean {
  return isRunning(cmd.phase) || phaseTone(cmd.phase) === "warning";
}

// The agent streams raw terminal output. The error box shows plain text, so
// the escape sequences go.
function lastLine(text: string): string {
  // eslint-disable-next-line no-control-regex
  const plain = text.replace(/\x1b\[[0-9;]*[A-Za-z]/g, "");
  const lines = plain.split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
  return lines[lines.length - 1] ?? "";
}

function argsLine(args: Record<string, string> | null | undefined): string {
  return Object.entries(args ?? {})
    .map(([k, v]) => `${k}=${v}`)
    .join(" ");
}

function Output({ text }: { text: string }) {
  return (
    <div className="terminal-output mt-2 max-h-48 overflow-y-auto whitespace-pre-wrap rounded p-3 font-mono text-xs">
      {text ? <span dangerouslySetInnerHTML={{ __html: ansiToHtml(text) }} /> : "Waiting for output..."}
    </div>
  );
}

// CommandTimeline lists a node's commands newest first. The newest running
// command shows its output live; a failed one shows the last line of its
// result; others can reveal their output on demand.
export function CommandTimeline({ commands, onDelete }: CommandTimelineProps) {
  const [filter, setFilter] = useState<Filter>("all");
  const [opened, setOpened] = useState<Set<string>>(new Set());

  const sorted = [...commands].sort(
    (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime(),
  );
  const liveID = sorted.find((c) => isRunning(c.phase))?.id;
  const counts: Record<Filter, number> = {
    all: sorted.length,
    running: sorted.filter(inProgress).length,
    failed: sorted.filter((c) => isFailed(c.phase)).length,
  };
  const shown = sorted.filter((c) =>
    filter === "running" ? inProgress(c) : filter === "failed" ? isFailed(c.phase) : true,
  );

  function toggle(id: string) {
    setOpened((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  if (sorted.length === 0) {
    return (
      <EmptyState
        icon={Terminal}
        title="No commands yet"
        text="Send a command to interact with this node."
      />
    );
  }

  const filters: { key: Filter; label: string }[] = [
    { key: "all", label: "All" },
    { key: "running", label: "Running" },
    { key: "failed", label: "Failed" },
  ];

  return (
    <div className="grid gap-3">
      <div role="group" aria-label="Filter commands" className="inline-flex w-fit rounded-md border bg-muted p-0.5">
        {filters.map((f) => (
          <button
            key={f.key}
            type="button"
            aria-pressed={filter === f.key}
            onClick={() => setFilter(f.key)}
            className={cn(
              "rounded px-2.5 py-1 text-xs font-medium text-muted-foreground",
              filter === f.key && "bg-background text-foreground shadow-sm",
            )}
          >
            {f.label} <span className="tabular-nums">{counts[f.key]}</span>
          </button>
        ))}
      </div>

      {shown.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">
          {filter === "running" ? "No command is running." : "No command failed."}
        </p>
      ) : (
        <ol aria-label="Commands" className="grid gap-0">
          {shown.map((cmd, i) => {
            const tone = phaseTone(cmd.phase);
            const Icon = toneIcon[tone];
            const args = argsLine(cmd.args);
            const failed = isFailed(cmd.phase);
            const pending = tone === "warning";
            const live = cmd.id === liveID;
            const canOpen = !live && !pending && !!cmd.result;
            const errLine = failed ? lastLine(cmd.result || "") : "";
            return (
              <li key={cmd.id} className="relative flex gap-3 pb-4 last:pb-0">
                {i < shown.length - 1 && (
                  <span aria-hidden="true" className="absolute left-3 top-7 bottom-0 w-px bg-border" />
                )}
                <span
                  className={cn(
                    "relative flex h-6 w-6 shrink-0 items-center justify-center rounded-full border bg-card",
                    toneText[tone],
                  )}
                >
                  <Icon className={cn("h-3.5 w-3.5", tone === "info" && "animate-spin")} aria-hidden="true" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <span className="font-mono text-sm font-medium">{cmd.command}</span>
                    <StatusBadge status={cmd.phase} />
                    {args && (
                      <span className="min-w-0 max-w-full truncate font-mono text-xs text-muted-foreground" title={args}>
                        {args}
                      </span>
                    )}
                    <span className="ml-auto flex items-center gap-1 text-xs text-muted-foreground">
                      {timeAgo(cmd.createdAt)}
                      {canOpen && (
                        <Button variant="link" size="sm" className="h-auto px-1 text-xs" onClick={() => toggle(cmd.id)}>
                          {opened.has(cmd.id) ? "Hide output" : "Show output"}
                        </Button>
                      )}
                      {!inProgress(cmd) && (
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Delete ${cmd.command} command`}
                          onClick={() => onDelete(cmd.id)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </span>
                  </div>
                  {live && <Output text={cmd.result} />}
                  {failed && errLine && !opened.has(cmd.id) && (
                    <div className="mt-2 rounded-md border border-danger/25 bg-danger/10 px-3 py-2 font-mono text-xs text-danger-foreground">
                      {errLine}
                    </div>
                  )}
                  {canOpen && opened.has(cmd.id) && <Output text={cmd.result} />}
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}
