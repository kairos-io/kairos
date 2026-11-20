import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";

import { DeployDialog } from "@/components/DeployDialog";
import { type NetbootStatus, getNetbootStatus } from "@/api/deployments";

vi.mock("@/api/deployments", () => ({
  getNetbootStatus: vi.fn(),
  getNetbootLogs: vi.fn().mockResolvedValue(""),
  startNetboot: vi.fn().mockResolvedValue(undefined),
  stopNetboot: vi.fn().mockResolvedValue(undefined),
  listBMCTargets: vi.fn().mockResolvedValue([]),
  createBMCTarget: vi.fn().mockResolvedValue(undefined),
  inspectHardware: vi.fn().mockResolvedValue(undefined),
  deployRedfish: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@/api/redfish", () => ({
  listQuirkProfiles: vi.fn().mockResolvedValue([]),
}));

function runningStatus(overrides: Partial<NetbootStatus> = {}): NetbootStatus {
  return {
    running: true,
    artifactId: "a5ec6329-e099-4720-97cf-aef72fc961f7",
    address: "0.0.0.0",
    port: "8090",
    ...overrides,
  };
}

function renderDialog() {
  return render(
    <DeployDialog
      artifactId="a5ec6329-e099-4720-97cf-aef72fc961f7"
      artifactFiles={["kairos.squashfs"]}
      hasNetboot={true}
      onClose={() => {}}
    />,
  );
}

describe("DeployDialog netboot address", () => {
  beforeEach(() => {
    vi.mocked(getNetbootStatus).mockReset();
  });

  it("shows the advertised host, not the wildcard bind address", async () => {
    vi.mocked(getNetbootStatus).mockResolvedValue(
      runningStatus({ advertisedAddress: "fleet.home.arpa" }),
    );

    renderDialog();

    await waitFor(() => {
      expect(screen.getByText("fleet.home.arpa:8090")).toBeTruthy();
    });
    expect(screen.queryByText("0.0.0.0:8090")).toBeNull();
  });

  it("shows an advertised IP when there is no configured URL", async () => {
    vi.mocked(getNetbootStatus).mockResolvedValue(
      runningStatus({ advertisedAddress: "10.0.0.5" }),
    );

    renderDialog();

    await waitFor(() => {
      expect(screen.getByText("10.0.0.5:8090")).toBeTruthy();
    });
  });

  it("falls back to the bind address against a server that does not send one", async () => {
    // A UI newer than its backend: advertisedAddress is absent, so the old
    // value is all there is to show.
    vi.mocked(getNetbootStatus).mockResolvedValue(runningStatus());

    renderDialog();

    await waitFor(() => {
      expect(screen.getByText("0.0.0.0:8090")).toBeTruthy();
    });
  });
});
