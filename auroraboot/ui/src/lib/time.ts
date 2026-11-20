// Shared time and size formatting for the fleet screens.

// timeAgo renders how long ago a timestamp was, in the largest whole unit:
// "12s ago", "5m ago", "3h ago", "2d ago". An empty or unparseable date reads
// as "Never". A date in the future (clock skew) reads as "0s ago".
export function timeAgo(date: string | null | undefined, now: number = Date.now()): string {
  if (!date) return "Never";
  const t = new Date(date).getTime();
  if (Number.isNaN(t)) return "Never";
  const seconds = Math.max(0, Math.floor((now - t) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

const units = ["KiB", "MiB", "GiB", "TiB", "PiB"];

// formatBytes renders a byte count in binary units with one decimal, for
// example "7.7 GiB". Values below 1 KiB are whole bytes.
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 1024) return `${Math.max(0, Math.round(n || 0))} B`;
  let value = n / 1024;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value.toFixed(1)} ${units[i]}`;
}
