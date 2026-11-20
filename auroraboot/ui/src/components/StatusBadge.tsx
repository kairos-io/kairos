import { Badge } from "@/components/ui/badge";
import { phaseTone, type Tone } from "@/lib/phase";
import { cn } from "@/lib/utils";

interface StatusBadgeProps {
  status: string;
  // Text shown instead of the status, which still decides the color.
  label?: string;
  className?: string;
}

// Written out in full so Tailwind can find every class in the source.
const toneStyles: Record<Tone, string> = {
  success: "bg-success/15 text-success-foreground border-success/25",
  warning: "bg-warning/15 text-warning-foreground border-warning/25",
  danger: "bg-danger/15 text-danger-foreground border-danger/25",
  info: "bg-info/15 text-info-foreground border-info/25",
  neutral: "bg-neutral/15 text-neutral-foreground border-neutral/25",
};

// A node whose phase is missing must not be able to throw out of render and
// take the whole page down with it, so an absent status reads as "unknown"
// rather than as a blank screen.
export function StatusBadge({ status, label: text, className }: StatusBadgeProps) {
  const label = text ?? (status || "unknown");
  return (
    <Badge variant="outline" className={cn(toneStyles[phaseTone(status)], className)}>
      {label}
    </Badge>
  );
}
