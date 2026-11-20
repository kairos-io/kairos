import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act, within } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";

import { Nodes } from "@/pages/Nodes";

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

const nodes = [
  makeNode("id-milan", "pos-milan-01", { labels: { site: "milan" } }),
  makeNode("id-rome", "pos-rome-01", { phase: "Offline" }),
  makeNode("id-lab", "lab-01"),
];

const groups = [
  { id: "g1", name: "edge-retail", description: "", createdAt: "", updatedAt: "" },
  { id: "g2", name: "lab", description: "", createdAt: "", updatedAt: "" },
];

type Call = { url: string; method: string; body: unknown };

function mockFetch() {
  const calls: Call[] = [];
  const json = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    const method = (init?.method ?? "GET").toUpperCase();
    if (method !== "GET") {
      calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      return json(url.endsWith("/commands") ? [] : { status: "ok" });
    }
    if (url.startsWith("/api/v1/groups")) return json(groups);
    if (url.startsWith("/api/v1/nodes")) return json(nodes);
    return json([]);
  });
  return { fn, calls };
}

function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="location">{loc.pathname + loc.search}</div>;
}

function renderAt(url = "/nodes") {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Nodes />
      <LocationProbe />
    </MemoryRouter>,
  );
}

async function selectTwo() {
  const t = within(await screen.findByRole("table"));
  await t.findByText("lab-01");
  fireEvent.click(t.getByRole("checkbox", { name: "Select pos-milan-01" }));
  fireEvent.click(t.getByRole("checkbox", { name: "Select lab-01" }));
}

let m: ReturnType<typeof mockFetch>;

beforeEach(() => {
  m = mockFetch();
  vi.stubGlobal("fetch", m.fn);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Nodes selection", () => {
  it("shows a selection bar with the count, and a checkbox click does not open the node", async () => {
    renderAt();
    expect(screen.queryByText(/selected$/)).not.toBeInTheDocument();
    await selectTwo();
    expect(screen.getByText("2 selected")).toBeInTheDocument();
    expect(screen.getByTestId("location").textContent).toBe("/nodes");

    fireEvent.click(screen.getByRole("button", { name: "Clear" }));
    expect(screen.queryByText("2 selected")).not.toBeInTheDocument();
  });

  it("sends the command to the selected node IDs only", async () => {
    renderAt();
    await selectTwo();
    // The page header targets the selection too.
    expect(screen.getByRole("button", { name: "Send Command to 2 nodes" })).toBeInTheDocument();

    const bar = within(screen.getByRole("region", { name: "Selection" }));
    fireEvent.click(bar.getByRole("button", { name: "Send command" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /Reboot/ }));
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: /^Reboot \d+ nodes$/ }));
    });

    await waitFor(() => expect(m.calls.filter((c) => c.url === "/api/v1/nodes/commands")).toHaveLength(1));
    const sent = m.calls.find((c) => c.url === "/api/v1/nodes/commands")!.body as {
      selector: { nodeIDs: string[] };
      command: string;
    };
    expect(sent.command).toBe("reboot");
    expect(sent.selector.nodeIDs).toEqual(["id-milan", "id-lab"]);
    expect(screen.queryByText("Send to all nodes")).not.toBeInTheDocument();
  });

  it("moves every selected node to a group", async () => {
    renderAt();
    await selectTwo();
    fireEvent.click(screen.getByRole("button", { name: "Move to group" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("radio", { name: "edge-retail" }));
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Move 2 nodes" }));
    });

    await waitFor(() => expect(m.calls.filter((c) => c.method === "PUT")).toHaveLength(2));
    const puts = m.calls.filter((c) => c.method === "PUT");
    expect(puts.map((c) => c.url).sort()).toEqual(["/api/v1/nodes/id-lab/group", "/api/v1/nodes/id-milan/group"]);
    for (const c of puts) expect(c.body).toEqual({ groupID: "g1" });
  });

  it("sends a null group when the selected nodes move to No group", async () => {
    renderAt();
    await selectTwo();
    fireEvent.click(screen.getByRole("button", { name: "Move to group" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("radio", { name: "No group" }));
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Move 2 nodes" }));
    });

    await waitFor(() => expect(m.calls.filter((c) => c.method === "PUT")).toHaveLength(2));
    for (const c of m.calls.filter((c) => c.method === "PUT")) expect(c.body).toEqual({ groupID: null });
  });

  it("adds a label to every selected node, keeping existing labels", async () => {
    renderAt();
    await selectTwo();
    fireEvent.click(screen.getByRole("button", { name: "Add label" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Label"), { target: { value: "tier=edge" } });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Add to 2 nodes" }));
    });

    await waitFor(() => expect(m.calls.filter((c) => c.method === "PUT")).toHaveLength(2));
    const byUrl = Object.fromEntries(m.calls.filter((c) => c.method === "PUT").map((c) => [c.url, c.body]));
    expect(byUrl["/api/v1/nodes/id-milan/labels"]).toEqual({ labels: { site: "milan", tier: "edge" } });
    expect(byUrl["/api/v1/nodes/id-lab/labels"]).toEqual({ labels: { tier: "edge" } });
  });

  it("switches to tiles with one tile per node and keeps view in the URL", async () => {
    renderAt("/nodes?q=");
    await within(await screen.findByRole("table")).findByText("lab-01");
    fireEvent.click(screen.getByRole("radio", { name: "Tiles" }));

    expect(screen.getByTestId("location").textContent).toContain("view=tiles");
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    const tiles = await screen.findAllByTestId("node-tile");
    expect(tiles).toHaveLength(3);
    expect(tiles.find((t) => t.textContent?.includes("pos-rome-01"))?.className).toContain("opacity");
  });

  it("opens in tiles when the URL says view=tiles", async () => {
    renderAt("/nodes?view=tiles&groupBy=group");
    expect(await screen.findAllByTestId("node-tile")).toHaveLength(3);
    expect(screen.getByText("Not in a group")).toBeInTheDocument();
  });
});
