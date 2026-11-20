import { useState, type FormEvent } from "react";
import { Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface LabelChipsProps {
  labels: Record<string, string>;
  // Saves the full new label set. A rejection keeps the editor open so what
  // was typed is not lost; the caller reports the error.
  onChange(next: Record<string, string>): Promise<void>;
}

function parseLabels(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  text
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
    .forEach((pair) => {
      const i = pair.indexOf("=");
      const k = (i < 0 ? pair : pair.slice(0, i)).trim();
      const v = i < 0 ? "" : pair.slice(i + 1).trim();
      if (k) out[k] = v;
    });
  return out;
}

function formatLabels(labels: Record<string, string>): string {
  return Object.entries(labels)
    .map(([k, v]) => `${k}=${v}`)
    .join(", ");
}

// LabelChips shows a node's labels as removable chips, with an inline
// "key=value" input to add one and an Edit mode for changing them all as text.
//
// The editors hold their own text, seeded only when they open. The page polls
// the node and passes fresh `labels` on every poll; seeding from props here
// would silently replace what the operator is typing.
export function LabelChips({ labels, onChange }: LabelChipsProps) {
  const [mode, setMode] = useState<"view" | "add" | "edit">("view");
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const entries = Object.entries(labels || {});

  async function save(next: Record<string, string>): Promise<boolean> {
    setSaving(true);
    try {
      await onChange(next);
      return true;
    } catch {
      return false;
    } finally {
      setSaving(false);
    }
  }

  function close() {
    setMode("view");
    setDraft("");
    setError("");
  }

  async function handleAdd(e: FormEvent) {
    e.preventDefault();
    const text = draft.trim();
    const i = text.indexOf("=");
    const key = (i < 0 ? text : text.slice(0, i)).trim();
    const value = i < 0 ? "" : text.slice(i + 1).trim();
    if (!key) {
      setError("Enter a label as key=value. The key cannot be empty.");
      return;
    }
    if (Object.prototype.hasOwnProperty.call(labels || {}, key)) {
      setError(`Label "${key}" already exists. Remove it first to change its value.`);
      return;
    }
    if (await save({ ...labels, [key]: value })) close();
  }

  async function handleRemove(key: string) {
    const next = { ...labels };
    delete next[key];
    await save(next);
  }

  async function handleSaveAll() {
    if (await save(parseLabels(draft))) close();
  }

  if (mode === "edit") {
    return (
      <div className="grid gap-2">
        <Label htmlFor="labels-bulk">Labels (comma-separated key=value pairs)</Label>
        <Input
          id="labels-bulk"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="role=worker, env=prod"
        />
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={close} disabled={saving}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSaveAll} loading={saving}>
            Save
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="grid gap-2">
      <ul className="flex flex-wrap items-center gap-1.5" aria-label="Labels">
        {entries.length === 0 && mode === "view" && (
          <li className="text-sm text-muted-foreground">No labels</li>
        )}
        {entries.map(([k, v]) => (
          <li
            key={k}
            className="inline-flex h-6 items-center gap-1 rounded-full border bg-muted pl-2.5 pr-1 font-mono text-xs"
          >
            <span>
              <span className="text-muted-foreground">{k}=</span>
              {v}
            </span>
            <button
              type="button"
              className="rounded-full p-0.5 text-muted-foreground hover:bg-background hover:text-foreground disabled:opacity-50"
              aria-label={`Remove label ${k}`}
              disabled={saving}
              onClick={() => handleRemove(k)}
            >
              <X className="h-3 w-3" aria-hidden="true" />
            </button>
          </li>
        ))}
        {mode === "view" && (
          <li>
            <button
              type="button"
              className="inline-flex h-6 items-center gap-1 rounded-full border border-dashed px-2.5 text-xs text-muted-foreground hover:border-primary hover:text-foreground"
              onClick={() => {
                setDraft("");
                setError("");
                setMode("add");
              }}
            >
              <Plus className="h-3 w-3" aria-hidden="true" />
              Add label
            </button>
          </li>
        )}
      </ul>
      {mode === "add" && (
        <form className="grid gap-1.5" onSubmit={handleAdd}>
          <div className="flex items-center gap-2">
            <Input
              aria-label="New label"
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? "new-label-error" : undefined}
              className="h-7 font-mono text-xs"
              value={draft}
              autoFocus
              onChange={(e) => {
                setDraft(e.target.value);
                setError("");
              }}
              onKeyDown={(e) => {
                if (e.key === "Escape") close();
              }}
              placeholder="key=value"
            />
            <Button type="submit" size="sm" loading={saving}>
              Add
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={close} disabled={saving}>
              Cancel
            </Button>
          </div>
          {error && (
            <p id="new-label-error" role="alert" className="text-xs text-danger">
              {error}
            </p>
          )}
        </form>
      )}
      {mode === "view" && (
        <div>
          <Button
            variant="link"
            size="sm"
            className="h-auto px-0 text-xs"
            onClick={() => {
              setDraft(formatLabels(labels || {}));
              setMode("edit");
            }}
          >
            Edit
          </Button>
        </div>
      )}
    </div>
  );
}
