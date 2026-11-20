import type { Tone } from "@/lib/phase";
import { cn } from "@/lib/utils";
import { toneText } from "./tones";

interface SparklineProps {
  values: number[];
  tone: Tone;
  height?: number;
  className?: string;
}

const WIDTH = 200;

// Sparkline draws values on a fixed 0–100 scale as a line with a soft area.
// With fewer than two points there is no line to draw, so it renders nothing.
export function Sparkline({ values, tone, height = 34, className }: SparklineProps) {
  const n = values.length;
  if (n < 2) return null;
  const pts = values.map((raw, i) => {
    const v = Math.min(100, Math.max(0, raw));
    const x = (i * WIDTH) / (n - 1);
    const y = height - 3 - (v / 100) * (height - 6);
    return [x.toFixed(1), y.toFixed(1)] as const;
  });
  const line = "M" + pts.map((p) => p.join(",")).join("L");
  const end = pts[n - 1];
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${height}`}
      preserveAspectRatio="none"
      aria-hidden="true"
      className={cn("block w-full", toneText[tone], className)}
      style={{ height }}
    >
      <path d={`${line}L${WIDTH},${height}L0,${height}Z`} fill="currentColor" fillOpacity={0.14} />
      <path
        d={line}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.6}
        vectorEffect="non-scaling-stroke"
      />
      <circle cx={end[0]} cy={end[1]} r={2.6} fill="currentColor" />
    </svg>
  );
}
