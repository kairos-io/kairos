import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { NodeDetail } from "@/pages/NodeDetail";
import { getNode } from "@/api/nodes";

const NODE_ID = "0f9c2f1e-5b4a-4c0e-9d3f-7a1b2c3d4e5f";

function nodeWithHostname(hostname: string) {
  return {
    id: NODE_ID,
    hostname,
    machineID: "a1b2c3d4",
    groupID: null,
    labels: { role: "worker" },
    phase: "online",
    osRelease: null,
    agentVersion: "v2.31.2",
    lastHeartbeat: "2026-08-28T10:00:00Z",
    createdAt: "2026-08-20T10:00:00Z",
    updatedAt: "2026-08-28T10:00:00Z",
  };
}

vi.mock("@/api/nodes", () => ({
  getNode: vi.fn(),
  sendCommand: vi.fn().mockResolvedValue(undefined),
  setLabels: vi.fn().mockResolvedValue(undefined),
  setGroup: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@/api/commands", () => ({
  listNodeCommands: vi.fn().mockResolvedValue([]),
  deleteCommand: vi.fn().mockResolvedValue(undefined),
  clearCommandHistory: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@/api/groups", () => ({
  listGroups: vi.fn().mockResolvedValue([]),
}));

// The real hook opens a WebSocket; jsdom has none and this page's live updates
// are not what is under test here.
vi.mock("@/hooks/useUIWebSocket", () => ({
  useUIWebSocket: () => ({ connected: true }),
}));

function renderPage() {
  return render(
    <MemoryRouter initialEntries={[`/nodes/${NODE_ID}`]}>
      <Routes>
        <Route path="/nodes/:id" element={<NodeDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

// Flush the promises the polled fetches resolve with. Advancing fake timers
// only fires the interval callback; the `.then` that calls setState runs on the
// microtask queue, so without this the assertions race the render.
async function tick(ms: number) {
  await act(async () => {
    vi.advanceTimersByTime(ms);
    await Promise.resolve();
  });
}

describe("NodeDetail hostname", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  // kairos-io/kairos#4196: the node registered as the image default because
  // phone-home ran before cloud-init applied `hostname: kairos-{{ ... }}`. Once
  // a heartbeat corrects it server-side, an open detail page has to follow
  // without a manual reload.
  it("picks up a hostname change from the server on the poll", async () => {
    vi.mocked(getNode)
      .mockResolvedValueOnce(nodeWithHostname("kairos"))
      .mockResolvedValue(nodeWithHostname("kairos-a1b2"));

    renderPage();

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "kairos" })).toBeInTheDocument();
    });

    await tick(10000);

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "kairos-a1b2" })).toBeInTheDocument();
    });
  });

  // The poll re-reads the whole node, and the label textbox is seeded from it.
  // Reseeding mid-edit would silently discard what the operator typed.
  it("does not reseed the label textbox from a poll while labels are being edited", async () => {
    vi.mocked(getNode).mockResolvedValue(nodeWithHostname("kairos"));

    renderPage();

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "kairos" })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const input = screen.getByDisplayValue("role=worker") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "role=worker, zone=eu" } });

    await tick(10000);

    expect(screen.getByDisplayValue("role=worker, zone=eu")).toBeInTheDocument();
  });
});
