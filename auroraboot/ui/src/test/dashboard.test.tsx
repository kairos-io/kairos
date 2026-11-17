import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, within, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { Dashboard } from "@/pages/Dashboard";

const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString();

const baseNode = {
  groupID: "g1",
  labels: {},
  osRelease: { KAIROS_VERSION: "v4.3.0" },
  agentVersion: "v2.31.2",
  lastHeartbeat: ago(1),
  createdAt: ago(3000),
  updatedAt: ago(1),
  bootState: "active",
};

const healthyNodes = [
  { ...baseNode, id: "n1", hostname: "edge-1", machineID: "m1", phase: "Online" },
  { ...baseNode, id: "n2", hostname: "edge-2", machineID: "m2", phase: "Online" },
];

const mixedNodes = [
  ...healthyNodes,
  { ...baseNode, id: "n3", hostname: "edge-3", machineID: "m3", phase: "Offline", lastHeartbeat: ago(180) },
  { ...baseNode, id: "n4", hostname: "edge-4", machineID: "m4", phase: "Pending" },
];

const readyArtifact = { id: "a0", name: "base", phase: "Ready", message: "", createdAt: ago(5000), updatedAt: ago(4990) };

const mixedArtifacts = [
  readyArtifact,
  { id: "a1", name: "kiosk", phase: "Building", message: "", createdAt: ago(6), updatedAt: ago(6) },
  { id: "a2", name: "broken", phase: "Error", message: "boom", createdAt: ago(120), updatedAt: ago(60) },
];

const mixedExtensions = [
  { id: "e1", name: "x1", phase: "Ready", createdAt: ago(4000), updatedAt: ago(3990) },
  { id: "e2", name: "x2", phase: "Ready", createdAt: ago(4000), updatedAt: ago(3990) },
  { id: "e3", name: "x3", phase: "Building", createdAt: ago(10), updatedAt: ago(10) },
  { id: "e4", name: "x4", phase: "Error", createdAt: ago(4000), updatedAt: ago(3990) },
];

const deployments = [
  { id: "d1", artifactId: "a0", method: "redfish", status: "Active", message: "", bmcTargetId: "bmc-rack-2", progress: 10, startedAt: ago(3) },
];

function json(body: unknown) {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

function mockApi(data: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const path = (typeof input === "string" ? input : input.toString()).split("?")[0];
      return json(data[path] ?? []);
    }),
  );
}

function renderPage() {
  render(
    <MemoryRouter>
      <Dashboard />
    </MemoryRouter>,
  );
}

// StatTile: label span -> label row -> tile.
function tile(label: string): HTMLElement {
  return screen.getByText(label, { selector: "span" }).parentElement!.parentElement!;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Dashboard", () => {
  it("renders the four summary tiles with the right numbers", async () => {
    mockApi({
      "/api/v1/nodes": mixedNodes,
      "/api/v1/artifacts": mixedArtifacts,
      "/api/v1/groups": [{ id: "g1", name: "edge", description: "" }],
      "/api/v1/deployments": deployments,
      "/api/v1/extensions": mixedExtensions,
    });
    renderPage();

    await screen.findByText("Needs attention");
    const fleet = tile("Fleet");
    expect(fleet).toHaveTextContent("2 / 4 online");
    expect(within(fleet).getByRole("img")).toHaveAttribute("aria-label", "2 online, 1 offline, 1 pending");

    const builds = tile("Builds");
    expect(builds).toHaveTextContent("1 building");
    expect(builds).toHaveTextContent("kiosk");
    expect(builds).toHaveTextContent("1 failed in the last 24 h");

    const deploys = await screen.findByText("bmc-rack-2");
    expect(tile("Deployments")).toContainElement(deploys);
    expect(tile("Deployments")).toHaveTextContent("1 running");

    await screen.findByText("2 ready");
    const exts = tile("Extensions");
    expect(exts).toHaveTextContent("2 ready");
    expect(exts).toHaveTextContent("1 building");
    expect(exts).toHaveTextContent("1 error");

    expect(screen.queryByText("Everything looks healthy")).not.toBeInTheDocument();
    expect(screen.getByText("edge-3 is offline")).toBeInTheDocument();
  });

  it("says everything looks healthy for a healthy fleet", async () => {
    mockApi({
      "/api/v1/nodes": healthyNodes,
      "/api/v1/artifacts": [readyArtifact],
      "/api/v1/groups": [{ id: "g1", name: "edge", description: "" }],
    });
    renderPage();
    expect(await screen.findByText("Everything looks healthy")).toBeInTheDocument();
    expect(tile("Fleet")).toHaveTextContent("2 / 2 online");
  });

  it("hides node events when the Builds filter is chosen", async () => {
    mockApi({
      "/api/v1/nodes": healthyNodes,
      "/api/v1/artifacts": [readyArtifact],
    });
    renderPage();
    await screen.findByText("Node registered: edge-1");
    expect(screen.getByText("Build ready: base")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Builds" }));
    expect(screen.queryByText("Node registered: edge-1")).not.toBeInTheDocument();
    expect(screen.getByText("Build ready: base")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Builds" })).toHaveAttribute("aria-pressed", "true");

    fireEvent.click(screen.getByRole("button", { name: "Nodes" }));
    expect(screen.getByText("Node registered: edge-1")).toBeInTheDocument();
    expect(screen.queryByText("Build ready: base")).not.toBeInTheDocument();
  });
});
