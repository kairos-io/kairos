import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { DeployDialog } from "@/components/DeployDialog";
import { ApiError } from "@/api/client";
import { getNetbootStatus, startNetboot } from "@/api/deployments";

vi.mock("@/api/deployments", () => ({
  getNetbootStatus: vi.fn().mockResolvedValue({ running: false, artifactId: "", address: "", port: "" }),
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

vi.mock("@/hooks/useUIWebSocket", () => ({
  useUIWebSocket: () => ({ connected: true }),
}));

function renderDialog(props: {
  files: string[];
  netboot: boolean;
  defaultMethod?: "pxe" | "redfish";
}) {
  return render(
    <MemoryRouter>
      <DeployDialog
        artifactId="artifact-1"
        artifactFiles={props.files}
        hasNetboot={props.netboot}
        defaultMethod={props.defaultMethod}
        onClose={() => {}}
      />
    </MemoryRouter>,
  );
}

describe("DeployDialog methods", () => {
  it("shows a disabled PXE tab with the reason for an ISO-only artifact", () => {
    renderDialog({ files: ["kairos.iso"], netboot: false });

    const pxe = screen.getByRole("tab", { name: /PXE/ });
    expect(pxe).toBeDisabled();
    expect(pxe.closest("[title]")).toHaveAttribute(
      "title",
      "This artifact has no Netboot output. Clone it and enable Netboot.",
    );

    const redfish = screen.getByRole("tab", { name: /RedFish/ });
    expect(redfish).not.toBeDisabled();
    expect(redfish).toHaveAttribute("aria-selected", "true");

    const description = screen.getByText(/Deploy this artifact/);
    expect(description).toHaveTextContent(/RedFish/);
    expect(description).not.toHaveTextContent(/PXE/);
  });

  it("shows a disabled RedFish tab with the reason for a netboot-only artifact", () => {
    renderDialog({ files: ["kairos.squashfs"], netboot: true });

    const redfish = screen.getByRole("tab", { name: /RedFish/ });
    expect(redfish).toBeDisabled();
    expect(redfish.closest("[title]")).toHaveAttribute(
      "title",
      "This artifact has no ISO output. Clone it and enable ISO.",
    );
    const description = screen.getByText(/Deploy this artifact/);
    expect(description).toHaveTextContent(/PXE/);
    expect(description).not.toHaveTextContent(/RedFish/);
  });

  it("opens on the requested method when it is available", () => {
    renderDialog({ files: ["kairos.iso"], netboot: true, defaultMethod: "redfish" });

    expect(screen.getByRole("tab", { name: /RedFish/ })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: /PXE/ })).toHaveAttribute("aria-selected", "false");
  });

  it("ignores a requested method that is not available", () => {
    renderDialog({ files: ["kairos.iso"], netboot: false, defaultMethod: "pxe" });

    expect(screen.getByRole("tab", { name: /RedFish/ })).toHaveAttribute("aria-selected", "true");
  });

  it("shows the reason the server gives when netboot cannot start", async () => {
    vi.mocked(startNetboot).mockRejectedValueOnce(
      new ApiError(400, { error: "artifact has no netboot files" }, ""),
    );
    renderDialog({ files: ["kairos.squashfs"], netboot: true });

    fireEvent.click(await screen.findByRole("button", { name: "Start Netboot" }));

    expect(
      await screen.findByText("Could not start netboot: artifact has no netboot files"),
    ).toBeInTheDocument();
  });

  it("shows the running server when netboot is already running", async () => {
    vi.mocked(startNetboot).mockRejectedValueOnce(
      new ApiError(409, { error: "netboot server is already running" }, ""),
    );
    renderDialog({ files: ["kairos.squashfs"], netboot: true });
    const start = await screen.findByRole("button", { name: "Start Netboot" });
    vi.mocked(getNetbootStatus).mockResolvedValueOnce({
      running: true,
      artifactId: "other-artifact",
      address: "0.0.0.0",
      port: "8090",
    });

    fireEvent.click(start);

    expect(
      await screen.findByText("Could not start netboot: netboot server is already running"),
    ).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Stop Netboot" })).toBeInTheDocument();
  });

  it("clears the netboot error when the operator tries again", async () => {
    vi.mocked(startNetboot)
      .mockRejectedValueOnce(new ApiError(500, { error: "internal error" }, ""))
      .mockResolvedValueOnce(undefined);
    renderDialog({ files: ["kairos.squashfs"], netboot: true });

    fireEvent.click(await screen.findByRole("button", { name: "Start Netboot" }));
    expect(await screen.findByText("Could not start netboot: internal error")).toBeInTheDocument();

    fireEvent.click(await screen.findByRole("button", { name: "Start Netboot" }));
    await waitFor(() =>
      expect(screen.queryByText("Could not start netboot: internal error")).not.toBeInTheDocument(),
    );
  });
});
