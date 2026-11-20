import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { Nodes } from "@/pages/Nodes";
import { Toaster } from "@/components/ui/toaster";

function makeNode(id: string, hostname: string) {
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
  };
}

const nodes = [
  makeNode("id-milan", "pos-milan-01"),
  makeNode("id-rome", "pos-rome-01"),
  makeNode("id-lab", "lab-01"),
];

type Sent = {
  selector: { groupID?: string; labels?: Record<string, string>; nodeIDs?: string[] };
  command: string;
  rebootTimeoutSeconds?: number;
  stopOnFailure?: boolean;
};

function mockFetch(opts: { failCommand?: boolean } = {}) {
  const sent: Sent[] = [];
  const json = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    const method = (init?.method ?? "GET").toUpperCase();
    if (url.startsWith("/api/v1/nodes/commands") && method === "POST") {
      sent.push(JSON.parse(String(init?.body)));
      if (opts.failCommand) return json({ error: "boom" }, 500);
      return json([]);
    }
    if (url.startsWith("/api/v1/nodes")) return json(nodes);
    return json([]);
  });
  return { fn, sent };
}

beforeEach(() => {
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function renderNodes() {
  return render(
    <MemoryRouter>
      <Nodes />
      <Toaster />
    </MemoryRouter>,
  );
}

async function chooseRebootAndSubmit() {
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: /Reboot/ }));
  await act(async () => {
    fireEvent.click(within(dialog).getByRole("button", { name: /^Reboot \d+ nodes$/ }));
  });
}

describe("Nodes bulk command", () => {
  it("targets only the nodes matching the hostname search", async () => {
    const m = mockFetch();
    vi.stubGlobal("fetch", m.fn);
    renderNodes();

    expect(await screen.findByText("lab-01")).toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText("Search by hostname..."), {
      target: { value: "pos" },
    });

    const button = await screen.findByRole("button", { name: "Send Command to 2 nodes" });
    fireEvent.click(button);
    await chooseRebootAndSubmit();

    await waitFor(() => expect(m.sent).toHaveLength(1));
    expect(m.sent[0].command).toBe("reboot");
    expect(m.sent[0].selector.nodeIDs).toEqual(["id-milan", "id-rome"]);
    expect(m.sent[0].selector.groupID).toBeUndefined();
    expect(screen.queryByText("Send to all nodes")).not.toBeInTheDocument();
  });

  it("sends the reboot timeout for a single node", async () => {
    const m = mockFetch();
    vi.stubGlobal("fetch", m.fn);
    renderNodes();

    expect(await screen.findByText("lab-01")).toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText("Search by hostname..."), {
      target: { value: "lab" },
    });
    fireEvent.click(await screen.findByRole("button", { name: "Send Command to 1 node" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /Reboot/ }));
    fireEvent.change(within(dialog).getByLabelText("Wait for a node to come back"), {
      target: { value: "30" },
    });
    await act(async () => {
      fireEvent.click(within(dialog).getByRole("button", { name: "Send reboot" }));
    });

    await waitFor(() => expect(m.sent).toHaveLength(1));
    expect(m.sent[0].selector.nodeIDs).toEqual(["id-lab"]);
    expect(m.sent[0].rebootTimeoutSeconds).toBe(1800);
    expect(m.sent[0].stopOnFailure).toBe(true);
  });

  it("asks for confirmation when no filter is set and then sends every node", async () => {
    const m = mockFetch();
    vi.stubGlobal("fetch", m.fn);
    renderNodes();

    fireEvent.click(await screen.findByRole("button", { name: "Send Command to 3 nodes" }));
    await chooseRebootAndSubmit();

    expect(await screen.findByText("Send to all nodes")).toBeInTheDocument();
    expect(screen.getByText("Send reboot to all 3 nodes?")).toBeInTheDocument();
    expect(m.sent).toHaveLength(0);

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Send to all" }));
    });

    await waitFor(() => expect(m.sent).toHaveLength(1));
    expect(m.sent[0].selector.nodeIDs).toEqual(["id-milan", "id-rome", "id-lab"]);
    expect(m.sent[0].selector.groupID).toBeUndefined();
  });

  it("shows an error toast when the bulk command fails", async () => {
    const m = mockFetch({ failCommand: true });
    vi.stubGlobal("fetch", m.fn);
    renderNodes();

    expect(await screen.findByText("lab-01")).toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText("Search by hostname..."), {
      target: { value: "pos" },
    });
    fireEvent.click(await screen.findByRole("button", { name: "Send Command to 2 nodes" }));
    await chooseRebootAndSubmit();

    await waitFor(() => expect(m.sent).toHaveLength(1));
    expect(await screen.findByText("Failed to send command: boom")).toBeInTheDocument();
  });
});
