import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { ArtifactDetail } from "@/pages/ArtifactDetail";
import { createDownloadLink, getArtifact, getArtifactLogs, type Artifact } from "@/api/artifacts";
import { toast } from "@/hooks/useToast";

const ARTIFACT_ID = "b1c2d3e4-0000-4000-8000-000000000001";

function readyArtifact(over: Partial<Artifact> = {}): Artifact {
  return {
    id: ARTIFACT_ID,
    name: "edge-build",
    phase: "Ready",
    message: "",
    baseImage: "ubuntu:24.04",
    kairosVersion: "v3.5.0",
    model: "generic",
    arch: "amd64",
    iso: true,
    cloudImage: false,
    netboot: false,
    rawDisk: false,
    tar: false,
    gce: false,
    vhd: false,
    maas: false,
    fips: false,
    trustedBoot: false,
    autoInstall: false,
    registerAuroraBoot: false,
    artifacts: ["out/kairos.iso", "out/kairos.sha256"],
    createdAt: "2026-09-10T10:00:00Z",
    updatedAt: "2026-09-10T10:05:00Z",
    ...over,
  };
}

vi.mock("@/api/artifacts", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/artifacts")>();
  return {
    ...actual,
    getArtifact: vi.fn(),
    getArtifactLogs: vi.fn(),
    createDownloadLink: vi.fn(),
    cancelArtifact: vi.fn().mockResolvedValue(undefined),
    deleteArtifact: vi.fn().mockResolvedValue(undefined),
    updateArtifact: vi.fn().mockResolvedValue(undefined),
  };
});

vi.mock("@/hooks/useToast", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useToast")>()),
  toast: vi.fn(),
}));

vi.mock("@/api/groups", () => ({
  listGroups: vi.fn().mockResolvedValue([]),
}));

vi.mock("@/hooks/useUIWebSocket", () => ({
  useUIWebSocket: () => ({ connected: true }),
}));

async function renderPage() {
  render(
    <MemoryRouter initialEntries={[`/artifacts/${ARTIFACT_ID}`]}>
      <Routes>
        <Route path="/artifacts/:id" element={<ArtifactDetail />} />
      </Routes>
    </MemoryRouter>,
  );
  await waitFor(() => expect(screen.getByText("Build logs")).toBeTruthy());
}

function headerActions(): HTMLElement {
  return screen.getByRole("group", { name: "Page actions" });
}

describe("ArtifactDetail header, downloads and configuration", () => {
  const writeText = vi.fn().mockResolvedValue(undefined);

  beforeEach(() => {
    vi.mocked(getArtifact).mockResolvedValue(readyArtifact());
    vi.mocked(getArtifactLogs).mockResolvedValue("done\n");
    writeText.mockClear();
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("has exactly one primary button in the header, and it deploys", async () => {
    await renderPage();

    // The split button's menu trigger shares the primary fill; it is part of
    // the same control, so it is left out by its aria-haspopup.
    const primaries = within(headerActions())
      .getAllByRole("button")
      .filter((b) => b.classList.contains("bg-primary") && !b.hasAttribute("aria-haspopup"));
    expect(primaries).toHaveLength(1);
    expect(primaries[0]).toHaveTextContent("Deploy");
    expect(within(headerActions()).getByRole("button", { name: "Clone" })).toHaveClass("border");
  });

  it("puts Delete, Export config and Save as template in the ⋯ menu", async () => {
    await renderPage();

    expect(within(headerActions()).queryByRole("button", { name: /delete/i })).toBeNull();
    fireEvent.keyDown(screen.getByRole("button", { name: "More actions" }), { key: "Enter" });

    expect(await screen.findByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Export config" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Save as template" })).toBeInTheDocument();
  });

  it("shows the saved chip and offers to remove the template when saved", async () => {
    vi.mocked(getArtifact).mockResolvedValue(readyArtifact({ saved: true }));
    await renderPage();

    expect(screen.getByText("Saved as template")).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("button", { name: "More actions" }), { key: "Enter" });
    expect(await screen.findByRole("menuitem", { name: "Remove template" })).toBeInTheDocument();
  });

  it("lists deploy methods in the split button menu and opens the deploy dialog", async () => {
    await renderPage();

    fireEvent.keyDown(screen.getByRole("button", { name: "Deploy methods" }), { key: "Enter" });
    const pxe = await screen.findByRole("menuitem", { name: /PXE/ });
    expect(pxe).toHaveAttribute("data-disabled");
    const redfish = screen.getByRole("menuitem", { name: /RedFish/ });
    expect(redfish).not.toHaveAttribute("data-disabled");
  });

  it("opens the deploy dialog on the RedFish tab from the RedFish menu item", async () => {
    vi.mocked(getArtifact).mockResolvedValue(readyArtifact({ netboot: true }));
    await renderPage();

    fireEvent.keyDown(screen.getByRole("button", { name: "Deploy methods" }), { key: "Enter" });
    fireEvent.click(await screen.findByRole("menuitem", { name: /RedFish/ }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("tab", { name: /RedFish/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(within(dialog).getByRole("tab", { name: /PXE/ })).toHaveAttribute(
      "aria-selected",
      "false",
    );
  });

  it("has no Deploy button in the success banner", async () => {
    await renderPage();

    const banner = screen.getByText("Build succeeded").closest("[data-slot='success-banner']") as HTMLElement;
    expect(banner).not.toBeNull();
    expect(within(banner).queryByRole("button", { name: /deploy/i })).toBeNull();
  });

  it("copies a signed link that expires and says when", async () => {
    vi.mocked(createDownloadLink).mockResolvedValue({
      path: `/api/v1/artifacts/${ARTIFACT_ID}/download/kairos.iso?exp=1760275800&sig=abc`,
      expiresAt: "2026-10-12T13:30:00Z",
    });
    await renderPage();

    const buttons = screen.getAllByRole("button", { name: "Copy link" });
    expect(buttons[0].getAttribute("title")).toMatch(/1 hour/);
    fireEvent.click(buttons[0]);

    await waitFor(() => expect(writeText).toHaveBeenCalledTimes(1));
    expect(createDownloadLink).toHaveBeenCalledWith(ARTIFACT_ID, "kairos.iso");
    const url = writeText.mock.calls[0][0] as string;
    expect(url).toBe(`${window.location.origin}/api/v1/artifacts/${ARTIFACT_ID}/download/kairos.iso?exp=1760275800&sig=abc`);
    expect(url).not.toContain("token=");
    const at = new Date("2026-10-12T13:30:00Z").toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    expect(toast).toHaveBeenCalledWith(`Link copied, expires in 1 hour (at ${at})`, "success");
  });

  it("links downloads and the container image without credentials", async () => {
    vi.mocked(getArtifact).mockResolvedValue(readyArtifact({ containerImage: "auroraboot-kairos:x" }));
    await renderPage();
    const hrefs = screen.getAllByRole("link").map((a) => a.getAttribute("href") ?? "");
    expect(hrefs).toContain(`/api/v1/artifacts/${ARTIFACT_ID}/download/kairos.iso`);
    expect(hrefs).toContain(`/api/v1/artifacts/${ARTIFACT_ID}/image`);
    expect(hrefs.filter((h) => h.includes("token="))).toEqual([]);
  });

  it("renders the build summary in the configuration section", async () => {
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Expand configuration" }));
    const title = screen.getByText("Build summary");
    // CardTitle -> CardHeader -> Card
    const summary = title.parentElement!.parentElement!;
    expect(within(summary).getByText("Base")).toBeInTheDocument();
    expect(within(summary).getByText("Formats").nextElementSibling).toHaveTextContent("ISO");
  });

  it("keeps the failure banner with Clone & retry and Jump to logs", async () => {
    vi.mocked(getArtifact).mockResolvedValue(
      readyArtifact({ phase: "Error", message: "boom", artifacts: [] }),
    );
    await renderPage();

    expect(screen.getByText("Build failed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Clone & retry/ })).toBeInTheDocument();
    expect(screen.getByText("Jump to logs")).toBeInTheDocument();
  });
});
