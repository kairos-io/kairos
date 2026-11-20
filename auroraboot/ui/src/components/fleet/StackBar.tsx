import type { Tone } from "@/lib/phase";
import { cn } from "@/lib/utils";
import { toneBg } from "./tones";

export interface StackBarCount {
  tone: Tone;
  count: number;
  label: string;
}

interface StackBarProps {
  counts: StackBarCount[];
  className?: string;
}

// StackBar splits a bar by count per status. Zero counts are left out of both
// the bar and its label; with nothing to show it renders an empty track.
export function StackBar({ counts, className }: StackBarProps) {
  const shown = counts.filter((c) => c.count > 0);
  const total = shown.reduce((sum, c) => sum + c.count, 0);
  const label = shown.length ? shown.map((c) => `${c.count} ${c.label}`).join(", ") : "None";
  return (
    <div
      role="img"
      aria-label={label}
      className={cn("flex h-2 w-full overflow-hidden rounded-full bg-muted", className)}
    >
      {shown.map((c) => (
        <div
          key={`${c.tone}-${c.label}`}
          data-tone={c.tone}
          className={cn("h-full", toneBg[c.tone])}
          style={{ width: `${(c.count / total) * 100}%` }}
        />
      ))}
    </div>
  );
}
