import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { Groups } from "@/pages/Groups";
import { Toaster } from "@/components/ui/toaster";

function makeNode(id: string, hostname: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    hostname,
    machineID: `m-${id}`,
    groupID: null,
    labels: {},
    phase: "Online",
    osRelease: null,
    agentVersion: "v2.31.2",
    lastHeartbeat: "2026-09-29T10:00:00Z",
    createdAt: "2026-09-20T10:00:00Z",
    updatedAt: "2026-09-29T10:00:00Z",
    ...extra,
  };
}

const groups = [
  { id: "g1", name: "edge-retail", description: "Shops", node_count: 1, createdAt: "", updatedAt: "" },
  { id: "g2", name: "lab", description: "", node_count: 0, createdAt: "", updatedAt: "" },
];

const nodes = [
  makeNode("n-new", "lab-new-box", { remoteIP: "10.0.0.7" }),
  makeNode("n-shop", "pos-milan-01", { groupID: "g1", osRelease: { KAIROS_VERSION: "v3.5.0" } }),
];

type Call = { url: string; method: string; body: unknown };

function mockFetch(opts: { failPut?: boolean } = {}) {
  const calls: Call[] = [];
  const json = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    const method = (init?.method ?? "GET").toUpperCase();
    if (method !== "GET") {
      calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      if (opts.failPut) return json({ error: "boom" }, 500);
      return json({ status: "ok" });
    }
    if (url.startsWith("/api/v1/groups")) return json(groups);
    if (url.startsWith("/api/v1/nodes")) return json(nodes);
    return json([]);
  });
  return { fn, calls };
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/groups"]}>
      <Groups />
      <Toaster />
    </MemoryRouter>,
  );
}

function column(name: string) {
  return screen.getByRole("region", { name });
}

function dataTransfer() {
  const data: Record<string, string> = {};
  return {
    setData: (k: string, v: string) => {
      data[k] = v;
    },
    getData: (k: string) => data[k] ?? "",
    effectAllowed: "move",
    dropEffect: "move",
  };
}

beforeEach(() => {
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Groups board", () => {
  it("shows the board by default with Not in a group first", async () => {
    vi.stubGlobal("fetch", mockFetch().fn);
    renderPage();
    await screen.findByText("lab-new-box");
    const cols = screen.getAllByRole("region");
    expect(cols[0]).toHaveAccessibleName("Not in a group");
    expect(cols[1]).toHaveAccessibleName("edge-retail");
    expect(within(column("Not in a group")).getByText("lab-new-box")).toBeInTheDocument();
    expect(within(column("edge-retail")).getByText("pos-milan-01")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "New group" }).length).toBeGreaterThan(0);
  });

  it("moves a dragged card onto another column", async () => {
    const f = mockFetch();
    vi.stubGlobal("fetch", f.fn);
    renderPage();
    const card = await screen.findByRole("button", { name: "lab-new-box" });
    const dt = dataTransfer();
    fireEvent.dragStart(card, { dataTransfer: dt });
    fireEvent.dragOver(column("lab"), { dataTransfer: dt });
    fireEvent.drop(column("lab"), { dataTransfer: dt });

    expect(within(column("lab")).getByText("lab-new-box")).toBeInTheDocument();
    await waitFor(() =>
      expect(f.calls).toContainEqual({ url: "/api/v1/nodes/n-new/group", method: "PUT", body: { groupID: "g2" } }),
    );
    expect(within(column("lab")).getByText("lab-new-box")).toBeInTheDocument();
  });

  it("sends a null group when a card is dropped on Not in a group", async () => {
    const f = mockFetch();
    vi.stubGlobal("fetch", f.fn);
    renderPage();
    const card = await screen.findByRole("button", { name: "pos-milan-01" });
    const dt = dataTransfer();
    fireEvent.dragStart(card, { dataTransfer: dt });
    fireEvent.dragOver(column("Not in a group"), { dataTransfer: dt });
    fireEvent.drop(column("Not in a group"), { dataTransfer: dt });

    await waitFor(() =>
      expect(f.calls).toContainEqual({ url: "/api/v1/nodes/n-shop/group", method: "PUT", body: { groupID: null } }),
    );
    expect(within(column("Not in a group")).getByText("pos-milan-01")).toBeInTheDocument();
  });

  it("moves the card back and shows a toast when the move fails", async () => {
    const f = mockFetch({ failPut: true });
    vi.stubGlobal("fetch", f.fn);
    renderPage();
    const card = await screen.findByRole("button", { name: "lab-new-box" });
    const dt = dataTransfer();
    fireEvent.dragStart(card, { dataTransfer: dt });
    fireEvent.drop(column("lab"), { dataTransfer: dt });

    expect(await screen.findByText(/could not move lab-new-box/i)).toBeInTheDocument();
    expect(within(column("Not in a group")).getByText("lab-new-box")).toBeInTheDocument();
    expect(within(column("lab")).queryByText("lab-new-box")).not.toBeInTheDocument();
  });

  it("opens a Move to menu when m is pressed on a focused card", async () => {
    const f = mockFetch();
    vi.stubGlobal("fetch", f.fn);
    renderPage();
    const card = await screen.findByRole("button", { name: "lab-new-box" });
    card.focus();
    fireEvent.keyDown(card, { key: "m" });

    const item = await screen.findByRole("menuitem", { name: "edge-retail" });
    expect(screen.getByRole("menuitem", { name: "lab" })).toBeInTheDocument();
    fireEvent.click(item);
    await waitFor(() =>
      expect(f.calls).toContainEqual({ url: "/api/v1/nodes/n-new/group", method: "PUT", body: { groupID: "g1" } }),
    );
  });

  it("shows the table with view=table", async () => {
    vi.stubGlobal("fetch", mockFetch().fn);
    render(
      <MemoryRouter initialEntries={["/groups?view=table"]}>
        <Groups />
      </MemoryRouter>,
    );
    expect(await screen.findByRole("table")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Not in a group" })).not.toBeInTheDocument();
  });
});
