import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
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
  };
});

vi.mock("@/api/groups", () => ({
  listGroups: vi.fn(async () => []),
}));

import { ArtifactBuilder } from "@/pages/ArtifactBuilder";
import { Toaster } from "@/components/ui/toaster";
import { createArtifact, getArtifact, listSecureBootKeySets } from "@/api/artifacts";
import { BUILD_CONFIG_KIND, BUILD_CONFIG_VERSION } from "@/lib/buildConfig";

beforeEach(() => {
  vi.mocked(createArtifact).mockClear();
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

async function clone(overrides: Record<string, unknown> = {}) {
  vi.mocked(getArtifact).mockResolvedValue({
    id: "src-artifact-id",
    name: "edge-gateway",
    phase: "Ready",
    baseImage: "ghcr.io/kairos-io/hadron:v0.5.3",
    kairosVersion: "v4.3.0",
    model: "generic",
    arch: "amd64",
    variant: "core",
    iso: true,
    cloudImage: false,
    netboot: true,
    rawDisk: true,
    tar: true,
    fips: false,
    trustedBoot: false,
    artifacts: [],
    ...overrides,
  } as never);
  renderBuilder("/artifacts/new?clone=src-artifact-id");
  await waitFor(() => {
    expect(screen.getByText(/Clone: edge-gateway/)).toBeTruthy();
  });
}

function openStep(name: string) {
  fireEvent.click(screen.getByRole("button", { name: new RegExp(`^${name}`) }));
}

function outputCard(label: string) {
  return screen.queryByRole("button", { name: new RegExp(`^${label}`) });
}

async function startBuild() {
  openStep("Review");
  fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));
  await waitFor(() => expect(createArtifact).toHaveBeenCalled());
  return vi.mocked(createArtifact).mock.calls[0][0];
}

describe("ArtifactBuilder: boot mode", () => {
  it("asks for the boot mode on the Output step and nowhere else", async () => {
    await clone();

    openStep("Access");
    expect(screen.queryByRole("checkbox", { name: /Trusted Boot/ })).toBeNull();

    openStep("Output");
    expect((screen.getByRole("radio", { name: /^Standard/ }) as HTMLInputElement).checked).toBe(true);
    expect((screen.getByRole("radio", { name: /^Trusted Boot/ }) as HTMLInputElement).checked).toBe(false);
    expect(outputCard("ISO")).toBeTruthy();
    expect(outputCard("Netboot")).toBeTruthy();
    expect(outputCard("Raw Disk")).toBeTruthy();
    expect(outputCard("UKI")).toBeNull();
  });

  it("lists only the outputs a Trusted Boot image ships as", async () => {
    await clone();
    openStep("Output");

    fireEvent.click(screen.getByRole("radio", { name: /^Trusted Boot/ }));

    expect(outputCard("UKI ISO")?.textContent).toContain("Signed installer image");
    expect(outputCard("ISO")).toBeNull();
    expect(outputCard("TAR")).toBeTruthy();
    for (const gone of ["Netboot", "Raw Disk", "Cloud Image", "Google Cloud", "Azure", "MAAS"]) {
      expect(outputCard(gone)).toBeNull();
    }
    expect(screen.getByText("UKI Signing Keys")).toBeTruthy();
  });

  it("drops the outputs Trusted Boot cannot build and keeps them dropped on the way back", async () => {
    await clone();
    openStep("Output");

    fireEvent.click(screen.getByRole("radio", { name: /^Trusted Boot/ }));
    fireEvent.click(screen.getByRole("radio", { name: /^Standard/ }));

    expect(outputCard("ISO")?.getAttribute("aria-pressed")).toBe("true");
    expect(outputCard("Raw Disk")?.getAttribute("aria-pressed")).toBe("false");

    const body = await startBuild();
    expect(body.outputs).toMatchObject({
      trustedBoot: false,
      iso: true,
      tar: true,
      netboot: false,
      rawDisk: false,
    });
  });

  it("refuses to start a Trusted Boot ISO build without a key set", async () => {
    await clone({ trustedBoot: true, iso: true, netboot: false, rawDisk: false });

    openStep("Review");
    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));

    expect(
      (await screen.findAllByText(/A secure boot key set must be selected for a Trusted Boot ISO/)).length,
    ).toBeGreaterThan(0);
    expect(createArtifact).not.toHaveBeenCalled();
  });

  // FIPS and Trusted Boot sit in the outputs object, but neither is a format
  // the build produces.
  it("refuses to start a Trusted Boot build with no output selected", async () => {
    await clone({ trustedBoot: true, fips: true, iso: false, netboot: false, rawDisk: false, tar: false });

    openStep("Review");
    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));

    expect(
      (await screen.findAllByText(/At least one output format must be selected/)).length,
    ).toBeGreaterThan(0);
    expect(createArtifact).not.toHaveBeenCalled();
  });

  it("opens a cloned Trusted Boot artifact in Trusted Boot mode with the ISO selected", async () => {
    await clone({ trustedBoot: true, iso: true, netboot: false, rawDisk: false, tar: false });
    openStep("Output");

    expect((screen.getByRole("radio", { name: /^Trusted Boot/ }) as HTMLInputElement).checked).toBe(true);
    expect(outputCard("UKI ISO")?.getAttribute("aria-pressed")).toBe("true");
  });

  it("sends a Trusted Boot ISO build from an imported config", async () => {
    await clone();
    await waitFor(() => expect(listSecureBootKeySets).toHaveBeenCalled());

    const input = document.querySelector(
      "input[accept='application/json,.json']",
    ) as HTMLInputElement;
    const config = {
      kind: BUILD_CONFIG_KIND,
      version: BUILD_CONFIG_VERSION,
      buildMode: "image",
      name: "imported",
      source: {
        baseImage: "ghcr.io/kairos-io/hadron:v0.5.3",
        kairosVersion: "v4.3.0",
        arch: "amd64",
        model: "generic",
        variant: "core",
      },
      outputs: { iso: true, rawDisk: true, netboot: true, trustedBoot: true },
      signing: { ukiKeySetName: "prod-keys" },
    };
    fireEvent.change(input, {
      target: { files: [new File([JSON.stringify(config)], "build.json", { type: "application/json" })] },
    });
    await screen.findByText("Config imported");

    const body = await startBuild();
    expect(body.outputs).toMatchObject({
      trustedBoot: true,
      iso: true,
      netboot: false,
      rawDisk: false,
    });
    expect(body.signing?.ukiKeySetId).toBe("ks-1");
  });
});
