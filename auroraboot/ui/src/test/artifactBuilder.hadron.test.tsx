import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";

vi.mock("@/api/artifacts", async () => {
  const actual = await vi.importActual<typeof import("@/api/artifacts")>(
    "@/api/artifacts",
  );
  return {
    ...actual,
    listSecureBootKeySets: vi.fn(async () => []),
    getArtifact: vi.fn(),
    createArtifact: vi.fn(),
    uploadOverlayFiles: vi.fn(),
  };
});

vi.mock("@/api/groups", () => ({
  listGroups: vi.fn(async () => []),
}));

import { ArtifactBuilder } from "@/pages/ArtifactBuilder";
import { getArtifact } from "@/api/artifacts";

// jsdom shim: shadcn's theme provider touches matchMedia on mount, and Radix
// Select portals need pointerEvents to not throw during focus management.
beforeEach(() => {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
  // Silent-stub the hadron catalog fetches so unhandled promise rejections do
  // not surface as noise in the test output.
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("[]", { status: 200 })),
  );
});

function renderBuilder(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/artifacts/new" element={<ArtifactBuilder />} />
      </Routes>
    </MemoryRouter>,
  );
}

// A template click keeps the Base step; the Hadron cards are on the System
// step, and Next needs a name first.
function gotoSystemStep() {
  fireEvent.change(screen.getByPlaceholderText(/Production v4\.0\.3/), {
    target: { value: "edge" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
}

describe("ArtifactBuilder: Hadron peer template", () => {
  it("renders Hadron as the first template tile", async () => {
    renderBuilder("/artifacts/new");

    const tiles = await screen.findAllByText(/auto-kairosified|Start from scratch|Kairos edge OS/i);
    expect(tiles.length).toBeGreaterThan(0);

    const grid = tiles[0].closest("div[class*='grid']");
    expect(grid).not.toBeNull();
    const cardTitles = within(grid as HTMLElement).getAllByText(
      /^(Hadron|Ubuntu|Fedora|openSUSE|Debian|Alpine|Rocky|Custom)/,
    );
    expect(cardTitles[0].textContent).toMatch(/^Hadron/);
    expect(cardTitles[cardTitles.length - 1].textContent).toMatch(/^Custom/);
  });

  it("advances to Configure and shows the Hadron advanced expander alongside the standard cards", async () => {
    renderBuilder("/artifacts/new");

    fireEvent.click(await screen.findByText(/^Hadron v/));
    gotoSystemStep();

    await waitFor(() => {
      expect(
        screen.getByText(/OS bundled with a Kubernetes distribution/i),
      ).toBeInTheDocument();
    });
    expect(screen.getByText(/Minimal OS, cloud-init only/i)).toBeInTheDocument();
    expect(
      screen.getByRole("button", {
        name: /Advanced: firmware, software layers, and version/i,
      }),
    ).toBeInTheDocument();
  });

  it("reveals the Kubernetes card when Standard is picked from a Hadron build", async () => {
    renderBuilder("/artifacts/new");
    fireEvent.click(await screen.findByText(/^Hadron v/));
    gotoSystemStep();

    const standardCopy = await screen.findByText(
      /OS bundled with a Kubernetes distribution/i,
    );
    fireEvent.click(standardCopy.closest("button") as HTMLElement);

    await waitFor(() => {
      expect(screen.getByText(/^Kubernetes$/)).toBeInTheDocument();
    });
  });
});

describe("ArtifactBuilder: Hadron clone", () => {
  it("preserves kubernetesDistro and kubernetesEnabled when cloning a Standard Hadron build", async () => {
    (getArtifact as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      id: "src-1",
      name: "src-hadron",
      phase: "done",
      message: "",
      baseImage: "",
      hadronBase: "ghcr.io/kairos-io/hadron:v0.5.1",
      hadronFirmware: [],
      hadronLayers: [],
      hadronExtra: "",
      kairosVersion: "v4.1.2",
      model: "generic",
      arch: "amd64",
      variant: "standard",
      kubernetesDistro: "k3s",
      kubernetesVersion: "v1.31.4+k3s1",
      kubernetesEnabled: true,
      kairosInitImage: "quay.io/kairos/kairos-init:v0.5.0",
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
      autoInstall: true,
      registerAuroraBoot: true,
      artifacts: [],
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
    });

    renderBuilder("/artifacts/new?clone=src-1");

    await waitFor(() => {
      // Hadron clones land directly on Configure. The Kubernetes card is
      // only rendered when variant === "standard", so its presence proves
      // the clone preserved variant + all K8s fields.
      expect(screen.getByText(/^Kubernetes$/)).toBeInTheDocument();
    });
    // The Kubernetes version input should carry the cloned version.
    expect(
      screen.getByDisplayValue(/v1\.31\.4\+k3s1/),
    ).toBeInTheDocument();

    // The kairos-init OCI image should also carry over (regression for the
    // clone silently dropping this field on Hadron-based sources). It lives
    // on the Output step, in the collapsed Advanced card.
    fireEvent.click(screen.getByRole("button", { name: /Output/i }));
    fireEvent.click(
      screen.getByRole("button", { name: /Advanced.*Cloud config/i }),
    );
    expect(
      screen.getByDisplayValue(/quay\.io\/kairos\/kairos-init:v0\.5\.0/),
    ).toBeInTheDocument();
  });
});
