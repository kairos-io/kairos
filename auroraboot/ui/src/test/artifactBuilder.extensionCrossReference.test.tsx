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
    uploadOverlayFiles: vi.fn(),
  };
});

vi.mock("@/api/groups", () => ({
  listGroups: vi.fn(async () => []),
}));

import { ArtifactBuilder } from "@/pages/ArtifactBuilder";

// A fresh instance has built nothing, so /api/v1/extensions answers with an
// empty list. That is the state kairos-io/kairos#4959 reports: the Configure
// step shows an empty extensions card, and nothing on it says the catalog
// picker exists two steps later.
const CATALOG = {
  repo: "ghcr.io/kairos-io/hadron-layers",
  layers: [
    {
      name: "tailscale",
      description: "Tailscale",
      latest: "v1.0.0",
      tags: [
        { tag: "v1.0.0", sysext: { amd64: { oci: "ghcr.io/x/tailscale@sha256:aa" } } },
      ],
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

// Both extension sections are on the Extensions step. The Base step needs a
// name before Next moves on.
async function gotoConfigureStep() {
  fireEvent.click(await screen.findByText(/^Hadron v/));
  fireEvent.change(screen.getByPlaceholderText(/Production v4\.0\.3/), {
    target: { value: "edge" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
  fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));
}

describe("ArtifactBuilder: the two extension sections point at each other", () => {
  it("offers the catalog from the locally-built card instead of dead-ending", async () => {
    renderBuilder();
    await gotoConfigureStep();

    // The empty state names what is missing and offers the other source,
    // rather than only sending the user to the Extensions page.
    const jump = await screen.findByRole("button", {
      name: /Pick from a catalog/i,
    });
    expect(
      screen.getByText(/Nothing built on this instance matches arch/i),
    ).toBeTruthy();

    fireEvent.click(jump);

    // The catalog picker is on the same step, and reaching the step through
    // goToStep means the lazy catalog fetch fired.
    await waitFor(() => {
      expect(screen.getByLabelText("Extension catalog URL")).toBeTruthy();
    });
    await waitFor(() => {
      const urls = vi.mocked(fetch).mock.calls.map((c) => String(c[0]));
      expect(urls.some((u) => u.includes("releases.json"))).toBe(true);
    });
  });

  it("names the catalog section from the body copy as well as the empty state", async () => {
    renderBuilder();
    await gotoConfigureStep();

    // The inline link is there whether or not anything was built locally, so
    // a populated card still says the catalog exists.
    const link = await screen.findByRole("button", {
      name: /^System Extensions$/,
    });
    fireEvent.click(link);

    await waitFor(() => {
      expect(screen.getByLabelText("Extension catalog URL")).toBeTruthy();
    });
  });
});
