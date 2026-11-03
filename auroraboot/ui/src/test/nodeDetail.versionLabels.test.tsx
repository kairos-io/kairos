import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { NodeDetail } from "@/pages/NodeDetail";
import { getNode } from "@/api/nodes";

const NODE_ID = "0f9c2f1e-5b4a-4c0e-9d3f-7a1b2c3d4e5f";

// node.agentVersion is the running Kairos version the agent reports; use a
// value distinct from the image version below so a mislabeled render can't
// pass by coincidence.
const AGENT_VERSION = "v4.3.0-47-ga83f003c";
// osRelease.KAIROS_VERSION is the version of the image the node was built
// from.
const IMAGE_VERSION = "v1.1.0";

function nodeWithVersions() {
  return {
    id: NODE_ID,
    hostname: "edge-01",
    machineID: "a1b2c3d4",
    groupID: null,
    labels: { role: "worker" },
    phase: "online",
    osRelease: { KAIROS_VERSION: IMAGE_VERSION },
    agentVersion: AGENT_VERSION,
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

describe("NodeDetail version labels", () => {
  beforeEach(() => {
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  // kairos-io/kairos#4617: "Agent Version" rendered node.agentVersion (the
  // running Kairos version) and "Kairos" rendered osRelease.KAIROS_VERSION
  // (the image version) -- the labels described the wrong thing for what
  // they held. Assert the corrected labels for both render paths, and that
  // the old bare strings are gone.
  it("labels node.agentVersion as Kairos Version and osRelease.KAIROS_VERSION as Image Version", async () => {
    vi.mocked(getNode).mockResolvedValue(nodeWithVersions());

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Kairos Version")).toBeInTheDocument();
    });
    expect(screen.getByText(AGENT_VERSION)).toBeInTheDocument();

    expect(screen.getByText("Image Version")).toBeInTheDocument();
    expect(screen.getByText(IMAGE_VERSION)).toBeInTheDocument();

    expect(screen.queryByText("Agent Version")).not.toBeInTheDocument();
    expect(screen.queryByText("Kairos", { selector: "span" })).not.toBeInTheDocument();
  });
});
