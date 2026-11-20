import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";

interface SelectionBarProps {
  count: number;
  onClear(): void;
  // The bulk actions, in placement order: the primary first, then the rest.
  children: ReactNode;
}

// SelectionBar is the dark action bar shown above a list while rows are
// selected. It renders nothing when the selection is empty.
export function SelectionBar({ count, onClear, children }: SelectionBarProps) {
  if (count === 0) return null;
  return (
    <section
      aria-label="Selection"
      className="mb-3 flex flex-wrap items-center gap-2 rounded-md border border-border bg-navy px-3 py-2 text-white dark:bg-card dark:text-foreground"
    >
      <span className="mr-2 text-sm font-semibold" aria-live="polite">
        {count} selected
      </span>
      {children}
      <Button variant="ghost" size="sm" className="ml-auto hover:bg-white/10 hover:text-inherit" onClick={onClear}>
        Clear
      </Button>
    </section>
  );
}
