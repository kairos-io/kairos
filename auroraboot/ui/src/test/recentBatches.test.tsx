import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { RecentBatches } from "@/components/fleet/RecentBatches";

const listBatches = vi.fn();
vi.mock("@/api/batches", () => ({
  listBatches: (...a: unknown[]) => listBatches(...a),
}));

function batch(over: Record<string, unknown> = {}) {
  return {
    id: "b1",
    command: "upgrade",
    args: {},
    selection: "group edge",
    groupID: "g1",
    concurrency: 2,
    stopOnFailure: true,
    rebootTimeoutSeconds: 600,
    phase: "stopped",
    createdAt: "2026-10-23T10:00:00Z",
    finishedAt: null,
    stoppedAt: null,
    stopReason: "",
    stoppedByNodeID: null,
    counts: { done: 3, failed: 1 },
    ...over,
  };
}

function renderCard(props: { groupID?: string; nodeID?: string }) {
  return render(
    <MemoryRouter>
      <RecentBatches {...props} />
    </MemoryRouter>,
  );
}

describe("RecentBatches", () => {
  // A beforeEach that returns a function has it run as teardown, so the
  // reset must not be the arrow's return value.
  beforeEach(() => {
    listBatches.mockReset();
  });

  it("lists a group's newest batches and links to them", async () => {
    listBatches.mockResolvedValue({ total: 1, batches: [batch()] });
    renderCard({ groupID: "g1" });
    expect(await screen.findByText("group edge")).toBeInTheDocument();
    expect(screen.getByText("Batches")).toBeInTheDocument();
    expect(screen.getByText("stopped").className).toContain("danger");
    expect(screen.getByRole("link", { name: /Upgrade/ })).toHaveAttribute("href", "/batches/b1");
    expect(screen.getByRole("link", { name: "View all" })).toHaveAttribute("href", "/batches");
    expect(listBatches).toHaveBeenCalledWith({ groupID: "g1", limit: 5 });
  });

  it("shows an empty state", async () => {
    listBatches.mockResolvedValue({ total: 0, batches: [] });
    renderCard({ groupID: "g1" });
    expect(await screen.findByText("No batches yet.")).toBeInTheDocument();
  });

  it("shows a failed fetch as an error instead of the empty state", async () => {
    listBatches.mockRejectedValue(new Error("connection refused"));
    renderCard({ groupID: "g1" });
    expect(await screen.findByText("Failed to load batches: connection refused")).toBeInTheDocument();
    expect(screen.queryByText("No batches yet.")).not.toBeInTheDocument();
  });

  it("drops an earlier error once a later fetch succeeds", async () => {
    listBatches.mockRejectedValueOnce(new Error("connection refused"));
    const { rerender } = renderCard({ groupID: "g1" });
    expect(await screen.findByText("Failed to load batches: connection refused")).toBeInTheDocument();

    listBatches.mockResolvedValue({ total: 1, batches: [batch({ selection: "group core" })] });
    rerender(
      <MemoryRouter>
        <RecentBatches groupID="g2" />
      </MemoryRouter>,
    );

    expect(await screen.findByText("group core")).toBeInTheDocument();
    expect(screen.queryByText(/Failed to load batches/)).not.toBeInTheDocument();
  });

  it("asks for the node's batches when given a node", async () => {
    listBatches.mockResolvedValue({ total: 0, batches: [] });
    renderCard({ nodeID: "n1" });
    await screen.findByText("No batches yet.");
    expect(listBatches).toHaveBeenCalledWith({ nodeID: "n1", limit: 5 });
  });
});
