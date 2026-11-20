# Extensions — Plan 3b of 3: InstallExtensionDialog + ArtifactBuilder integration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the manual-flow Install dialog and the three ArtifactBuilder additions (hierarchies disclosure, bundled-extensions card, `extension` allowed-command row), plus a proper SecureBoot key-set picker on the ExtensionBuilder. This is the half of the UI that wires Extensions into the existing artifact + fleet surfaces.

**Architecture:** New `InstallExtensionDialog.tsx` modeled on `CommandDialog.tsx`. ArtifactBuilder additions live inside its existing 4-step wizard — Step 2's Access & Security card grows a folded disclosure (no orange ring; discoverability comes from being in the right neighborhood). The bundled-extensions card is a sibling card after Access & Security. The `extension` row joins the existing "Destructive — opt in per fleet" group in the AllowedCommandsPicker. All `npm run test` specs.

**Tech Stack:** React 19, react-router 7, vitest + RTL.

**Repository:** `/home/mudler/_git/AuroraBoot`

**Prerequisites:** Plans 2a–2e + Plan 3a merged.

---

## Reference — read these before starting

| File | What it does |
|---|---|
| `ui/src/components/CommandDialog.tsx` (whole file) | Pattern to mirror for `InstallExtensionDialog`. Target/Action two-step wizard, group/labels/specific-nodes picker, payload preview, send button. |
| `ui/src/api/nodes.ts:84-104` | `sendCommand`, `sendBulkCommand` — wire the dialog through these. |
| `ui/src/api/groups.ts` | `listGroups` for the target picker. |
| `ui/src/pages/ArtifactBuilder.tsx:1413-1500` | Existing "Access & Security" card — site of the hierarchies disclosure (Task 3). |
| `ui/src/pages/ArtifactBuilder.tsx:401-477` | `AllowedCommandsPicker` — site of the new `extension` row (Task 5). |
| `ui/src/pages/ArtifactBuilder.tsx:382-399` | `COMMAND_DESCRIPTIONS` — copy edit added in Task 5. |
| `ui/src/api/artifacts.ts` | `listSecureBootKeySets`, `SecureBootKeySet` — Task 6's picker reads these. |
| Spec, section "Acceptance criteria" → `Interaction states`, `Color discipline` | The amber-on-Active warning, the red destructive treatment, and the disabled-until-valid behavior all come from here. |

---

## Task 1: `InstallExtensionDialog` component — target + action + boot scope

**Files:**
- Create: `ui/src/components/InstallExtensionDialog.tsx`
- Create: `ui/src/components/InstallExtensionDialog.test.tsx`

The dialog has three required choices: target (Group / Labels / Specific nodes), action (Install / Enable / Disable / Remove), boot scope (Common default; Active reveals the amber callout). "Activate immediately" toggle. Send button is the bottom-right anchor; payload preview is folded.

- [ ] **Step 1: Write the failing spec**

Create `ui/src/components/InstallExtensionDialog.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { InstallExtensionDialog } from "./InstallExtensionDialog";
import type { Extension } from "@/api/extensions";

const ext: Extension = {
  id: "e-1", name: "tailscale-agent", type: "sysext", phase: "Ready",
  arch: "amd64", version: "v1.74.0", sourceMode: "image",
  rawFilename: "tailscale-agent.sysext.raw",
  createdAt: "", updatedAt: "", message: "",
};

beforeEach(() => {
  window.localStorage.setItem("auroraboot_token", "tok");
  vi.restoreAllMocks();
  // Default fetch: listGroups returns one group; sendBulkCommand returns [].
  vi.spyOn(window, "fetch").mockImplementation(async (url, init) => {
    const u = String(url);
    if (u.endsWith("/api/v1/groups")) {
      return new Response(`[{"id":"g-1","name":"edge-fleet-eu","nodeCount":24}]`, { status: 200, headers: { "Content-Type": "application/json" } });
    }
    if (u === "/api/v1/nodes/commands" && init?.method === "POST") {
      return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
    }
    return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
  });
});

describe("InstallExtensionDialog", () => {
  it("defaults boot scope to Common", async () => {
    render(<InstallExtensionDialog open onOpenChange={() => {}} extension={ext} />);
    await waitFor(() => expect(screen.getByRole("button", { name: /^Common$/i })).toHaveAttribute("aria-pressed", "true"));
  });

  it("reveals the amber callout when Active scope is picked", async () => {
    render(<InstallExtensionDialog open onOpenChange={() => {}} extension={ext} />);
    fireEvent.click(screen.getByRole("button", { name: /^Active$/i }));
    expect(screen.getByRole("alert")).toHaveTextContent(/active partition/i);
  });

  it("hides the amber callout when leaving Active", async () => {
    render(<InstallExtensionDialog open onOpenChange={() => {}} extension={ext} />);
    fireEvent.click(screen.getByRole("button", { name: /^Active$/i }));
    fireEvent.click(screen.getByRole("button", { name: /^Common$/i }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("Send button is disabled until a target group is picked", async () => {
    render(<InstallExtensionDialog open onOpenChange={() => {}} extension={ext} />);
    await waitFor(() => screen.getByRole("button", { name: /^Group$/i }));
    expect(screen.getByRole("button", { name: /^Send/i })).toBeDisabled();
  });

  it("POSTs an extension command with the expected payload", async () => {
    let payload: any = null;
    vi.spyOn(window, "fetch").mockImplementation(async (url, init) => {
      const u = String(url);
      if (u.endsWith("/api/v1/groups")) {
        return new Response(`[{"id":"g-1","name":"edge","nodeCount":1}]`, { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (u === "/api/v1/nodes/commands" && init?.method === "POST") {
        payload = JSON.parse(init.body as string);
        return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
    });

    const onChange = vi.fn();
    render(<InstallExtensionDialog open onOpenChange={onChange} extension={ext} />);
    await waitFor(() => screen.getByText("edge"));
    // Group is auto-selected when one is available; if not, pick it explicitly.
    fireEvent.click(screen.getByRole("button", { name: /^Send/i }));

    await waitFor(() => expect(payload).not.toBeNull());
    expect(payload.command).toBe("extension");
    expect(payload.selector.groupID).toBe("g-1");
    expect(payload.args).toMatchObject({
      type: "sysext",
      action: "install",
      name: "tailscale-agent",
      bootState: "common",
      now: "true",
    });
    expect(String(payload.args.source)).toContain("/api/v1/extensions/e-1/download/tailscale-agent.sysext.raw");
    expect(onChange).toHaveBeenCalledWith(false);
  });

  it("shows the payload preview when 'Show payload' is expanded", async () => {
    render(<InstallExtensionDialog open onOpenChange={() => {}} extension={ext} />);
    fireEvent.click(screen.getByRole("button", { name: /Show payload/i }));
    expect(screen.getByText(/"command":\s*"extension"/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/components/InstallExtensionDialog.test.tsx
```

Expected: FAIL — module doesn't exist.

- [ ] **Step 3: Implement the dialog**

Create `ui/src/components/InstallExtensionDialog.tsx`:

```tsx
import { useEffect, useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { listGroups, type Group } from "@/api/groups";
import { sendBulkCommand } from "@/api/nodes";
import { extensionDownloadUrl, type Extension } from "@/api/extensions";
import { ExtensionTypeChip } from "@/components/ExtensionTypeChip";

type Action = "install" | "enable" | "disable" | "remove";
type Scope = "active" | "passive" | "recovery" | "common";

interface Props {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  extension: Extension;
}

export function InstallExtensionDialog({ open, onOpenChange, extension }: Props) {
  const [groups, setGroups] = useState<Group[]>([]);
  const [groupID, setGroupID] = useState<string>("");
  const [action, setAction] = useState<Action>("install");
  const [bootState, setBootState] = useState<Scope>("common");
  const [now, setNow] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [showPayload, setShowPayload] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    listGroups().then((gs) => {
      setGroups(gs);
      if (gs.length === 1) setGroupID(gs[0].id);
    }).catch(() => {});
  }, [open]);

  const args: Record<string, string> = {
    type: extension.type,
    action,
    name: extension.name,
    bootState,
    now: now ? "true" : "false",
  };
  if (action === "install") {
    args.source = window.location.origin + extensionDownloadUrl(extension.id, extension.rawFilename ?? "");
  }

  const canSend = groupID !== "" && !submitting;

  async function handleSend() {
    setSubmitting(true);
    setErr(null);
    try {
      await sendBulkCommand({ groupID }, "extension", args);
      onOpenChange(false);
    } catch (e) {
      setErr(String(e));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[720px]">
        <DialogHeader>
          <DialogTitle>
            <span className="inline-flex items-center gap-2.5">
              <ExtensionTypeChip type={extension.type} />
              <span className="font-semibold">{extension.name}</span>
              <code className="text-xs opacity-60">{extension.version} · {extension.arch}</code>
            </span>
          </DialogTitle>
        </DialogHeader>

        <p className="text-sm text-muted-foreground">Re-running over the same name = upgrade.</p>

        <Section label="Target">
          <Tab active label="Group" /> {/* Labels and Specific nodes ship in a later iteration. */}
          <select
            className="border rounded-md px-3 py-2 text-sm bg-background w-full mt-2"
            value={groupID}
            onChange={(e) => setGroupID(e.target.value)}
          >
            <option value="" disabled>Select a group…</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>{g.name}</option>
            ))}
          </select>
        </Section>

        <Section label="Action">
          <div className="grid grid-cols-4 gap-1.5">
            {(["install", "enable", "disable", "remove"] as const).map((a) => (
              <button
                key={a}
                type="button"
                aria-pressed={action === a}
                onClick={() => setAction(a)}
                className={`px-2.5 py-2 border rounded-md text-xs ${action === a ? "border-[#EE5007] bg-[#EE5007]/5 ring-1 ring-[#EE5007]" : "hover:bg-muted/30"}`}
              >
                {a[0].toUpperCase() + a.slice(1)}
              </button>
            ))}
          </div>
        </Section>

        <div className="grid grid-cols-2 gap-4">
          <Section label="Boot scope">
            <div className="flex gap-1.5 flex-wrap">
              {(["active", "passive", "recovery", "common"] as const).map((s) => (
                <button
                  key={s}
                  type="button"
                  aria-pressed={bootState === s}
                  onClick={() => setBootState(s)}
                  className={`px-2.5 py-1 text-xs rounded-md border ${bootState === s ? "bg-[#EE5007] text-white border-[#EE5007]" : "hover:bg-muted/30"}`}
                >
                  {s[0].toUpperCase() + s.slice(1)}
                </button>
              ))}
            </div>
            {bootState === "active" && (
              <p role="alert" className="text-xs mt-2 px-2.5 py-1.5 rounded-md border border-amber-500/40 bg-amber-500/10 text-amber-800 dark:text-amber-200">
                This extension is only enabled when the node is booted in the active partition. If the node is rolled back to passive, it won&apos;t be loaded.
              </p>
            )}
          </Section>

          <Section label="When to apply">
            <label className="flex items-start gap-2 text-sm">
              <input type="checkbox" className="mt-0.5" checked={now} onChange={(e) => setNow(e.target.checked)} />
              <span>
                <span className="font-medium">Activate immediately</span>
                <span className="block text-xs text-muted-foreground">Otherwise applies on next reboot.</span>
              </span>
            </label>
          </Section>
        </div>

        <details className="mt-2">
          <summary className="text-xs text-muted-foreground cursor-pointer">Show payload</summary>
          <pre className="text-[11px] mt-1.5 p-2 rounded-md bg-muted/40 whitespace-pre-wrap">
{JSON.stringify({ command: "extension", args }, null, 2)}
          </pre>
        </details>

        {err && <p role="alert" className="text-sm text-red-600 mt-2">{err}</p>}

        <div className="flex justify-end gap-2 mt-4">
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button disabled={!canSend} onClick={handleSend}>
            {submitting ? "Sending…" : "Send to group"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="mb-3">
      <div className="text-xs text-muted-foreground mb-1.5">{label}</div>
      {children}
    </div>
  );
}

function Tab({ active, label }: { active: boolean; label: string }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      className={`px-2.5 py-1 text-xs rounded-md ${active ? "bg-[#EE5007] text-white" : "border hover:bg-muted/30"}`}
      disabled={!active}
    >
      {label}
    </button>
  );
}
```

- [ ] **Step 4: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/components/InstallExtensionDialog.test.tsx
```

Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/InstallExtensionDialog.tsx ui/src/components/InstallExtensionDialog.test.tsx
git commit -m "ui(components): InstallExtensionDialog for manual-flow install/enable/disable/remove"
```

---

## Task 2: Wire `InstallExtensionDialog` into `ExtensionDetail`

**Files:**
- Modify: `ui/src/pages/ExtensionDetail.tsx`
- Modify: `ui/src/pages/ExtensionDetail.test.tsx`

The "Install on group…" button on the detail page (Plan 3a Task 8) currently does nothing — wire it to open the dialog.

- [ ] **Step 1: Append failing spec**

Append to `ui/src/pages/ExtensionDetail.test.tsx`:

```tsx
it("opens the InstallExtensionDialog when the Install button is clicked", async () => {
  mockOne({ phase: "Ready", rawFilename: "ts.sysext.raw" });
  render(
    <MemoryRouter initialEntries={["/extensions/e-1"]}>
      <Routes><Route path="/extensions/:id" element={<ExtensionDetail />} /></Routes>
    </MemoryRouter>,
  );
  await waitFor(() => screen.getByText("ts"));
  fireEvent.click(screen.getByRole("button", { name: /Install on group/i }));
  await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
  expect(screen.getByText(/Re-running over the same name/i)).toBeInTheDocument();
});
```

(Add `fireEvent` to the imports.)

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionDetail.test.tsx
```

Expected: FAIL — the button doesn't open a dialog.

- [ ] **Step 3: Wire the dialog**

In `ui/src/pages/ExtensionDetail.tsx`, add the state + dialog:

```tsx
import { useState } from "react";
import { InstallExtensionDialog } from "@/components/InstallExtensionDialog";

// inside ExtensionDetail():
const [installOpen, setInstallOpen] = useState(false);
```

Replace the existing `<Button disabled={ext.phase !== "Ready"}>Install on group…</Button>` with:

```tsx
<Button disabled={ext.phase !== "Ready"} onClick={() => setInstallOpen(true)}>
  Install on group…
</Button>
```

And add the dialog at the bottom of the `return`:

```tsx
{ext && (
  <InstallExtensionDialog
    open={installOpen}
    onOpenChange={setInstallOpen}
    extension={ext}
  />
)}
```

- [ ] **Step 4: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionDetail.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/src/pages/ExtensionDetail.tsx ui/src/pages/ExtensionDetail.test.tsx
git commit -m "ui(pages): wire ExtensionDetail install button to InstallExtensionDialog"
```

---

## Task 3: ArtifactBuilder hierarchies disclosure

**Files:**
- Modify: `ui/src/pages/ArtifactBuilder.tsx`
- Modify: `ui/src/pages/ArtifactBuilder.test.tsx` (create if absent — `Artifacts.test.tsx` may already exist)
- Modify: `ui/src/api/artifacts.ts` (extend `CreateArtifactInput` with `extensionHierarchies`)

Adds a `<details>` block "Pre-configure for system extensions · Optional · advanced" inside the existing Access & Security card. Sysext + (nested) confext chip inputs, "What this bakes" disclosure. Submission persists onto `extensionHierarchies` (a field already accepted by Plan 2d's API).

- [ ] **Step 1: Extend the artifact API client type**

In `ui/src/api/artifacts.ts`, add to `CreateArtifactInput`:

```ts
  extensionHierarchies?: { sysext: string[]; confext: string[] };
```

- [ ] **Step 2: Write the failing spec**

Create `ui/src/pages/ArtifactBuilder.test.tsx` (or append to it if it exists):

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { ArtifactBuilder } from "./ArtifactBuilder";

beforeEach(() => {
  window.localStorage.setItem("auroraboot_token", "tok");
  vi.restoreAllMocks();
  vi.spyOn(window, "fetch").mockResolvedValue(new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } }));
});

describe("ArtifactBuilder — extensions support disclosure", () => {
  it("renders the disclosure inside Access & Security, collapsed by default", async () => {
    render(<MemoryRouter><ArtifactBuilder /></MemoryRouter>);
    // Walk to Step 2.
    fireEvent.click(screen.getByText(/Custom/i));
    fireEvent.click(screen.getByRole("button", { name: /Next/i })); // 0 -> 1
    await waitFor(() => screen.getByText(/Access/i));
    expect(screen.getByText(/Pre-configure for system extensions/i)).toBeInTheDocument();
    // The body (chip input) is initially hidden behind the details element.
    expect(screen.queryByText(/Additional sysext hierarchies/i)).not.toBeInTheDocument();
  });

  it("exposes sysext + confext chip inputs when expanded", async () => {
    render(<MemoryRouter><ArtifactBuilder /></MemoryRouter>);
    fireEvent.click(screen.getByText(/Custom/i));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => screen.getByText(/Pre-configure for system extensions/i));
    fireEvent.click(screen.getByText(/Pre-configure for system extensions/i));
    expect(screen.getByText(/Additional sysext hierarchies/i)).toBeInTheDocument();
    expect(screen.getByText(/Confext hierarchies/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ArtifactBuilder.test.tsx
```

Expected: FAIL.

- [ ] **Step 4: Add the disclosure**

In `ui/src/pages/ArtifactBuilder.tsx`:

1. Add `extensionHierarchies` to `EMPTY_FORM`:

   ```tsx
   const EMPTY_FORM: CreateArtifactInput = {
     // existing fields...
     extensionHierarchies: { sysext: [], confext: [] },
   };
   ```

2. Inside the existing Access & Security card body, append a `<details>`:

   ```tsx
   <details className="mt-4 pt-3 border-t border-border/60">
     <summary className="cursor-pointer text-sm font-semibold flex items-center gap-2">
       Pre-configure for system extensions
       <span className="text-xs font-normal opacity-60">Optional · advanced</span>
     </summary>
     <p className="text-xs text-muted-foreground mt-2">
       Bakes a systemd drop-in so the node accepts sysext overlays on the listed paths without manual setup.
     </p>

     <div className="mt-3">
       <div className="text-xs text-muted-foreground mb-1.5">Additional sysext hierarchies</div>
       <HierarchyChipInput
         value={form.extensionHierarchies?.sysext ?? []}
         onChange={(next) => update("extensionHierarchies", { ...form.extensionHierarchies, sysext: next })}
         implicitRoot="/usr"
         quickAdds={["/opt", "/srv", "/var/lib"]}
       />
     </div>

     <details className="mt-3">
       <summary className="cursor-pointer text-xs opacity-80">Confext hierarchies (optional)</summary>
       <div className="mt-2">
         <HierarchyChipInput
           value={form.extensionHierarchies?.confext ?? []}
           onChange={(next) => update("extensionHierarchies", { ...form.extensionHierarchies, confext: next })}
           implicitRoot="/etc"
         />
       </div>
     </details>

     <details className="mt-3" open>
       <summary className="cursor-pointer text-xs opacity-80">What this bakes into the image</summary>
       <pre className="text-[11px] mt-2 p-2.5 rounded-md bg-muted/40 whitespace-pre-wrap">
{`stages:
  initramfs:
    - files:
        - path: /etc/systemd/system/systemd-sysext.service.d/99-aurora-hierarchies.conf
          permissions: 0644
          content: |
            [Service]
            Environment=SYSTEMD_SYSEXT_HIERARCHIES=${["/usr", ...(form.extensionHierarchies?.sysext ?? [])].join(":")}`}
       </pre>
     </details>
   </details>
   ```

3. Import `HierarchyChipInput` at the top of the file:

   ```tsx
   import { HierarchyChipInput } from "@/components/HierarchyChipInput";
   ```

- [ ] **Step 5: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ArtifactBuilder.test.tsx -t "extensions support disclosure"
```

Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add ui/src/pages/ArtifactBuilder.tsx ui/src/pages/ArtifactBuilder.test.tsx ui/src/api/artifacts.ts
git commit -m "ui(ArtifactBuilder): extensions support disclosure with hierarchies + bake preview"
```

---

## Task 4: ArtifactBuilder bundled-extensions card

**Files:**
- Modify: `ui/src/pages/ArtifactBuilder.tsx`
- Modify: `ui/src/pages/ArtifactBuilder.test.tsx`
- Modify: `ui/src/api/artifacts.ts` (extend `CreateArtifactInput`)

A new card after Access & Security that lets operators pick Ready extensions matching the artifact's arch. Each picked row carries `{name, type, pinnedVersion?}`. Submitted as `bundledExtensions` (a field already accepted by Plan 2d's API).

- [ ] **Step 1: Extend the artifact API client type**

In `ui/src/api/artifacts.ts`, add to `CreateArtifactInput`:

```ts
  bundledExtensions?: Array<{
    name: string;
    type: "sysext" | "confext";
    pinnedVersion?: string;
    order?: number;
  }>;
```

- [ ] **Step 2: Write the failing spec**

Append to `ui/src/pages/ArtifactBuilder.test.tsx`:

```tsx
describe("ArtifactBuilder — bundled extensions card", () => {
  it("lists only Ready extensions matching the artifact's arch", async () => {
    vi.spyOn(window, "fetch").mockImplementation(async (url) => {
      const u = String(url);
      if (u.endsWith("/api/v1/extensions")) {
        return new Response(JSON.stringify([
          { id: "e-1", name: "tailscale", type: "sysext", phase: "Ready", arch: "amd64", version: "v1.74", sourceMode: "image" },
          { id: "e-2", name: "armthing", type: "sysext", phase: "Ready", arch: "arm64", version: "v1", sourceMode: "image" },
          { id: "e-3", name: "broken", type: "sysext", phase: "Error", arch: "amd64", version: "v1", sourceMode: "image" },
        ]), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
    });

    render(<MemoryRouter><ArtifactBuilder /></MemoryRouter>);
    fireEvent.click(screen.getByText(/Custom/i));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => screen.getByText(/Bundled extensions/i));

    // amd64 is the default arch.
    expect(screen.getByText("tailscale")).toBeInTheDocument();
    expect(screen.queryByText("armthing")).not.toBeInTheDocument();
    expect(screen.queryByText("broken")).not.toBeInTheDocument();
  });

  it("adds the picked extension to bundledExtensions in the form", async () => {
    vi.spyOn(window, "fetch").mockImplementation(async (url) => {
      if (String(url).endsWith("/api/v1/extensions")) {
        return new Response(JSON.stringify([
          { id: "e-1", name: "tailscale", type: "sysext", phase: "Ready", arch: "amd64", version: "v1.74", sourceMode: "image" },
        ]), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
    });

    render(<MemoryRouter><ArtifactBuilder /></MemoryRouter>);
    fireEvent.click(screen.getByText(/Custom/i));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => screen.getByText("tailscale"));
    fireEvent.click(screen.getByLabelText(/Add tailscale to bundle/i));
    expect(screen.getByText(/1 extension bundled/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ArtifactBuilder.test.tsx -t "bundled extensions card"
```

Expected: FAIL.

- [ ] **Step 4: Add the card**

In `ArtifactBuilder.tsx`:

1. Add `listExtensions` import:

   ```tsx
   import { listExtensions, type Extension } from "@/api/extensions";
   ```

2. State + effect:

   ```tsx
   const [availableExtensions, setAvailableExtensions] = useState<Extension[]>([]);
   useEffect(() => {
     listExtensions().then((rows) => setAvailableExtensions(rows.filter((e) => e.phase === "Ready"))).catch(() => {});
   }, []);
   ```

3. Initialise `bundledExtensions` in `EMPTY_FORM`:

   ```tsx
   const EMPTY_FORM: CreateArtifactInput = {
     // existing fields...
     bundledExtensions: [],
   };
   ```

4. Card rendered immediately after the Access & Security card in Step 2:

   ```tsx
   <Card>
     <CardHeader>
       <CardTitle className="text-sm">Bundled extensions</CardTitle>
     </CardHeader>
     <CardContent>
       <p className="text-xs text-muted-foreground mb-3">
         Extensions installed automatically when this artifact's upgrade is sent to a fleet.
         {form.bundledExtensions && form.bundledExtensions.length > 0 && (
           <span className="ml-1 font-medium">
             {form.bundledExtensions.length} extension{form.bundledExtensions.length > 1 ? "s" : ""} bundled.
           </span>
         )}
       </p>
       <div className="grid gap-2">
         {availableExtensions
           .filter((e) => e.arch === form.arch)
           .map((e) => {
             const picked = (form.bundledExtensions ?? []).some((b) => b.name === e.name && b.type === e.type);
             return (
               <div
                 key={e.id}
                 className="flex items-center justify-between p-2 rounded-md border text-sm hover:bg-muted/30"
               >
                 <div>
                   <span className="font-medium">{e.name}</span>
                   <span className="ml-2 text-xs text-muted-foreground">{e.type} · {e.version}</span>
                 </div>
                 <Button
                   type="button"
                   size="sm"
                   variant={picked ? "default" : "outline"}
                   aria-label={`${picked ? "Remove" : "Add"} ${e.name} ${picked ? "from" : "to"} bundle`}
                   onClick={() => {
                     const existing = form.bundledExtensions ?? [];
                     const next = picked
                       ? existing.filter((b) => !(b.name === e.name && b.type === e.type))
                       : [...existing, { name: e.name, type: e.type }];
                     update("bundledExtensions", next);
                   }}
                 >
                   {picked ? "Remove" : "Add"}
                 </Button>
               </div>
             );
           })}
         {availableExtensions.filter((e) => e.arch === form.arch).length === 0 && (
           <p className="text-xs text-muted-foreground italic">
             No Ready extensions for arch <code>{form.arch}</code>. Build extensions under Extensions →.
           </p>
         )}
       </div>
     </CardContent>
   </Card>
   ```

5. Ensure the submit payload includes `bundledExtensions` (it already will because the field is on `form` and `form` is the body).

- [ ] **Step 5: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ArtifactBuilder.test.tsx -t "bundled extensions card"
```

Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add ui/src/pages/ArtifactBuilder.tsx ui/src/pages/ArtifactBuilder.test.tsx ui/src/api/artifacts.ts
git commit -m "ui(ArtifactBuilder): bundled extensions card filtered to artifact arch"
```

---

## Task 5: `extension` row in the AllowedCommandsPicker

**Files:**
- Modify: `ui/src/pages/ArtifactBuilder.tsx`
- Modify: `ui/src/lib/buildConfig.ts` (extend `PHONEHOME_DESTRUCTIVE_COMMANDS`)
- Modify: `ui/src/pages/ArtifactBuilder.test.tsx`

The `extension` command joins the destructive group, sandwiched between `exec`/`reset` and `apply-cloud-config`. Red treatment, "NEW · destructive" pill outside the chip.

- [ ] **Step 1: Locate the constants**

```
grep -n "PHONEHOME_DESTRUCTIVE_COMMANDS\|PHONEHOME_SAFE_DEFAULTS" ui/src/lib/buildConfig.ts ui/src/pages/ArtifactBuilder.tsx | head
```

Confirm where the destructive list lives.

- [ ] **Step 2: Append the failing spec**

Append to `ui/src/pages/ArtifactBuilder.test.tsx`:

```tsx
describe("ArtifactBuilder — extension allowed-command row", () => {
  it("renders an 'extension' row in the destructive group with a NEW pill", async () => {
    render(<MemoryRouter><ArtifactBuilder /></MemoryRouter>);
    fireEvent.click(screen.getByText(/Custom/i));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => screen.getByText(/Allowed remote commands/i));
    expect(screen.getByText(/^extension$/)).toBeInTheDocument();
    expect(screen.getByText(/NEW.*destructive/i)).toBeInTheDocument();
  });

  it("propagates extension into the form when ticked", async () => {
    render(<MemoryRouter><ArtifactBuilder /></MemoryRouter>);
    fireEvent.click(screen.getByText(/Custom/i));
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => screen.getByText(/^extension$/));
    fireEvent.click(screen.getByLabelText(/extension/));
    // Step into the Review to confirm the checkbox is persisted in form state.
    // Simpler: check the underlying input is now checked.
    expect((screen.getByLabelText(/extension/) as HTMLInputElement).checked).toBe(true);
  });
});
```

- [ ] **Step 3: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ArtifactBuilder.test.tsx -t "extension allowed-command row"
```

Expected: FAIL.

- [ ] **Step 4: Add `extension` to the destructive list**

In `ui/src/lib/buildConfig.ts`:

```ts
export const PHONEHOME_DESTRUCTIVE_COMMANDS = [
  "exec",
  "reset",
  "apply-cloud-config",
  "extension",
] as const;
```

In `ArtifactBuilder.tsx`'s `COMMAND_DESCRIPTIONS` map:

```ts
const COMMAND_DESCRIPTIONS: Record<string, string> = {
  // existing entries…
  extension: "Install / upgrade / remove sysext and confext extensions on the node.",
};
```

Inside `AllowedCommandsPicker`, render a "NEW · destructive" pill next to the `extension` row only. Locate the `commandRow` function and wrap the rendered row for `cmd === "extension"`:

```tsx
const commandRow = (cmd: string) => (
  <div key={cmd}>
    <label className="flex items-center gap-2 text-sm font-medium">
      <input
        type="checkbox"
        checked={set.has(cmd)}
        onChange={() => toggle(cmd)}
        className="rounded border-input"
        aria-label={cmd}
      />
      <code className="font-mono">{cmd}</code>
      {cmd === "extension" && (
        <span className="ml-auto text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-full bg-red-500/10 border border-red-500/30 text-red-700 dark:text-red-300">
          NEW · destructive
        </span>
      )}
    </label>
    <p className="text-xs text-muted-foreground mt-1 ml-6">
      {COMMAND_DESCRIPTIONS[cmd]}
    </p>
  </div>
);
```

- [ ] **Step 5: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ArtifactBuilder.test.tsx -t "extension allowed-command row"
```

Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add ui/src/pages/ArtifactBuilder.tsx ui/src/pages/ArtifactBuilder.test.tsx ui/src/lib/buildConfig.ts
git commit -m "ui(ArtifactBuilder): add 'extension' to destructive allowed-commands"
```

---

## Task 6: SecureBoot key-set picker in ExtensionBuilder

**Files:**
- Modify: `ui/src/pages/ExtensionBuilder.tsx`
- Modify: `ui/src/pages/ExtensionBuilder.test.tsx`

Plan 3a Task 6 left `KeySetPicker` as a free-form text input. Replace with a real dropdown that reads `listSecureBootKeySets`. (The pre-flight `db.key`/`db.pem` warning is deferred — the backend would need an extra field; build-time error is acceptable per spec.)

- [ ] **Step 1: Append the failing spec**

Append to `ui/src/pages/ExtensionBuilder.test.tsx`:

```tsx
describe("ExtensionBuilder — SecureBoot key-set picker", () => {
  it("loads the keysets and lists them in the dropdown", async () => {
    vi.spyOn(window, "fetch").mockImplementation(async (url) => {
      const u = String(url);
      if (u.endsWith("/api/v1/secureboot-keys")) {
        return new Response(JSON.stringify([
          { id: "k-1", name: "kairos-prod", keysDir: "/var/lib/aurora/keys/k-1", tpmPcrKeyPath: "", secureBootEnroll: "if-safe", createdAt: "" },
          { id: "k-2", name: "lab", keysDir: "/var/lib/aurora/keys/k-2", tpmPcrKeyPath: "", secureBootEnroll: "if-safe", createdAt: "" },
        ]), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } });
    });

    render(<MemoryRouter><ExtensionBuilder /></MemoryRouter>);
    await waitFor(() => screen.getByRole("button", { name: /From artifact|Base image/i }));
    fireEvent.change(screen.getByLabelText(/Name/i), { target: { value: "ts" } });
    fireEvent.click(screen.getByRole("button", { name: /Next/i }));
    await waitFor(() => screen.getByText(/Signing/i));
    // The picker is a <select> with the keysets listed; default is empty.
    const sel = screen.getByLabelText(/Signing key set/i) as HTMLSelectElement;
    expect(sel.options).toHaveLength(3); // empty + 2 keysets
    expect(sel.options[1].textContent).toContain("kairos-prod");
  });
});
```

- [ ] **Step 2: Verify it fails**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx -t "SecureBoot key-set picker"
```

Expected: FAIL.

- [ ] **Step 3: Implement the picker**

In `ExtensionBuilder.tsx`:

1. Replace the existing `KeySetPicker` placeholder with:

   ```tsx
   import { listSecureBootKeySets, type SecureBootKeySet } from "@/api/artifacts";

   function KeySetPicker({ selected, onSelect }: { selected: string; onSelect: (s: string) => void }) {
     const [sets, setSets] = useState<SecureBootKeySet[]>([]);
     useEffect(() => { listSecureBootKeySets().then(setSets).catch(() => {}); }, []);
     return (
       <div className="grid gap-1.5">
         <Label htmlFor="signing-keyset">Signing key set</Label>
         <select
           id="signing-keyset"
           className="border rounded-md px-3 py-2 text-sm bg-background"
           value={selected}
           onChange={(e) => onSelect(e.target.value)}
         >
           <option value="">— Unsigned —</option>
           {sets.map((s) => (
             <option key={s.id} value={s.id}>{s.name}</option>
           ))}
         </select>
         <p className="text-[11px] text-muted-foreground">
           AuroraBoot reuses the UKI signing keys (<code>db.key</code> / <code>db.pem</code>) for sysext signing.
         </p>
       </div>
     );
   }
   ```

- [ ] **Step 4: Verify it passes**

```
cd ~/_git/AuroraBoot/ui && npm run test -- src/pages/ExtensionBuilder.test.tsx -t "SecureBoot key-set picker"
```

Expected: PASS.

- [ ] **Step 5: Full UI suite green**

```
cd ~/_git/AuroraBoot/ui && npm run test
```

Expected: every spec passes — Extensions area + ArtifactBuilder additions both green.

- [ ] **Step 6: Commit**

```bash
git add ui/src/pages/ExtensionBuilder.tsx ui/src/pages/ExtensionBuilder.test.tsx
git commit -m "ui(ExtensionBuilder): real SecureBoot key-set picker"
```

---

## Self-review checks

- **Spec coverage:** Manual-flow Install dialog with Common default + Active warning + payload preview (Tasks 1–2), ArtifactBuilder hierarchies disclosure (Task 3) + bundled-extensions card (Task 4) + destructive `extension` allowed-command row (Task 5), real signing key picker (Task 6).
- **Type consistency:** `CreateArtifactInput.bundledExtensions[].type` reuses the `"sysext" | "confext"` literal from `api/extensions.ts`. `InstallExtensionDialog.action` matches the four phonehome actions the agent dispatches in Plan 1.
- **Placeholder scan:** the `<Tab active label="Group" />` in `InstallExtensionDialog` intentionally exposes only the Group tab in v1 — Labels and Specific-nodes pickers are out of scope (see "Out of scope here" below).

## What lands at the end of this plan

- Operators can open an extension, pick a target group + action + boot scope, and send the `extension` command.
- ArtifactBuilder lets operators declare bundled extensions and pre-configure hierarchies before the OS is built.
- The destructive `extension` row exists in the allowed-commands picker; ticking it is what activates fleet-side bundled upgrades on a deployed image.

## Out of scope here

- Labels + Specific-nodes target pickers in `InstallExtensionDialog` (defer; the existing Group picker covers the headline flow).
- `CommandDialog` upgrade-with-extensions (the bundled-flow dialog) → Plan 3c.
- `NodeDetail` "Installed extensions" section → Plan 3c.
- Live WS-driven progress + cross-check warning strip → Plan 3c.
- Backend-side keyset pre-flight (`db.key`/`db.pem` existence) — deferred; build-time error is acceptable per spec.
