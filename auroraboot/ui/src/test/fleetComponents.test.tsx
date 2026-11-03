import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { Server } from "lucide-react";

import type { Node } from "@/api/nodes";
import { memGiB, cpuCount, imageVersion, nodeAddress, hasBootIssue } from "@/lib/nodeInfo";
import { StatusDot } from "@/components/fleet/StatusDot";
import { StackBar } from "@/components/fleet/StackBar";
import { StatTile } from "@/components/fleet/StatTile";
import { EmptyState } from "@/components/fleet/EmptyState";
import { PageHeader } from "@/components/PageHeader";

function node(overrides: Partial<Node> = {}): Node {
  return {
    id: "n1",
    hostname: "edge-1",
    machineID: "m1",
    groupID: null,
    labels: {},
    phase: "Online",
    osRelease: null,
    agentVersion: "",
    lastHeartbeat: null,
    createdAt: "",
    updatedAt: "",
    ...overrides,
  };
}

describe("nodeInfo", () => {
  it("parses MEM_TOTAL into GiB", () => {
    expect(memGiB(node({ osRelease: { MEM_TOTAL: "8024512 kB" } }))).toBe("7.7 GiB");
  });

  it("returns null when memory is unknown or malformed", () => {
    expect(memGiB(node())).toBeNull();
    expect(memGiB(node({ osRelease: { MEM_TOTAL: "lots" } }))).toBeNull();
  });

  it("reads the CPU count", () => {
    expect(cpuCount(node({ osRelease: { CPU_COUNT: "4" } }))).toBe(4);
    expect(cpuCount(node())).toBeNull();
    expect(cpuCount(node({ osRelease: { CPU_COUNT: "x" } }))).toBeNull();
  });

  it("reads the image version", () => {
    expect(imageVersion(node({ osRelease: { KAIROS_VERSION: "v3.4.0" } }))).toBe("v3.4.0");
    expect(imageVersion(node())).toBe("");
  });

  it("prefers remoteIP, then the first address, then empty", () => {
    const addresses = [
      { type: "ipv4", address: "10.0.0.5" },
      { type: "ipv4", address: "10.0.0.6" },
    ];
    expect(nodeAddress(node({ remoteIP: "192.168.1.9", addresses }))).toBe("192.168.1.9");
    expect(nodeAddress(node({ addresses }))).toBe("10.0.0.5");
    expect(nodeAddress(node({ addresses: [] }))).toBe("");
    expect(nodeAddress(node())).toBe("");
  });

  it("flags recovery and passive boots", () => {
    expect(hasBootIssue(node({ bootState: "recovery" }))).toBe(true);
    expect(hasBootIssue(node({ bootState: "Passive" }))).toBe(true);
    expect(hasBootIssue(node({ bootState: "active" }))).toBe(false);
    expect(hasBootIssue(node())).toBe(false);
  });
});

describe("StatusDot", () => {
  it("renders a dot in the tone color", () => {
    const { container } = render(<StatusDot tone="success" className="extra" />);
    const dot = container.firstElementChild as HTMLElement;
    expect(dot.className).toContain("bg-success");
    expect(dot.className).toContain("extra");
    expect(dot).toHaveAttribute("aria-hidden", "true");
  });
});

describe("StackBar", () => {
  it("labels the counts and skips zero segments", () => {
    const { container } = render(
      <StackBar
        counts={[
          { tone: "success", count: 8, label: "online" },
          { tone: "warning", count: 0, label: "pending" },
          { tone: "danger", count: 3, label: "offline" },
        ]}
      />,
    );
    const bar = screen.getByRole("img");
    expect(bar).toHaveAttribute("aria-label", "8 online, 3 offline");
    const segments = container.querySelectorAll("[data-tone]");
    expect(segments).toHaveLength(2);
    expect((segments[0] as HTMLElement).className).toContain("bg-success");
    expect((segments[1] as HTMLElement).className).toContain("bg-danger");
  });

  it("renders a neutral track when every count is zero", () => {
    const { container } = render(
      <StackBar counts={[{ tone: "success", count: 0, label: "online" }]} />,
    );
    const bar = screen.getByRole("img");
    expect(bar).toHaveAttribute("aria-label", "None");
    expect(container.querySelectorAll("[data-tone]")).toHaveLength(0);
    expect(bar.className).toContain("bg-muted");
  });
});

describe("StatTile", () => {
  it("renders the label, value, sub-line and children", () => {
    render(
      <StatTile label="Nodes" icon={Server} value={11} sub="8 online" tone="success">
        <span>child</span>
      </StatTile>,
    );
    expect(screen.getByText("Nodes")).toBeInTheDocument();
    expect(screen.getByText("11")).toBeInTheDocument();
    expect(screen.getByText("8 online")).toBeInTheDocument();
    expect(screen.getByText("child")).toBeInTheDocument();
  });
});

describe("EmptyState", () => {
  it("renders the title, text and action", () => {
    render(
      <EmptyState icon={Server} title="No nodes yet" text="Boot a machine to add one." action={<button>Add node</button>} />,
    );
    expect(screen.getByText("No nodes yet")).toBeInTheDocument();
    expect(screen.getByText("Boot a machine to add one.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add node" })).toBeInTheDocument();
  });
});

describe("PageHeader", () => {
  it("renders breadcrumb links, status and meta", () => {
    render(
      <MemoryRouter>
        <PageHeader
          title="edge-1"
          description="A node"
          breadcrumb={[{ label: "Nodes", to: "/nodes" }, { label: "edge-1" }]}
          status={<span>Online</span>}
          meta={<span>10.0.0.5</span>}
        >
          <button>Send command</button>
        </PageHeader>
      </MemoryRouter>,
    );
    const nav = screen.getByRole("navigation", { name: "Breadcrumb" });
    const link = screen.getByRole("link", { name: "Nodes" });
    expect(nav).toContainElement(link);
    expect(link).toHaveAttribute("href", "/nodes");
    expect(nav).toHaveTextContent("edge-1");
    expect(screen.getByRole("heading", { level: 1, name: "edge-1" })).toBeInTheDocument();
    expect(screen.getByText("Online")).toBeInTheDocument();
    expect(screen.getByText("10.0.0.5")).toBeInTheDocument();
    expect(screen.getByText("A node")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send command" })).toBeInTheDocument();
  });

  it("renders without a breadcrumb", () => {
    render(<PageHeader title="Nodes" />);
    expect(screen.queryByRole("navigation")).toBeNull();
    expect(screen.getByRole("heading", { level: 1, name: "Nodes" })).toBeInTheDocument();
  });
});
