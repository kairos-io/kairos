import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, within, cleanup } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { Extensions } from "@/pages/Extensions";
import { ExtensionDetail } from "@/pages/ExtensionDetail";
import { InstallExtensionDialog } from "@/components/InstallExtensionDialog";
import type { Extension } from "@/api/extensions";

const ARTIFACT_ID = "a1b2c3d4-0000-4000-8000-000000000001";
const KEYSET_ID = "k1b2c3d4-0000-4000-8000-000000000002";
const READY_ID = "e1b2c3d4-0000-4000-8000-00000000000a";
const BUILDING_ID = "e1b2c3d4-0000-4000-8000-00000000000b";
const ERROR_ID = "e1b2c3d4-0000-4000-8000-00000000000c";

function ext(over: Partial<Extension>): Extension {
  return {
    id: READY_ID,
    name: "store-pos-config",
    type: "confext",
    phase: "Ready",
    message: "",
    arch: "amd64",
    version: "2026.09.1",
    sourceMode: "image",
    sourceImage: "ghcr.io/example/x:1",
    createdAt: "2026-09-29T09:00:00Z",
    updatedAt: new Date(Date.now() - 5 * 60_000).toISOString(),
    ...over,
  };
}

const extensions: Extension[] = [
  ext({
    id: READY_ID,
    name: "store-pos-config",
    sourceMode: "artifact",
    sourceImage: undefined,
    sourceArtifactId: ARTIFACT_ID,
    signingKeySetId: KEYSET_ID,
    rawFilename: "store-pos-config.raw",
    downloadToken: "dl",
  }),
  ext({
    id: BUILDING_ID,
    name: "vision-runtime",
    type: "sysext",
    phase: "Building",
    message: "42%",
    sourceImage: "ghcr.io/example/vision-runtime:0.9.0",
  }),
  ext({
    id: ERROR_ID,
    name: "plc-gateway-arm",
    type: "sysext",
    phase: "Error",
    message: "no files under /usr or /opt in the image\nmore detail",
    arch: "arm64",
  }),
];

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

function mockFetch() {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(String(input), "http://localhost");
    const p = url.pathname;
    if (p === "/api/v1/extensions") return json(extensions);
    if (p === `/api/v1/extensions/${READY_ID}/nodes`)
      return json([
        { nodeId: "n1", name: "store-pos-config", type: "confext", bootState: "common", version: "1", installedAt: "2026-09-29T08:00:00Z", updatedAt: "2026-09-29T08:00:00Z" },
        { nodeId: "n1", name: "store-pos-config", type: "confext", bootState: "active", version: "1", installedAt: "2026-09-29T08:00:00Z", updatedAt: "2026-09-29T08:00:00Z" },
        { nodeId: "n2", name: "store-pos-config", type: "confext", bootState: "common", version: "1", installedAt: "2026-09-29T08:00:00Z", updatedAt: "2026-09-29T08:00:00Z" },
      ]);
    if (/^\/api\/v1\/extensions\/[^/]+\/nodes$/.test(p)) return json([]);
    if (p === `/api/v1/extensions/${READY_ID}`) return json(extensions[0]);
    if (p === `/api/v1/extensions/${READY_ID}/logs`) return new Response("build ok", { status: 200 });
    if (p === "/api/v1/artifacts") return json([{ id: ARTIFACT_ID, name: "ubuntu-24.04-edge-retail", phase: "Ready", message: "" }]);
    if (p === `/api/v1/artifacts/${ARTIFACT_ID}`) return json({ id: ARTIFACT_ID, name: "ubuntu-24.04-edge-retail", phase: "Ready", message: "" });
    if (p === "/api/v1/secureboot-keys") return json([{ id: KEYSET_ID, name: "fleet-2026", hasTpmPcrKey: false, secureBootEnroll: "", createdAt: "" }]);
    if (p === "/api/v1/groups") return json([{ id: "g1", name: "edge-retail" }]);
    if (p === "/api/v1/nodes")
      return json([
        { id: "n1", hostname: "pos-01", machineID: "m1", groupID: "g1", labels: {}, phase: "Online", lastHeartbeat: "", createdAt: "", updatedAt: "" },
        { id: "n2", hostname: "pos-02", machineID: "m2", groupID: "g1", labels: {}, phase: "Offline", lastHeartbeat: "", createdAt: "", updatedAt: "" },
      ]);
    return json({ error: "not found" }, 404);
  });
}

function renderList() {
  return render(
    <MemoryRouter initialEntries={["/extensions"]}>
      <Routes>
        <Route path="/extensions" element={<Extensions />} />
      </Routes>
    </MemoryRouter>,
  );
}

function rowFor(name: string): HTMLElement {
  const row = screen.getByText(name).closest("tr");
  if (!row) throw new Error(`no row for ${name}`);
  return row as HTMLElement;
}

function cellUnder(row: HTMLElement, header: string): HTMLElement {
  const headers = screen.getAllByRole("columnheader").map((h) => h.textContent?.trim());
  const idx = headers.indexOf(header);
  if (idx < 0) throw new Error(`no column ${header} in ${headers.join(",")}`);
  return within(row).getAllByRole("cell")[idx];
}

describe("Extensions list", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", mockFetch());
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("shows build progress in the status cell and the time in its own Updated cell", async () => {
    renderList();
    await screen.findByText("vision-runtime");
    const row = rowFor("vision-runtime");
    const status = cellUnder(row, "Status");
    expect(within(status).getByRole("progressbar")).toBeInTheDocument();
    expect(status.textContent).toContain("42%");
    const updated = cellUnder(row, "Updated");
    expect(updated.textContent).toBe("5m ago");
    expect(within(updated).queryByRole("progressbar")).toBeNull();
  });

  it("names the source artifact instead of its UUID", async () => {
    renderList();
    await screen.findByText(/ubuntu-24\.04-edge-retail/);
    const row = rowFor("store-pos-config");
    expect(row.textContent).toContain("from ubuntu-24.04-edge-retail · amd64");
    expect(row.textContent).not.toContain(ARTIFACT_ID);
  });

  it("counts the nodes each extension is installed on", async () => {
    renderList();
    await screen.findByText("store-pos-config");
    await waitFor(() =>
      expect(cellUnder(rowFor("store-pos-config"), "Installed on").textContent).toBe("2 nodes"),
    );
    await waitFor(() =>
      expect(cellUnder(rowFor("vision-runtime"), "Installed on").textContent).toBe("Not installed"),
    );
  });

  it("shows the key-set name in the signed column", async () => {
    renderList();
    await screen.findByText("store-pos-config");
    await waitFor(() =>
      expect(cellUnder(rowFor("store-pos-config"), "Signed").textContent).toBe("fleet-2026"),
    );
    expect(cellUnder(rowFor("vision-runtime"), "Signed").textContent).toBe("Unsigned");
  });

  it("raises an alert with View log for an extension in Error", async () => {
    renderList();
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("plc-gateway-arm");
    expect(alert.textContent).toContain("no files under /usr or /opt in the image");
    const link = within(alert).getByRole("link", { name: "View log" });
    expect(link.getAttribute("href")).toBe(`/extensions/${ERROR_ID}#logs`);
  });
});

describe("Extension detail", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", mockFetch());
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("uses the name and type as subtitle and lists the nodes it is installed on", async () => {
    render(
      <MemoryRouter initialEntries={[`/extensions/${READY_ID}`]}>
        <Routes>
          <Route path="/extensions/:id" element={<ExtensionDetail />} />
        </Routes>
      </MemoryRouter>,
    );
    await screen.findByRole("heading", { name: "store-pos-config" });
    expect(screen.queryByText(new RegExp(READY_ID))).toBeNull();
    expect(screen.getByText("store-pos-config · confext")).toBeInTheDocument();
    const card = (await screen.findByText("pos-01")).closest("[data-slot=installed-on]") as HTMLElement;
    expect(card).not.toBeNull();
    expect(within(card).getByText("pos-02")).toBeInTheDocument();
    expect(within(card).getAllByText("Online").length).toBeGreaterThan(0);
  });
});

describe("Install extension dialog", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", mockFetch());
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("picks the target with the shared Select, not a native select", async () => {
    render(<InstallExtensionDialog open onOpenChange={() => {}} extension={extensions[0]} />);
    expect(await screen.findByRole("combobox")).toBeInTheDocument();
    expect(document.querySelector("select")).toBeNull();
    expect(
      screen.getByText("Installing an extension with the same name again upgrades it."),
    ).toBeInTheDocument();
  });
});
