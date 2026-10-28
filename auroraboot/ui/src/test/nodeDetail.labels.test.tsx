import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import { Component, type ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";

import { NodeDetail } from "@/pages/NodeDetail";
import { Toaster } from "@/components/ui/toaster";

const NODE_ID = "0f9c2f1e-5b4a-4c0e-9d3f-7a1b2c3d4e5f";

const node = {
  id: NODE_ID,
  hostname: "edge-01",
  machineID: "9f8e7d6c5b4a",
  groupID: null,
  labels: { role: "worker" },
  phase: "active",
  osRelease: null,
  agentVersion: "v2.31.2",
  lastHeartbeat: "2026-08-28T10:00:00Z",
  createdAt: "2026-08-20T10:00:00Z",
  updatedAt: "2026-08-28T10:00:00Z",
};

// The page dies to a white screen because a render throws, so an assertion on
// "the page is still there" only means something with a boundary to catch it.
// Without one React unwinds past the test and the failure reads as a stray
// unhandled error rather than as this page blanking.
class Boundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null };
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  componentDidCatch() {
    // Swallow the console noise; the rendered fallback is the assertion.
  }
  render() {
    if (this.state.error) {
      return <div data-testid="crashed">{this.state.error.message}</div>;
    }
    return this.props.children;
  }
}

// Route each call the page makes to a canned response. `labelsBody` is the
// interesting knob: it is what the real server sends back from the labels PUT.
function mockFetch(opts: { labelsStatus?: number; labelsBody?: unknown } = {}) {
  const { labelsStatus = 200, labelsBody = { status: "ok" } } = opts;
  const calls: string[] = [];

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
      calls.push(`${method} ${url}`);

      if (url.endsWith("/labels") && method === "PUT") {
        return json(labelsBody, labelsStatus);
      }
      if (url === `/api/v1/nodes/${NODE_ID}`) return json(node);
    if (url.startsWith("/api/v1/batches")) return json({ batches: [], total: 0 });
      return json([]);
    }),
  };
}

beforeEach(() => {
  // The page opens a UI WebSocket on mount; jsdom would try to dial it.
  vi.stubGlobal(
    "WebSocket",
    class {
      close() {}
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function renderNodeDetail() {
  return render(
    <Boundary>
      <MemoryRouter initialEntries={[`/nodes/${NODE_ID}`]}>
        <Routes>
          <Route path="nodes/:id" element={<NodeDetail />} />
        </Routes>
      </MemoryRouter>
      <Toaster />
    </Boundary>,
  );
}

async function saveLabels() {
  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  const input = screen.getByDisplayValue("role=worker");
  fireEvent.change(input, { target: { value: "role=worker, zone=eu" } });
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
  });
}

describe("NodeDetail label save", () => {
  it("keeps the page rendered after a successful save", async () => {
    // PUT /nodes/:id/labels answers {"status":"ok"} — not a node. Feeding that
    // to setNode() is what left `phase` undefined and blanked the page.
    const fetchMock = mockFetch();
    vi.stubGlobal("fetch", fetchMock.fn);

    renderNodeDetail();
    expect(await screen.findByRole("heading", { name: "edge-01" })).toBeInTheDocument();

    await saveLabels();

    await waitFor(() =>
      expect(fetchMock.calls).toContain(`PUT /api/v1/nodes/${NODE_ID}/labels`),
    );
    expect(screen.queryByTestId("crashed")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "edge-01" })).toBeInTheDocument();
  });

  it("re-reads the node from the server so the shown labels are the stored ones", async () => {
    const fetchMock = mockFetch();
    vi.stubGlobal("fetch", fetchMock.fn);

    renderNodeDetail();
    await screen.findByRole("heading", { name: "edge-01" });
    const before = fetchMock.calls.filter((c) => c === `GET /api/v1/nodes/${NODE_ID}`).length;

    await saveLabels();

    // The PUT returns no node, so the only way back to a correct view is to ask.
    await waitFor(() =>
      expect(
        fetchMock.calls.filter((c) => c === `GET /api/v1/nodes/${NODE_ID}`).length,
      ).toBeGreaterThan(before),
    );
  });

  it("surfaces a failed save instead of throwing past the page", async () => {
    const fetchMock = mockFetch({
      labelsStatus: 500,
      labelsBody: { error: "failed to set labels" },
    });
    vi.stubGlobal("fetch", fetchMock.fn);

    renderNodeDetail();
    await screen.findByRole("heading", { name: "edge-01" });

    await saveLabels();

    expect(await screen.findByText(/failed to save labels/i)).toBeInTheDocument();
    expect(screen.queryByTestId("crashed")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "edge-01" })).toBeInTheDocument();
  });
});
