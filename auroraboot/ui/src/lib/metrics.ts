import type { DiskMetrics, NodeMetrics } from "@/api/metrics";
import type { Tone } from "@/lib/phase";

// All percentages are rounded to integers. A missing or empty field gives null
// so the UI can show "—" instead of a made-up zero.

export function cpuPercent(m: NodeMetrics | null | undefined): number | null {
  if (!m?.cpu) return null;
  return Math.round(m.cpu.usedPercent);
}

export function memPercent(m: NodeMetrics | null | undefined): number | null {
  const mem = m?.memory;
  if (!mem || !mem.totalBytes) return null;
  return Math.round(((mem.totalBytes - mem.availableBytes) / mem.totalBytes) * 100);
}

export function diskPercent(d: DiskMetrics): number {
  if (!d.totalBytes) return 0;
  return Math.round((d.usedBytes / d.totalBytes) * 100);
}

// maxDiskPercent is the usage of the fullest disk.
export function maxDiskPercent(m: NodeMetrics | null | undefined): number | null {
  const disks = m?.disks;
  if (!disks || disks.length === 0) return null;
  return Math.max(...disks.map(diskPercent));
}

// formatUptime shows the two largest units: "45d 2h", "3h 12m", "4m".
export function formatUptime(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}

export const WARNING_PERCENT = 80;
export const DANGER_PERCENT = 90;

export function usageTone(p: number): Tone {
  if (p >= DANGER_PERCENT) return "danger";
  if (p >= WARNING_PERCENT) return "warning";
  return "success";
}

// sustainedHighCpu is true when each of the last `window` samples has a CPU at
// or above threshold. Fewer samples than the window, or a sample without CPU,
// is not enough to call it sustained.
export function sustainedHighCpu(
  samples: NodeMetrics[],
  threshold = 85,
  window = 10,
): boolean {
  if (samples.length < window) return false;
  return samples.slice(-window).every((s) => {
    const c = cpuPercent(s);
    return c !== null && c >= threshold;
  });
}
