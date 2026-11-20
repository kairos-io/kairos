import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { Dashboard } from "@/pages/Dashboard";

const baseNode = {
  groupID: null,
  labels: {},
  osRelease: null,
  agentVersion: "v2.31.2",
  lastHeartbeat: "2026-09-28T10:00:00Z",
  createdAt: "2026-09-20T10:00:00Z",
  updatedAt: "2026-09-28T10:00:00Z",
};

const nodes = [
  { ...baseNode, id: "n1", hostname: "node-online", machineID: "m1", phase: "Online" },
  { ...baseNode, id: "n2", hostname: "node-offline", machineID: "m2", phase: "Offline" },
  { ...baseNode, id: "n3", hostname: "node-pending", machineID: "m3", phase: "Pending" },
  { ...baseNode, id: "n4", hostname: "node-registered", machineID: "m4", phase: "Registered" },
];

const artifacts = [
  {
    id: "a1",
    name: "build-in-progress",
    phase: "Building",
    createdAt: "2026-09-28T09:00:00Z",
    updatedAt: "2026-09-28T09:00:00Z",
  },
];

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      const path = url.split("?")[0];
      if (path === "/api/v1/nodes") return json(nodes);
      if (path === "/api/v1/artifacts") return json(artifacts);
      if (path === "/api/v1/groups") return json([]);
      if (path === "/api/v1/deployments") return json([]);
      return json([]);
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// StatTile: label span -> label row -> tile.
function tile(label: string): HTMLElement {
  return screen.getByText(label, { selector: "span" }).parentElement!.parentElement!;
}

describe("Dashboard counts", () => {
  it("shows the running build and lists only the offline node as offline", async () => {
    render(
      <MemoryRouter>
        <Dashboard />
      </MemoryRouter>,
    );

    await screen.findByText("Needs attention");
    // Phases arrive capitalized ("Building"); the Builds tile must count it.
    const builds = tile("Builds");
    expect(builds).toHaveTextContent("1 building");
    expect(within(builds).getByText("build-in-progress")).toBeInTheDocument();

    // Only the Offline node is reported offline; Pending and Registered wait.
    expect(tile("Fleet")).toHaveTextContent("1 offline · 2 waiting");
    expect(screen.getByText("node-offline is offline")).toBeInTheDocument();
    expect(screen.queryByText("node-online is offline")).not.toBeInTheDocument();
    expect(screen.queryByText("node-pending is offline")).not.toBeInTheDocument();
    expect(screen.queryByText("node-registered is offline")).not.toBeInTheDocument();
  });
});
