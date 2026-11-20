import { useState, type ReactNode } from "react";

import { InfoTooltip } from "@/components/InfoTooltip";
import { SegmentedControl } from "@/components/wizard/SegmentedControl";
import { PRODUCT_NAME } from "@/lib/branding";

// The identifiers are the ones the builder bakes into
// phonehome.allowed_commands (see PHONEHOME_SAFE_DEFAULTS and
// PHONEHOME_DESTRUCTIVE_COMMANDS in lib/buildConfig.ts).
export const SAFE_COMMANDS = ["upgrade", "upgrade-recovery", "reboot", "unregister"];
export const FULL_COMMANDS = [...SAFE_COMMANDS, "exec", "reset", "apply-cloud-config", "extension"];

export type CommandPreset = "safe" | "full" | "custom";

function sameSet(a: string[], b: string[]): boolean {
  const s = new Set(a);
  return s.size === b.length && b.every((c) => s.has(c));
}

export function presetFor(commands: string[]): CommandPreset {
  if (sameSet(commands, SAFE_COMMANDS)) return "safe";
  if (sameSet(commands, FULL_COMMANDS)) return "full";
  return "custom";
}

export interface CommandPresetsProps {
  value: string[];
  onChange(next: string[]): void;
  renderCustom(): ReactNode;
}

// CommandPresets picks phonehome.allowed_commands from two presets, or shows
// the full checkbox picker for Custom. Custom stays open once chosen, even when
// the ticked commands happen to match a preset again.
export function CommandPresets({ value, onChange, renderCustom }: CommandPresetsProps) {
  const [customOpen, setCustomOpen] = useState(false);
  const preset: CommandPreset = customOpen ? "custom" : presetFor(value);

  const choose = (p: CommandPreset) => {
    if (p === "safe") {
      setCustomOpen(false);
      onChange([...SAFE_COMMANDS]);
    } else if (p === "full") {
      setCustomOpen(false);
      onChange([...FULL_COMMANDS]);
    } else {
      setCustomOpen(true);
    }
  };

  return (
    <div className="grid gap-3">
      <div>
        <p className="text-xs font-medium">
          Allowed remote commands
          <InfoTooltip>
            Baked into <code className="font-mono">phonehome.allowed_commands</code> in the
            node's cloud-config. Commands not listed here are refused by the
            node, even if {PRODUCT_NAME} requests them.
          </InfoTooltip>
        </p>
        <p className="text-xs text-muted-foreground mt-1">
          Commands not listed here will be denied by the node.
        </p>
      </div>
      <SegmentedControl<CommandPreset>
        ariaLabel="Remote command preset"
        value={preset}
        onChange={choose}
        options={[
          { value: "safe", label: "Safe", hint: "Upgrade, reboot, unregister" },
          { value: "full", label: "Full", hint: "Adds exec, reset, cloud-config, extensions" },
          { value: "custom", label: "Custom", hint: "Pick each command" },
        ]}
      />
      {preset === "custom" && renderCustom()}
    </div>
  );
}
