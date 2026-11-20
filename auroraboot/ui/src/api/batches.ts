import { apiFetch } from "./client";

export type BatchPhase = "running" | "completed" | "failed" | "stopped";
export type BatchNodeState =
  | "waiting"
  | "running"
  | "rebooting"
  | "done"
  | "failed"
  | "timed_out"
  | "skipped"
  | "canceled";

export interface BatchNode {
  batchID: string;
  nodeID: string;
  position: number;
  hostname: string;
  commandID: string | null;
  state: BatchNodeState;
  message: string;
  startedAt: string | null;
  deadline: string | null;
  finishedAt: string | null;
}

export interface Batch {
  id: string;
  command: string;
  args: Record<string, string>;
  selection: string;
  groupID: string | null;
  concurrency: number;
  stopOnFailure: boolean;
  rebootTimeoutSeconds: number;
  phase: BatchPhase;
  createdAt: string;
  finishedAt: string | null;
  stoppedAt: string | null;
  stopReason: string;
  stoppedByNodeID: string | null;
  counts: Partial<Record<BatchNodeState, number>>;
  // The hostnames of the first few nodes in start order. Only the batch list
  // has them.
  hostnames?: string[];
  nodes?: BatchNode[];
}

// Rollout is how a command spreads over several nodes. An absent concurrency
// means every node at once.
export interface Rollout {
  concurrency?: number;
  stopOnFailure: boolean;
  rebootTimeoutSeconds: number;
}

export function listBatches(opts?: {
  groupID?: string;
  nodeID?: string;
  limit?: number;
  offset?: number;
}): Promise<{ batches: Batch[]; total: number }> {
  const query = new URLSearchParams();
  if (opts?.groupID) query.set("groupID", opts.groupID);
  if (opts?.nodeID) query.set("nodeID", opts.nodeID);
  if (opts?.limit) query.set("limit", String(opts.limit));
  if (opts?.offset) query.set("offset", String(opts.offset));
  const qs = query.toString();
  return apiFetch<{ batches: Batch[]; total: number }>(
    `/api/v1/batches${qs ? `?${qs}` : ""}`,
  );
}

export function getBatch(id: string): Promise<Batch> {
  return apiFetch<Batch>(`/api/v1/batches/${encodeURIComponent(id)}`);
}

export function stopBatch(id: string): Promise<Batch> {
  return apiFetch<Batch>(`/api/v1/batches/${encodeURIComponent(id)}/stop`, {
    method: "POST",
  });
}
