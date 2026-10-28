import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, within, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { Groups } from "@/pages/Groups";
import { GroupDetail } from "@/pages/GroupDetail";

// node_count is what the server counted when the page loaded. The fixtures
// below keep it deliberately out of step with the node list so each test says
// which of the two the page is reading.
function makeGroup(id: string, name: string, nodeCount?: number) {
  return {
    id,
    name,
    description: "",
    ...(nodeCount === undefined ? {} : { node_count: nodeCount }),
    createdAt: "2026-09-01T10:00:00Z",
    updatedAt: "2026-09-01T10:00:00Z",
  };
}

function makeNode(id: string, hostname: string, groupID: string) {
  return {
    id,
    hostname,
    machineID: `m-${id}`,
    groupID,
    labels: {},
    phase: "Online",
    osRelease: null,
    agentVersion: "v2.31.2",
    lastHeartbeat: "2026-09-29T10:00:00Z",
    createdAt: "2026-09-20T10:00:00Z",
    updatedAt: "2026-09-29T10:00:00Z",
  };
}

// The server counts are deliberately wrong here: the page must read the node
// list, and fall back to these only when it cannot.
const groups = [makeGroup("g1", "edge-retail", 7), makeGroup("g2", "lab", 3)];
const nodes = [makeNode("n-shop", "pos-milan-01", "g1"), makeNode("n-new", "lab-new-box", "")];

function mockFetch(opts: { groups?: unknown[]; failNodes?: boolean } = {}) {
  const json = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    if ((init?.method ?? "GET").toUpperCase() !== "GET") return json({ status: "ok" });
    if (url.startsWith("/api/v1/groups")) return json(opts.groups ?? groups);
    if (url.startsWith("/api/v1/nodes")) {
      if (opts.failNodes) return json({ error: "boom" }, 500);
      return json(nodes);
    }
    if (url.startsWith("/api/v1/batches")) return json({ batches: [], total: 0 });
    return json([]);
  });
}

function nodesCell(groupName: string) {
  const row = screen.getByText(groupName).closest("tr");
  expect(row).not.toBeNull();
  const headers = screen.getAllByRole("columnheader").map((h) => h.textContent);
  const idx = headers.indexOf("Nodes");
  expect(idx).toBeGreaterThanOrEqual(0);
  return within(row as HTMLElement).getAllByRole("cell")[idx];
}

function renderPage(view: "board" | "table") {
  return render(
    <MemoryRouter initialEntries={[view === "table" ? "/groups?view=table" : "/groups"]}>
      <Groups />
    </MemoryRouter>,
  );
}

// moveToLab drags the card named hostname onto the "lab" column.
function moveToLab(hostname: string) {
  const card = screen.getByRole("button", { name: hostname });
  const data: Record<string, string> = {};
  const dataTransfer = {
    setData: (k: string, v: string) => {
      data[k] = v;
    },
    getData: (k: string) => data[k] ?? "",
    effectAllowed: "move",
    dropEffect: "move",
  };
  const target = screen.getByRole("region", { name: "lab" });
  fireEvent.dragStart(card, { dataTransfer });
  fireEvent.dragOver(target, { dataTransfer });
  fireEvent.drop(target, { dataTransfer });
}

// openDeleteDialog clicks the per-row delete button of the table view.
async function openDeleteDialog(groupName: string) {
  fireEvent.click(screen.getByRole("button", { name: `Delete ${groupName}` }));
  return screen.findByRole("dialog");
}

async function closeDialog() {
  fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
}

function showTable() {
  fireEvent.click(screen.getByRole("radio", { name: "Table" }));
  return screen.findByRole("table");
}

beforeEach(() => {
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Groups node count", () => {
  it("counts the nodes the group holds, not the count the page loaded with", async () => {
    vi.stubGlobal("fetch", mockFetch());
    renderPage("table");
    await screen.findByText("edge-retail");
    await waitFor(() => expect(nodesCell("edge-retail")).toHaveTextContent(/^1$/));
    expect(nodesCell("lab")).toHaveTextContent(/^0$/);
  });

  it("follows a drag-and-drop move on both groups, with no reload", async () => {
    vi.stubGlobal("fetch", mockFetch());
    renderPage("board");
    await screen.findByText("pos-milan-01");

    moveToLab("pos-milan-01");

    await showTable();
    // The server still says edge-retail holds 7 and lab holds 3.
    await waitFor(() => expect(nodesCell("edge-retail")).toHaveTextContent(/^0$/));
    expect(nodesCell("lab")).toHaveTextContent(/^1$/);
  });

  it("reports the count a move left behind in the delete confirmation", async () => {
    vi.stubGlobal("fetch", mockFetch());
    renderPage("board");
    await screen.findByText("pos-milan-01");

    moveToLab("pos-milan-01");
    await showTable();

    const emptied = await openDeleteDialog("edge-retail");
    expect(emptied).toHaveTextContent(/Delete "edge-retail"\? This group has no nodes\./);
    await closeDialog();

    const filled = await openDeleteDialog("lab");
    expect(filled).toHaveTextContent(/Delete "lab"\? 1 node\(s\) will be moved out/);
  });

  it("falls back to the server's count when the node list cannot be read", async () => {
    vi.stubGlobal("fetch", mockFetch({ failNodes: true }));
    renderPage("table");
    await screen.findByText("edge-retail");
    expect(nodesCell("edge-retail")).toHaveTextContent(/^7$/);
    expect(nodesCell("lab")).toHaveTextContent(/^3$/);
  });

  it("does not call a group empty when neither count is known", async () => {
    vi.stubGlobal("fetch", mockFetch({ failNodes: true, groups: [makeGroup("g1", "edge-retail")] }));
    renderPage("table");
    await screen.findByText("edge-retail");

    const dialog = await openDeleteDialog("edge-retail");
    expect(dialog).toHaveTextContent(/Any nodes in it will be moved out/);
    expect(dialog).not.toHaveTextContent(/This group has no nodes/);
  });
});

describe("Group detail delete confirmation", () => {
  function mockDetailFetch(opts: { group: unknown; nodes?: unknown[]; failNodes?: boolean }) {
    const json = (body: unknown, status = 200) =>
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
    return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();
      if ((init?.method ?? "GET").toUpperCase() !== "GET") return json({ status: "ok" });
      if (url.startsWith("/api/v1/groups/g1")) return json(opts.group);
      if (url.startsWith("/api/v1/nodes")) {
        if (opts.failNodes) return json({ error: "boom" }, 500);
        return json(opts.nodes ?? []);
      }
    if (url.startsWith("/api/v1/batches")) return json({ batches: [], total: 0 });
      return json([]);
    });
  }

  async function renderDetailAndDelete() {
    render(
      <MemoryRouter initialEntries={["/groups/g1"]}>
        <Routes>
          <Route path="/groups/:id" element={<GroupDetail />} />
        </Routes>
      </MemoryRouter>,
    );
    await screen.findByRole("heading", { name: "edge-retail" });
    fireEvent.keyDown(screen.getByRole("button", { name: "More actions" }), { key: "Enter" });
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));
    return screen.findByRole("dialog");
  }

  it("counts the nodes the group holds, not the count the page loaded with", async () => {
    vi.stubGlobal(
      "fetch",
      mockDetailFetch({
        group: makeGroup("g1", "edge-retail", 7),
        nodes: [makeNode("n-shop", "pos-milan-01", "g1")],
      }),
    );
    const dialog = await renderDetailAndDelete();
    await waitFor(() => expect(dialog).toHaveTextContent(/Delete "edge-retail"\? 1 node\(s\) will be moved out/));
  });

  it("does not call a group empty when neither count is known", async () => {
    vi.stubGlobal("fetch", mockDetailFetch({ group: makeGroup("g1", "edge-retail"), failNodes: true }));
    const dialog = await renderDetailAndDelete();
    expect(dialog).toHaveTextContent(/Any nodes in it will be moved out/);
    expect(dialog).not.toHaveTextContent(/This group has no nodes/);
  });
});
