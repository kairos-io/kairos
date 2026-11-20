import { useId, useRef, type KeyboardEvent } from "react";

import { cn } from "@/lib/utils";

export interface SegmentedOption<T extends string> {
  value: T;
  label: string;
  hint?: string;
  disabled?: boolean;
}

export interface SegmentedControlProps<T extends string> {
  value: T;
  onChange(v: T): void;
  options: SegmentedOption<T>[];
  ariaLabel: string;
}

// SegmentedControl is a compact single-choice control (a radio group) for a
// few short options. Arrow keys move the selection and skip disabled options;
// only the selected option is in the tab order.
export function SegmentedControl<T extends string>({
  value,
  onChange,
  options,
  ariaLabel,
}: SegmentedControlProps<T>) {
  const id = useId();
  const refs = useRef<(HTMLButtonElement | null)[]>([]);

  const move = (from: number, dir: 1 | -1) => {
    const n = options.length;
    for (let step = 1; step <= n; step++) {
      const i = (from + dir * step + n) % n;
      if (!options[i].disabled) {
        onChange(options[i].value);
        refs.current[i]?.focus();
        return;
      }
    }
  };

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>, index: number) => {
    if (e.key === "ArrowRight" || e.key === "ArrowDown") {
      e.preventDefault();
      move(index, 1);
    } else if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
      e.preventDefault();
      move(index, -1);
    }
  };

  const hasSelected = options.some((o) => o.value === value && !o.disabled);

  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      className="inline-flex flex-wrap gap-1 rounded-lg border border-border bg-muted/40 p-1"
    >
      {options.map((o, i) => {
        const selected = o.value === value;
        const focusable = selected || (!hasSelected && i === 0);
        const hintId = o.hint ? `${id}-${i}-hint` : undefined;
        return (
          <button
            key={o.value}
            ref={(el) => {
              refs.current[i] = el;
            }}
            type="button"
            role="radio"
            aria-checked={selected}
            aria-label={o.label}
            aria-describedby={hintId}
            disabled={o.disabled}
            tabIndex={focusable ? 0 : -1}
            onClick={() => onChange(o.value)}
            onKeyDown={(e) => onKeyDown(e, i)}
            className={cn(
              "flex flex-col items-start rounded-md px-3 py-1.5 text-left text-sm transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              "disabled:cursor-not-allowed disabled:opacity-50",
              selected
                ? "bg-card text-foreground shadow-sm ring-1 ring-primary"
                : "text-muted-foreground hover:bg-card/60 hover:text-foreground",
            )}
          >
            <span className="font-medium">{o.label}</span>
            {o.hint && (
              <span id={hintId} className="text-xs text-muted-foreground">
                {o.hint}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
