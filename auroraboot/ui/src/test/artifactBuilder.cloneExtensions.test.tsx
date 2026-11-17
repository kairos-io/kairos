import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";

vi.mock("@/api/artifacts", async () => {
  const actual = await vi.importActual<typeof import("@/api/artifacts")>(
    "@/api/artifacts",
  );
  return {
    ...actual,
    listSecureBootKeySets: vi.fn(async () => []),
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
import { getArtifact, listBundleExtensions } from "@/api/artifacts";

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

// The source artifact every case below clones. It carries both extension
// fields, which is the whole point: a clone that drops either one rebuilds an
// image that refuses the overlays the original accepted, or stops carrying
// the extensions the original shipped on upgrade.
const SOURCE = {
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
  fips: false,
  trustedBoot: false,
  outputs: {},
  extensionHierarchies: {
    sysext: ["/usr/local/lib/vendor"],
    confext: ["/etc/vendor-site"],
  },
};

const BUNDLE = [
  {
    artifactId: "src-artifact-id",
    extensionName: "monitoring",
    extensionType: "sysext",
    pinnedVersion: "1.2.0",
    order: 1,
  },
  {
    artifactId: "src-artifact-id",
    extensionName: "site-config",
    extensionType: "confext",
    order: 0,
  },
];

describe("ArtifactBuilder: cloning carries the extension configuration", () => {
  it("restores the bundled extensions onto the cloned form", async () => {
    vi.mocked(getArtifact).mockResolvedValue(SOURCE as never);
    vi.mocked(listBundleExtensions).mockResolvedValue(BUNDLE as never);

    renderBuilder("/artifacts/new?clone=src-artifact-id");

    // A clone lands on the Review step, which lists the bundle by name.
    await waitFor(() => {
      expect(screen.getByText(/Clone: edge-gateway/)).toBeTruthy();
    });
    await waitFor(() => {
      expect(screen.getByText(/monitoring/)).toBeTruthy();
      expect(screen.getByText(/site-config/)).toBeTruthy();
    });
    expect(vi.mocked(listBundleExtensions)).toHaveBeenCalledWith(
      "src-artifact-id",
    );
  });

  it("restores the extension hierarchies onto the cloned form", async () => {
    vi.mocked(getArtifact).mockResolvedValue(SOURCE as never);
    vi.mocked(listBundleExtensions).mockResolvedValue([] as never);

    renderBuilder("/artifacts/new?clone=src-artifact-id");

    await waitFor(() => {
      expect(screen.getByText(/Clone: edge-gateway/)).toBeTruthy();
    });

    // The hierarchies live on the Extensions step, which a clone can reach
    // because a cloned form has every step unlocked.
    fireEvent.click(screen.getByRole("button", { name: /Extensions/ }));

    await waitFor(() => {
      expect(screen.getByText("/usr/local/lib/vendor")).toBeTruthy();
      expect(screen.getByText("/etc/vendor-site")).toBeTruthy();
    });
  });

  it("leaves the bundle empty when the source artifact has none", async () => {
    vi.mocked(getArtifact).mockResolvedValue(SOURCE as never);
    vi.mocked(listBundleExtensions).mockResolvedValue([] as never);

    renderBuilder("/artifacts/new?clone=src-artifact-id");

    await waitFor(() => {
      expect(screen.getByText(/Clone: edge-gateway/)).toBeTruthy();
    });
    expect(screen.queryByText(/monitoring/)).toBeNull();
  });

  it("still clones when the bundle read fails", async () => {
    vi.mocked(getArtifact).mockResolvedValue(SOURCE as never);
    vi.mocked(listBundleExtensions).mockRejectedValue(new Error("boom"));

    renderBuilder("/artifacts/new?clone=src-artifact-id");

    // A bundle that cannot be read must not cost the operator the clone.
    await waitFor(() => {
      expect(screen.getByText(/Clone: edge-gateway/)).toBeTruthy();
    });
  });

  // A failed read used to be swallowed, so the review step showed the "None"
  // of a source artifact that genuinely had no bundle, and the operator
  // started a build believing nothing was lost.
  it("says the bundle is unknown on review when the read fails", async () => {
    vi.mocked(getArtifact).mockResolvedValue(SOURCE as never);
    vi.mocked(listBundleExtensions).mockRejectedValue(new Error("boom"));

    renderBuilder("/artifacts/new?clone=src-artifact-id");

    await waitFor(() => {
      expect(screen.getByText(/Clone: edge-gateway/)).toBeTruthy();
    });

    // The clone lands on Review, which is the last screen before Start build.
    await waitFor(() => {
      expect(
        screen.getByText(/could not be read from the cloned artifact/i),
      ).toBeTruthy();
    });
    expect(screen.getByText("Unknown")).toBeTruthy();
  });

  it("keeps the failure on the Extensions step, where the empty list is shown", async () => {
    vi.mocked(getArtifact).mockResolvedValue(SOURCE as never);
    vi.mocked(listBundleExtensions).mockRejectedValue(new Error("boom"));

    renderBuilder("/artifacts/new?clone=src-artifact-id");

    await waitFor(() => {
      expect(screen.getByText(/Clone: edge-gateway/)).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: /Extensions/ }));

    // The toast is gone by the time the operator walks back to this step.
    await waitFor(() => {
      expect(
        screen.getByText(/bundled extensions of the cloned artifact could not be read/i),
      ).toBeTruthy();
    });
  });

  // The mirror of the two cases above: a source that really has no bundle
  // must not be labelled unknown, or the warning means nothing.
  it("says None, not Unknown, when the source really has no bundle", async () => {
    vi.mocked(getArtifact).mockResolvedValue(SOURCE as never);
    vi.mocked(listBundleExtensions).mockResolvedValue([] as never);

    renderBuilder("/artifacts/new?clone=src-artifact-id");

    await waitFor(() => {
      expect(screen.getByText(/Clone: edge-gateway/)).toBeTruthy();
    });
    expect(
      screen.queryByText(/could not be read from the cloned artifact/i),
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: /Extensions/ }));
    await waitFor(() => {
      expect(screen.getByText("/etc/vendor-site")).toBeTruthy();
    });
    expect(
      screen.queryByText(/bundled extensions of the cloned artifact could not be read/i),
    ).toBeNull();
  });
});
