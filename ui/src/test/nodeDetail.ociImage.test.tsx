import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { NodeDetail } from "@/pages/NodeDetail";
import { getNode } from "@/api/nodes";

const NODE_ID = "0f9c2f1e-5b4a-4c0e-9d3f-7a1b2c3d4e5f";

const IMAGE_REPO = "quay.io/kairos/ubuntu";
const IMAGE_TAG = "24.04-standard-amd64-generic-v3.4.2";
const OCI_IMAGE = `${IMAGE_REPO}:${IMAGE_TAG}`;

function nodeWithImage(osRelease: Record<string, string>) {
  return {
    id: NODE_ID,
    hostname: "edge-01",
    machineID: "a1b2c3d4",
    groupID: "",
    labels: {},
    phase: "online",
    osRelease,
    agentVersion: "v4.3.0",
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

// kairos-io/kairos#4547 asks for the OCI image the node runs. The agent has
// reported it since sdk/versioneer started writing KAIROS_IMAGE_REPO and
// KAIROS_IMAGE_LABEL into /etc/kairos-release, which the heartbeat merges into
// osRelease, but the node view never rendered it.
describe("NodeDetail OCI image", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("shows the running OCI image under the image version", async () => {
    vi.mocked(getNode).mockResolvedValue(
      nodeWithImage({
        KAIROS_VERSION: "v3.4.2",
        KAIROS_IMAGE_REPO: IMAGE_REPO,
        KAIROS_IMAGE_LABEL: IMAGE_TAG,
      }),
    );

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Image Version")).toBeInTheDocument();
    });

    const rendered = screen.getByText(OCI_IMAGE);
    expect(rendered).toBeInTheDocument();
    // The reference is long enough to be truncated in the tile, so the full
    // value has to stay readable on hover.
    expect(rendered).toHaveAttribute("title", OCI_IMAGE);
  });

  it("keeps the Not reported sub-line when the node reported no image at all", async () => {
    vi.mocked(getNode).mockResolvedValue(nodeWithImage({}));

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Image Version")).toBeInTheDocument();
    });
    expect(screen.getByText("Not reported")).toBeInTheDocument();
  });
});
