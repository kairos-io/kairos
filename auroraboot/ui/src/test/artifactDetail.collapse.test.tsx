import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { ArtifactDetail } from "@/pages/ArtifactDetail";
import { getArtifact, getArtifactLogs, type Artifact } from "@/api/artifacts";

const ARTIFACT_ID = "a5ec6329-e099-4720-97cf-aef72fc961f7";

function readyArtifact(): Artifact {
  return {
    id: ARTIFACT_ID,
    name: "test-build",
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
    uki: false,
    fips: false,
    trustedBoot: false,
    autoInstall: false,
    registerAuroraBoot: false,
    artifacts: ["kairos.iso"],
    createdAt: "2026-09-10T10:00:00Z",
    updatedAt: "2026-09-10T10:05:00Z",
  };
}

vi.mock("@/api/artifacts", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/artifacts")>();
  return {
    ...actual,
    getArtifact: vi.fn(),
    getArtifactLogs: vi.fn(),
    cancelArtifact: vi.fn().mockResolvedValue(undefined),
    deleteArtifact: vi.fn().mockResolvedValue(undefined),
    updateArtifact: vi.fn().mockResolvedValue(undefined),
  };
});

vi.mock("@/api/groups", () => ({
  listGroups: vi.fn().mockResolvedValue([]),
}));

// The real hook opens a WebSocket; jsdom has none and the live log stream is
// not what is under test here.
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

// Both sections start collapsed, so the collapse control is found by the
// accessible name it carries in that state. Locating it by name keeps the
// assertions below about where the control sits, not about how it was found.
function expandControl(name: RegExp): HTMLElement {
  return screen.getByRole("button", { name });
}

describe("ArtifactDetail collapse affordances (kairos#4598)", () => {
  beforeEach(() => {
    vi.mocked(getArtifact).mockResolvedValue(readyArtifact());
    vi.mocked(getArtifactLogs).mockResolvedValue("building...\ndone\n");
  });

  it("puts the build-logs chevron after the copy and download actions", async () => {
    await renderPage();

    const chevron = expandControl(/^Expand logs$/);
    const copy = expandControl(/^Copy logs$/);
    const download = expandControl(/^Download \.log$/);

    // Same toolbar row, so "before" and "after" are meaningful.
    expect(chevron.parentElement).toBe(copy.parentElement);
    expect(chevron.parentElement).toBe(download.parentElement);

    // DOCUMENT_POSITION_FOLLOWING == 4: the chevron follows both actions and
    // is the last control in the group. Before the fix it sat at the head of
    // the left-hand group while the Configuration chevron sat at the end of
    // its row, so the same control appeared at opposite ends of two stacked
    // sections. Moving it here puts it two siblings from the auto-scroll
    // arrow, which is what the divider asserted below is for.
    expect(copy.compareDocumentPosition(chevron) & 4).toBe(4);
    expect(download.compareDocumentPosition(chevron) & 4).toBe(4);
    expect(chevron.parentElement?.lastElementChild).toBe(chevron);
  });

  it("separates the build-logs chevron from the neighbouring log actions", async () => {
    await renderPage();

    // The divider is what tells a reader the chevron is not a third icon
    // action. Without it the fix is only a reordering. px-2 is the other
    // half: with the row at px-4 it lines the glyph up with the px-6
    // Configuration header above, which is the misalignment kairos#4598
    // reported. Neither can revert without a red spec.
    expect(expandControl(/^Expand logs$/).className).toContain("border-l");
    expect(expandControl(/^Expand logs$/).className).toContain("px-2");
  });

  it("keeps the configuration chevron the last element of its header row", async () => {
    await renderPage();

    const header = expandControl(/^Expand configuration$/);
    const chevron = header.lastElementChild;
    expect(chevron?.tagName.toLowerCase()).toBe("svg");
    expect(chevron?.getAttribute("class")).toContain("h-4");
  });

  it("reports collapsed state to assistive tech on both sections", async () => {
    await renderPage();

    for (const [collapsed, expanded] of [
      [/^Expand logs$/, /^Collapse logs$/],
      [/^Expand configuration$/, /^Collapse configuration$/],
    ] as const) {
      const control = expandControl(collapsed);
      expect(control.getAttribute("aria-expanded")).toBe("false");

      fireEvent.click(control);
      const open = expandControl(expanded);
      expect(open.getAttribute("aria-expanded")).toBe("true");
    }
  });
});
