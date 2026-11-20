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
    createArtifact: vi.fn(async () => ({ id: "build-1", phase: "Pending", message: "", artifacts: [] })),
    uploadOverlayFiles: vi.fn(),
  };
});

vi.mock("@/api/groups", () => ({
  listGroups: vi.fn(async () => []),
}));

import { ArtifactBuilder } from "@/pages/ArtifactBuilder";
import {
  catalogExtensionsForArch,
  serializeExtensionSelection,
  parseExtensionSelection,
} from "@/lib/catalogExtensions";
import { createArtifact } from "@/api/artifacts";

const CATALOG_URL = "https://kairos-io.github.io/hadron-layers/releases.json";

// Shaped like the published hadron-layers index: a per-version `sysext` map
// keyed by architecture is what decides whether an extension can be
// materialized for the build at all.
const CATALOG = {
  repo: "ghcr.io/kairos-io/hadron-layers",
  layers: [
    {
      name: "nvidia",
      description: "NVIDIA drivers",
      latest: "v2.0.0",
      tags: [
        { tag: "v1.0.0", sysext: { amd64: { oci: "ghcr.io/x/nvidia@sha256:aa" } } },
        { tag: "v2.0.0", sysext: { amd64: { oci: "ghcr.io/x/nvidia@sha256:bb" } } },
      ],
    },
    {
      name: "rpi-firmware",
      description: "Raspberry Pi firmware",
      latest: "v1.0.0",
      tags: [{ tag: "v1.0.0", sysext: { arm64: { oci: "ghcr.io/x/rpi@sha256:cc" } } }],
    },
  ],
};

beforeEach(() => {
  vi.clearAllMocks();
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
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("releases.json")) {
        return new Response(JSON.stringify(CATALOG), { status: 200 });
      }
      return new Response("[]", { status: 200 });
    }),
  );
});

function renderBuilder() {
  return render(
    <MemoryRouter initialEntries={["/artifacts/new"]}>
      <Routes>
        <Route path="/artifacts/new" element={<ArtifactBuilder />} />
      </Routes>
    </MemoryRouter>,
  );
}

// The catalog picker is on the Extensions step. The Base step needs a name
// before Next moves on.
async function gotoExtensionsStep() {
  fireEvent.click(await screen.findByText(/^Hadron v/));
  fireEvent.change(screen.getByPlaceholderText(/Production v4\.0\.3/), {
    target: { value: "edge" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
  fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));
}

function gotoReviewStep() {
  fireEvent.click(screen.getByRole("button", { name: "Next: Access" }));
  fireEvent.click(screen.getByRole("button", { name: "Next: Output" }));
  fireEvent.click(screen.getByRole("button", { name: "Next: Review" }));
}

describe("catalog extension helpers", () => {
  it("keeps only the versions published for the target architecture", () => {
    const items = [
      {
        name: "nvidia",
        versions: [
          { version: "v1", archs: ["amd64"] },
          { version: "v2", archs: ["amd64", "arm64"] },
        ],
      },
      { name: "rpi", versions: [{ version: "v1", archs: ["arm64"] }] },
    ];

    const amd64 = catalogExtensionsForArch(items, "amd64");
    expect(amd64.map((i) => i.name)).toEqual(["nvidia"]);
    expect(amd64[0].versions.map((v) => v.version)).toEqual(["v1", "v2"]);

    const arm64 = catalogExtensionsForArch(items, "arm64");
    expect(arm64.map((i) => i.name)).toEqual(["nvidia", "rpi"]);
    expect(arm64[0].versions.map((v) => v.version)).toEqual(["v2"]);

    expect(catalogExtensionsForArch(items, "riscv64")).toEqual([]);
  });

  it("serializes a pinned version and leaves a latest pick as the bare name", () => {
    const selection = parseExtensionSelection(["nvidia", "tailscale@v1.2.3"]);
    expect(serializeExtensionSelection(selection, ["nvidia", "tailscale"])).toEqual([
      "nvidia",
      "tailscale@v1.2.3",
    ]);
  });

  it("round-trips through parse and serialize", () => {
    const values = ["nvidia", "tailscale@v1.2.3"];
    const selection = parseExtensionSelection(values);
    expect(serializeExtensionSelection(selection, Object.keys(selection).sort())).toEqual(values);
  });
});

describe("ArtifactBuilder: catalog extension picker", () => {
  it("reads the default catalog and offers only what it publishes for the selected arch", async () => {
    renderBuilder();
    await gotoExtensionsStep();

    expect(await screen.findByText("nvidia")).toBeInTheDocument();
    // The Hadron template builds amd64, and rpi-firmware publishes arm64
    // only, so offering it would produce a build that cannot resolve it.
    expect(screen.queryByText("rpi-firmware")).not.toBeInTheDocument();

    const calls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.map((c) =>
      String(c[0]),
    );
    expect(calls).toContain(CATALOG_URL);
  });

  it("sends the selection with no catalog override when the default catalog is used", async () => {
    renderBuilder();
    await gotoExtensionsStep();

    fireEvent.click(await screen.findByLabelText("nvidia"));
    gotoReviewStep();
    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));

    await waitFor(() => expect(createArtifact).toHaveBeenCalled());
    const input = (createArtifact as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(input.extensions).toEqual(["nvidia"]);
    // The server owns the default catalog URL, so a build that did not
    // override it must not pin today's value.
    expect(input.extensionsCatalogs).toBeUndefined();
  });

  it("sends the catalog when the operator points the build at another one", async () => {
    renderBuilder();
    await gotoExtensionsStep();

    fireEvent.click(await screen.findByLabelText("nvidia"));
    fireEvent.change(screen.getByLabelText(/Extension catalog URL/i), {
      target: { value: "https://example.test/mine/releases.json" },
    });
    gotoReviewStep();
    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));

    await waitFor(() => expect(createArtifact).toHaveBeenCalled());
    const input = (createArtifact as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(input.extensions).toEqual(["nvidia"]);
    expect(input.extensionsCatalogs).toEqual(["https://example.test/mine/releases.json"]);
  });

  it("sends nothing when no extension is selected", async () => {
    renderBuilder();
    await gotoExtensionsStep();
    await screen.findByText("nvidia");

    gotoReviewStep();
    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));

    await waitFor(() => expect(createArtifact).toHaveBeenCalled());
    const input = (createArtifact as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(input.extensions).toBeUndefined();
    expect(input.extensionsCatalogs).toBeUndefined();
  });

  it("reports a catalog it cannot read instead of showing an empty picker", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        if (String(input).includes("releases.json")) {
          return new Response("nope", { status: 404 });
        }
        return new Response("[]", { status: 200 });
      }),
    );

    renderBuilder();
    await gotoExtensionsStep();

    expect(await screen.findByText(/Could not read that catalog/i)).toBeInTheDocument();
  });
});

describe("ArtifactBuilder: catalog per flavor", () => {
  // stubFetch serves the catalog at any releases.json URL and the given
  // catalog settings at the settings endpoint.
  function stubFetch(settings: { launch: string[]; saved: string[] }) {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/api/v1/settings/extension-catalogs")) {
          return new Response(JSON.stringify(settings), { status: 200 });
        }
        if (url.includes("releases.json")) {
          return new Response(JSON.stringify(CATALOG), { status: 200 });
        }
        return new Response("[]", { status: 200 });
      }),
    );
  }

  function fetchedURLs(): string[] {
    return (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.map((c) => String(c[0]));
  }

  async function gotoUbuntuExtensionsStep() {
    fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
    fireEvent.change(screen.getByPlaceholderText(/Production v4\.0\.3/), {
      target: { value: "edge" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));
  }

  it("does not read the Hadron catalog for another flavor and proposes catalogs instead", async () => {
    stubFetch({ launch: [], saved: [] });
    renderBuilder();
    await gotoUbuntuExtensionsStep();

    expect(await screen.findByRole("button", { name: "Use Kairos hadron-layers" })).toBeInTheDocument();
    expect(screen.getByLabelText(/Extension catalog URL/i)).toHaveValue("");
    expect(screen.queryByText("nvidia")).not.toBeInTheDocument();
    expect(fetchedURLs()).not.toContain(CATALOG_URL);
  });

  it("reads a proposed catalog on request and states it in the build", async () => {
    stubFetch({ launch: [], saved: [] });
    renderBuilder();
    await gotoUbuntuExtensionsStep();

    fireEvent.click(await screen.findByRole("button", { name: "Use Kairos hadron-layers" }));
    fireEvent.click(await screen.findByLabelText("nvidia"));
    gotoReviewStep();
    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));

    await waitFor(() => expect(createArtifact).toHaveBeenCalled());
    const input = (createArtifact as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(input.extensions).toEqual(["nvidia"]);
    // The server default is the Hadron catalog, so a non-Hadron build names
    // its catalog even when it is that same URL.
    expect(input.extensionsCatalogs).toEqual([CATALOG_URL]);
  });

  it("reads the first configured catalog for another flavor and offers the rest", async () => {
    const launch = "https://launch.example.test/releases.json";
    const saved = "https://saved.example.test/releases.json";
    stubFetch({ launch: [launch], saved: [saved] });
    renderBuilder();
    await gotoUbuntuExtensionsStep();

    expect(await screen.findByText("nvidia")).toBeInTheDocument();
    expect(screen.getByLabelText(/Extension catalog URL/i)).toHaveValue(launch);
    expect(screen.queryByRole("button", { name: "Use Kairos hadron-layers" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: saved })).toBeInTheDocument();
    expect(fetchedURLs()).toContain(launch);
    expect(fetchedURLs()).not.toContain(CATALOG_URL);

    fireEvent.click(screen.getByLabelText("nvidia"));
    gotoReviewStep();
    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));

    await waitFor(() => expect(createArtifact).toHaveBeenCalled());
    const input = (createArtifact as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(input.extensionsCatalogs).toEqual([launch]);
  });

  it("offers the configured catalogs next to the default one for Hadron", async () => {
    const saved = "https://saved.example.test/releases.json";
    stubFetch({ launch: [], saved: [saved] });
    renderBuilder();
    await gotoExtensionsStep();

    expect(await screen.findByText("nvidia")).toBeInTheDocument();
    expect(screen.getByLabelText(/Extension catalog URL/i)).toHaveValue(CATALOG_URL);
    expect(screen.getByRole("button", { name: CATALOG_URL })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: saved })).toHaveAttribute("aria-pressed", "false");
  });

  it("leaves the picks out of the build when switching to a flavor with no catalog", async () => {
    stubFetch({ launch: [], saved: [] });
    renderBuilder();
    await gotoExtensionsStep();
    fireEvent.click(await screen.findByLabelText("nvidia"));

    // Back to Base, switch to Ubuntu, and forward again.
    fireEvent.click(screen.getByRole("button", { name: /Back/i }));
    fireEvent.click(screen.getByRole("button", { name: /Back/i }));
    fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
    fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));

    expect(await screen.findByText(/No catalog is set, so nvidia/)).toBeInTheDocument();
    gotoReviewStep();
    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));

    await waitFor(() => expect(createArtifact).toHaveBeenCalled());
    const input = (createArtifact as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(input.extensions).toBeUndefined();
    expect(input.extensionsCatalogs).toBeUndefined();
  });
});

describe("ArtifactBuilder: the catalog follows the flavor from any step", () => {
  function stubFetch(settings: { launch: string[]; saved: string[] }) {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("/api/v1/settings/extension-catalogs")) {
          return new Response(JSON.stringify(settings), { status: 200 });
        }
        if (url.includes("releases.json")) {
          return new Response(JSON.stringify(CATALOG), { status: 200 });
        }
        return new Response("[]", { status: 200 });
      }),
    );
  }

  // The stepper is the second way to change step, and it can jump straight
  // to a step already reached without passing through Extensions.
  function stepper(label: string) {
    const nav = screen.getByRole("navigation", { name: "Steps" });
    return within(nav).getByRole("button", { name: new RegExp(label) });
  }

  it("drops the Hadron picks when the flavor changes on Base and Review is opened from the stepper", async () => {
    stubFetch({ launch: [], saved: [] });
    renderBuilder();
    await gotoExtensionsStep();
    fireEvent.click(await screen.findByLabelText("nvidia"));
    // Reach Review once so the stepper can jump back to it.
    gotoReviewStep();

    // Change the flavor on Base, then jump to Review with the stepper. The
    // Extensions step is never opened again, which is what used to leave the
    // Hadron catalog and the Hadron picks on an Ubuntu build.
    fireEvent.click(stepper("Base"));
    fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
    fireEvent.click(stepper("Review"));

    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));
    await waitFor(() => expect(createArtifact).toHaveBeenCalled());
    const input = (createArtifact as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(input.baseImage).toContain("ubuntu");
    expect(input.extensions).toBeUndefined();
    expect(input.extensionsCatalogs).toBeUndefined();
  });

  it("re-selects the flavor's catalog when the flavor changes on Base", async () => {
    const saved = "https://saved.example.test/releases.json";
    stubFetch({ launch: [], saved: [saved] });
    renderBuilder();
    await gotoExtensionsStep();
    await screen.findByText("nvidia");
    expect(screen.getByLabelText(/Extension catalog URL/i)).toHaveValue(CATALOG_URL);

    fireEvent.click(stepper("Base"));
    fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
    fireEvent.click(stepper("Extensions"));

    // Ubuntu has one configured catalog, so that one is selected, not the
    // Hadron default the build was carrying.
    await waitFor(() =>
      expect(screen.getByLabelText(/Extension catalog URL/i)).toHaveValue(saved),
    );
  });

  it("keeps a catalog the operator typed across a flavor change", async () => {
    stubFetch({ launch: [], saved: [] });
    renderBuilder();
    await gotoExtensionsStep();
    await screen.findByText("nvidia");
    fireEvent.change(screen.getByLabelText(/Extension catalog URL/i), {
      target: { value: "https://mine.example.test/releases.json" },
    });
    // A typed catalog is not read until Load: until then the field names one
    // catalog while the entries came from another, so nothing is offered.
    expect(screen.queryByLabelText("nvidia")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Load" }));
    fireEvent.click(await screen.findByLabelText("nvidia"));
    gotoReviewStep();

    fireEvent.click(stepper("Base"));
    fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
    fireEvent.click(stepper("Review"));

    fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));
    await waitFor(() => expect(createArtifact).toHaveBeenCalled());
    const input = (createArtifact as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(input.extensions).toEqual(["nvidia"]);
    expect(input.extensionsCatalogs).toEqual(["https://mine.example.test/releases.json"]);
  });
});
