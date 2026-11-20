import { useState, type KeyboardEvent } from "react";
import { Label } from "@/components/ui/label";

interface Props {
  value: string[];
  onChange: (next: string[]) => void;
  implicitRoot: string; // "/usr" or "/etc"
  quickAdds?: string[];
  label?: string;
  placeholder?: string;
  disabled?: boolean;
}

export function HierarchyChipInput({
  value,
  onChange,
  implicitRoot,
  quickAdds = [],
  label,
  placeholder = "Add path · Enter to confirm",
  disabled,
}: Props) {
  const [draft, setDraft] = useState("");
  const [err, setErr] = useState<string | null>(null);

  function commit(raw: string) {
    const normalized = raw.replace(/\/+$/g, "").trim();
    if (!normalized) {
      setErr("path is empty");
      return;
    }
    if (!normalized.startsWith("/")) {
      setErr("path must start with /");
      return;
    }
    if (normalized.includes("..")) {
      setErr("path must not contain '..'");
      return;
    }
    if (normalized.length > 256) {
      setErr("path exceeds 256 characters");
      return;
    }
    if (normalized === implicitRoot || normalized === "/") {
      setErr(`${normalized} is implicit and cannot be listed`);
      return;
    }
    if (value.includes(normalized)) {
      setErr(null);
      setDraft("");
      return;
    }
    setErr(null);
    setDraft("");
    onChange([...value, normalized].sort());
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      commit(draft);
    } else if (e.key === "Backspace" && draft === "" && value.length > 0) {
      onChange(value.slice(0, -1));
    }
  }

  function remove(p: string) {
    onChange(value.filter((v) => v !== p));
  }

  return (
    <div>
      {label && <Label>{label}</Label>}
      <p className="text-xs text-muted-foreground mt-1">
        <code className="font-mono">{implicitRoot}</code> is always included.
      </p>

      <div className="mt-2 rounded-md border bg-background px-2 py-1.5 flex flex-wrap gap-1.5 items-center">
        {value.map((p) => (
          <span
            key={p}
            className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-muted border"
          >
            <code className="font-mono">{p}</code>
            <button
              type="button"
              aria-label={`Remove ${p}`}
              className="opacity-60 hover:opacity-100 focus-visible:opacity-100 focus-visible:outline focus-visible:outline-1"
              onClick={() => remove(p)}
            >
              ×
            </button>
          </span>
        ))}
        <input
          type="text"
          role="textbox"
          className="flex-1 min-w-[180px] bg-transparent outline-none text-xs px-1 py-0.5"
          placeholder={placeholder}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          disabled={disabled}
        />
      </div>

      {quickAdds.length > 0 && (
        <div className="flex flex-wrap gap-1.5 mt-2 items-center">
          <span className="text-[11px] text-muted-foreground">Quick add:</span>
          {quickAdds.map((p) => (
            <button
              key={p}
              type="button"
              aria-label={`Add ${p}`}
              disabled={disabled || value.includes(p)}
              onClick={() => commit(p)}
              className="text-[11px] px-2 py-0.5 rounded-full border border-dashed disabled:opacity-40 hover:bg-muted"
            >
              + <code className="font-mono">{p}</code>
            </button>
          ))}
        </div>
      )}

      {err && (
        <p role="alert" className="text-xs text-red-600 mt-1.5">
          {err}
        </p>
      )}
    </div>
  );
}
