import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import type { Tone } from "@/lib/phase";
import { cn } from "@/lib/utils";
import { toneText } from "./tones";

interface StatTileProps {
  label: string;
  icon?: LucideIcon;
  value: ReactNode;
  sub?: ReactNode;
  tone?: Tone;
  children?: ReactNode;
}

// StatTile is one cell of a summary band: a label, a large value, an
// optional sub-line and optional extra content such as a StackBar.
export function StatTile({ label, icon: Icon, value, sub, tone, children }: StatTileProps) {
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-lg border bg-card p-4">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        {Icon && <Icon className={cn("h-4 w-4 shrink-0", tone && toneText[tone])} aria-hidden="true" />}
        <span className="truncate">{label}</span>
      </div>
      <div className="text-2xl font-semibold tracking-tight">{value}</div>
      {sub && <div className="text-xs text-muted-foreground">{sub}</div>}
      {children}
    </div>
  );
}
