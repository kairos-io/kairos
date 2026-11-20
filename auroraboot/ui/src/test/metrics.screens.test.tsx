import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, within, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { Nodes } from "@/pages/Nodes";
import { Dashboard } from "@/pages/Dashboard";
import { Groups } from "@/pages/Groups";
import { needsAttention } from "@/lib/activity";
import type { Node } from "@/api/nodes";

const GiB = 1024 ** 3;

function makeNode(id: string, hostname: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    hostname,
    machineID: `m-${id}`,
    groupID: "g1",
    labels: {},
    phase: "Online",
    osRelease: { KAIROS_VERSION: "edge-2026.09", CPU_COUNT: "4", MEM_TOTAL: "8024512 kB" },
    agentVersion: "v2.31.2",
    lastHeartbeat: new Date().toISOString(),
    createdAt: "2026-09-20T10:00:00Z",
    updatedAt: "2026-09-29T10:00:00Z",
    bootState: "active",
    ...extra,
  };
}

const nodes = [makeNode("n-hot", "ipc-line-a-plc-gw"), makeNode("n-quiet", "pos-rome-01")];

const groups = [{ id: "g1", name: "factory-floor", description: "", createdAt: "", updatedAt: "" }];

// Only n-hot reports metrics: CPU 91 %, memory 50 %, disk 91 %.
const latest = {
  "n-hot": {
    sampledAt: "2026-09-29T09:41:07Z",
    uptimeSeconds: 3600,
    cpu: { usedPercent: 91.2 },
    memory: { totalBytes: 8 * GiB, availableBytes: 4 * GiB },
    disks: [{ label: "COS_PERSISTENT", mount: "/usr/local", totalBytes: 100 * GiB, usedBytes: 91 * GiB }],
  },
};

function json(body: unknown) {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

// withMetrics false makes the metrics endpoint fail, as on a server without it.
function mockApi(withMetrics: boolean) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = (typeof input === "string" ? input : input.toString()).split("?")[0];
      if (url === "/api/v1/metrics/latest") {
        return withMetrics ? json(latest) : new Response("not found", { status: 404 });
      }
      if (url.startsWith("/api/v1/groups")) return json(groups);
      if (url.startsWith("/api/v1/nodes")) return json(nodes);
      if (url.startsWith("/api/v1/artifacts")) return json([{ id: "a1", name: "img", phase: "Ready", message: "", createdAt: "", updatedAt: "" }]);
      return json([]);
    }),
  );
}

function renderAt(ui: React.ReactNode, url = "/") {
  return render(<MemoryRouter initialEntries={[url]}>{ui}</MemoryRouter>);
}

beforeEach(() => {
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("node list with metrics", () => {
  it("replaces Kairos and Hardware with CPU, Memory and Disk", async () => {
    mockApi(true);
    renderAt(<Nodes />, "/nodes");
    const table = within(await screen.findByRole("table"));
    expect(await table.findByRole("columnheader", { name: "CPU" })).toBeInTheDocument();
    expect(table.getByRole("columnheader", { name: "Memory" })).toBeInTheDocument();
    expect(table.getByRole("columnheader", { name: "Disk" })).toBeInTheDocument();
    expect(table.queryByRole("columnheader", { name: "Kairos" })).toBeNull();
    expect(table.queryByRole("columnheader", { name: "Hardware" })).toBeNull();

    const hot = table.getByText("ipc-line-a-plc-gw").closest("tr")!;
    expect(within(hot).getAllByText("91%")).toHaveLength(2);
    expect(within(hot).getByText("50%")).toBeInTheDocument();
    const quiet = table.getByText("pos-rome-01").closest("tr")!;
    expect(within(quiet).getAllByText("—").length).toBeGreaterThanOrEqual(3);
  });

  it("colors tiles by CPU with the danger tone at 91%", async () => {
    mockApi(true);
    renderAt(<Nodes />, "/nodes?view=tiles");
    const colorBy = await screen.findByRole("radiogroup", { name: "Color by" });
    fireEvent.click(within(colorBy).getByRole("radio", { name: "CPU" }));
    const tiles = screen.getAllByTestId("node-tile");
    const hot = tiles.find((t) => t.textContent?.includes("ipc-line-a-plc-gw"))!;
    expect(hot.querySelector("[data-tone]")).toHaveAttribute("data-tone", "danger");
    expect(within(hot).getByText("91%")).toBeInTheDocument();
  });

  it("looks as before without metrics", async () => {
    mockApi(false);
    renderAt(<Nodes />, "/nodes");
    const table = within(await screen.findByRole("table"));
    expect(await table.findByText("ipc-line-a-plc-gw")).toBeInTheDocument();
    expect(table.getByRole("columnheader", { name: "Kairos" })).toBeInTheDocument();
    expect(table.getByRole("columnheader", { name: "Hardware" })).toBeInTheDocument();
    expect(table.queryByRole("columnheader", { name: "CPU" })).toBeNull();
  });

  it("has no Color by control on tiles without metrics", async () => {
    mockApi(false);
    renderAt(<Nodes />, "/nodes?view=tiles");
    await screen.findAllByTestId("node-tile");
    expect(screen.queryByRole("radiogroup", { name: "Color by" })).toBeNull();
  });
});

describe("dashboard with metrics", () => {
  it("shows the Fleet resources card and a disk attention row", async () => {
    mockApi(true);
    renderAt(<Dashboard />);
    expect(await screen.findByText("Fleet resources")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Average CPU 91%" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Average memory 50%" })).toBeInTheDocument();
    expect(screen.getByText("ipc-line-a-plc-gw disk at 91%")).toBeInTheDocument();
    expect(screen.getByText("ipc-line-a-plc-gw CPU at 91%")).toBeInTheDocument();
  });

  it("shows no Fleet resources card without metrics", async () => {
    mockApi(false);
    renderAt(<Dashboard />);
    expect(await screen.findByText("Needs attention")).toBeInTheDocument();
    expect(screen.queryByText("Fleet resources")).toBeNull();
    expect(screen.queryByText(/disk at/)).toBeNull();
  });
});

describe("groups board with metrics", () => {
  it("shows three bars on a card with metrics", async () => {
    mockApi(true);
    renderAt(<Groups />, "/groups");
    const card = await screen.findByRole("button", { name: "ipc-line-a-plc-gw" });
    const meters = await within(card).findAllByRole("meter");
    expect(meters.map((m) => m.getAttribute("aria-label"))).toEqual(["CPU", "Memory", "Disk"]);
    expect(meters[0]).toHaveAttribute("aria-valuenow", "91");
    const quiet = screen.getByRole("button", { name: "pos-rome-01" });
    expect(within(quiet).queryAllByRole("meter")).toHaveLength(0);
  });

  it("shows no bars without metrics", async () => {
    mockApi(false);
    renderAt(<Groups />, "/groups");
    await screen.findByRole("button", { name: "ipc-line-a-plc-gw" });
    expect(screen.queryAllByRole("meter")).toHaveLength(0);
  });
});

describe("needsAttention with metrics", () => {
  const n = makeNode("n-hot", "ipc-line-a-plc-gw") as unknown as Node;

  it("adds rows at 90% and not at 89%", () => {
    const at = (cpu: number, disk: number) =>
      needsAttention({
        nodes: [n],
        artifacts: [],
        extensions: [],
        metrics: {
          "n-hot": {
            sampledAt: "",
            cpu: { usedPercent: cpu },
            disks: [{ mount: "/", totalBytes: 100, usedBytes: disk }],
          },
        },
      }).map((r) => r.kind);
    expect(at(90, 90)).toEqual(["cpu", "disk"]);
    expect(at(89, 89)).toEqual([]);
  });

  it("is unchanged without metrics", () => {
    expect(needsAttention({ nodes: [n], artifacts: [], extensions: [] })).toEqual([]);
  });
});
