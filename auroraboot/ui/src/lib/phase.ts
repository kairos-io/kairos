// Phase strings come from the store in mixed case ("Building", "Online") and
// from older code paths in lower case, so every comparison goes through these
// helpers, which lowercase first.

export type Tone = "success" | "warning" | "danger" | "info" | "neutral";

type Phase = string | undefined | null;

const tones: Record<string, Tone> = {
  online: "success",
  ready: "success",
  completed: "success",
  active: "success",
  pending: "warning",
  queued: "warning",
  delivered: "warning",
  offline: "danger",
  failed: "danger",
  error: "danger",
  running: "info",
  building: "info",
  upgrading: "info",
  installing: "info",
};

function norm(phase: Phase): string {
  return (phase ?? "").toLowerCase();
}

export function phaseTone(phase: Phase): Tone {
  return tones[norm(phase)] ?? "neutral";
}

export function isOnline(phase: Phase): boolean {
  return norm(phase) === "online";
}

export function isOffline(phase: Phase): boolean {
  return norm(phase) === "offline";
}

export function isBuilding(phase: Phase): boolean {
  return norm(phase) === "building";
}

export function isReady(phase: Phase): boolean {
  return norm(phase) === "ready";
}

export function isFailed(phase: Phase): boolean {
  const p = norm(phase);
  return p === "failed" || p === "error";
}

export function isRunning(phase: Phase): boolean {
  return phaseTone(phase) === "info";
}
