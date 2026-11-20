import { Monitor, Moon, Sun, type LucideIcon } from "lucide-react";
import { useTheme } from "@/hooks/useTheme";
import type { ThemeMode } from "@/lib/theme";
import { cn } from "@/lib/utils";

const options: { mode: ThemeMode; icon: LucideIcon; label: string }[] = [
  { mode: "system", icon: Monitor, label: "System theme" },
  { mode: "light", icon: Sun, label: "Light theme" },
  { mode: "dark", icon: Moon, label: "Dark theme" },
];

const nextMode: Record<ThemeMode, ThemeMode> = {
  system: "light",
  light: "dark",
  dark: "system",
};

// ThemeToggle switches between system, light and dark. The full control has
// one button per mode; the compact one (icon rail) cycles through them.
export function ThemeToggle({ compact = false }: { compact?: boolean }) {
  const { mode, setMode } = useTheme();

  if (compact) {
    const current = options.find((o) => o.mode === mode) ?? options[0];
    const Icon = current.icon;
    return (
      <button
        type="button"
        aria-label={`Theme: ${mode}`}
        title={`Theme: ${mode}`}
        onClick={() => setMode(nextMode[mode])}
        className="inline-flex h-8 w-8 items-center justify-center rounded-md text-sidebar-fg opacity-70 transition-colors hover:bg-sidebar-muted hover:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Icon className="h-4 w-4" />
      </button>
    );
  }

  return (
    <div role="group" aria-label="Theme" className="flex w-full gap-1 rounded-md bg-sidebar-muted p-1">
      {options.map(({ mode: m, icon: Icon, label }) => (
        <button
          key={m}
          type="button"
          aria-label={label}
          aria-pressed={mode === m}
          title={label}
          onClick={() => setMode(m)}
          className={cn(
            "inline-flex h-7 flex-1 items-center justify-center rounded text-sidebar-fg transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            mode === m ? "bg-sidebar-accent text-white" : "opacity-70 hover:opacity-100",
          )}
        >
          <Icon className="h-4 w-4" />
        </button>
      ))}
    </div>
  );
}
