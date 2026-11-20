import { apiFetch } from "./client";

// These types mirror the JSON of store.NodeMetrics in pkg/store/metrics.go.
// Every field except sampledAt is optional, as the agent may omit it.

export interface CPUMetrics {
  usedPercent: number;
}

export interface MemoryMetrics {
  totalBytes: number;
  availableBytes: number;
}

export interface DiskMetrics {
  label?: string;
  mount: string;
  totalBytes: number;
  usedBytes: number;
}

export interface NodeMetrics {
  sampledAt: string;
  uptimeSeconds?: number;
  load?: number[];
  cpu?: CPUMetrics;
  memory?: MemoryMetrics;
  disks?: DiskMetrics[];
  temperatureC?: number;
}

export interface NodeMetricsResponse {
  latest: NodeMetrics | null;
  samples: NodeMetrics[];
}

export async function getNodeMetrics(id: string): Promise<NodeMetricsResponse> {
  const res = await apiFetch<NodeMetricsResponse>(
    `/api/v1/nodes/${encodeURIComponent(id)}/metrics`,
  );
  return { latest: res?.latest ?? null, samples: res?.samples ?? [] };
}

export async function getLatestMetrics(): Promise<Record<string, NodeMetrics>> {
  const res = await apiFetch<Record<string, NodeMetrics>>("/api/v1/metrics/latest");
  return res ?? {};
}
