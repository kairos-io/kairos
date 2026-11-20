import type { Batch, BatchNode, BatchNodeState } from "@/api/batches";

const commandLabels: Record<string, string> = {
  upgrade: "Upgrade",
  "upgrade-recovery": "Upgrade recovery",
  reset: "Reset",
  "apply-cloud-config": "Apply config",
  extension: "Install extension",
  reboot: "Reboot",
  exec: "Run command",
};

// A stopped batch needs attention, so its badge reads as a failure.
export function phaseStatus(phase: string): string {
  return phase === "stopped" ? "failed" : phase;
}

export function commandLabel(command: string): string {
  return commandLabels[command] ?? command;
}

export interface CommandDetail {
  label: string;
  value: string;
}

function extensionsText(raw: string): string {
  try {
    const rows = JSON.parse(raw) as { type: string; name: string }[];
    return rows.map((e) => `${e.type}/${e.name}`).join(", ");
  } catch {
    return raw;
  }
}

// commandDetails turns a command's arguments into labelled values a person
// can read. artifactName resolves an artifact ID to its name, if it has one.
// The recovery flag is left out because the command label already says it.
export function commandDetails(
  command: string,
  args: Record<string, string> | null | undefined,
  artifactName: (id: string) => string | undefined,
): CommandDetail[] {
  const details: CommandDetail[] = [];
  for (const [key, value] of Object.entries(args ?? {})) {
    if (key === "source" && value.startsWith("oci:")) {
      details.push({ label: "Image", value: value.slice("oci:".length) });
    } else if (key === "source" && value.startsWith("artifact:")) {
      const id = value.slice("artifact:".length);
      details.push({ label: "Artifact", value: artifactName(id) || id });
    } else if (key === "source") {
      details.push({ label: "Source", value });
    } else if (key === "extensions" && command !== "extension") {
      details.push({ label: "Extensions", value: extensionsText(value) });
    } else if (key === "recovery" && command === "upgrade-recovery") {
      continue;
    } else if (key === "config") {
      details.push({ label: "Cloud config", value });
    } else if (key === "command") {
      details.push({ label: "Shell command", value });
    } else {
      details.push({ label: key, value });
    }
  }
  return details;
}

// commandSummary is the most telling argument of a command on one line, such
// as the image an upgrade installs, or "" when it has none.
export function commandSummary(details: CommandDetail[]): string {
  const first = details[0];
  if (!first) return "";
  if (first.label === "Image" || first.label === "Shell command") return first.value;
  return `${first.label.toLowerCase()} ${first.value.split("\n")[0]}`;
}

// targetsText names the batch's first nodes and counts the ones left out.
export function targetsText(batch: Batch): string {
  const names = batch.hostnames ?? [];
  if (names.length === 0) return "";
  const total = Object.values(batch.counts).reduce((sum, n) => sum + (n ?? 0), 0);
  const rest = total - names.length;
  return rest > 0 ? `${names.join(", ")} and ${rest} more` : names.join(", ");
}

// The states a node can end in without having finished its command.
const remainingStates: BatchNodeState[] = ["failed", "timed_out", "skipped", "canceled"];

const stateOrder: BatchNodeState[] = [
  "waiting",
  "running",
  "rebooting",
  "done",
  "failed",
  "timed_out",
  "skipped",
  "canceled",
];

export const stateLabels: Record<BatchNodeState, string> = {
  waiting: "waiting",
  running: "running",
  rebooting: "rebooting",
  done: "done",
  failed: "failed",
  timed_out: "timed out",
  skipped: "skipped",
  canceled: "canceled",
};

export const nodeDeletedMessage = "node deleted";

// remainingNodes lists the nodes that did not finish the batch's command. A
// row keeps its node after the batch is final, so a node deleted since then
// is only left out when existing, the IDs of the nodes that exist now, is
// given.
export function remainingNodes(batch: Batch, existing?: ReadonlySet<string> | null): BatchNode[] {
  return (batch.nodes ?? []).filter(
    (n) =>
      remainingStates.includes(n.state) &&
      n.message !== nodeDeletedMessage &&
      (!existing || existing.has(n.nodeID)),
  );
}

// nonZeroCounts lists the states that have nodes, in lifecycle order, as
// [state label, count] pairs.
export function nonZeroCounts(batch: Batch): [string, number][] {
  return stateOrder
    .filter((s) => (batch.counts[s] ?? 0) > 0)
    .map((s) => [stateLabels[s], batch.counts[s] as number]);
}

export function countsText(batch: Batch): string {
  return nonZeroCounts(batch)
    .map(([label, n]) => `${n} ${label}`)
    .join(", ");
}

export function commandReboots(command: string): boolean {
  return command === "upgrade" || command === "reset" || command === "reboot";
}

// A batch is live while it is running or any node is still in flight.
export function isBatchActive(batch: Batch): boolean {
  return (
    batch.phase === "running" ||
    (batch.nodes ?? []).some((n) => n.state === "running" || n.state === "rebooting")
  );
}
