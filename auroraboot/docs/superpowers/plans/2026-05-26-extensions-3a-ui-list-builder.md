# Extensions — Plan 3a of 3: UI API client + Extensions list, builder, detail

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the standalone Extensions area: API client, list page (empty / error / populated), 3-step builder wizard (From artifact / Base image / Dockerfile), and detail page. The shared `HierarchyChipInput` and type-chip helpers land here because both the Extensions UI and the ArtifactBuilder integration in Plan 3b use them.

**Architecture:** New TypeScript module at `ui/src/api/extensions.ts` mirrors `api/artifacts.ts`. Pages live under `ui/src/pages/` (`Extensions.tsx`, `ExtensionBuilder.tsx`, `ExtensionDetail.tsx`). Shared components in `ui/src/components/` (`HierarchyChipInput.tsx`, `ExtensionTypeChip.tsx`). Validation rules mirror the server's (Plan 2c) — clearer error UX without a round-trip. Tests use vitest + React Testing Library, the existing `ui/src/test/setup.ts`, and a thin `fetch` mock per spec.

**Tech Stack:** React 19, react-router 7, Tailwind 4 + shadcn/ui (`Button`, `Card`, `Input`, `Label`, `Select`, `Textarea`), lucide-react icons, vitest + RTL.

**Repository:** `/home/mudler/_git/AuroraBoot`

**Prerequisites:** Plans 2a–2e merged (the backend REST surface this UI talks to).

---

## Reference — read these before starting

| File | What it does |
|---|---|
| `ui/src/api/artifacts.ts` (1-90) | Pattern for typed API helpers — copy. |
| `ui/src/api/client.ts` | `apiFetch`, `apiFetchText` — reused. |
| `ui/src/pages/Artifacts.tsx` | List page pattern. |
| `ui/src/pages/ArtifactBuilder.tsx` | 4-step wizard pattern (step indicator, validation, focus restoration). |
| `ui/src/pages/ArtifactDetail.tsx` | Phase + log streaming over WS, action buttons. |
| `ui/src/components/PageHeader.tsx` | Page chrome. |
| `ui/src/components/ui/*.tsx` | shadcn primitives — never roll your own button. |
| `ui/src/components/Layout.tsx:43` | Existing nav items — add Extensions entry. |
| `ui/src/test/smoke.test.tsx` | matchMedia mock + react-router test harness — copy. |

---

## Task 1: `api/extensions.ts` — types + functions

**Files:**
- Create: `ui/src/api/extensions.ts`
- Create: `ui/src/api/extensions.test.ts`

The API client. All endpoints from Plans 2c–2e are typed here.

- [ ] **Step 1: Write the failing spec**

Create `ui/src/api/extensions.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  listExtensions,
  getExtension,
  createExtension,
  deleteExtension,
  cancelExtension,
  getExtensionLogs,
  extensionDownloadUrl,
  type CreateExtensionInput,
} from "./extensions";

beforeEach(() => {
  window.localStorage.clear();
  window.localStorage.setItem("auroraboot_token", "test-token");
  vi.restoreAllMocks();
});

describe("api/extensions", () => {
  it("listExtensions GETs /api/v1/extensions", async () => {
    const fetchSpy = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    await listExtensions();
    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/v1/extensions",
      expect.objectContaining({ headers: expect.objectContaining({ Authorization: "Bearer test-token" }) }),
    );
  });

  it("createExtension POSTs the typed body", async () => {
    const fetchSpy = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(`{"id":"e-1","phase":"Pending"}`, { status: 201, headers: { "Content-Type": "application/json" } }),
    );
    const input: CreateExtensionInput = {
      name: "ts",
      type: "sysext",
      arch: "amd64",
      version: "v1.74.0",
      source: { mode: "image", baseImage: "ubuntu:24.04" },
      hierarchies: ["/opt"],
      serviceReload: false,
    };
    const got = await createExtension(input);
    expect(got.id).toBe("e-1");
    const [, init] = fetchSpy.mock.calls[0];
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toEqual(input);
  });

  it("extensionDownloadUrl embeds the token in the query string", () => {
    const url = extensionDownloadUrl("e-1", "ts.sysext.raw");
    expect(url).toBe("/api/v1/extensions/e-1/download/ts.sysext.raw?token=test-token");
  });

  it("getExtensionLogs returns text", async () => {
    vi.spyOn(window, "fetch").mockResolvedValue(
      new Response("step 1\nstep 2", { status: 200, headers: { "Content-Type": "text/plain" } }),
    );
    const got = await getExtensionLogs("e-1");
    expect(got).toBe("step 1\nstep 2");
  });

  it("deleteExtension DELETEs and tolerates 204", async () => {
    const fetchSpy = vi.spyOn(window, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await deleteExtension("e-1");
    expect(fetchSpy.mock.calls[0][1]?.method).toBe("DELETE");
  });

  it("cancelExtension POSTs to /cancel", async () => {
    const fetchSpy = vi.spyOn(window, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    await cancelExtension("e-1");
    expect(fetchSpy).toHaveBeenCalledWith(
      "/api/v1/extensions/e-1/cancel",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("getExtension parses the record", async () => {
    vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(
        `{"id":"e-1","name":"ts","type":"sysext","phase":"Ready","arch":"amd64","version":"v1.74.0","hierarchies":["/opt"]}`,
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    const got = await getExtension("e-1");
    expect(got.name).toBe("ts");
    expect(got.hierarchies).toEqual(["/opt"]);
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/api/extensions.test.ts
```

Expected: FAIL — module doesn't exist.

- [ ] **Step 3: Implement the client**

Create `ui/src/api/extensions.ts`:

```ts
import { apiFetch, apiFetchText } from "./client";

export type ExtensionType = "sysext" | "confext";

export interface ExtensionSource {
  mode: "artifact" | "image" | "dockerfile";
  artifactId?: string;
  baseImage?: string;
  dockerfile?: string;
  extraSteps?: string;
  buildContextDir?: string;
}

export interface Extension {
  id: string;
  name: string;
  type: ExtensionType;
  phase: string; // Pending | Building | Ready | Error
  message: string;
  arch: string;
  version: string;
  sourceMode: ExtensionSource["mode"];
  sourceArtifactId?: string;
  sourceImage?: string;
  dockerfile?: string;
  extraSteps?: string;
  signingKeySetId?: string;
  hierarchies?: string[];
  serviceReload?: boolean;
  containerImage?: string;
  rawFilename?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CreateExtensionInput {
  name: string;
  type: ExtensionType;
  arch: string;
  version: string;
  source: ExtensionSource;
  signingKeySetId?: string;
  hierarchies?: string[];
  serviceReload?: boolean;
}

export interface ExtensionBuildStatus {
  id: string;
  phase: string;
  message: string;
  rawFile?: string;
  containerImage?: string;
}

export function listExtensions(): Promise<Extension[]> {
  return apiFetch<Extension[]>("/api/v1/extensions");
}

export function getExtension(id: string): Promise<Extension> {
  return apiFetch<Extension>(`/api/v1/extensions/${id}`);
}

export function createExtension(input: CreateExtensionInput): Promise<ExtensionBuildStatus> {
  return apiFetch<ExtensionBuildStatus>("/api/v1/extensions", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updateExtension(id: string, patch: { name?: string }): Promise<Extension> {
  return apiFetch<Extension>(`/api/v1/extensions/${id}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

export function deleteExtension(id: string): Promise<void> {
  return apiFetch<void>(`/api/v1/extensions/${id}`, { method: "DELETE" });
}

export function cancelExtension(id: string): Promise<void> {
  return apiFetch<void>(`/api/v1/extensions/${id}/cancel`, { method: "POST" });
}

export function getExtensionLogs(id: string): Promise<string> {
  return apiFetchText(`/api/v1/extensions/${id}/logs`);
}

export function extensionDownloadUrl(id: string, filename: string): string {
  const token = localStorage.getItem("auroraboot_token") ?? "";
  return `/api/v1/extensions/${id}/download/${filename}?token=${token}`;
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/api/extensions.test.ts
```

Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add ui/src/api/extensions.ts ui/src/api/extensions.test.ts
git commit -m "ui(api): typed client for /api/v1/extensions"
```

---

## Task 2: `ExtensionTypeChip` shared component

**Files:**
- Create: `ui/src/components/ExtensionTypeChip.tsx`
- Create: `ui/src/components/ExtensionTypeChip.test.tsx`

Tiny chip used everywhere the type appears. Sky family for `sysext`, violet family for `confext`. Matches the polish-pass color rules in the spec.

- [ ] **Step 1: Write the failing spec**

Create `ui/src/components/ExtensionTypeChip.test.tsx`:

```tsx
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { ExtensionTypeChip } from "./ExtensionTypeChip";

describe("ExtensionTypeChip", () => {
  it("renders 'sysext' with the sky palette", () => {
    const { container } = render(<ExtensionTypeChip type="sysext" />);
    expect(screen.getByText("sysext")).toBeInTheDocument();
    expect(container.firstChild).toHaveClass(/sky/);
  });

  it("renders 'confext' with the violet palette", () => {
    const { container } = render(<ExtensionTypeChip type="confext" />);
    expect(screen.getByText("confext")).toBeInTheDocument();
    expect(container.firstChild).toHaveClass(/violet/);
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/components/ExtensionTypeChip.test.tsx
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Create `ui/src/components/ExtensionTypeChip.tsx`:

```tsx
import type { ExtensionType } from "@/api/extensions";

const STYLES: Record<ExtensionType, string> = {
  sysext: "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300",
  confext: "border-violet-500/30 bg-violet-500/10 text-violet-700 dark:text-violet-300",
};

export function ExtensionTypeChip({ type }: { type: ExtensionType }) {
  return (
    <span
      className={`inline-flex items-center text-[11px] font-medium px-2 py-0.5 rounded-full border ${STYLES[type]}`}
    >
      {type}
    </span>
  );
}
```

- [ ] **Step 4: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/components/ExtensionTypeChip.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/ExtensionTypeChip.tsx ui/src/components/ExtensionTypeChip.test.tsx
git commit -m "ui(components): ExtensionTypeChip"
```

---

## Task 3: `HierarchyChipInput` shared component

**Files:**
- Create: `ui/src/components/HierarchyChipInput.tsx`
- Create: `ui/src/components/HierarchyChipInput.test.tsx`

Free-form chip input. `/usr` (or `/etc` for confext) is implicit and never a chip — shown in help text. Quick-add buttons offer common paths. Enter or comma adds. Backspace on empty input removes the last chip. Each chip has a real `<button aria-label="Remove /opt">` × control.

Client-side validation mirrors the server: must start with `/`, no `..`, length ≤ 256, not equal to the implicit root.

- [ ] **Step 1: Write the failing spec**

Create `ui/src/components/HierarchyChipInput.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { HierarchyChipInput } from "./HierarchyChipInput";

function setup(initial: string[] = []) {
  const onChange = vi.fn();
  const utils = render(
    <HierarchyChipInput
      value={initial}
      onChange={onChange}
      implicitRoot="/usr"
      quickAdds={["/opt", "/srv"]}
      label="Hierarchies"
    />,
  );
  return { ...utils, onChange };
}

describe("HierarchyChipInput", () => {
  it("renders existing chips with accessible remove buttons", () => {
    setup(["/opt", "/srv"]);
    expect(screen.getByText("/opt")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /remove \/opt/i })).toBeInTheDocument();
  });

  it("adds a chip on Enter and calls onChange", () => {
    const { onChange } = setup();
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "/var/lib" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChange).toHaveBeenCalledWith(["/var/lib"]);
  });

  it("rejects a path without a leading slash", () => {
    const { onChange } = setup();
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "opt" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent(/start with/i);
  });

  it("rejects the implicit root", () => {
    const { onChange } = setup();
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "/usr" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent(/implicit/i);
  });

  it("rejects a path containing ..", () => {
    const { onChange } = setup();
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "/opt/../etc" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("strips trailing slashes and dedupes", () => {
    const { onChange } = setup(["/opt"]);
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "/opt/" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("Quick-add adds /opt", () => {
    const { onChange } = setup();
    fireEvent.click(screen.getByRole("button", { name: /add \/opt/i }));
    expect(onChange).toHaveBeenCalledWith(["/opt"]);
  });

  it("Backspace on empty input removes the last chip", () => {
    const { onChange } = setup(["/opt", "/srv"]);
    const input = screen.getByRole("textbox");
    fireEvent.keyDown(input, { key: "Backspace" });
    expect(onChange).toHaveBeenCalledWith(["/opt"]);
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/components/HierarchyChipInput.test.tsx
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Create `ui/src/components/HierarchyChipInput.tsx`:

```tsx
import { useState, type KeyboardEvent } from "react";
import { Label } from "@/components/ui/label";

interface Props {
  value: string[];
  onChange: (next: string[]) => void;
  implicitRoot: string;        // "/usr" or "/etc"
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
```

- [ ] **Step 4: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/components/HierarchyChipInput.test.tsx
```

Expected: PASS (8 tests).

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/HierarchyChipInput.tsx ui/src/components/HierarchyChipInput.test.tsx
git commit -m "ui(components): HierarchyChipInput with client-side validation"
```

---

## Task 4: Extensions list page (empty + populated)

**Files:**
- Create: `ui/src/pages/Extensions.tsx`
- Create: `ui/src/pages/Extensions.test.tsx`

The list page. Empty state: one-line value prop, primary "Build extension" button, three template chips. Populated: table mirroring the Artifacts list with the columns the spec calls out (Name · Type · Arch · Version · Signed · Phase · Updated · Actions). Phase Building gets a determinate progress bar; Error rows carry an inline message excerpt.

- [ ] **Step 1: Write the failing spec**

Create `ui/src/pages/Extensions.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { Extensions } from "./Extensions";
import type { Extension } from "@/api/extensions";

function mockList(rows: Extension[]) {
  vi.spyOn(window, "fetch").mockResolvedValue(
    new Response(JSON.stringify(rows), { status: 200, headers: { "Content-Type": "application/json" } }),
  );
}

beforeEach(() => {
  window.localStorage.setItem("auroraboot_token", "tok");
  vi.restoreAllMocks();
});

describe("Extensions page", () => {
  it("renders the empty state with template chips when the API returns []", async () => {
    mockList([]);
    render(<MemoryRouter><Extensions /></MemoryRouter>);
    await waitFor(() =>
      expect(screen.getByText(/No extensions yet/i)).toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: /Build extension/i })).toBeInTheDocument();
    expect(screen.getByText(/Tailscale/i)).toBeInTheDocument();
    expect(screen.getByText(/Fluent-bit/i)).toBeInTheDocument();
  });

  it("renders rows for the returned extensions", async () => {
    mockList([
      { id: "e-1", name: "tailscale-agent", type: "sysext", phase: "Ready", arch: "amd64",
        version: "v1.74.0", sourceMode: "image",
        createdAt: "2026-05-26T10:00:00Z", updatedAt: "2026-05-26T10:00:00Z", message: "" } as Extension,
      { id: "e-2", name: "fluent-bit", type: "confext", phase: "Error", arch: "amd64",
        version: "v0.2", sourceMode: "image",
        message: "systemd-repart: device too small for verity",
        createdAt: "", updatedAt: "" } as Extension,
    ]);
    render(<MemoryRouter><Extensions /></MemoryRouter>);
    await waitFor(() => expect(screen.getByText("tailscale-agent")).toBeInTheDocument());
    expect(screen.getByText("fluent-bit")).toBeInTheDocument();
    expect(screen.getByText(/systemd-repart/)).toBeInTheDocument();
  });

  it("renders a Building progress bar for in-progress builds", async () => {
    mockList([
      { id: "e-3", name: "nv", type: "sysext", phase: "Building", arch: "arm64",
        version: "v1.16.1", sourceMode: "image", message: "64",
        createdAt: "", updatedAt: "" } as Extension,
    ]);
    render(<MemoryRouter><Extensions /></MemoryRouter>);
    await waitFor(() => expect(screen.getByText("Building")).toBeInTheDocument());
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/Extensions.test.tsx
```

Expected: FAIL.

- [ ] **Step 3: Implement the list page**

Create `ui/src/pages/Extensions.tsx`:

```tsx
import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { listExtensions, type Extension } from "@/api/extensions";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/PageHeader";
import { ExtensionTypeChip } from "@/components/ExtensionTypeChip";

const TEMPLATES = ["Tailscale", "Fluent-bit", "Nvidia container toolkit"];

export function Extensions() {
  const navigate = useNavigate();
  const [rows, setRows] = useState<Extension[] | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    listExtensions().then(setRows).catch((e) => setErr(String(e)));
  }, []);

  if (err) {
    return <div className="text-red-600 p-4">Failed to load: {err}</div>;
  }
  if (rows === null) {
    return <div className="p-4 text-muted-foreground text-sm">Loading…</div>;
  }
  if (rows.length === 0) {
    return (
      <div>
        <PageHeader title="Extensions" description="System and config extensions installed on nodes alongside the OS image." />
        <div className="text-center py-16">
          <p className="text-xl font-semibold mb-1.5">No extensions yet</p>
          <p className="text-sm text-muted-foreground max-w-md mx-auto mb-5">
            System and config extensions extend a running Kairos node without re-imaging — ship a binary,
            a config drop-in, or a whole agent on top of an OS artifact.
          </p>
          <Button onClick={() => navigate("/extensions/new")}>Build extension</Button>
          <div className="flex gap-2 justify-center mt-4 text-xs text-muted-foreground items-center">
            <span>Or start from a template:</span>
            {TEMPLATES.map((t) => (
              <Link key={t} to={`/extensions/new?template=${encodeURIComponent(t)}`}
                    className="px-2.5 py-0.5 rounded-full border border-dashed">
                {t}
              </Link>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div>
      <PageHeader title="Extensions" description="System and config extensions installed on nodes alongside the OS image.">
        <Button onClick={() => navigate("/extensions/new")}>Build extension</Button>
      </PageHeader>

      <div className="rounded-md border overflow-hidden text-sm">
        <div className="grid grid-cols-[1.6fr_.7fr_.7fr_.7fr_.6fr_1.4fr_.9fr_.7fr] px-3 py-2 bg-muted/50 text-[11px] uppercase tracking-wider text-muted-foreground">
          <div>Name</div><div>Type</div><div>Arch</div><div>Version</div><div>Signed</div>
          <div>Phase</div><div>Updated</div><div className="text-right">Actions</div>
        </div>
        {rows.map((r) => <ExtensionRow key={r.id} ext={r} />)}
      </div>
    </div>
  );
}

function ExtensionRow({ ext }: { ext: Extension }) {
  const navigate = useNavigate();
  const progress = ext.phase === "Building" ? parseInt(ext.message, 10) || 50 : null;
  return (
    <div
      className="grid grid-cols-[1.6fr_.7fr_.7fr_.7fr_.6fr_1.4fr_.9fr_.7fr] px-3 py-3 items-center border-t hover:bg-muted/30 cursor-pointer"
      onClick={() => navigate(`/extensions/${ext.id}`)}
    >
      <div>
        <div className="font-medium">{ext.name}</div>
        <div className="text-[11px] text-muted-foreground">{sourceLabel(ext)}</div>
      </div>
      <div><ExtensionTypeChip type={ext.type} /></div>
      <div><code className="text-[11px]">{ext.arch}</code></div>
      <div><code className="text-[11px]">{ext.version}</code></div>
      <div>{ext.signingKeySetId ? <span className="text-emerald-600">✓</span> : <span className="opacity-40">—</span>}</div>
      <div>
        {ext.phase === "Building" && progress !== null ? (
          <div className="flex items-center gap-2">
            <span className="text-[11px] px-2 py-0.5 rounded-full bg-amber-500/15 text-amber-700">Building</span>
            <div role="progressbar" aria-valuenow={progress} className="h-1 flex-1 bg-muted rounded">
              <div className="h-1 bg-amber-500 rounded" style={{ width: `${progress}%` }} />
            </div>
            <span className="text-[11px] text-muted-foreground">{progress}%</span>
          </div>
        ) : ext.phase === "Error" ? (
          <span>
            <span className="text-[11px] px-2 py-0.5 rounded-full bg-red-500/15 text-red-700 mr-2">Error</span>
            <span className="text-[11px] text-red-700/80">{ext.message?.split("\n")[0]}</span>
          </span>
        ) : (
          <PhasePill phase={ext.phase} />
        )}
      </div>
      <div className="text-[11px] text-muted-foreground">{relTime(ext.updatedAt)}</div>
      <div className="text-right text-[11px] text-muted-foreground">View · ⋯</div>
    </div>
  );
}

function PhasePill({ phase }: { phase: string }) {
  const cls = phase === "Ready"
    ? "bg-emerald-500/15 text-emerald-700"
    : "bg-muted text-foreground/70";
  return <span className={`text-[11px] px-2 py-0.5 rounded-full ${cls}`}>{phase}</span>;
}

function sourceLabel(ext: Extension): string {
  switch (ext.sourceMode) {
    case "artifact": return `From artifact ${ext.sourceArtifactId ?? ""}${ext.extraSteps ? " + steps" : ""}`;
    case "dockerfile": return "From Dockerfile";
    case "image": return `From ${ext.sourceImage}`;
    default: return "";
  }
}

function relTime(iso?: string): string {
  if (!iso) return "";
  const ms = Date.now() - new Date(iso).getTime();
  if (ms < 60_000) return "just now";
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)} min ago`;
  if (ms < 86_400_000) return `${Math.floor(ms / 3_600_000)} h ago`;
  return new Date(iso).toLocaleDateString();
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/Extensions.test.tsx
```

Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add ui/src/pages/Extensions.tsx ui/src/pages/Extensions.test.tsx
git commit -m "ui(pages): Extensions list with empty/populated/Building/Error states"
```

---

## Task 5: ExtensionBuilder Step 1 (Source) — `From artifact` default

**Files:**
- Create: `ui/src/pages/ExtensionBuilder.tsx`
- Create: `ui/src/pages/ExtensionBuilder.test.tsx`

Three-mode source picker. `From artifact` is selected by default when at least one Ready artifact exists. The mode tabs reorder as `From artifact | Base image | Dockerfile` to match the spec's polish-pass ordering.

This task ships only Step 1 (Source) and the wizard frame; Steps 2 and 3 land in Tasks 6 and 8.

- [ ] **Step 1: Write the failing spec**

Create `ui/src/pages/ExtensionBuilder.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { ExtensionBuilder } from "./ExtensionBuilder";

function mockArtifacts(rows: unknown[]) {
  vi.spyOn(window, "fetch").mockImplementation(async (url) => {
    const u = String(url);
    if (u.includes("/api/v1/artifacts") && !u.includes("/extensions")) {
      return new Response(JSON.stringify(rows), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
  });
}

beforeEach(() => {
  window.localStorage.setItem("auroraboot_token", "tok");
  vi.restoreAllMocks();
});

describe("ExtensionBuilder — Step 1 (Source)", () => {
  it("defaults to 'From artifact' when a Ready artifact exists", async () => {
    mockArtifacts([{ id: "a-1", name: "edge-os-v4.1", phase: "Ready", arch: "amd64",
                     containerImage: "quay.io/myorg/edge-os:v4.1.0" }]);
    render(<MemoryRouter><ExtensionBuilder /></MemoryRouter>);
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /From artifact/i })).toHaveAttribute("data-active", "true");
    });
    expect(screen.getByText(/edge-os-v4\.1/)).toBeInTheDocument();
  });

  it("falls back to 'Base image' when no Ready artifacts exist", async () => {
    mockArtifacts([]);
    render(<MemoryRouter><ExtensionBuilder /></MemoryRouter>);
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /Base image/i })).toHaveAttribute("data-active", "true");
    });
  });

  it("switches to Dockerfile mode and shows a textarea", async () => {
    mockArtifacts([]);
    render(<MemoryRouter><ExtensionBuilder /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: /Dockerfile/i }));
    expect(screen.getByPlaceholderText(/FROM/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx
```

Expected: FAIL.

- [ ] **Step 3: Implement Step 1**

Create `ui/src/pages/ExtensionBuilder.tsx`:

```tsx
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { listArtifacts, type Artifact } from "@/api/artifacts";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { PageHeader } from "@/components/PageHeader";

type SourceMode = "artifact" | "image" | "dockerfile";

export function ExtensionBuilder() {
  const navigate = useNavigate();
  const [step, setStep] = useState(0);
  const [name, setName] = useState("");
  const [sourceMode, setSourceMode] = useState<SourceMode>("image"); // overridden by effect
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [selectedArtifactId, setSelectedArtifactId] = useState("");
  const [extraSteps, setExtraSteps] = useState("");
  const [baseImage, setBaseImage] = useState("");
  const [dockerfile, setDockerfile] = useState("");

  useEffect(() => {
    listArtifacts().then((rows) => {
      const ready = rows.filter((a) => a.phase === "Ready" && a.containerImage);
      setArtifacts(ready);
      if (ready.length > 0) {
        setSourceMode("artifact");
        setSelectedArtifactId(ready[0].id);
      }
    }).catch(() => {});
  }, []);

  // Step indicator + body. Steps 2 and 3 are added in Tasks 6 and 8.
  return (
    <div>
      <PageHeader title="Build extension" description="A sysext extends /usr; a confext extends /etc. Both ship as a single signed .raw." />

      <StepIndicator current={step} />

      {step === 0 && (
        <div className="grid gap-6">
          <div className="max-w-md grid gap-1.5">
            <Label>Name</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. tailscale-agent" />
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-sm">Image source</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-4">
              <div className="flex gap-2">
                <ModeButton active={sourceMode === "artifact"} onClick={() => setSourceMode("artifact")}>From artifact</ModeButton>
                <ModeButton active={sourceMode === "image"} onClick={() => setSourceMode("image")}>Base image</ModeButton>
                <ModeButton active={sourceMode === "dockerfile"} onClick={() => setSourceMode("dockerfile")}>Dockerfile</ModeButton>
              </div>

              {sourceMode === "artifact" && (
                <ArtifactPicker
                  artifacts={artifacts}
                  selectedId={selectedArtifactId}
                  onSelect={setSelectedArtifactId}
                  extraSteps={extraSteps}
                  onExtraStepsChange={setExtraSteps}
                />
              )}
              {sourceMode === "image" && (
                <div className="grid gap-1.5">
                  <Label>Base image</Label>
                  <Input value={baseImage} onChange={(e) => setBaseImage(e.target.value)} placeholder="e.g. ubuntu:24.04" />
                </div>
              )}
              {sourceMode === "dockerfile" && (
                <div className="grid gap-1.5">
                  <Label>Dockerfile</Label>
                  <Textarea
                    rows={8}
                    value={dockerfile}
                    onChange={(e) => setDockerfile(e.target.value)}
                    placeholder="FROM ubuntu:24.04\nRUN apt-get install -y curl"
                    className="font-mono text-sm"
                  />
                </div>
              )}
            </CardContent>
          </Card>

          <div className="flex justify-between">
            <Button variant="outline" onClick={() => navigate("/extensions")}>Cancel</Button>
            <Button onClick={() => setStep(1)}>Next →</Button>
          </div>
        </div>
      )}
      {step > 0 && <div className="text-muted-foreground p-4">Step 2 / 3 — wired up in Tasks 6 and 8.</div>}
    </div>
  );
}

function ModeButton({
  active, onClick, children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant={active ? "default" : "outline"}
      onClick={onClick}
      data-active={active}
    >
      {children}
    </Button>
  );
}

function ArtifactPicker({
  artifacts, selectedId, onSelect, extraSteps, onExtraStepsChange,
}: {
  artifacts: Artifact[];
  selectedId: string;
  onSelect: (id: string) => void;
  extraSteps: string;
  onExtraStepsChange: (s: string) => void;
}) {
  if (artifacts.length === 0) {
    return <p className="text-sm text-muted-foreground">No Ready artifacts yet — build one first.</p>;
  }
  const selected = artifacts.find((a) => a.id === selectedId);
  return (
    <div className="grid gap-3">
      <Label>Pick an existing artifact</Label>
      <select
        className="border rounded-md px-3 py-2 text-sm bg-background"
        value={selectedId}
        onChange={(e) => onSelect(e.target.value)}
      >
        {artifacts.map((a) => (
          <option key={a.id} value={a.id}>
            {a.name || a.id} — {a.arch} — {a.kairosVersion}
          </option>
        ))}
      </select>
      {selected && (
        <p className="text-[11px] text-muted-foreground font-mono">{selected.containerImage}</p>
      )}
      <div className="grid gap-1.5">
        <Label className="text-xs">Steps on top (optional)</Label>
        <Textarea
          rows={4}
          value={extraSteps}
          onChange={(e) => onExtraStepsChange(e.target.value)}
          placeholder="RUN curl -fsSL https://tailscale.com/install.sh | sh"
          className="font-mono text-xs"
        />
        <p className="text-[11px] text-muted-foreground">
          Wrapped in <code>FROM &lt;artifact-image&gt;</code> before the extractor runs. Lines starting with <code>FROM</code> are rejected.
        </p>
      </div>
    </div>
  );
}

function StepIndicator({ current }: { current: number }) {
  const steps = ["Source", "Configure", "Review"];
  return (
    <div className="flex gap-3 items-center text-sm mb-6">
      {steps.map((label, i) => (
        <span key={label} className={`inline-flex items-center gap-1.5 ${i === current ? "text-[#EE5007] font-semibold" : "text-muted-foreground"}`}>
          <span className={`h-6 w-6 rounded-full border inline-flex items-center justify-center text-xs ${i === current ? "bg-[#EE5007] text-white border-[#EE5007]" : ""}`}>
            {i + 1}
          </span>
          {label}
        </span>
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Verify the specs pass**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx
```

Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add ui/src/pages/ExtensionBuilder.tsx ui/src/pages/ExtensionBuilder.test.tsx
git commit -m "ui(pages): ExtensionBuilder Step 1 (Source) with From-artifact default"
```

---

## Task 6: ExtensionBuilder Step 2 (Configure)

**Files:**
- Modify: `ui/src/pages/ExtensionBuilder.tsx`
- Modify: `ui/src/pages/ExtensionBuilder.test.tsx`

Step 2 carries: Type (sysext/confext), Arch (amd64/arm64/riscv64), Version, Signing keyset picker, sysext-only `HierarchyChipInput` + `Reload services` toggle. Picking confext hides hierarchies and reload-services.

- [ ] **Step 1: Append failing specs**

Append to `ui/src/pages/ExtensionBuilder.test.tsx`:

```tsx
describe("ExtensionBuilder — Step 2 (Configure)", () => {
  async function gotoStep2() {
    mockArtifacts([{ id: "a-1", name: "x", phase: "Ready", arch: "amd64", containerImage: "img" }]);
    render(<MemoryRouter><ExtensionBuilder /></MemoryRouter>);
    await waitFor(() => screen.getByRole("button", { name: /From artifact/i }));
    fireEvent.change(screen.getByLabelText(/Name/i), { target: { value: "tailscale-agent" } });
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
  }

  it("shows the Hierarchies card when sysext type is selected", async () => {
    await gotoStep2();
    fireEvent.click(screen.getByRole("button", { name: /^sysext$/i }));
    expect(screen.getByText(/Hierarchies/i)).toBeInTheDocument();
    expect(screen.getByText(/Reload services after install/i)).toBeInTheDocument();
  });

  it("hides the Hierarchies card when confext is selected", async () => {
    await gotoStep2();
    fireEvent.click(screen.getByRole("button", { name: /^confext$/i }));
    expect(screen.queryByText(/Hierarchies/i)).not.toBeInTheDocument();
  });

  it("disables Next until the required fields are filled", async () => {
    await gotoStep2();
    const next = screen.getByRole("button", { name: /Next/i });
    expect(next).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /^sysext$/i }));
    fireEvent.click(screen.getByRole("button", { name: /^amd64$/i }));
    fireEvent.change(screen.getByLabelText(/Version/i), { target: { value: "v1.74.0" } });
    expect(next).toBeEnabled();
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx -t "Step 2"
```

Expected: FAIL.

- [ ] **Step 3: Implement Step 2**

Replace the `{step > 0 && …}` placeholder in `ExtensionBuilder.tsx` with:

```tsx
      {step === 1 && (
        <ConfigureStep
          type={type} onType={setType}
          arch={arch} onArch={setArch}
          version={version} onVersion={setVersion}
          signingKeySetId={signingKeySetId} onSigningKeySetId={setSigningKeySetId}
          hierarchies={hierarchies} onHierarchies={setHierarchies}
          serviceReload={serviceReload} onServiceReload={setServiceReload}
          onBack={() => setStep(0)} onNext={() => setStep(2)}
        />
      )}
```

Add the missing state declarations at the top of `ExtensionBuilder`:

```tsx
  const [type, setType] = useState<"sysext" | "confext">("sysext");
  const [arch, setArch] = useState<"amd64" | "arm64" | "riscv64">("amd64");
  const [version, setVersion] = useState("v1.0");
  const [signingKeySetId, setSigningKeySetId] = useState("");
  const [hierarchies, setHierarchies] = useState<string[]>([]);
  const [serviceReload, setServiceReload] = useState(false);
```

Append the `ConfigureStep` component:

```tsx
function ConfigureStep({
  type, onType, arch, onArch, version, onVersion,
  signingKeySetId, onSigningKeySetId,
  hierarchies, onHierarchies,
  serviceReload, onServiceReload,
  onBack, onNext,
}: {
  type: "sysext" | "confext"; onType: (t: "sysext" | "confext") => void;
  arch: "amd64" | "arm64" | "riscv64"; onArch: (a: "amd64" | "arm64" | "riscv64") => void;
  version: string; onVersion: (v: string) => void;
  signingKeySetId: string; onSigningKeySetId: (s: string) => void;
  hierarchies: string[]; onHierarchies: (h: string[]) => void;
  serviceReload: boolean; onServiceReload: (s: boolean) => void;
  onBack: () => void; onNext: () => void;
}) {
  const required = type && arch && version.trim();
  return (
    <div className="grid gap-6">
      <div className="grid md:grid-cols-2 gap-4">
        <Card>
          <CardHeader><CardTitle className="text-sm">Extension type</CardTitle></CardHeader>
          <CardContent className="grid grid-cols-2 gap-2">
            <TypeCard label="sysext" desc="Overlay on /usr (and additional paths)" active={type === "sysext"} onClick={() => onType("sysext")} />
            <TypeCard label="confext" desc="Overlay on /etc" active={type === "confext"} onClick={() => onType("confext")} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle className="text-sm">Architecture</CardTitle></CardHeader>
          <CardContent className="flex gap-2">
            {(["amd64", "arm64", "riscv64"] as const).map((a) => (
              <Button key={a} type="button" size="sm" variant={arch === a ? "default" : "outline"} onClick={() => onArch(a)}>
                {a}
              </Button>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle className="text-sm">Version</CardTitle></CardHeader>
          <CardContent>
            <Input value={version} onChange={(e) => onVersion(e.target.value)} placeholder="v1.0" />
            <p className="text-[11px] text-muted-foreground mt-1.5">Tracked server-side for staleness detection.</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle className="text-sm">Signing (optional)</CardTitle></CardHeader>
          <CardContent>
            <KeySetPicker selected={signingKeySetId} onSelect={onSigningKeySetId} />
          </CardContent>
        </Card>
      </div>

      {type === "sysext" && (
        <Card>
          <CardHeader><CardTitle className="text-sm">Hierarchies</CardTitle></CardHeader>
          <CardContent className="grid gap-4">
            <HierarchyChipInput
              value={hierarchies}
              onChange={onHierarchies}
              implicitRoot="/usr"
              quickAdds={["/opt", "/srv", "/var/lib"]}
            />
            <label className="flex gap-2 items-start text-sm">
              <input type="checkbox" checked={serviceReload} onChange={(e) => onServiceReload(e.target.checked)} className="mt-0.5" />
              <span>
                <span className="font-medium">Reload services after install</span>
                <span className="block text-xs text-muted-foreground">
                  Sets <code className="font-mono">EXTENSION_RELOAD_MANAGER=1</code>. Only needed when the extension ships systemd units.
                </span>
              </span>
            </label>
          </CardContent>
        </Card>
      )}

      <div className="flex justify-between">
        <Button variant="outline" onClick={onBack}>← Back</Button>
        <Button disabled={!required} onClick={onNext}>Next →</Button>
      </div>
    </div>
  );
}

function TypeCard({ label, desc, active, onClick }: { label: string; desc: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`text-left rounded-md border p-3 transition-colors ${active ? "border-[#EE5007] bg-[#EE5007]/5 ring-1 ring-[#EE5007]" : "hover:bg-muted/30"}`}
    >
      <div className="font-medium text-sm">{label}</div>
      <div className="text-xs text-muted-foreground mt-0.5">{desc}</div>
    </button>
  );
}

function KeySetPicker({ selected, onSelect }: { selected: string; onSelect: (s: string) => void }) {
  // Plan-2a hookup: SecureBootKeySetStore exposes ListWithSysextReady or similar.
  // For now we render a "(none)" + free-form id field; the real picker comes in
  // Plan 3b when the SecureBoot key store is consumed by the artifact-builder flow.
  return (
    <Input value={selected} onChange={(e) => onSelect(e.target.value)} placeholder="key-set id (optional)" />
  );
}
```

Add the `HierarchyChipInput` import at the top of the file:

```tsx
import { HierarchyChipInput } from "@/components/HierarchyChipInput";
```

- [ ] **Step 4: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx -t "Step 2"
```

Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add ui/src/pages/ExtensionBuilder.tsx ui/src/pages/ExtensionBuilder.test.tsx
git commit -m "ui(pages): ExtensionBuilder Step 2 (Configure) with sysext-only hierarchies"
```

---

## Task 7: ExtensionBuilder Step 3 (Review) + submit

**Files:**
- Modify: `ui/src/pages/ExtensionBuilder.tsx`
- Modify: `ui/src/pages/ExtensionBuilder.test.tsx`

Review step shows the summary + an equivalent-CLI panel. Build button calls `createExtension` and navigates to the detail page on success.

- [ ] **Step 1: Append failing spec**

Append to `ui/src/pages/ExtensionBuilder.test.tsx`:

```tsx
describe("ExtensionBuilder — Step 3 (Review) + submit", () => {
  it("POSTs the typed payload and navigates to the detail page", async () => {
    const calls: Array<[string, RequestInit | undefined]> = [];
    vi.spyOn(window, "fetch").mockImplementation(async (url, init) => {
      calls.push([String(url), init]);
      if (String(url).includes("/api/v1/artifacts") && !String(url).includes("/extensions")) {
        return new Response(`[{"id":"a-1","name":"x","phase":"Ready","arch":"amd64","containerImage":"img"}]`,
          { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (String(url) === "/api/v1/extensions") {
        return new Response(`{"id":"e-new","phase":"Pending"}`, { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
    });

    render(<MemoryRouter><ExtensionBuilder /></MemoryRouter>);
    await waitFor(() => screen.getByRole("button", { name: /From artifact/i }));
    fireEvent.change(screen.getByLabelText(/Name/i), { target: { value: "tailscale-agent" } });
    fireEvent.click(screen.getByRole("button", { name: /Next/i })); // step 1 -> 2
    fireEvent.click(screen.getByRole("button", { name: /^sysext$/i }));
    fireEvent.click(screen.getByRole("button", { name: /^amd64$/i }));
    fireEvent.change(screen.getByLabelText(/Version/i), { target: { value: "v1.74.0" } });
    fireEvent.click(screen.getByRole("button", { name: /Next/i })); // step 2 -> 3
    fireEvent.click(screen.getByRole("button", { name: /^Build$/i }));

    await waitFor(() => {
      const post = calls.find(([u, i]) => u === "/api/v1/extensions" && i?.method === "POST");
      expect(post).toBeDefined();
      const body = JSON.parse(post![1]!.body as string);
      expect(body.name).toBe("tailscale-agent");
      expect(body.type).toBe("sysext");
      expect(body.arch).toBe("amd64");
      expect(body.version).toBe("v1.74.0");
    });
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx -t "Step 3"
```

Expected: FAIL.

- [ ] **Step 3: Implement Step 3 + submit**

In `ExtensionBuilder.tsx`, replace any remaining `{step > 0 && …}` placeholder with:

```tsx
      {step === 2 && (
        <ReviewStep
          summary={{
            name, type, arch, version,
            sourceMode,
            artifactId: selectedArtifactId,
            baseImage,
            dockerfile,
            extraSteps,
            hierarchies,
            serviceReload,
            signingKeySetId,
          }}
          onBack={() => setStep(1)}
          onSubmit={async () => {
            const status = await createExtension({
              name, type, arch, version,
              source: {
                mode: sourceMode,
                artifactId: sourceMode === "artifact" ? selectedArtifactId : undefined,
                baseImage: sourceMode === "image" ? baseImage : undefined,
                dockerfile: sourceMode === "dockerfile" ? dockerfile : undefined,
                extraSteps: sourceMode === "artifact" && extraSteps ? extraSteps : undefined,
              },
              hierarchies: type === "sysext" ? hierarchies : undefined,
              serviceReload: type === "sysext" ? serviceReload : undefined,
              signingKeySetId: signingKeySetId || undefined,
            });
            navigate(`/extensions/${status.id}`);
          }}
        />
      )}
```

Add the `createExtension` import:

```tsx
import { createExtension } from "@/api/extensions";
```

Append `ReviewStep`:

```tsx
function ReviewStep({
  summary, onBack, onSubmit,
}: {
  summary: {
    name: string; type: string; arch: string; version: string;
    sourceMode: SourceMode; artifactId: string; baseImage: string;
    dockerfile: string; extraSteps: string;
    hierarchies: string[]; serviceReload: boolean; signingKeySetId: string;
  };
  onBack: () => void;
  onSubmit: () => Promise<void>;
}) {
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader><CardTitle className="text-sm">Summary</CardTitle></CardHeader>
        <CardContent>
          <dl className="grid grid-cols-[140px_1fr] gap-y-2 text-sm">
            <dt className="text-muted-foreground">Name</dt><dd>{summary.name}</dd>
            <dt className="text-muted-foreground">Type</dt><dd>{summary.type}</dd>
            <dt className="text-muted-foreground">Arch</dt><dd><code>{summary.arch}</code></dd>
            <dt className="text-muted-foreground">Version</dt><dd><code>{summary.version}</code></dd>
            <dt className="text-muted-foreground">Source</dt>
            <dd>
              {summary.sourceMode === "artifact" && `From artifact ${summary.artifactId}${summary.extraSteps ? " + steps" : ""}`}
              {summary.sourceMode === "image" && `From ${summary.baseImage}`}
              {summary.sourceMode === "dockerfile" && "From Dockerfile"}
            </dd>
            {summary.type === "sysext" && (
              <>
                <dt className="text-muted-foreground">Hierarchies</dt>
                <dd>{summary.hierarchies.length === 0 ? "/usr only" : ["/usr", ...summary.hierarchies].join(", ")}</dd>
              </>
            )}
            <dt className="text-muted-foreground">Signing</dt>
            <dd>{summary.signingKeySetId || "—"}</dd>
          </dl>
        </CardContent>
      </Card>

      {err && <p role="alert" className="text-sm text-red-600">{err}</p>}

      <div className="flex justify-between">
        <Button variant="outline" onClick={onBack}>← Back</Button>
        <Button
          disabled={submitting}
          onClick={async () => {
            setSubmitting(true);
            setErr(null);
            try { await onSubmit(); } catch (e) { setErr(String(e)); setSubmitting(false); }
          }}
        >
          {submitting ? "Building…" : "Build"}
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx -t "Step 3"
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/src/pages/ExtensionBuilder.tsx ui/src/pages/ExtensionBuilder.test.tsx
git commit -m "ui(pages): ExtensionBuilder Step 3 (Review) + submit"
```

---

## Task 8: `ExtensionDetail` page

**Files:**
- Create: `ui/src/pages/ExtensionDetail.tsx`
- Create: `ui/src/pages/ExtensionDetail.test.tsx`

Phase + logs strip, download link, "Install on…" action button, build metadata sidebar.

- [ ] **Step 1: Write the failing spec**

Create `ui/src/pages/ExtensionDetail.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { ExtensionDetail } from "./ExtensionDetail";

function mockOne(rec: Partial<{ phase: string; rawFilename: string; logs: string; arch: string }>) {
  vi.spyOn(window, "fetch").mockImplementation(async (url) => {
    const u = String(url);
    if (u.endsWith("/api/v1/extensions/e-1")) {
      return new Response(JSON.stringify({
        id: "e-1", name: "ts", type: "sysext", phase: rec.phase ?? "Ready",
        arch: rec.arch ?? "amd64", version: "v1.74.0",
        sourceMode: "image", rawFilename: rec.rawFilename ?? "ts.sysext.raw",
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    if (u.endsWith("/logs")) {
      return new Response(rec.logs ?? "log line 1", { status: 200, headers: { "Content-Type": "text/plain" } });
    }
    return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
  });
}

beforeEach(() => {
  window.localStorage.setItem("auroraboot_token", "tok");
  vi.restoreAllMocks();
});

describe("ExtensionDetail", () => {
  it("renders the extension's metadata and a download link when Ready", async () => {
    mockOne({ phase: "Ready", rawFilename: "ts.sysext.raw" });
    render(
      <MemoryRouter initialEntries={["/extensions/e-1"]}>
        <Routes><Route path="/extensions/:id" element={<ExtensionDetail />} /></Routes>
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText("ts")).toBeInTheDocument());
    const link = screen.getByRole("link", { name: /Download/i });
    expect(link.getAttribute("href")).toContain("/extensions/e-1/download/ts.sysext.raw");
  });

  it("hides the Download link while Building", async () => {
    mockOne({ phase: "Building" });
    render(
      <MemoryRouter initialEntries={["/extensions/e-1"]}>
        <Routes><Route path="/extensions/:id" element={<ExtensionDetail />} /></Routes>
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText("Building")).toBeInTheDocument());
    expect(screen.queryByRole("link", { name: /Download/i })).not.toBeInTheDocument();
  });

  it("displays the log text", async () => {
    mockOne({ phase: "Ready", logs: "step 1\nstep 2" });
    render(
      <MemoryRouter initialEntries={["/extensions/e-1"]}>
        <Routes><Route path="/extensions/:id" element={<ExtensionDetail />} /></Routes>
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/step 1/)).toBeInTheDocument());
    expect(screen.getByText(/step 2/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionDetail.test.tsx
```

Expected: FAIL.

- [ ] **Step 3: Implement**

Create `ui/src/pages/ExtensionDetail.tsx`:

```tsx
import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { getExtension, getExtensionLogs, extensionDownloadUrl, type Extension } from "@/api/extensions";
import { PageHeader } from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { ExtensionTypeChip } from "@/components/ExtensionTypeChip";

export function ExtensionDetail() {
  const { id = "" } = useParams<{ id: string }>();
  const [ext, setExt] = useState<Extension | null>(null);
  const [logs, setLogs] = useState<string>("");
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!id) return;
    getExtension(id).then(setExt).catch((e) => setErr(String(e)));
    getExtensionLogs(id).then(setLogs).catch(() => {});
  }, [id]);

  if (err) return <div className="text-red-600 p-4">{err}</div>;
  if (!ext) return <div className="p-4 text-muted-foreground text-sm">Loading…</div>;

  return (
    <div>
      <PageHeader title={ext.name}>
        {ext.phase === "Ready" && ext.rawFilename && (
          <a
            href={extensionDownloadUrl(ext.id, ext.rawFilename)}
            className="inline-flex items-center px-3 h-9 rounded-md border text-sm hover:bg-muted/40"
          >
            Download .raw
          </a>
        )}
        <Button disabled={ext.phase !== "Ready"}>Install on group…</Button>
      </PageHeader>

      <div className="grid md:grid-cols-[1fr_280px] gap-6">
        <section>
          <h2 className="text-sm font-semibold mb-2">Logs</h2>
          <pre className="text-xs bg-muted/40 rounded-md p-3 max-h-[480px] overflow-auto whitespace-pre-wrap">
            {logs || "(no logs yet)"}
          </pre>
        </section>

        <aside className="text-sm">
          <h2 className="font-semibold mb-2">Build</h2>
          <dl className="grid grid-cols-[100px_1fr] gap-y-1.5">
            <dt className="text-muted-foreground">Type</dt><dd><ExtensionTypeChip type={ext.type} /></dd>
            <dt className="text-muted-foreground">Phase</dt><dd>{ext.phase}</dd>
            <dt className="text-muted-foreground">Arch</dt><dd><code className="text-xs">{ext.arch}</code></dd>
            <dt className="text-muted-foreground">Version</dt><dd><code className="text-xs">{ext.version}</code></dd>
            <dt className="text-muted-foreground">Source</dt><dd className="text-xs">{ext.sourceMode}</dd>
            {ext.containerImage && (<><dt className="text-muted-foreground">Image</dt><dd className="text-[11px] font-mono break-all">{ext.containerImage}</dd></>)}
            {ext.signingKeySetId && (<><dt className="text-muted-foreground">Signing</dt><dd className="text-xs">{ext.signingKeySetId}</dd></>)}
            {ext.hierarchies && ext.hierarchies.length > 0 && (<><dt className="text-muted-foreground">Hierarchies</dt><dd className="text-xs">{["/usr", ...ext.hierarchies].join(", ")}</dd></>)}
          </dl>
        </aside>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionDetail.test.tsx
```

Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add ui/src/pages/ExtensionDetail.tsx ui/src/pages/ExtensionDetail.test.tsx
git commit -m "ui(pages): ExtensionDetail with download link + log view"
```

---

## Task 9: Nav entry + route registration

**Files:**
- Modify: `ui/src/App.tsx`
- Modify: `ui/src/components/Layout.tsx`

Routes for `/extensions`, `/extensions/new`, `/extensions/:id`; new sidebar entry between Artifacts and Nodes.

- [ ] **Step 1: Write the failing spec**

Extend `ui/src/test/smoke.test.tsx` (or add a sibling) — append:

```tsx
import { Extensions } from "@/pages/Extensions";

it("registers /extensions in the route table", async () => {
  window.localStorage.setItem("auroraboot_token", "tok");
  vi.spyOn(window, "fetch").mockResolvedValue(
    new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } }),
  );
  render(
    <MemoryRouter initialEntries={["/extensions"]}>
      <App />
    </MemoryRouter>,
  );
  expect(await screen.findByText(/No extensions yet/i)).toBeInTheDocument();
});

it("renders Extensions nav entry between Artifacts and Nodes", () => {
  window.localStorage.setItem("auroraboot_token", "tok");
  render(
    <MemoryRouter initialEntries={["/"]}>
      <App />
    </MemoryRouter>,
  );
  const labels = screen.getAllByRole("link").map((el) => el.textContent ?? "");
  const artifactsIdx = labels.findIndex((l) => l.includes("Artifacts"));
  const extensionsIdx = labels.findIndex((l) => l.includes("Extensions"));
  const nodesIdx = labels.findIndex((l) => l.includes("Nodes"));
  expect(extensionsIdx).toBeGreaterThan(-1);
  expect(extensionsIdx).toBeGreaterThan(artifactsIdx);
  expect(extensionsIdx).toBeLessThan(nodesIdx);
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/test/smoke.test.tsx
```

Expected: FAIL — routes not registered, nav entry missing.

- [ ] **Step 3: Add the routes**

In `ui/src/App.tsx`, add the imports + routes between Artifacts and Nodes:

```tsx
import { Extensions } from "./pages/Extensions";
import { ExtensionBuilder } from "./pages/ExtensionBuilder";
import { ExtensionDetail } from "./pages/ExtensionDetail";
// ...
        <Route path="extensions" element={<Extensions />} />
        <Route path="extensions/new" element={<ExtensionBuilder />} />
        <Route path="extensions/:id" element={<ExtensionDetail />} />
```

- [ ] **Step 4: Add the nav entry**

In `ui/src/components/Layout.tsx`, find the nav-items array (around the line with `{ to: "/artifacts", ... }`) and insert immediately after it:

```tsx
{ to: "/extensions", icon: Layers, label: "Extensions" },
```

Add `Layers` to the lucide-react import at the top of the file.

- [ ] **Step 5: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test
```

Expected: every spec across the UI green.

- [ ] **Step 6: Commit**

```bash
git add ui/src/App.tsx ui/src/components/Layout.tsx ui/src/test/smoke.test.tsx
git commit -m "ui: register /extensions routes + sidebar nav entry"
```

---

## Self-review checks

- **Spec coverage:** API client (Task 1), shared chip + hierarchies components (Tasks 2–3), list page with empty + populated + Building + Error states (Task 4), 3-step builder including From-artifact default, sysext-only hierarchies, and submit (Tasks 5–7), detail page with download (Task 8), routes + nav (Task 9). Together these match every standalone-Extensions surface from the spec.
- **Type consistency:** `Extension.sourceMode` and `CreateExtensionInput.source.mode` both use the same `"artifact" | "image" | "dockerfile"` union.
- **Placeholder scan:** `KeySetPicker` is intentionally minimal here — the production picker (with the `db.key`/`db.pem` pre-flight) lands in Plan 3b alongside the SecureBoot keys integration.

## What lands at the end of this plan

- `npm run test` is green across the UI.
- Operators can navigate to `/extensions`, see an empty state, click "Build extension", complete the 3-step wizard, and land on a detail page.
- No fleet-deploy surface yet — that's Plan 3b (InstallExtensionDialog + ArtifactBuilder integration + CommandDialog upgrade-with-extensions).

## Out of scope here

- `InstallExtensionDialog` (manual flow) → Plan 3b.
- ArtifactBuilder hierarchies disclosure + bundled-extensions card + `extension` allowed-command row → Plan 3b.
- `CommandDialog` upgrade-with-extensions section → Plan 3c.
- `NodeDetail` "Installed extensions" section → Plan 3c.
- WebSocket log streaming during a live build → Plan 3c (mirrors the artifact pattern).
