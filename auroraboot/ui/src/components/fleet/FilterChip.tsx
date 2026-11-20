import type { LucideIcon } from "lucide-react";
import { ChevronDown, X } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

export interface FilterChipOption {
  value: string;
  label: string;
}

interface FilterChipProps {
  label: string;
  icon?: LucideIcon;
  // The active value. Empty means the filter is off.
  value?: string;
  options: FilterChipOption[];
  onSelect(value: string): void;
  onClear(): void;
  emptyText?: string;
}

// FilterChip opens a small menu of values. When a value is active the chip
// shows it and a remove button.
export function FilterChip({ label, icon: Icon, value, options, onSelect, onClear, emptyText }: FilterChipProps) {
  const active = !!value;
  const shown = options.find((o) => o.value === value)?.label ?? value;
  return (
    <div
      className={cn(
        "inline-flex h-8 items-center rounded-full border text-sm",
        active ? "border-primary bg-primary-soft text-foreground" : "border-border bg-background text-muted-foreground",
      )}
    >
      <DropdownMenu>
        <DropdownMenuTrigger
          className="inline-flex h-full items-center gap-1.5 rounded-full px-3 outline-none hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring"
          aria-label={active ? `${label}: ${shown}` : `Filter by ${label.toLowerCase()}`}
        >
          {Icon && <Icon className="h-3.5 w-3.5" aria-hidden="true" />}
          <span>{label}</span>
          {active && <span className="font-medium text-foreground">{shown}</span>}
          {!active && <ChevronDown className="h-3.5 w-3.5" aria-hidden="true" />}
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="max-h-72 overflow-y-auto">
          <DropdownMenuLabel>{label}</DropdownMenuLabel>
          {options.length === 0 && (
            <div className="px-2 py-1.5 text-sm text-muted-foreground">{emptyText ?? "Nothing to filter by"}</div>
          )}
          {options.map((o) => (
            <DropdownMenuItem key={o.value} onSelect={() => onSelect(o.value)}>
              {o.label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      {active && (
        <button
          type="button"
          aria-label={`Remove ${label} filter`}
          onClick={onClear}
          className="mr-1 inline-flex h-6 w-6 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <X className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      )}
    </div>
  );
}
