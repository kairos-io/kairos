import { useState, useEffect, useId } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import type { Rollout } from "@/api/batches";
import type { CommandKey } from "@/lib/commandKeys";
import { listArtifacts, resolveBundle, Artifact, type ResolvedBundleEntry } from "@/api/artifacts";
import {
  ArrowUpCircle,
  RotateCcw,
  RefreshCcw,
  FileText,
  Power,
  Terminal,
  ChevronLeft,
  type LucideIcon,
} from "lucide-react";

// CommandDialog runs a two-step wizard: first the user picks an action
// from a grid of cards (no dropdown), then they configure the arguments
// for that specific action. When opened from a quick-action menu the
// parent passes defaultCommand and we skip straight to step 2.

export type { CommandKey };

export interface CommandDef {
  key: CommandKey;
  label: string;
  // What the user will see on the primary button ("Send upgrade", etc).
  verb: string;
  description: string;
  icon: LucideIcon;
  // Visual accent for the icon chip on the action card.
  tone: "orange" | "blue" | "amber" | "red";
}

export const COMMANDS: CommandDef[] = [
  {
    key: "upgrade",
    label: "Upgrade",
    verb: "Send upgrade",
    description:
      "Upgrade the node's active partition to a new image or artifact.",
    icon: ArrowUpCircle,
    tone: "orange",
  },
  {
    key: "upgrade-recovery",
    label: "Upgrade recovery",
    verb: "Send recovery upgrade",
    description:
      "Replace the recovery partition — active partition stays untouched.",
    icon: RotateCcw,
    tone: "blue",
  },
  {
    key: "apply-cloud-config",
    label: "Apply config",
    verb: "Send config",
    description:
      "Push a cloud-config YAML to /oem. Takes effect on next boot.",
    icon: FileText,
    tone: "blue",
  },
  {
    key: "exec",
    label: "Run command",
    verb: "Run command",
    description: "Execute a shell command on the node and capture its output.",
    icon: Terminal,
    tone: "blue",
  },
  {
    key: "reboot",
    label: "Reboot",
    verb: "Send reboot",
    description: "Restart the node after a short delay.",
    icon: Power,
    tone: "amber",
  },
  {
    key: "reset",
    label: "Reset",
    verb: "Send reset",
    description:
      "Factory reset — wipe state partitions and re-run first-boot provisioning.",
    icon: RefreshCcw,
    tone: "red",
  },
];

// Mapped once so the render loop stays tidy. Tones map to the icon chip
// background and the action-card hover affordance.
const TONE_CLASSES: Record<CommandDef["tone"], { chip: string; hover: string }> = {
  orange: {
    chip: "bg-primary/10 text-primary",
    hover: "hover:border-primary/60 hover:bg-primary-soft",
  },
  blue: {
    chip: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
    hover: "hover:border-sky-500/60 hover:bg-sky-500/5",
  },
  amber: {
    chip: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
    hover: "hover:border-amber-500/60 hover:bg-amber-500/5",
  },
  red: {
    chip: "bg-red-500/10 text-red-600 dark:text-red-400",
    hover: "hover:border-red-500/60 hover:bg-red-500/5",
  },
};

// originalExtensions returns the type/name keys of the extensions a repeated
// batch sent with the given artifact, or null when the batch used another
// source. An upgrade with every bundled extension unticked sends no
// extensions argument, so a missing argument is an empty selection.
function originalExtensions(
  extensions: string | undefined,
  source: string | undefined,
  artifactId: string,
): Set<string> | null {
  if (source !== "artifact:" + artifactId) return null;
  if (!extensions) return new Set();
  try {
    const rows = JSON.parse(extensions) as { type: string; name: string }[];
    return new Set(rows.map((r) => `${r.type}/${r.name}`));
  } catch {
    return null;
  }
}

type UpgradeSourceMode = "image" | "artifact";

interface CommandDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (
    command: string,
    args: Record<string, unknown>,
    rollout?: Rollout,
    nodeIDs?: string[],
  ) => void;
  // How many nodes the command reaches. Setting it, or nodeChoices, means the
  // command starts a batch, so onSubmit always receives a rollout. The Rollout
  // section shows every setting for more than one node and only the reboot
  // timeout, for commands that reboot, for one node.
  targetCount?: number;
  title?: string;
  // If set, opens the dialog straight on the configure step with this
  // command preselected. Used by GroupDetail/NodeDetail quick-action
  // menus so the user doesn't re-pick something they just clicked.
  defaultCommand?: CommandKey | null;
  // Prefill for the rollout settings and the command's arguments, used to
  // repeat a batch.
  defaultRollout?: Rollout;
  defaultArgs?: Record<string, string>;
  // When set, the dialog lists these nodes with a checkbox each (all checked)
  // and passes the checked ids as the fourth argument of onSubmit. The target
  // count is then the number checked.
  nodeChoices?: { id: string; label: string }[];
}

export function CommandDialog({
  open,
  onOpenChange,
  onSubmit,
  title = "Send command",
  defaultCommand,
  targetCount: targetCountProp,
  defaultRollout,
  defaultArgs,
  nodeChoices,
}: CommandDialogProps) {
  const rolloutIds = useId();
  const [concurrencyArg, setConcurrencyArg] = useState("");
  // A number input reports text it cannot read, such as "two", as an empty
  // value, which would otherwise mean all nodes at once.
  const [concurrencyUnreadable, setConcurrencyUnreadable] = useState(false);
  const [stopOnFailure, setStopOnFailure] = useState(true);
  const [timeoutMinutes, setTimeoutMinutes] = useState("10");
  const [command, setCommand] = useState<CommandKey | "">("");
  const [imageArg, setImageArg] = useState("");
  const [configArg, setConfigArg] = useState("");
  const [shellCmd, setShellCmd] = useState("");
  const [upgradeSourceMode, setUpgradeSourceMode] =
    useState<UpgradeSourceMode>("image");
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [selectedArtifactId, setSelectedArtifactId] = useState("");
  // The resolved bundle is stored together with the artifact it belongs to, so
  // switching artifacts never shows the previous artifact's extensions while the
  // new resolve is in flight -- and so the effect below never has to clear state
  // synchronously (react-hooks/set-state-in-effect).
  const [bundleState, setBundleState] = useState<{
    artifactId: string;
    rows: ResolvedBundleEntry[];
  }>({ artifactId: "", rows: [] });
  const [bundlePicks, setBundlePicks] = useState<Record<string, boolean>>({});
  // True once the user ticks or unticks a bundled extension, which makes the
  // dialog's picks authoritative over defaultArgs.extensions.
  const [picksTouched, setPicksTouched] = useState(false);

  const [unchecked, setUnchecked] = useState<Set<string>>(new Set());
  const checkedIDs = nodeChoices
    ?.filter((n) => !unchecked.has(n.id))
    .map((n) => n.id);
  const targetCount = checkedIDs ? checkedIDs.length : targetCountProp;
  const startsBatch = targetCountProp !== undefined || nodeChoices !== undefined;
  const isRollout = startsBatch && (targetCount ?? 0) > 1;
  const reboots =
    command === "upgrade" || command === "reset" || command === "reboot";
  const isUpgrade = command === "upgrade" || command === "upgrade-recovery";
  const activeCommand = COMMANDS.find((c) => c.key === command);
  const showRollout = isRollout || (startsBatch && reboots);
  // The server reads a missing concurrency as all nodes at once, so a value
  // that is not a whole number must block the submit instead of being dropped.
  const concurrencyValid =
    !isRollout || (!concurrencyUnreadable && /^\d*$/.test(concurrencyArg.trim()));
  // The resolve effect reads the defaults through these strings, so a parent
  // that passes a new defaultArgs object with the same content (a polled
  // batch) does not resolve again and reset the operator's picks.
  const defaultExtensions = defaultArgs?.extensions;
  const defaultSource = defaultArgs?.source;

  function seedFromDefaults() {
    if (defaultRollout) {
      setConcurrencyArg(
        defaultRollout.concurrency ? String(defaultRollout.concurrency) : "",
      );
      setConcurrencyUnreadable(false);
      setStopOnFailure(defaultRollout.stopOnFailure);
      setTimeoutMinutes(String(defaultRollout.rebootTimeoutSeconds / 60));
    }
    const source = defaultArgs?.source ?? "";
    if (source.startsWith("artifact:")) {
      setUpgradeSourceMode("artifact");
      setSelectedArtifactId(source.slice("artifact:".length));
    } else if (source.startsWith("oci:")) {
      setUpgradeSourceMode("image");
      setImageArg(source.slice("oci:".length));
    }
    if (defaultArgs?.config) setConfigArg(defaultArgs.config);
    if (defaultArgs?.command) setShellCmd(defaultArgs.command);
  }

  // Prefill or reset state on open/defaultCommand change. On open transition
  // we seed `command` with `defaultCommand`; on close transition we wipe
  // every field so the next open starts clean. Uses React's "adjust state
  // during rendering" pattern — the conditional setState converges in one
  // extra render and avoids setState-in-effect.
  const [prevOpen, setPrevOpen] = useState(open);
  const [prevDefaultCommand, setPrevDefaultCommand] = useState(defaultCommand);
  if (open !== prevOpen) {
    setPrevOpen(open);
    setPrevDefaultCommand(defaultCommand);
    if (open) {
      setCommand(defaultCommand ?? "");
      seedFromDefaults();
    } else {
      setUnchecked(new Set());
      setPicksTouched(false);
      setCommand("");
      setImageArg("");
      setConfigArg("");
      setShellCmd("");
      setUpgradeSourceMode("image");
      setSelectedArtifactId("");
      setConcurrencyArg("");
      setConcurrencyUnreadable(false);
      setStopOnFailure(true);
      setTimeoutMinutes("10");
    }
  } else if (open && defaultCommand !== prevDefaultCommand) {
    setPrevDefaultCommand(defaultCommand);
    setCommand(defaultCommand ?? "");
  }

  useEffect(() => {
    if (open && isUpgrade && upgradeSourceMode === "artifact") {
      listArtifacts()
        .then((all) =>
          setArtifacts(
            all.filter((a) => a.phase === "Ready" && !!a.containerImage),
          ),
        )
        .catch(() => setArtifacts([]));
    }
  }, [open, isUpgrade, upgradeSourceMode]);

  // Resolve the artifact's bundled extensions whenever the operator picks one.
  // Every entry is pre-selected; the operator can untick to drop individual
  // bundled extensions from this upgrade.
  useEffect(() => {
    if (!isUpgrade || upgradeSourceMode !== "artifact" || !selectedArtifactId) {
      return;
    }
    let cancelled = false;
    resolveBundle(selectedArtifactId)
      .then((rows) => {
        if (cancelled) return;
        setBundleState({ artifactId: selectedArtifactId, rows });
        // When repeating a batch from the same artifact, only the extensions
        // the original sent start ticked.
        const original = originalExtensions(defaultExtensions, defaultSource, selectedArtifactId);
        setBundlePicks(
          Object.fromEntries(
            rows.map((r) => {
              const k = `${r.type}/${r.name}`;
              return [k, original ? original.has(k) : true];
            }),
          ),
        );
      })
      .catch(() => {
        if (cancelled) return;
        setBundleState({ artifactId: selectedArtifactId, rows: [] });
        setBundlePicks({});
      });
    return () => {
      cancelled = true;
    };
  }, [isUpgrade, upgradeSourceMode, selectedArtifactId, defaultExtensions, defaultSource]);

  const bundle =
    isUpgrade &&
    upgradeSourceMode === "artifact" &&
    bundleState.artifactId === selectedArtifactId
      ? bundleState.rows
      : [];

  function handleSubmit() {
    if (!command || !concurrencyValid) return;
    const args: Record<string, unknown> = {};

    if (isUpgrade) {
      if (upgradeSourceMode === "artifact") {
        args.source = "artifact:" + selectedArtifactId;
        // Attach bundled extensions for the agent. Omitted entirely when the
        // operator unticks every entry so back-compat behavior is preserved.
        const picked = bundle.filter(
          (e) => bundlePicks[`${e.type}/${e.name}`],
        );
        if (
          !picksTouched &&
          defaultArgs?.extensions &&
          defaultArgs.source === args.source
        ) {
          args.extensions = defaultArgs.extensions;
        } else if (picked.length > 0) {
          args.extensions = JSON.stringify(picked);
        }
      } else if (imageArg) {
        args.source = "oci:" + imageArg;
        // The form has no extension picker for an image, but the API takes
        // extensions with any source, so a repeated upgrade of the same image
        // keeps the ones it carried.
        if (defaultArgs?.extensions && defaultArgs.source === args.source) {
          args.extensions = defaultArgs.extensions;
        }
      }
      if (command === "upgrade-recovery") {
        args.recovery = "true";
      }
    }

    if (command === "apply-cloud-config") {
      if (configArg.trim()) args.config = configArg.trim();
    }

    if (shellCmd) args.command = shellCmd;

    if (startsBatch) {
      const concurrency = Number.parseInt(concurrencyArg, 10);
      const minutes = Number.parseFloat(timeoutMinutes);
      const rollout: Rollout = {
        stopOnFailure,
        rebootTimeoutSeconds:
          Number.isFinite(minutes) && minutes > 0 ? Math.round(minutes * 60) : 600,
      };
      if (isRollout && Number.isFinite(concurrency) && concurrency > 0) {
        rollout.concurrency = concurrency;
      }
      onSubmit(command, args, rollout, checkedIDs);
    } else {
      onSubmit(command, args, undefined, checkedIDs);
    }
    onOpenChange(false);
  }

  // Whether the Send button should be enabled given the current args.
  const canSubmit = (() => {
    if (!command) return false;
    if (checkedIDs && checkedIDs.length === 0) return false;
    if (!concurrencyValid) return false;
    if (command === "exec") return shellCmd.trim().length > 0;
    if (command === "upgrade" || command === "upgrade-recovery") {
      if (upgradeSourceMode === "image") return imageArg.trim().length > 0;
      return selectedArtifactId !== "";
    }
    return true;
  })();

  const onPickStep = !command;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <div className="flex items-center gap-2">
            {!onPickStep && (
              <button
                type="button"
                onClick={() => setCommand("")}
                className="inline-flex items-center text-xs text-muted-foreground hover:text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded"
                title="Pick a different action"
              >
                <ChevronLeft className="h-3.5 w-3.5 mr-0.5" />
                Back
              </button>
            )}
            <DialogTitle className="flex-1 truncate">
              {onPickStep
                ? title
                : `${activeCommand?.label ?? ""}`}
            </DialogTitle>
          </div>
          {onPickStep && (
            <DialogDescription>
              Pick an action. You'll configure its details on the next step.
            </DialogDescription>
          )}
        </DialogHeader>

        {/* STEP 1 — pick an action */}
        {onPickStep && (
          <div className="grid grid-cols-2 gap-3 py-2">
            {COMMANDS.map((c) => {
              const Icon = c.icon;
              const tone = TONE_CLASSES[c.tone];
              return (
                <button
                  key={c.key}
                  type="button"
                  onClick={() => setCommand(c.key)}
                  className={
                    "group flex flex-col items-start gap-2 rounded-lg border border-border p-3 text-left transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 " +
                    tone.hover
                  }
                >
                  <div className={`flex h-8 w-8 items-center justify-center rounded-md ${tone.chip}`}>
                    <Icon className="h-4 w-4" />
                  </div>
                  <div>
                    <div className="text-sm font-semibold leading-tight">{c.label}</div>
                    <p className="text-xs text-muted-foreground mt-1 leading-snug">
                      {c.description}
                    </p>
                  </div>
                </button>
              );
            })}
          </div>
        )}

        {/* STEP 2 — configure the picked action */}
        {!onPickStep && activeCommand && (
          <div className="grid gap-4 py-2">
            {/* Active action summary row */}
            <div className="flex items-start gap-3 rounded-lg border bg-muted/30 px-3 py-2.5">
              <div
                className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md ${TONE_CLASSES[activeCommand.tone].chip}`}
              >
                <activeCommand.icon className="h-4 w-4" />
              </div>
              <div className="flex-1 min-w-0">
                <div className="text-sm font-semibold">{activeCommand.label}</div>
                <p className="text-xs text-muted-foreground">
                  {activeCommand.description}
                </p>
              </div>
            </div>

            {isUpgrade && (
              <>
                <div className="grid gap-2">
                  <Label>Upgrade source</Label>
                  <Select
                    value={upgradeSourceMode}
                    onValueChange={(v) => setUpgradeSourceMode(v as UpgradeSourceMode)}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="image">From image reference</SelectItem>
                      <SelectItem value="artifact">From built artifact</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {upgradeSourceMode === "image" && (
                  <div className="grid gap-2">
                    <Label>Image</Label>
                    <Input
                      placeholder="e.g. quay.io/kairos/kairos:latest"
                      value={imageArg}
                      onChange={(e) => setImageArg(e.target.value)}
                    />
                  </div>
                )}

                {upgradeSourceMode === "artifact" && (
                  <div className="grid gap-2">
                    <Label>Artifact</Label>
                    {artifacts.length === 0 ? (
                      <p className="text-sm text-muted-foreground">
                        No ready artifacts with a container image available.
                      </p>
                    ) : (
                      <Select
                        value={selectedArtifactId}
                        onValueChange={setSelectedArtifactId}
                      >
                        <SelectTrigger className="max-w-full">
                          <SelectValue placeholder="Select artifact..." />
                        </SelectTrigger>
                        <SelectContent className="max-w-[min(28rem,90vw)]">
                          {artifacts.filter((a) => a.saved).length > 0 && (
                            <>
                              <div className="px-2 py-1.5 text-xs font-medium text-muted-foreground">
                                Saved
                              </div>
                              {artifacts
                                .filter((a) => a.saved)
                                .map((a) => (
                                  <SelectItem key={a.id} value={a.id}>
                                    <div className="flex flex-col gap-0.5 min-w-0">
                                      <span className="truncate font-medium">
                                        {a.name || a.id.slice(0, 8)}
                                      </span>
                                      {a.name && (
                                        <span className="text-xs text-muted-foreground truncate">
                                          {a.id.slice(0, 8)}
                                        </span>
                                      )}
                                    </div>
                                  </SelectItem>
                                ))}
                              <div className="-mx-1 my-1 h-px bg-muted" />
                            </>
                          )}
                          {artifacts
                            .filter((a) => !a.saved)
                            .map((a) => (
                              <SelectItem key={a.id} value={a.id}>
                                <div className="flex flex-col gap-0.5 min-w-0">
                                  <span className="truncate">
                                    {a.name || a.id.slice(0, 8)}
                                  </span>
                                  {a.name && (
                                    <span className="text-xs text-muted-foreground truncate">
                                      {a.id.slice(0, 8)}
                                    </span>
                                  )}
                                </div>
                              </SelectItem>
                            ))}
                        </SelectContent>
                      </Select>
                    )}

                    {bundle.length > 0 && (
                      <div className="mt-4">
                        <div className="text-xs text-muted-foreground mb-1.5">
                          Also push these extensions
                        </div>
                        <div className="grid gap-1">
                          {bundle.map((entry) => {
                            const k = `${entry.type}/${entry.name}`;
                            const checked = bundlePicks[k] ?? false;
                            return (
                              <label
                                key={k}
                                className="flex items-center gap-2 text-sm px-2 py-1.5 rounded-md border"
                              >
                                <input
                                  type="checkbox"
                                  aria-label={`include ${entry.name}`}
                                  checked={checked}
                                  onChange={(e) => {
                                    setPicksTouched(true);
                                    setBundlePicks((prev) => ({
                                      ...prev,
                                      [k]: e.target.checked,
                                    }));
                                  }}
                                />
                                <span className="font-medium">{entry.name}</span>
                                <span className="text-xs text-muted-foreground">
                                  {entry.type} · {entry.version}
                                </span>
                              </label>
                            );
                          })}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </>
            )}

            {command === "reset" && (
              <div className="grid gap-3">
                <Label>Reset options</Label>
                <p className="text-sm text-muted-foreground">
                  Automatic state reset takes no options. The node reboots into
                  the state-reset entry, resets itself there and comes back on
                  its active entry. The OEM partition is kept, so the
                  cloud-config already on the node is what provisions it again.
                </p>
                <p className="text-xs text-amber-700 dark:text-amber-300">
                  Reset will wipe all persistent data on the node. This action is irreversible.
                </p>
              </div>
            )}

            {command === "apply-cloud-config" && (
              <div className="grid gap-2">
                <Label>Cloud configuration (YAML)</Label>
                <Textarea
                  placeholder={"#cloud-config\nstages:\n  boot:\n    - name: example"}
                  value={configArg}
                  onChange={(e) => setConfigArg(e.target.value)}
                  rows={6}
                  className="font-mono text-sm"
                />
                <p className="text-xs text-muted-foreground">
                  Written to /oem/99_phonehome_remote.yaml. Reboot the node to apply.
                </p>
              </div>
            )}

            {command === "reboot" && (
              <div className="rounded-md border border-amber-400/50 bg-amber-50 px-3 py-2.5 text-sm text-amber-900 dark:border-amber-400/40 dark:bg-amber-950/60 dark:text-amber-100">
                The node will restart after a 10-second delay.
              </div>
            )}

            {command === "exec" && (
              <div className="grid gap-2">
                <Label>Shell command</Label>
                <Input
                  placeholder="e.g. uname -a"
                  value={shellCmd}
                  onChange={(e) => setShellCmd(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Output will be captured in the command history on the node page.
                </p>
              </div>
            )}

            {nodeChoices && (
              <div className="grid gap-2 border-t pt-4">
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
            )}

            {showRollout && (
              <div className="grid gap-3 border-t pt-4">
                <div className="flex items-baseline gap-2">
                  <h3 className="text-sm font-semibold">Rollout</h3>
                  <span className="text-xs text-muted-foreground">
                    {targetCount} {targetCount === 1 ? "node" : "nodes"}
                  </span>
                </div>
                {isRollout && (
                  <>
                    <div className="grid gap-2">
                      <Label htmlFor={`${rolloutIds}-concurrency`}>Nodes at a time</Label>
                      <Input
                        id={`${rolloutIds}-concurrency`}
                        type="number"
                        min={0}
                        placeholder="All at once"
                        value={concurrencyArg}
                        aria-invalid={!concurrencyValid}
                        aria-describedby={`${rolloutIds}-concurrency-help`}
                        onChange={(e) => setConcurrencyArg(e.target.value)}
                        // React drops the change event while the value stays
                        // empty, so the unreadable flag is read on every input.
                        onInput={(e) => setConcurrencyUnreadable(e.currentTarget.validity.badInput)}
                      />
                      {concurrencyValid ? (
                        <p id={`${rolloutIds}-concurrency-help`} className="text-xs text-muted-foreground">
                          The next node starts when one finishes. Leave empty to run on
                          all nodes at once.
                        </p>
                      ) : (
                        <p id={`${rolloutIds}-concurrency-help`} className="text-xs text-danger-foreground">
                          Enter a whole number, or leave it empty to run on all nodes at once.
                        </p>
                      )}
                    </div>
                    <div className="grid gap-1">
                      <label className="flex items-center gap-2 text-sm">
                        <input
                          type="checkbox"
                          checked={stopOnFailure}
                          onChange={(e) => setStopOnFailure(e.target.checked)}
                        />
                        Stop on the first failure
                      </label>
                      <p className="text-xs text-muted-foreground">
                        Nodes that have not received the command are canceled.
                        Nodes already running it finish.
                      </p>
                    </div>
                  </>
                )}
                {reboots && (
                  <div className="grid gap-2">
                    <Label htmlFor={`${rolloutIds}-timeout`}>
                      Wait for a node to come back
                    </Label>
                    <div className="flex items-center gap-2">
                      <Input
                        id={`${rolloutIds}-timeout`}
                        type="number"
                        min={1}
                        value={timeoutMinutes}
                        onChange={(e) => setTimeoutMinutes(e.target.value)}
                      />
                      <span className="text-sm text-muted-foreground">minutes</span>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      A node that has not come back by then counts as failed.
                    </p>
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          {!onPickStep && (
            <Button
              onClick={handleSubmit}
              disabled={!canSubmit}
            >
              {isRollout
                ? `${activeCommand?.label ?? "Send"} ${targetCount} nodes`
                : (activeCommand?.verb ?? "Send")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
