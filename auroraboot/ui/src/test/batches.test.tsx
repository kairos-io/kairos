import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { Batches } from "@/pages/Batches";
import { commandDetails, commandLabel, targetsText } from "@/lib/batch";
import type { Batch } from "@/api/batches";

const listBatches = vi.fn();
vi.mock("@/api/batches", () => ({
  listBatches: (...a: unknown[]) => listBatches(...a),
}));
const listArtifacts = vi.fn();
vi.mock("@/api/artifacts", () => ({
  listArtifacts: (...a: unknown[]) => listArtifacts(...a),
}));

function makeBatch(extra: Partial<Batch> = {}): Batch {
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
    counts: { done: 3, failed: 1, waiting: 0 },
    ...extra,
  };
}

function renderList() {
  return render(
    <MemoryRouter>
      <Batches />
    </MemoryRouter>,
  );
}

describe("Batches", () => {
  it("shows what each batch runs and the nodes it targets", async () => {
    listArtifacts.mockResolvedValue([{ id: "a1", name: "edge v4.4" }]);
    listBatches.mockResolvedValue({
      total: 2,
      batches: [
        makeBatch({
          args: { source: "oci:quay.io/kairos/ubuntu:v4.4.0" },
          selection: "3 selected nodes",
          hostnames: ["host-a", "host-b", "host-c"],
          counts: { done: 3 },
        }),
        makeBatch({
          id: "b2",
          args: { source: "artifact:a1" },
          hostnames: ["host-1", "host-2", "host-3", "host-4", "host-5"],
          counts: { done: 6, waiting: 2 },
        }),
      ],
    });
    renderList();

    expect(await screen.findByText("quay.io/kairos/ubuntu:v4.4.0")).toBeInTheDocument();
    expect(screen.getByText("host-a, host-b, host-c")).toBeInTheDocument();
    expect(screen.getByText("3 selected nodes")).toBeInTheDocument();
    expect(await screen.findByText("artifact edge v4.4")).toBeInTheDocument();
    expect(screen.getByText("host-1, host-2, host-3, host-4, host-5 and 3 more")).toBeInTheDocument();
  });

  it("lists batches with their non-zero counts and links to them", async () => {
    listArtifacts.mockResolvedValue([]);
    listBatches.mockResolvedValue({
      total: 1,
      batches: [
        {
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
          counts: { done: 3, failed: 1, waiting: 0 },
        },
      ],
    });
    render(
      <MemoryRouter>
        <Batches />
      </MemoryRouter>,
    );
    expect(await screen.findByText("3 done, 1 failed")).toBeInTheDocument();
    expect(screen.getByText("group edge")).toBeInTheDocument();
    expect(screen.getByText("stopped").className).toContain("danger");
    const link = screen.getByRole("link", { name: /Upgrade/ });
    expect(link).toHaveAttribute("href", "/batches/b1");
    expect(listBatches).toHaveBeenCalledWith({ limit: 50, offset: 0 });
  });

  it("shows a failed fetch as an error instead of the empty state", async () => {
    listArtifacts.mockResolvedValue([]);
    listBatches.mockRejectedValue(new Error("connection refused"));
    render(
      <MemoryRouter>
        <Batches />
      </MemoryRouter>,
    );
    expect(await screen.findByText("Failed to load batches: connection refused")).toBeInTheDocument();
    expect(screen.queryByText("No batches yet")).not.toBeInTheDocument();
  });
});

describe("commandDetails", () => {
  const noArtifacts = () => undefined;

  it("names the image or artifact an upgrade installs", () => {
    expect(commandDetails("upgrade", { source: "oci:quay.io/k:v4" }, noArtifacts)).toEqual([
      { label: "Image", value: "quay.io/k:v4" },
    ]);
    expect(commandDetails("upgrade", { source: "artifact:a1" }, (id) => (id === "a1" ? "edge" : undefined))).toEqual([
      { label: "Artifact", value: "edge" },
    ]);
    expect(commandDetails("upgrade", { source: "artifact:a2" }, noArtifacts)).toEqual([
      { label: "Artifact", value: "a2" },
    ]);
  });

  it("lists the extensions an artifact upgrade installs and leaves out the recovery flag", () => {
    const args = {
      source: "artifact:a1",
      recovery: "true",
      extensions: JSON.stringify([
        { type: "sysext", name: "k3s" },
        { type: "confext", name: "net" },
      ]),
    };
    expect(commandDetails("upgrade-recovery", args, noArtifacts)).toEqual([
      { label: "Artifact", value: "a1" },
      { label: "Extensions", value: "sysext/k3s, confext/net" },
    ]);
  });

  it("shows a shell command, a cloud config and arguments it does not know", () => {
    expect(commandDetails("exec", { command: "uptime" }, noArtifacts)).toEqual([
      { label: "Shell command", value: "uptime" },
    ]);
    expect(commandDetails("apply-cloud-config", { config: "#cloud-config\nusers: []" }, noArtifacts)).toEqual([
      { label: "Cloud config", value: "#cloud-config\nusers: []" },
    ]);
    expect(commandDetails("extension", { action: "install", name: "k3s" }, noArtifacts)).toEqual([
      { label: "action", value: "install" },
      { label: "name", value: "k3s" },
    ]);
    expect(commandDetails("reboot", {}, noArtifacts)).toEqual([]);
  });
});

describe("targetsText", () => {
  it("names the first nodes and counts the rest", () => {
    expect(targetsText(makeBatch({ hostnames: ["a", "b"], counts: { done: 2 } }))).toBe("a, b");
    expect(targetsText(makeBatch({ hostnames: ["a", "b"], counts: { done: 2, waiting: 1 } }))).toBe("a, b and 1 more");
    expect(targetsText(makeBatch({ hostnames: undefined }))).toBe("");
  });
});

describe("commandLabel", () => {
  it("names the commands the agent runs", () => {
    expect(commandLabel("apply-cloud-config")).toBe("Apply config");
    expect(commandLabel("extension")).toBe("Install extension");
  });
});
