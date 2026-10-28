import { usageTone } from "@/lib/metrics";
import { cn } from "@/lib/utils";
import { toneBg } from "./tones";

interface MeterBarProps {
  value: number | null;
  className?: string;
}

// MeterBar is a small usage bar with its percentage, or "—" with no data.
export function MeterBar({ value, className }: MeterBarProps) {
  if (value == null) {
    return <span className={cn("text-muted-foreground", className)}>—</span>;
  }
  const v = Math.round(Math.min(100, Math.max(0, value)));
  const tone = usageTone(v);
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <div className="h-1.5 w-16 overflow-hidden rounded-full bg-muted" aria-hidden="true">
        <div data-tone={tone} className={cn("h-full rounded-full", toneBg[tone])} style={{ width: `${v}%` }} />
      </div>
      <span className="text-xs font-semibold tabular-nums">{v}%</span>
    </div>
  );
}
