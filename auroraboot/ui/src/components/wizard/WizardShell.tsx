import type { ReactNode } from "react";
import { AlertCircle, Check } from "lucide-react";

import { Button } from "@/components/ui/button";
import { StatusDot } from "@/components/fleet/StatusDot";
import type { Tone } from "@/lib/phase";
import { cn } from "@/lib/utils";

export type WizardStep = {
  key: string;
  label: string;
  summary?: string;
  state: "todo" | "current" | "done" | "error";
};

export interface WizardFooter {
  onBack?(): void;
  backLabel?: string;
  status?: { tone: Tone; text: string };
  secondary?: ReactNode;
  primary: { label: string; onClick(): void; disabled?: boolean; loading?: boolean };
}

export interface WizardShellProps {
  steps: WizardStep[];
  current: string;
  onStepChange(key: string): void;
  aside?: ReactNode;
  footer: WizardFooter;
  children: ReactNode;
}

// Written out in full so Tailwind can find every class in the source.
const toneInk: Record<Tone, string> = {
  success: "text-success-foreground",
  warning: "text-warning-foreground",
  danger: "text-danger-foreground",
  info: "text-info-foreground",
  neutral: "text-neutral-foreground",
};

function StepMarker({ index, state, isCurrent }: { index: number; state: WizardStep["state"]; isCurrent: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-xs font-semibold",
        isCurrent && "border-primary bg-primary text-primary-foreground",
        !isCurrent && state === "done" && "border-success/30 bg-success/15 text-success-foreground",
        !isCurrent && state === "error" && "border-danger/30 bg-danger/15 text-danger-foreground",
        !isCurrent && state === "todo" && "border-border bg-background text-muted-foreground",
      )}
    >
      {!isCurrent && state === "done" ? (
        <Check className="h-3.5 w-3.5" />
      ) : !isCurrent && state === "error" ? (
        <AlertCircle className="h-3.5 w-3.5" />
      ) : (
        index + 1
      )}
    </span>
  );
}

// WizardShell lays out a multi-step form: a stepper, the form, an optional
// summary aside and a sticky footer with the step actions. Below 1024px the
// stepper becomes a grid above the form and the aside moves below it.
export function WizardShell({ steps, current, onStepChange, aside, footer, children }: WizardShellProps) {
  const { onBack, backLabel = "Back", status, secondary, primary } = footer;

  return (
    <div className="flex min-w-0 flex-col">
      <div
        className={cn(
          "grid min-w-0 gap-6 lg:items-start",
          aside ? "lg:grid-cols-[220px_minmax(0,1fr)_280px]" : "lg:grid-cols-[220px_minmax(0,1fr)]",
        )}
      >
        <nav aria-label="Steps" className="grid grid-cols-2 gap-1 sm:grid-cols-3 lg:sticky lg:top-4 lg:grid-cols-1">
          {steps.map((step, i) => {
            const isCurrent = step.key === current;
            const clickable = isCurrent || step.state === "done" || step.state === "error";
            return (
              <button
                key={step.key}
                type="button"
                disabled={!clickable}
                aria-current={isCurrent ? "step" : undefined}
                onClick={() => onStepChange(step.key)}
                className={cn(
                  "flex min-w-0 items-start gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors",
                  "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
                  "disabled:cursor-not-allowed",
                  isCurrent ? "bg-primary-soft" : clickable && "hover:bg-muted",
                )}
              >
                <StepMarker index={i} state={step.state} isCurrent={isCurrent} />
                <span className="flex min-w-0 flex-col">
                  <span
                    className={cn(
                      "truncate text-sm font-medium",
                      step.state === "todo" && !isCurrent ? "text-muted-foreground" : "text-foreground",
                    )}
                  >
                    {step.label}
                  </span>
                  <span
                    className={cn(
                      "truncate text-xs",
                      step.state === "error" && !isCurrent ? "text-danger-foreground" : "text-muted-foreground",
                    )}
                  >
                    {step.summary ?? (step.state === "todo" && !isCurrent ? "Not set yet" : "")}
                  </span>
                </span>
              </button>
            );
          })}
        </nav>

        <div className="min-w-0">{children}</div>

        {aside && <aside className="min-w-0 lg:sticky lg:top-4">{aside}</aside>}
      </div>

      <footer className="sticky bottom-0 z-10 mt-6 flex flex-wrap items-center gap-2 border-t border-border bg-background/95 py-3 backdrop-blur">
        {onBack && (
          <Button variant="outline" onClick={onBack}>
            {backLabel}
          </Button>
        )}
        {status && (
          <span className={cn("flex items-center gap-2 text-sm", toneInk[status.tone])}>
            <StatusDot tone={status.tone} />
            {status.text}
          </span>
        )}
        <span className="flex-1" />
        {secondary}
        <Button onClick={primary.onClick} disabled={primary.disabled} loading={primary.loading}>
          {primary.label}
        </Button>
      </footer>
    </div>
  );
}
