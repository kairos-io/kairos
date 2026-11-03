import type { LucideIcon } from "lucide-react";
import { ChevronDown } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

export interface SplitButtonItem {
  label: string;
  onSelect(): void;
  disabled?: boolean;
  // Why the item is disabled, or what it does. Shown under the label.
  hint?: string;
}

export interface SplitButtonProps {
  label: string;
  icon?: LucideIcon;
  onClick(): void;
  items: SplitButtonItem[];
  // Accessible name of the menu trigger, for example "Deploy methods".
  menuLabel: string;
}

// SplitButton is a primary action with a menu of alternatives. The two halves
// read as one control: the left runs the default action, the chevron opens
// the menu.
export function SplitButton({ label, icon: Icon, onClick, items, menuLabel }: SplitButtonProps) {
  return (
    <div className="inline-flex items-stretch rounded-md shadow-sm" role="group" aria-label={label}>
      <Button className="rounded-r-none shadow-none" onClick={onClick}>
        {Icon && <Icon aria-hidden="true" />}
        {label}
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            size="icon"
            className="w-7 rounded-l-none border-l border-primary-foreground/25 shadow-none"
            aria-label={menuLabel}
          >
            <ChevronDown aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-56">
          {items.map((item) => (
            <DropdownMenuItem
              key={item.label}
              disabled={item.disabled}
              title={item.hint}
              onSelect={() => item.onSelect()}
              className="flex-col items-start gap-0.5"
            >
              <span>{item.label}</span>
              {item.hint && <span className="text-xs text-muted-foreground">{item.hint}</span>}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
