import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { NodeDetail } from "@/pages/NodeDetail";
import type { NodeMetrics } from "@/api/metrics";
import { sustainedHighCpu } from "@/lib/metrics";

const NODE_ID = "n-metrics-1";

const node = {
  id: NODE_ID,
  hostname: "edge-01",
  machineID: "m1",
  groupID: null,
  labels: {},
  phase: "online",
  osRelease: { CPU_COUNT: "16", ARCH: "amd64" },
  agentVersion: "v4.3.0",
  lastHeartbeat: "2026-09-29T09:41:00Z",
  remoteIP: "10.0.0.5",
  bootState: "active",
  createdAt: "2026-08-20T10:00:00Z",
  updatedAt: "2026-09-29T09:41:00Z",
};

function sample(cpu: number): NodeMetrics {
  return {
    sampledAt: "2026-09-29T09:41:07Z",
    uptimeSeconds: 45 * 86400 + 2 * 3600 + 120,
    load: [13.9, 12.1, 10.4],
    cpu: { usedPercent: cpu },
    memory: { totalBytes: 33501757440, availableBytes: 7086696448 },
    disks: [
      { label: "COS_PERSISTENT", mount: "/usr/local", totalBytes: 257698037760, usedBytes: 169651208192 },
    ],
    temperatureC: 71,
  };
}

function mockFetch(metrics: unknown | null) {
  const json = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url === `/api/v1/nodes/${NODE_ID}`) return json(node);
    if (url === `/api/v1/nodes/${NODE_ID}/metrics`) {
      return metrics === null ? json({ error: "not found" }, 404) : json(metrics);
    }
    if (url.startsWith("/api/v1/batches")) return json({ batches: [], total: 0 });
    return json([]);
  });
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
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function renderPage() {
  return render(
    <MemoryRouter initialEntries={[`/nodes/${NODE_ID}`]}>
      <Routes>
        <Route path="nodes/:id" element={<NodeDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("NodeDetail resources card", () => {
  it("shows CPU, disk and uptime from the latest sample", async () => {
    const s = sample(87.2);
    vi.stubGlobal("fetch", mockFetch({ latest: s, samples: [sample(40), s] }));
    renderPage();

    const card = await screen.findByRole("region", { name: "Resources" });
    expect(within(card).getByRole("img", { name: "CPU 87%" })).toBeInTheDocument();
    expect(within(card).getByText("COS_PERSISTENT")).toBeInTheDocument();
    expect(within(card).getByText("66%")).toBeInTheDocument();
    expect(within(card).getByText("45d 2h")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Hardware" })).not.toBeInTheDocument();
    expect(screen.queryByText(/CPU above 85%/)).not.toBeInTheDocument();
  });

  it("warns when the last 10 samples are at 85% or more", async () => {
    const samples = Array.from({ length: 10 }, () => sample(90));
    vi.stubGlobal("fetch", mockFetch({ latest: samples[9], samples }));
    renderPage();

    await screen.findByRole("region", { name: "Resources" });
    expect(screen.getByRole("alert")).toHaveTextContent(/CPU above 85%/);
  });

  it("shows the Hardware card and no gauge without metrics", async () => {
    vi.stubGlobal("fetch", mockFetch(null));
    renderPage();

    expect(await screen.findByRole("region", { name: "Hardware" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Resources" })).not.toBeInTheDocument();
    expect(screen.queryByRole("img", { name: /CPU/ })).not.toBeInTheDocument();
  });

  it("shows the Hardware card when the metrics endpoint is not mocked", async () => {
    vi.stubGlobal("fetch", mockFetch({ latest: null, samples: [] }));
    renderPage();

    expect(await screen.findByRole("region", { name: "Hardware" })).toBeInTheDocument();
  });
});

describe("sustainedHighCpu", () => {
  it("needs a full window at or above the threshold", () => {
    const high = Array.from({ length: 10 }, () => sample(85));
    expect(sustainedHighCpu(high)).toBe(true);
    expect(sustainedHighCpu(high.slice(1))).toBe(false);
    expect(sustainedHighCpu([sample(50), ...high])).toBe(true);
    expect(sustainedHighCpu([...high.slice(1), sample(84)])).toBe(false);
    expect(sustainedHighCpu([{ sampledAt: "x" }, ...high.slice(1)])).toBe(false);
  });
});
