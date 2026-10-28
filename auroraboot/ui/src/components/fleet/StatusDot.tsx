import type { Tone } from "@/lib/phase";
import { cn } from "@/lib/utils";
import { toneBg } from "./tones";

interface StatusDotProps {
  tone: Tone;
  className?: string;
}

// StatusDot is decorative: the status text next to it carries the meaning.
export function StatusDot({ tone, className }: StatusDotProps) {
  return (
    <span
      aria-hidden="true"
      className={cn("inline-block h-2 w-2 shrink-0 rounded-full", toneBg[tone], className)}
    />
  );
}
