import { usageTone } from "@/lib/metrics";
import { cn } from "@/lib/utils";
import { toneText } from "./tones";

interface GaugeProps {
  value: number;
  label: string;
  sub?: string;
  size?: number;
  className?: string;
}

const RADIUS = 40;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

// Gauge is a ring that fills to value percent, in a tone by threshold.
export function Gauge({ value, label, sub, size = 96, className }: GaugeProps) {
  const v = Math.round(Math.min(100, Math.max(0, value)));
  const tone = usageTone(v);
  return (
    <div
      role="img"
      aria-label={`${label} ${v}%`}
      data-tone={tone}
      className={cn("relative shrink-0", toneText[tone], className)}
      style={{ width: size, height: size }}
    >
      <svg viewBox="0 0 100 100" width="100%" height="100%" aria-hidden="true">
        <circle
          cx="50"
          cy="50"
          r={RADIUS}
          fill="none"
          strokeWidth="9"
          className="stroke-muted"
        />
        <circle
          cx="50"
          cy="50"
          r={RADIUS}
          fill="none"
          strokeWidth="9"
          stroke="currentColor"
          strokeLinecap="round"
          strokeDasharray={`${((CIRCUMFERENCE * v) / 100).toFixed(1)} ${CIRCUMFERENCE.toFixed(1)}`}
          transform="rotate(-90 50 50)"
        />
      </svg>
      <div
        aria-hidden="true"
        className="absolute inset-0 grid place-items-center text-center leading-tight"
      >
        <div>
          <b className="block text-lg font-extrabold tabular-nums text-foreground">{v}%</b>
          <small className="block text-[10.5px] font-semibold text-muted-foreground">
            {sub ?? label}
          </small>
        </div>
      </div>
    </div>
  );
}
