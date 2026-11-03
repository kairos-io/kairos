import { describe, it, expect, vi, beforeEach, afterEach, onTestFinished } from "vitest";
import { render, screen, waitFor, fireEvent, act, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { NodeDetail } from "@/pages/NodeDetail";
import { Toaster } from "@/components/ui/toaster";

const NODE_ID = "0f9c2f1e-5b4a-4c0e-9d3f-7a1b2c3d4e5f";

function makeNode(over: Record<string, unknown> = {}) {
  return {
    id: NODE_ID,
    hostname: "edge-01",
    machineID: "9f8e7d6c5b4a3f2e1d0c",
    groupID: null,
    labels: { role: "worker" },
    phase: "online",
    osRelease: { KAIROS_VERSION: "v1.1.0", CPU_COUNT: "4", MEM_TOTAL: "8024512 kB" },
    agentVersion: "v4.3.0",
    lastHeartbeat: "2026-08-28T10:00:00Z",
    remoteIP: "10.0.0.5",
    bootState: "active",
    createdAt: "2026-08-20T10:00:00Z",
    updatedAt: "2026-08-28T10:00:00Z",
    ...over,
  };
}

interface Call {
  method: string;
  url: string;
  body?: string;
}

function mockFetch(opts: { node?: unknown; commands?: unknown[] } = {}) {
  const node = opts.node ?? makeNode();
  const commands = opts.commands ?? [];
  const calls: Call[] = [];
  const json = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  return {
    calls,
    fn: vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();
      const method = (init?.method ?? "GET").toUpperCase();
      calls.push({ method, url, body: init?.body as string | undefined });
      if (url.endsWith("/labels") && method === "PUT") return json({ status: "ok" });
      if (url === `/api/v1/nodes/${NODE_ID}`) return json(node);
      if (url === `/api/v1/nodes/${NODE_ID}/commands`) return json(commands);
    if (url.startsWith("/api/v1/batches")) return json({ batches: [], total: 0 });
      return json([]);
    }),
  };
}

beforeEach(() => {
  vi.stubGlobal(
    "WebSocket",
    class {
      close() {}
    },
  );
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function renderPage() {
  return render(
    <>
      <MemoryRouter initialEntries={[`/nodes/${NODE_ID}`]}>
        <Routes>
          <Route path="nodes/:id" element={<NodeDetail />} />
        </Routes>
      </MemoryRouter>
      <Toaster />
    </>,
  );
}

describe("NodeDetail layout", () => {
  it("shows a recovery boot state in the warning tone", async () => {
    vi.stubGlobal("fetch", mockFetch({ node: makeNode({ bootState: "recovery" }) }).fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    const strip = screen.getByRole("region", { name: "Health" });
    const value = within(strip).getByText("Recovery");
    expect(value).toHaveClass("text-warning");
  });

  it("shows an active boot state in the success tone", async () => {
    vi.stubGlobal("fetch", mockFetch().fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    const strip = screen.getByRole("region", { name: "Health" });
    expect(within(strip).getByText("Active")).toHaveClass("text-success");
  });

  it("shows when the node last upgraded and the state of its upgrade", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-08-28T12:00:00Z"));
    vi.stubGlobal(
      "fetch",
      mockFetch({ node: makeNode({ lastUpgrade: "2026-08-28T10:00:00Z", upgradeState: "pending" }) }).fn,
    );
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    const strip = screen.getByRole("region", { name: "Health" });
    const tile = within(strip).getByText("Upgrade").closest("div[class*='border']") as HTMLElement;
    expect(within(tile).getByText("2h ago")).toBeInTheDocument();
    expect(within(tile).getByText("Pending")).toBeInTheDocument();
  });

  it("says when a node was never upgraded", async () => {
    vi.stubGlobal("fetch", mockFetch().fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    const strip = screen.getByRole("region", { name: "Health" });
    const tile = within(strip).getByText("Upgrade").closest("div[class*='border']") as HTMLElement;
    expect(within(tile).getByText("Never")).toBeInTheDocument();
    expect(within(tile).getByText("No upgrade pending")).toBeInTheDocument();
  });

  it.each([
    ["upgrade", { upgradeState: "failed" }],
    ["reset", { resetState: "failed" }],
  ])("shows a failed %s in the danger tone", async (_kind, over) => {
    vi.stubGlobal("fetch", mockFetch({ node: makeNode(over) }).fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    const strip = screen.getByRole("region", { name: "Health" });
    expect(within(strip).getByText("Failed")).toHaveClass("text-danger");
  });

  it("sends a null group when the group is set to None", async () => {
    // jsdom has no scrollIntoView, which the open Select calls on its items.
    Element.prototype.scrollIntoView = vi.fn();
    onTestFinished(() => {
      delete (Element.prototype as Partial<Element>).scrollIntoView;
    });
    const f = mockFetch({ node: makeNode({ groupID: "g1" }) });
    vi.stubGlobal("fetch", f.fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    fireEvent.keyDown(screen.getByRole("combobox", { name: "Group" }), { key: "Enter" });
    fireEvent.keyDown(await screen.findByRole("option", { name: "None" }), { key: "Enter" });

    await waitFor(() =>
      expect(f.calls).toContainEqual({
        method: "PUT",
        url: `/api/v1/nodes/${NODE_ID}/group`,
        body: JSON.stringify({ groupID: null }),
      }),
    );
  });

  it("puts Decommission and Clear command history in the more-actions menu", async () => {
    vi.stubGlobal("fetch", mockFetch().fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    // Decommission is not a top-level button any more.
    expect(screen.queryByRole("button", { name: /decommission/i })).not.toBeInTheDocument();

    const trigger = screen.getByRole("button", { name: "More actions" });
    fireEvent.keyDown(trigger, { key: "Enter" });

    expect(await screen.findByRole("menuitem", { name: "Decommission" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Clear command history" })).toBeInTheDocument();
  });

  it("adds a label chip and saves the merged labels", async () => {
    const f = mockFetch();
    vi.stubGlobal("fetch", f.fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    fireEvent.click(screen.getByRole("button", { name: "Add label" }));
    fireEvent.change(screen.getByRole("textbox", { name: "New label" }), {
      target: { value: "env=prod" },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Add" }));
    });

    await waitFor(() => {
      const put = f.calls.find((c) => c.method === "PUT" && c.url === `/api/v1/nodes/${NODE_ID}/labels`);
      expect(put).toBeDefined();
      expect(JSON.parse(put!.body!)).toEqual({ labels: { role: "worker", env: "prod" } });
    });
  });

  it("rejects a label key that is already used, without saving", async () => {
    const f = mockFetch();
    vi.stubGlobal("fetch", f.fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    fireEvent.click(screen.getByRole("button", { name: "Add label" }));
    fireEvent.change(screen.getByRole("textbox", { name: "New label" }), {
      target: { value: "role=db" },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Add" }));
    });

    expect(screen.getByText(/label "role" already exists/i)).toBeInTheDocument();
    expect(f.calls.some((c) => c.method === "PUT")).toBe(false);
  });

  it("removes a label through its chip", async () => {
    const f = mockFetch({ node: makeNode({ labels: { role: "worker", zone: "eu" } }) });
    vi.stubGlobal("fetch", f.fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Remove label zone" }));
    });
    await waitFor(() => {
      const put = f.calls.find((c) => c.method === "PUT");
      expect(put).toBeDefined();
      expect(JSON.parse(put!.body!)).toEqual({ labels: { role: "worker" } });
    });
  });

  it("shows the last result line of a failed command and filters by status", async () => {
    const commands = [
      {
        id: "c1",
        managedNodeID: NODE_ID,
        command: "upgrade",
        args: { image: "quay.io/x:v2" },
        phase: "Failed",
        result: "pulling image\nverifying\nerror: no space left on device\n",
        expiresAt: null,
        deliveredAt: null,
        completedAt: null,
        createdAt: "2026-08-28T09:00:00Z",
      },
      {
        id: "c2",
        managedNodeID: NODE_ID,
        command: "reboot",
        args: {},
        phase: "Completed",
        result: "ok",
        expiresAt: null,
        deliveredAt: null,
        completedAt: null,
        createdAt: "2026-08-28T08:00:00Z",
      },
    ];
    vi.stubGlobal("fetch", mockFetch({ commands }).fn);
    renderPage();
    await screen.findByRole("heading", { name: "edge-01" });

    expect(await screen.findByText("error: no space left on device")).toBeInTheDocument();
    expect(screen.queryByText("pulling image")).not.toBeInTheDocument();
    expect(screen.getByText("image=quay.io/x:v2")).toBeInTheDocument();

    const timeline = screen.getByRole("list", { name: "Commands" });
    expect(within(timeline).getAllByRole("listitem")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: /^Failed/ }));
    expect(within(timeline).getAllByRole("listitem")).toHaveLength(1);
    expect(within(timeline).getByText("upgrade")).toBeInTheDocument();
  });
});
