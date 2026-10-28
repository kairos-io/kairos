import { describe, it, expect } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { NodeTable } from "@/components/NodeTable";
import { nodeArch } from "@/lib/nodeInfo";
import type { Node } from "@/api/nodes";

function makeNode(id: string, hostname: string, osRelease: Record<string, string>): Node {
  return {
    id,
    hostname,
    machineID: `m-${id}`,
    groupID: null,
    labels: {},
    phase: "Online",
    osRelease,
    agentVersion: "v4.4.0",
    lastHeartbeat: "2026-11-19T10:00:00Z",
    createdAt: "2026-11-19T10:00:00Z",
    updatedAt: "2026-11-19T10:00:00Z",
  } as unknown as Node;
}

describe("nodeArch", () => {
  it("reads KAIROS_ARCH from the os-release the agent reports", () => {
    expect(nodeArch(makeNode("a", "a", { KAIROS_ARCH: "riscv64" }))).toBe("riscv64");
  });

  it("prefers ARCH when the agent reports it", () => {
    expect(nodeArch(makeNode("a", "a", { ARCH: "amd64", KAIROS_ARCH: "riscv64" }))).toBe("amd64");
  });

  it("is empty when the agent does not report it", () => {
    expect(nodeArch(makeNode("a", "a", {}))).toBe("");
    expect(nodeArch({ ...makeNode("a", "a", {}), osRelease: null } as unknown as Node)).toBe("");
  });
});

describe("NodeTable architecture column", () => {
  it("shows the architecture next to the image version", () => {
    const nodes = [
      makeNode("n1", "visionfive", { KAIROS_VERSION: "v4.4.0", KAIROS_ARCH: "riscv64" }),
      makeNode("n2", "server", { KAIROS_VERSION: "v4.3.0", KAIROS_ARCH: "amd64" }),
    ];
    render(
      <MemoryRouter>
        <NodeTable nodes={nodes} />
      </MemoryRouter>,
    );
    expect(screen.getByRole("columnheader", { name: "Arch" })).toBeInTheDocument();
    const row = screen.getByText("visionfive").closest("tr") as HTMLElement;
    expect(within(row).getByText("riscv64")).toBeInTheDocument();
    const other = screen.getByText("server").closest("tr") as HTMLElement;
    expect(within(other).getByText("amd64")).toBeInTheDocument();
  });
});
