# Extensions — Plan 3c of 3: Bundled upgrade dialog + cross-check + NodeDetail + final verify

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close out the UI. CommandDialog's `upgrade` action grows an "Also push these extensions" section driven by `POST /artifacts/:id/bundle-resolve`. ExtensionBuilder's "From artifact" picker grows a green/amber cross-check strip against the artifact's declared hierarchies. ExtensionDetail and NodeDetail show what's installed where. The whole UI suite runs green and a smoke test exercises the live binary.

**Architecture:** No new top-level pages. Two new API helpers (`resolveBundle`, `listExtensionsForNode`). `CommandDialog` learns about extensions via a new section rendered only when `command === "upgrade"` and an artifact source is selected. The cross-check strip is a pure UI component fed by the artifact's `extensionHierarchies` field. `NodeDetail` gains a section that maps `node_extensions` rows into a small table.

**Tech Stack:** React 19, react-router 7, vitest + RTL.

**Repository:** `/home/mudler/_git/AuroraBoot`

**Prerequisites:** Plans 2a–2e + Plan 3a + Plan 3b merged.

---

## Reference — read these before starting

| File | What it does |
|---|---|
| `ui/src/components/CommandDialog.tsx` (whole file) | Existing artifact upgrade dialog — extended in Tasks 1–2. |
| `ui/src/api/artifacts.ts` | Existing `Artifact` type — `extensionHierarchies` added in Plan 2d Task 3; add the resolve-bundle call here. |
| `ui/src/api/extensions.ts` | Plan 3a Task 1 — extend with `listExtensionsForNode` and `listNodesForExtension`. |
| `ui/src/pages/NodeDetail.tsx` | Add the "Installed extensions" section. |
| `ui/src/pages/ExtensionDetail.tsx` | Plan 3a Task 8 — extend with "Used by" + "Installed on" sections. |
| `ui/src/pages/ExtensionBuilder.tsx` | Plan 3a Task 5 — add the cross-check strip inside the `ArtifactPicker`. |

---

## Task 1: `CommandDialog` — "Also push these extensions" section

**Files:**
- Modify: `ui/src/api/artifacts.ts` (add `resolveBundle`)
- Modify: `ui/src/components/CommandDialog.tsx`
- Modify: `ui/src/components/CommandDialog.test.tsx` (create if absent — the file may already exist via the artifact test suite)

When the operator opens an upgrade dialog and picks "Artifact" as the source, the dialog calls `POST /api/v1/artifacts/:id/bundle-resolve` to get a list of `{name, type, version, source}` entries. Each is pre-selected; the operator can untick to drop one. The list is JSON-encoded into `args.extensions` before the command is sent.

- [ ] **Step 1: Add `resolveBundle` to the artifact API**

In `ui/src/api/artifacts.ts`:

```ts
export interface ResolvedBundleEntry {
  name: string;
  type: "sysext" | "confext";
  version: string;
  source: string;
}

export function resolveBundle(artifactId: string): Promise<ResolvedBundleEntry[]> {
  return apiFetch<ResolvedBundleEntry[]>(`/api/v1/artifacts/${artifactId}/bundle-resolve`, {
    method: "POST",
  });
}
```

- [ ] **Step 2: Write the failing spec**

Create `ui/src/components/CommandDialog.test.tsx` (or append to it):

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { CommandDialog } from "./CommandDialog";

beforeEach(() => {
  window.localStorage.setItem("auroraboot_token", "tok");
  vi.restoreAllMocks();
});

function mockApis({ artifacts = [], bundle = [] }: { artifacts?: unknown[]; bundle?: unknown[] }) {
  vi.spyOn(window, "fetch").mockImplementation(async (url, init) => {
    const u = String(url);
    if (u.endsWith("/api/v1/artifacts")) {
      return new Response(JSON.stringify(artifacts), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    if (u.match(/\/api\/v1\/artifacts\/[^/]+\/bundle-resolve$/) && init?.method === "POST") {
      return new Response(JSON.stringify(bundle), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
  });
}

describe("CommandDialog — upgrade with bundled extensions", () => {
  it("shows the bundle section after picking an artifact with a non-empty bundle", async () => {
    mockApis({
      artifacts: [{ id: "a-1", name: "edge-os", phase: "Ready", arch: "amd64", containerImage: "quay.io/edge:v1" }],
      bundle: [
        { name: "tailscale-agent", type: "sysext", version: "v1.74.0", source: "https://x/y" },
        { name: "fluent-bit-config", type: "confext", version: "2026.05.20", source: "https://x/z" },
      ],
    });
    const onSubmit = vi.fn();
    render(<CommandDialog open onOpenChange={() => {}} onSubmit={onSubmit} defaultCommand="upgrade" />);

    // Wait for the upgrade source picker to settle.
    await waitFor(() => screen.getByText(/Upgrade/i));
    // Switch to "Artifact" source mode and select.
    fireEvent.click(screen.getByRole("radio", { name: /Artifact/i }));
    await waitFor(() => screen.getByText("edge-os"));
    fireEvent.click(screen.getByText("edge-os"));

    await waitFor(() => screen.getByText(/Also push these extensions/i));
    expect(screen.getByText(/tailscale-agent/)).toBeInTheDocument();
    expect(screen.getByText(/fluent-bit-config/)).toBeInTheDocument();
    // Both bundled entries are pre-selected.
    expect((screen.getByLabelText(/include tailscale-agent/i) as HTMLInputElement).checked).toBe(true);
    expect((screen.getByLabelText(/include fluent-bit-config/i) as HTMLInputElement).checked).toBe(true);
  });

  it("submits with JSON-encoded extensions in args", async () => {
    mockApis({
      artifacts: [{ id: "a-1", name: "edge-os", phase: "Ready", arch: "amd64", containerImage: "img" }],
      bundle: [{ name: "tailscale-agent", type: "sysext", version: "v1.74.0", source: "https://x/y" }],
    });
    const onSubmit = vi.fn();
    render(<CommandDialog open onOpenChange={() => {}} onSubmit={onSubmit} defaultCommand="upgrade" />);
    fireEvent.click(screen.getByRole("radio", { name: /Artifact/i }));
    await waitFor(() => screen.getByText("edge-os"));
    fireEvent.click(screen.getByText("edge-os"));
    await waitFor(() => screen.getByText(/tailscale-agent/));
    fireEvent.click(screen.getByRole("button", { name: /Send upgrade/i }));

    expect(onSubmit).toHaveBeenCalled();
    const [cmd, args] = onSubmit.mock.calls[0];
    expect(cmd).toBe("upgrade");
    expect(args.source).toBe("artifact:a-1");
    const ext = JSON.parse(String(args.extensions));
    expect(ext).toHaveLength(1);
    expect(ext[0].name).toBe("tailscale-agent");
    expect(ext[0].source).toBe("https://x/y");
  });

  it("omits args.extensions when the operator unticks every entry", async () => {
    mockApis({
      artifacts: [{ id: "a-1", name: "edge-os", phase: "Ready", arch: "amd64", containerImage: "img" }],
      bundle: [{ name: "tailscale-agent", type: "sysext", version: "v1.74.0", source: "https://x/y" }],
    });
    const onSubmit = vi.fn();
    render(<CommandDialog open onOpenChange={() => {}} onSubmit={onSubmit} defaultCommand="upgrade" />);
    fireEvent.click(screen.getByRole("radio", { name: /Artifact/i }));
    await waitFor(() => screen.getByText("edge-os"));
    fireEvent.click(screen.getByText("edge-os"));
    await waitFor(() => screen.getByLabelText(/include tailscale-agent/i));
    fireEvent.click(screen.getByLabelText(/include tailscale-agent/i)); // untick
    fireEvent.click(screen.getByRole("button", { name: /Send upgrade/i }));

    const [, args] = onSubmit.mock.calls[0];
    expect(args.extensions).toBeUndefined();
  });
});
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/components/CommandDialog.test.tsx
```

Expected: FAIL.

- [ ] **Step 4: Wire bundle resolution into `CommandDialog`**

In `ui/src/components/CommandDialog.tsx`:

1. Add a state slot near the existing artifact-pick state:

   ```tsx
   import { resolveBundle, type ResolvedBundleEntry } from "@/api/artifacts";

   const [bundle, setBundle] = useState<ResolvedBundleEntry[]>([]);
   const [bundlePicks, setBundlePicks] = useState<Record<string, boolean>>({});
   ```

2. When the operator selects an artifact (the existing `setSelectedArtifactId` handler is the trigger), fire a resolve and pre-select every entry:

   ```tsx
   useEffect(() => {
     if (!selectedArtifactId) {
       setBundle([]);
       setBundlePicks({});
       return;
     }
     resolveBundle(selectedArtifactId)
       .then((rows) => {
         setBundle(rows);
         setBundlePicks(Object.fromEntries(rows.map((r) => [`${r.type}/${r.name}`, true])));
       })
       .catch(() => { setBundle([]); setBundlePicks({}); });
   }, [selectedArtifactId]);
   ```

3. Render the bundle section when `command === "upgrade"`, `upgradeSourceMode === "artifact"`, and `bundle.length > 0`. Place it directly below the artifact picker:

   ```tsx
   {isUpgrade && upgradeSourceMode === "artifact" && bundle.length > 0 && (
     <div className="mt-4">
       <div className="text-xs text-muted-foreground mb-1.5">Also push these extensions</div>
       <div className="grid gap-1">
         {bundle.map((entry) => {
           const k = `${entry.type}/${entry.name}`;
           const checked = bundlePicks[k] ?? false;
           return (
             <label key={k} className="flex items-center gap-2 text-sm px-2 py-1.5 rounded-md border">
               <input
                 type="checkbox"
                 aria-label={`include ${entry.name}`}
                 checked={checked}
                 onChange={(e) => setBundlePicks((prev) => ({ ...prev, [k]: e.target.checked }))}
               />
               <span className="font-medium">{entry.name}</span>
               <span className="text-xs text-muted-foreground">{entry.type} · {entry.version}</span>
             </label>
           );
         })}
       </div>
     </div>
   )}
   ```

4. In `handleSubmit`, after `args.source = "artifact:" + selectedArtifactId;` set the `extensions` arg only when at least one bundle entry is ticked:

   ```tsx
   if (isUpgrade && upgradeSourceMode === "artifact") {
     const picked = bundle.filter((e) => bundlePicks[`${e.type}/${e.name}`]);
     if (picked.length > 0) {
       args.extensions = JSON.stringify(picked);
     }
   }
   ```

- [ ] **Step 5: Verify the specs pass**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/components/CommandDialog.test.tsx
```

Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add ui/src/api/artifacts.ts ui/src/components/CommandDialog.tsx ui/src/components/CommandDialog.test.tsx
git commit -m "ui(CommandDialog): bundled extensions section for artifact upgrade"
```

---

## Task 2: ExtensionBuilder cross-check strip on the "From artifact" picker

**Files:**
- Modify: `ui/src/pages/ExtensionBuilder.tsx`
- Modify: `ui/src/pages/ExtensionBuilder.test.tsx`
- Modify: `ui/src/api/artifacts.ts` (extend `Artifact` with `extensionHierarchies`)

When the operator picks "From artifact" and the user's selected hierarchies are non-empty, render a strip showing each hierarchy with a `✓` (declared by the artifact) or `⚠` (missing). Source: the artifact's stored `extensionHierarchies.sysext` list. Colorblind-safe (glyph + color).

- [ ] **Step 1: Extend the artifact API type**

In `ui/src/api/artifacts.ts`, append to `Artifact`:

```ts
  extensionHierarchies?: { sysext: string[]; confext: string[] };
```

- [ ] **Step 2: Write the failing spec**

Append to `ui/src/pages/ExtensionBuilder.test.tsx`:

```tsx
describe("ExtensionBuilder — From artifact cross-check", () => {
  function mockReady(artifact: { extensionHierarchies?: { sysext: string[]; confext: string[] } }) {
    vi.spyOn(window, "fetch").mockImplementation(async (url) => {
      if (String(url).endsWith("/api/v1/artifacts")) {
        return new Response(JSON.stringify([{
          id: "a-1", name: "edge-os", phase: "Ready", arch: "amd64",
          containerImage: "img", ...artifact,
        }]), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
    });
  }

  it("renders ✓ for hierarchies declared by the artifact", async () => {
    mockReady({ extensionHierarchies: { sysext: ["/opt", "/srv"], confext: [] } });
    render(<MemoryRouter><ExtensionBuilder /></MemoryRouter>);
    await waitFor(() => screen.getByText(/From artifact/i));
    // Walk to Step 2 + add a hierarchy.
    fireEvent.change(screen.getByLabelText(/Name/i), { target: { value: "ts" } });
    fireEvent.click(screen.getByRole("button", { name: /^Next$/i }));
    await waitFor(() => screen.getByText(/Hierarchies/));
    fireEvent.click(screen.getByRole("button", { name: /Add \/opt/i }));
    // Back to Source step.
    fireEvent.click(screen.getByRole("button", { name: /Back/i }));
    await waitFor(() => screen.getByText(/Artifact declares hierarchies/i));
    expect(screen.getByText("/opt").parentElement?.textContent).toContain("✓");
  });

  it("renders ⚠ for hierarchies the artifact does NOT declare", async () => {
    mockReady({ extensionHierarchies: { sysext: ["/opt"], confext: [] } });
    render(<MemoryRouter><ExtensionBuilder /></MemoryRouter>);
    await waitFor(() => screen.getByText(/From artifact/i));
    fireEvent.change(screen.getByLabelText(/Name/i), { target: { value: "ts" } });
    fireEvent.click(screen.getByRole("button", { name: /^Next$/i }));
    await waitFor(() => screen.getByText(/Hierarchies/));
    // Add /srv via free-form input — the artifact only declares /opt.
    const chipInput = screen.getAllByRole("textbox").find((el) => (el as HTMLInputElement).placeholder?.includes("Add path"));
    fireEvent.change(chipInput!, { target: { value: "/srv" } });
    fireEvent.keyDown(chipInput!, { key: "Enter" });
    fireEvent.click(screen.getByRole("button", { name: /Back/i }));
    await waitFor(() => screen.getByText(/Artifact declares hierarchies/i));
    expect(screen.getByText("/srv").parentElement?.textContent).toContain("⚠");
  });
});
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx -t "cross-check"
```

Expected: FAIL.

- [ ] **Step 4: Add the strip**

In `ExtensionBuilder.tsx`, inside `ArtifactPicker`, render the strip below the `<select>`+container-image display when the parent component passes the current hierarchies:

```tsx
function ArtifactPicker({
  artifacts, selectedId, onSelect, extraSteps, onExtraStepsChange,
  userHierarchies,
}: {
  artifacts: Artifact[];
  selectedId: string;
  onSelect: (id: string) => void;
  extraSteps: string;
  onExtraStepsChange: (s: string) => void;
  userHierarchies: string[];   // new
}) {
  // ... existing body ...

  const selected = artifacts.find((a) => a.id === selectedId);
  const declared = selected?.extensionHierarchies?.sysext ?? [];
  return (
    <div className="grid gap-3">
      {/* existing dropdown + container image */}
      {userHierarchies.length > 0 && selected && (
        <div className="text-xs flex flex-wrap items-center gap-1.5 mt-1 px-2.5 py-1.5 rounded-md border bg-emerald-500/5 border-emerald-500/30">
          <span className="text-muted-foreground">Artifact declares hierarchies:</span>
          {userHierarchies.map((p) => {
            const ok = declared.includes(p) || p === "/usr";
            return (
              <span key={p} className={`px-2 py-0.5 rounded-full border ${ok ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-700" : "border-amber-500/40 bg-amber-500/10 text-amber-700"}`}>
                <span className="mr-1">{ok ? "✓" : "⚠"}</span>
                <code className="font-mono">{p}</code>
              </span>
            );
          })}
          {userHierarchies.every((p) => declared.includes(p)) ? (
            <span className="ml-auto text-emerald-700">Supported.</span>
          ) : (
            <span className="ml-auto text-amber-700">
              Some hierarchies aren&apos;t declared by this artifact — bake them in or pick another base.
            </span>
          )}
        </div>
      )}
      {/* existing extra-steps textarea */}
    </div>
  );
}
```

Then in the parent `ExtensionBuilder`, pass `hierarchies` into `<ArtifactPicker userHierarchies={hierarchies} … />`.

- [ ] **Step 5: Verify the specs pass**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx -t "cross-check"
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add ui/src/pages/ExtensionBuilder.tsx ui/src/pages/ExtensionBuilder.test.tsx ui/src/api/artifacts.ts
git commit -m "ui(ExtensionBuilder): cross-check strip against artifact's extensionHierarchies"
```

---

## Task 3: NodeDetail — "Installed extensions" section

**Files:**
- Modify: `ui/src/api/extensions.ts` (add `listExtensionsForNode`)
- Modify: `ui/src/pages/NodeDetail.tsx`
- Modify: `ui/src/pages/NodeDetail.test.tsx` (create if absent)

A small table of `(Name, Type chip, Version, Boot scope, Installed at)` rows for the node. Each row has a "Remove" button that fires the manual `extension` remove action against this single node.

- [ ] **Step 1: Add the API helper**

In `ui/src/api/extensions.ts`:

```ts
export interface NodeExtensionRow {
  nodeId: string;
  name: string;
  type: ExtensionType;
  bootState: string;
  version: string;
  installedAt: string;
  extensionId?: string;
}

export function listExtensionsForNode(nodeID: string): Promise<NodeExtensionRow[]> {
  return apiFetch<NodeExtensionRow[]>(`/api/v1/nodes/${nodeID}/extensions`);
}
```

- [ ] **Step 2: Write the failing spec**

Create `ui/src/pages/NodeDetail.test.tsx` (or append to an existing one):

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { NodeDetail } from "./NodeDetail";

beforeEach(() => {
  window.localStorage.setItem("auroraboot_token", "tok");
  vi.restoreAllMocks();
  vi.spyOn(window, "fetch").mockImplementation(async (url) => {
    const u = String(url);
    if (u.match(/\/api\/v1\/nodes\/[^/]+$/)) {
      return new Response(JSON.stringify({ id: "n-1", hostname: "edge-eu-01", phase: "Online", labels: {}, osRelease: null, agentVersion: "v0.1", lastHeartbeat: null, createdAt: "", updatedAt: "" }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    if (u.endsWith("/api/v1/nodes/n-1/extensions")) {
      return new Response(JSON.stringify([
        { nodeId: "n-1", name: "tailscale-agent", type: "sysext", bootState: "common", version: "v1.74.0", installedAt: "2026-05-26T10:00:00Z" },
        { nodeId: "n-1", name: "fluent-bit-config", type: "confext", bootState: "common", version: "2026.05.20", installedAt: "" },
      ]), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
  });
});

describe("NodeDetail — Installed extensions section", () => {
  it("renders one row per node_extension", async () => {
    render(
      <MemoryRouter initialEntries={["/nodes/n-1"]}>
        <Routes><Route path="/nodes/:id" element={<NodeDetail />} /></Routes>
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText("tailscale-agent")).toBeInTheDocument());
    expect(screen.getByText("fluent-bit-config")).toBeInTheDocument();
    expect(screen.getByText("v1.74.0")).toBeInTheDocument();
    expect(screen.getByText("common")).toBeInTheDocument();
  });

  it("shows the empty state when the node has no extensions", async () => {
    vi.spyOn(window, "fetch").mockImplementation(async (url) => {
      if (String(url).match(/\/api\/v1\/nodes\/[^/]+$/)) {
        return new Response(JSON.stringify({ id: "n-1", hostname: "e", phase: "Online", labels: {}, osRelease: null, agentVersion: "", lastHeartbeat: null, createdAt: "", updatedAt: "" }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
    });
    render(
      <MemoryRouter initialEntries={["/nodes/n-1"]}>
        <Routes><Route path="/nodes/:id" element={<NodeDetail />} /></Routes>
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/No extensions installed/i)).toBeInTheDocument());
  });
});
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/NodeDetail.test.tsx
```

Expected: FAIL.

- [ ] **Step 4: Add the section**

In `ui/src/pages/NodeDetail.tsx`:

1. State + effect alongside the existing data fetches:

   ```tsx
   import { listExtensionsForNode, type NodeExtensionRow } from "@/api/extensions";
   import { ExtensionTypeChip } from "@/components/ExtensionTypeChip";

   const [exts, setExts] = useState<NodeExtensionRow[]>([]);
   useEffect(() => {
     if (!id) return;
     listExtensionsForNode(id).then(setExts).catch(() => setExts([]));
   }, [id]);
   ```

2. Render below the existing body content:

   ```tsx
   <section className="mt-6">
     <h2 className="text-sm font-semibold mb-2">Installed extensions</h2>
     {exts.length === 0 ? (
       <p className="text-sm text-muted-foreground">No extensions installed on this node.</p>
     ) : (
       <div className="rounded-md border overflow-hidden text-sm">
         <div className="grid grid-cols-[1.4fr_.8fr_.8fr_.8fr_1fr_.6fr] px-3 py-2 bg-muted/50 text-[11px] uppercase tracking-wider text-muted-foreground">
           <div>Name</div><div>Type</div><div>Version</div><div>Boot scope</div><div>Installed</div><div className="text-right">Actions</div>
         </div>
         {exts.map((row) => (
           <div key={`${row.type}/${row.name}/${row.bootState}`} className="grid grid-cols-[1.4fr_.8fr_.8fr_.8fr_1fr_.6fr] px-3 py-2.5 items-center border-t">
             <div className="font-medium">{row.name}</div>
             <div><ExtensionTypeChip type={row.type} /></div>
             <div><code className="text-[11px]">{row.version}</code></div>
             <div className="text-xs">{row.bootState}</div>
             <div className="text-[11px] text-muted-foreground">{row.installedAt ? new Date(row.installedAt).toLocaleString() : "—"}</div>
             <div className="text-right text-[11px] text-muted-foreground">⋯</div>
           </div>
         ))}
       </div>
     )}
   </section>
   ```

(Removing an extension from a node is left as a follow-up — the existing manual `InstallExtensionDialog` already supports `action="remove"` and reaches the same agent endpoint, so operators can use it from the extension's detail page until a per-row action lands.)

- [ ] **Step 5: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/NodeDetail.test.tsx
```

Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add ui/src/api/extensions.ts ui/src/pages/NodeDetail.tsx ui/src/pages/NodeDetail.test.tsx
git commit -m "ui(NodeDetail): Installed extensions section"
```

---

## Task 4: ExtensionDetail — "Used by" + "Installed on" sections

**Files:**
- Modify: `ui/src/api/extensions.ts` (add `listNodesForExtension`)
- Modify: `ui/src/pages/ExtensionDetail.tsx`
- Modify: `ui/src/pages/ExtensionDetail.test.tsx`

Two small sections on the detail page: which artifacts bundle this extension, and which nodes currently have it. The bundles list is best-effort (404 → empty); the nodes list reads `/api/v1/extensions/:id/nodes`.

- [ ] **Step 1: Add the API helper**

In `ui/src/api/extensions.ts`:

```ts
export function listNodesForExtension(id: string): Promise<NodeExtensionRow[]> {
  return apiFetch<NodeExtensionRow[]>(`/api/v1/extensions/${id}/nodes`);
}
```

- [ ] **Step 2: Append the failing spec**

Append to `ui/src/pages/ExtensionDetail.test.tsx`:

```tsx
it("lists nodes that have the extension installed", async () => {
  vi.spyOn(window, "fetch").mockImplementation(async (url) => {
    const u = String(url);
    if (u.endsWith("/api/v1/extensions/e-1")) {
      return new Response(JSON.stringify({
        id: "e-1", name: "ts", type: "sysext", phase: "Ready",
        arch: "amd64", version: "v1.74.0", sourceMode: "image",
        rawFilename: "ts.sysext.raw", message: "", createdAt: "", updatedAt: "",
      }), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    if (u.endsWith("/logs")) return new Response("", { status: 200 });
    if (u.endsWith("/api/v1/extensions/e-1/nodes")) {
      return new Response(JSON.stringify([
        { nodeId: "n-1", name: "ts", type: "sysext", bootState: "common", version: "v1.74.0", installedAt: "" },
        { nodeId: "n-2", name: "ts", type: "sysext", bootState: "active", version: "v1.74.0", installedAt: "" },
      ]), { status: 200, headers: { "Content-Type": "application/json" } });
    }
    return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
  });

  render(
    <MemoryRouter initialEntries={["/extensions/e-1"]}>
      <Routes><Route path="/extensions/:id" element={<ExtensionDetail />} /></Routes>
    </MemoryRouter>,
  );
  await waitFor(() => screen.getByText(/Installed on 2 nodes/i));
});
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionDetail.test.tsx
```

Expected: FAIL.

- [ ] **Step 4: Add the section**

In `ExtensionDetail.tsx`:

```tsx
import { listNodesForExtension } from "@/api/extensions";

// inside the component:
const [nodes, setNodes] = useState<NodeExtensionRow[]>([]);
useEffect(() => {
  if (!id) return;
  listNodesForExtension(id).then(setNodes).catch(() => setNodes([]));
}, [id]);
```

Render the section under the existing main panel:

```tsx
<section className="mt-6">
  <h2 className="text-sm font-semibold mb-2">
    Installed on {nodes.length} node{nodes.length === 1 ? "" : "s"}
  </h2>
  {nodes.length === 0 ? (
    <p className="text-sm text-muted-foreground">No nodes have this extension yet.</p>
  ) : (
    <div className="rounded-md border overflow-hidden text-sm">
      {nodes.map((row) => (
        <div
          key={`${row.nodeId}/${row.bootState}`}
          className="grid grid-cols-[1fr_.6fr_.6fr_.6fr] px-3 py-2 items-center border-b last:border-b-0"
        >
          <Link to={`/nodes/${row.nodeId}`} className="font-medium hover:underline">{row.nodeId}</Link>
          <div className="text-xs">{row.bootState}</div>
          <div className="text-xs"><code>{row.version}</code></div>
          <div className="text-[11px] text-muted-foreground text-right">
            {row.installedAt ? new Date(row.installedAt).toLocaleString() : "—"}
          </div>
        </div>
      ))}
    </div>
  )}
</section>
```

(Add `Link` to the react-router import.)

- [ ] **Step 5: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionDetail.test.tsx
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add ui/src/api/extensions.ts ui/src/pages/ExtensionDetail.tsx ui/src/pages/ExtensionDetail.test.tsx
git commit -m "ui(ExtensionDetail): Installed-on-nodes section"
```

---

## Task 5: Final verification + smoke test

**Files:** none (verification only)

- [ ] **Step 1: Full UI test suite**

```
cd ~/_git/AuroraBoot/ui && npm run test
```

Expected: every spec across `pkg/handlers/...` (UI side) and `pages/`/`components/` is green.

- [ ] **Step 2: Type check + lint**

```
cd ~/_git/AuroraBoot/ui && npm run build && npm run lint
```

Expected: `tsc -b && vite build` succeeds; eslint reports no errors. Warnings are acceptable.

- [ ] **Step 3: Backend regression**

```
cd ~/_git/AuroraBoot && go test ./... -count=1
```

Expected: every Go package green.

- [ ] **Step 4: Live smoke (manual)**

1. Build the binary:

   ```
   make build
   ```

2. Start with a fresh data dir:

   ```
   ./build/auroraboot web --data-dir /tmp/aurora-smoke --listen :18080 &
   ```

3. Open `http://localhost:18080`, log in with the password from `/tmp/aurora-smoke/secrets/admin-password`, and walk the happy path:

   - **Extensions** in the sidebar exists between **Artifacts** and **Nodes**.
   - **/extensions** is empty with the three template chips visible.
   - Click **Build extension**: the wizard opens, **From artifact** is the default tab (if at least one Ready artifact exists), Step 2's Hierarchies chip input renders, Step 3 shows the Review.
   - Cancel out, navigate to **Artifacts**, open **Build artifact**, advance to Step 2, expand "Pre-configure for system extensions", verify the chip input and bake preview both render.
   - On Step 2, verify the **Bundled extensions** card lists filtered Ready extensions.
   - On Step 2, verify the **Allowed remote commands** picker lists `extension` with a red "NEW · destructive" pill.

4. Tear down:

   ```
   kill %1
   ```

Expected: every check above passes.

- [ ] **Step 5: Commit (verify-only / no changes)**

If smoke surfaces any issues, file them as follow-ups. Otherwise no commit is needed for Task 5.

---

## Self-review checks

- **Spec coverage:** bundled-upgrade dialog (Task 1), cross-check strip (Task 2), per-node tracking surfaces (Tasks 3–4), final verify (Task 5). Together with Plans 3a–3b, every UI surface in the design doc is now covered.
- **Type consistency:** `ResolvedBundleEntry` matches the server's `ResolvedBundleEntry` shape from Plan 2d Task 6. `NodeExtensionRow` mirrors the backend's `store.NodeExtensionRow`.
- **Placeholder scan:** none.

## What lands at the end of this plan

- Operators can run a bundled OS upgrade with the artifact's extensions riding along.
- The Extensions builder warns when an operator picks an artifact whose declared hierarchies don't match.
- Each node's detail page lists its installed extensions; each extension's detail page lists the nodes it lives on.
- `npm run test`, `npm run build`, `npm run lint`, and `go test ./...` are all green; the live binary smoke-passes.

## Out of scope here

- Live WS log streaming during a build (deferred — `getExtensionLogs` is polled on mount and on a button click; the UI WS subscription pattern matches Artifacts and can be added later without spec changes).
- "Remove" action button per row in NodeDetail's "Installed extensions" — operators currently use the InstallExtensionDialog from the extension's detail page.
- Per-node + per-version transition labels in the pre-action diff (the spec's three-line diff). The current dialog ships the bundle multi-select; the diff is deferred as a polish enhancement because it requires per-target-node version lookups against `node_extensions`.
