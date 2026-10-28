import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";

import { Nodes } from "@/pages/Nodes";

function makeNode(id: string, hostname: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    hostname,
    machineID: `m-${id}`,
    groupID: null,
    labels: {},
    phase: "Online",
    osRelease: null,
    agentVersion: "v2.31.2",
    lastHeartbeat: "2026-09-29T10:00:00Z",
    createdAt: "2026-09-20T10:00:00Z",
    updatedAt: "2026-09-29T10:00:00Z",
    ...extra,
  };
}

const nodes = [
  makeNode("id-milan", "pos-milan-01", {
    groupID: "g1",
    labels: { site: "milan", role: "pos" },
    remoteIP: "10.0.0.11",
    osRelease: { KAIROS_VERSION: "edge-2026.09", CPU_COUNT: "4", MEM_TOTAL: "8024512 kB" },
  }),
  makeNode("id-rome", "pos-rome-01", { phase: "Offline", remoteIP: "10.0.0.12" }),
  makeNode("id-lab", "lab-01", { groupID: "g2", bootState: "recovery" }),
];

const groups = [
  { id: "g1", name: "edge-retail", description: "", createdAt: "", updatedAt: "" },
  { id: "g2", name: "lab", description: "", createdAt: "", updatedAt: "" },
];

function mockFetch() {
  const json = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url.startsWith("/api/v1/groups")) return json(groups);
    if (url.startsWith("/api/v1/nodes")) return json(nodes);
    return json([]);
  });
}

// The summary strip also names nodes, so row checks look inside the table.
async function table() {
  return within(await screen.findByRole("table"));
}

function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="search">{loc.search}</div>;
}

function renderAt(url: string) {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Nodes />
      <LocationProbe />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.stubGlobal("fetch", mockFetch());
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Nodes list", () => {
  it("reads the search and grouping from the URL and shows group header rows", async () => {
    renderAt("/nodes?q=pos&groupBy=group");

    const t = await table();
    expect(await t.findByText("pos-milan-01")).toBeInTheDocument();
    expect(t.getByText("pos-rome-01")).toBeInTheDocument();
    expect(t.queryByText("lab-01")).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText("Search by hostname...")).toHaveValue("pos");

    const headers = await screen.findAllByTestId("group-header");
    expect(headers.map((h) => within(h).getByTestId("group-label").textContent)).toEqual([
      "edge-retail",
      "Not in a group",
    ]);
    expect(within(headers[0]).getByText("1 node")).toBeInTheDocument();
  });

  it("removing a filter chip removes it from the URL and keeps other keys", async () => {
    renderAt("/nodes?phase=Offline&view=list");

    const t = await table();
    expect(await t.findByText("pos-rome-01")).toBeInTheDocument();
    expect(t.queryByText("pos-milan-01")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Remove Status filter" }));

    expect(await (await table()).findByText("pos-milan-01")).toBeInTheDocument();
    const search = screen.getByTestId("search").textContent ?? "";
    expect(search).not.toContain("phase");
    expect(search).toContain("view=list");
  });

  it("typing in the search writes q to the URL", async () => {
    renderAt("/nodes");
    expect(await (await table()).findByText("lab-01")).toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText("Search by hostname..."), { target: { value: "10.0.0.12" } });
    expect(screen.getByTestId("search").textContent).toContain("q=10.0.0.12");
    const t = await table();
    expect(t.queryByText("pos-milan-01")).not.toBeInTheDocument();
    expect(t.getByText("pos-rome-01")).toBeInTheDocument();
  });

  it("shows status only for nodes that are not online, and a recovery pill", async () => {
    renderAt("/nodes");
    const t = await table();
    const milanRow = (await t.findByText("pos-milan-01")).closest("tr")!;
    const romeRow = t.getByText("pos-rome-01").closest("tr")!;
    const labRow = t.getByText("lab-01").closest("tr")!;

    expect(within(milanRow).queryByText(/online/i)).not.toBeInTheDocument();
    expect(within(romeRow).getByText("Offline")).toBeInTheDocument();
    expect(within(labRow).getByText("recovery")).toBeInTheDocument();
    expect(within(labRow).queryByText(/online/i)).not.toBeInTheDocument();

    expect(within(milanRow).getByText("edge-2026.09")).toBeInTheDocument();
    expect(within(milanRow).getByText("4 vCPU · 7.7 GiB")).toBeInTheDocument();
    expect(within(milanRow).getByText("10.0.0.11")).toBeInTheDocument();
    expect(within(milanRow).getByText("+1")).toBeInTheDocument();
  });

  it("describes the whole fleet in the summary strip even when filtered", async () => {
    renderAt("/nodes?q=lab");
    expect(await screen.findByRole("img", { name: "2 online, 1 offline" })).toBeInTheDocument();
    const attention = screen.getByRole("region", { name: "Needs attention" });
    expect(within(attention).getByText("pos-rome-01")).toBeInTheDocument();
    expect(within(attention).getByText("lab-01")).toBeInTheDocument();
  });
});
