import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";

vi.mock("@/api/artifacts", async () => {
  const actual = await vi.importActual<typeof import("@/api/artifacts")>(
    "@/api/artifacts",
  );
  return {
    ...actual,
    listSecureBootKeySets: vi.fn(async () => [
      {
        id: "ks-1",
        name: "prod-keys",
        hasTpmPcrKey: true,
        secureBootEnroll: "if-safe",
        createdAt: "2026-01-01T00:00:00Z",
      },
    ]),
    getArtifact: vi.fn(),
    listBundleExtensions: vi.fn(async () => []),
    createArtifact: vi.fn(async () => ({
      id: "build-1",
      phase: "Pending",
      message: "",
      artifacts: [],
    })),
    uploadOverlayFiles: vi.fn(),
  };
});

vi.mock("@/api/groups", () => ({
  listGroups: vi.fn(async () => []),
}));

import { ArtifactBuilder } from "@/pages/ArtifactBuilder";
import { Toaster } from "@/components/ui/toaster";
import { ApiError } from "@/api/client";
import { createArtifact, getArtifact, uploadOverlayFiles } from "@/api/artifacts";
import { BUILD_CONFIG_KIND, BUILD_CONFIG_VERSION } from "@/lib/buildConfig";

const OVERLAY_ID = "6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a";

beforeEach(() => {
  vi.mocked(createArtifact).mockClear();
  vi.mocked(uploadOverlayFiles).mockReset();
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
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("[]", { status: 200 })),
  );
});

function renderBuilder(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Toaster />
      <Routes>
        <Route path="/artifacts/new" element={<ArtifactBuilder />} />
      </Routes>
    </MemoryRouter>,
  );
}

function source(overrides: Record<string, unknown> = {}) {
  return {
    id: "src-artifact-id",
    name: "edge-gateway",
    phase: "Ready",
    baseImage: "ghcr.io/kairos-io/ubuntu:24.04",
    kairosVersion: "v3.5.0",
    model: "generic",
    arch: "amd64",
    variant: "core",
    iso: true,
    cloudImage: false,
    netboot: false,
    uki: false,
    fips: false,
    trustedBoot: false,
    artifacts: [],
    ...overrides,
  };
}

// A clone has every step unlocked, so the stepper can jump to Output.
async function cloneAndOpenOutput(overrides: Record<string, unknown> = {}) {
  vi.mocked(getArtifact).mockResolvedValue(source(overrides) as never);
  renderBuilder("/artifacts/new?clone=src-artifact-id");
  await waitFor(() => {
    expect(screen.getByText(/Clone: edge-gateway/)).toBeTruthy();
  });
  fireEvent.click(screen.getByRole("button", { name: /^Output/ }));
  await screen.findByRole("region", { name: "Overlay Files" });
}

function overlayCard(): HTMLElement {
  return screen.getByRole("region", { name: "Overlay Files" });
}

async function startBuild() {
  fireEvent.click(screen.getByRole("button", { name: /^Review/ }));
  fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));
  await waitFor(() => expect(createArtifact).toHaveBeenCalled());
  return vi.mocked(createArtifact).mock.calls[0][0];
}

describe("ArtifactBuilder: overlay files", () => {
  it("sends the ID the upload returned as overlayId", async () => {
    vi.mocked(uploadOverlayFiles).mockResolvedValue(OVERLAY_ID);
    await cloneAndOpenOutput();

    const input = overlayCard().querySelector("input[type='file']") as HTMLInputElement;
    const file = new File(["hello\n"], "motd", { type: "text/plain" });
    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => expect(within(overlayCard()).getByText("motd")).toBeTruthy());
    expect(uploadOverlayFiles).toHaveBeenCalled();

    const body = await startBuild();
    expect(body.overlayId).toBe(OVERLAY_ID);
    expect(body).not.toHaveProperty("overlayRootfs");
  });

  it("reports a failed upload and attaches no overlay", async () => {
    vi.mocked(uploadOverlayFiles).mockRejectedValue(
      new ApiError(400, { error: "no files uploaded" }, ""),
    );
    await cloneAndOpenOutput();

    const input = overlayCard().querySelector("input[type='file']") as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(["x"], "motd")] } });

    expect(await screen.findByText("Overlay upload failed: no files uploaded")).toBeTruthy();
    expect(within(overlayCard()).getByText(/Drop files or a .tar.gz here/)).toBeTruthy();

    const body = await startBuild();
    expect(body.overlayId).toBeUndefined();
  });

  it("carries the overlay of the cloned artifact and says so", async () => {
    await cloneAndOpenOutput({ overlayId: OVERLAY_ID });

    expect(within(overlayCard()).getByText("Uses a previously uploaded overlay")).toBeTruthy();

    const body = await startBuild();
    expect(body.overlayId).toBe(OVERLAY_ID);
  });

  it("drops the cloned overlay when the operator clears it", async () => {
    await cloneAndOpenOutput({ overlayId: OVERLAY_ID });

    fireEvent.click(within(overlayCard()).getByRole("button", { name: /Clear/i }));
    expect(within(overlayCard()).getByText(/Drop files or a .tar.gz here/)).toBeTruthy();

    const body = await startBuild();
    expect(body.overlayId).toBeUndefined();
  });
});

describe("ArtifactBuilder: overlay of a Hadron clone", () => {
  it("carries the overlay of the cloned Hadron artifact", async () => {
    await cloneAndOpenOutput({
      hadronBase: "ghcr.io/kairos-io/hadron:v0.0.1",
      overlayId: OVERLAY_ID,
    });

    expect(within(overlayCard()).getByText("Uses a previously uploaded overlay")).toBeTruthy();
  });
});

describe("ArtifactBuilder: overlay of an imported config", () => {
  async function importConfig(config: Record<string, unknown>) {
    const input = document.querySelector(
      "input[accept='application/json,.json']",
    ) as HTMLInputElement;
    const file = new File([JSON.stringify(config)], "build.json", { type: "application/json" });
    fireEvent.change(input, { target: { files: [file] } });
    await screen.findByText("Config imported");
    // An import lands on Base; walk to Output the way the operator would.
    for (const next of ["System", "Extensions", "Access", "Output"]) {
      fireEvent.click(screen.getByRole("button", { name: `Next: ${next}` }));
    }
    await screen.findByRole("region", { name: "Overlay Files" });
  }

  const CONFIG = {
    kind: BUILD_CONFIG_KIND,
    version: BUILD_CONFIG_VERSION,
    buildMode: "image",
    name: "imported",
    source: {
      baseImage: "ghcr.io/kairos-io/ubuntu:24.04",
      kairosVersion: "v1.0",
      arch: "amd64",
      model: "generic",
      variant: "core",
    },
    outputs: { iso: true },
  };

  async function uploadMotd() {
    vi.mocked(uploadOverlayFiles).mockResolvedValue("11111111-2222-4333-8444-555555555555");
    await cloneAndOpenOutput();
    const input = overlayCard().querySelector("input[type='file']") as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(["x"], "motd")] } });
    await waitFor(() => expect(within(overlayCard()).getByText("motd")).toBeTruthy());
  }

  it("shows the imported overlay, not the file names of an earlier upload", async () => {
    await uploadMotd();

    await importConfig({ ...CONFIG, overlayId: OVERLAY_ID });

    expect(within(overlayCard()).queryByText("motd")).toBeNull();
    expect(within(overlayCard()).getByText("Uses a previously uploaded overlay")).toBeTruthy();
  });

  it("drops an earlier upload when the imported config has no overlay", async () => {
    await uploadMotd();

    await importConfig(CONFIG);

    expect(within(overlayCard()).queryByText("motd")).toBeNull();
    expect(within(overlayCard()).getByText(/Drop files or a .tar.gz here/)).toBeTruthy();
    // The build summary beside the step counts no files either.
    expect(screen.getByText("0 files")).toBeTruthy();
  });
});

describe("ArtifactBuilder: UKI signing", () => {
  it("signs with a saved key set and offers no server path inputs", async () => {
    await cloneAndOpenOutput({ trustedBoot: true, uki: true });

    expect(screen.getByText("UKI Signing Keys")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Manual Paths/i })).toBeNull();
    expect(screen.queryByPlaceholderText(/\/path\/to\//)).toBeNull();
    expect(screen.getByText("Enrollment Policy")).toBeTruthy();
  });

  it("refuses to start a UKI build without a saved key set", async () => {
    vi.mocked(getArtifact).mockResolvedValue(source({ trustedBoot: true, uki: true }) as never);
    renderBuilder("/artifacts/new?clone=src-artifact-id");

    // A clone lands on Review, where Start Build checks every step.
    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));

    expect(
      (await screen.findAllByText(/A secure boot key set must be selected for a Trusted Boot ISO/)).length,
    ).toBeGreaterThan(0);
    expect(createArtifact).not.toHaveBeenCalled();
  });
});
