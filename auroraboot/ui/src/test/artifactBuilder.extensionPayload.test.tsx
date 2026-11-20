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

vi.mock("@/api/extensions", () => ({
  listExtensions: vi.fn(async () => [
    {
      id: "ext-1",
      name: "tailscale",
      type: "sysext",
      version: "v1.2.3",
      arch: "amd64",
      phase: "Ready",
    },
  ]),
}));

import { ArtifactBuilder } from "@/pages/ArtifactBuilder";
import { createArtifact } from "@/api/artifacts";

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
    vi.fn(async () => new Response("[]", { status: 200 })),
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

// Both pickers under test live on the Extensions step. The Base step wants a
// name before Next moves on.
async function gotoExtensionsStep() {
  fireEvent.click(await screen.findByText(/^Hadron v/));
  fireEvent.change(screen.getByPlaceholderText(/Production v4\.0\.3/), {
    target: { value: "edge" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
  fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));
}

function submitFromReview() {
  fireEvent.click(screen.getByRole("button", { name: "Next: Access" }));
  fireEvent.click(screen.getByRole("button", { name: "Next: Output" }));
  fireEvent.click(screen.getByRole("button", { name: "Next: Review" }));
}

async function payloadAfterSubmit() {
  submitFromReview();
  fireEvent.click(await screen.findByRole("button", { name: /Start Build/i }));
  await waitFor(() => expect(createArtifact).toHaveBeenCalled());
  return (createArtifact as unknown as ReturnType<typeof vi.fn>).mock
    .calls[0][0];
}

// handleSubmit builds CreateArtifactInput field by field rather than spreading
// `form`, so a field the form collects is dropped at submit until it is named
// in that literal. Both fields below are validated and stored by
// pkg/handlers/artifacts.go, so the drop was silent: the build started, and
// the picks were simply not on the artifact afterwards.
describe("ArtifactBuilder: extension fields reach the create payload", () => {
  it("sends the extensions built on this instance", async () => {
    renderBuilder();
    await gotoExtensionsStep();

    fireEvent.click(
      await screen.findByRole("button", { name: "Add tailscale to bundle" }),
    );

    const input = await payloadAfterSubmit();
    expect(input.bundledExtensions).toEqual([
      { name: "tailscale", type: "sysext", order: 0 },
    ]);
  });

  it("sends the sysext hierarchies the advanced section collects", async () => {
    renderBuilder();
    await gotoExtensionsStep();

    fireEvent.click(await screen.findByText("Pre-configure for system extensions"));
    fireEvent.click(await screen.findByRole("button", { name: "Add /opt" }));

    const input = await payloadAfterSubmit();
    expect(input.extensionHierarchies).toEqual({
      sysext: ["/opt"],
      confext: [],
    });
  });

  // An extension is built for one architecture, and the handler refuses a
  // build whose bundle does not match the artifact arch
  // (pkg/handlers/artifacts.go). Before the payload carried the bundle this
  // flow started a build; once it does, keeping the picks across an arch
  // change turns Start Build into a 400 that handleSubmit does not catch, so
  // nothing starts and nothing says why. The picks go instead.
  it("drops the bundle when the operator changes arch after picking it", async () => {
    renderBuilder();
    await gotoExtensionsStep();

    fireEvent.click(
      await screen.findByRole("button", { name: "Add tailscale to bundle" }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    fireEvent.click(await screen.findByRole("radio", { name: "ARM64" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));

    const input = await payloadAfterSubmit();
    expect(input.arch).toBe("arm64");
    expect(input.bundledExtensions).toBeUndefined();
  });

  // Returning to the step and reselecting the same arch is not a change, so
  // it must not throw away picks the operator still wants.
  it("keeps the bundle when the arch is reselected unchanged", async () => {
    renderBuilder();
    await gotoExtensionsStep();

    fireEvent.click(
      await screen.findByRole("button", { name: "Add tailscale to bundle" }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    fireEvent.click(await screen.findByRole("radio", { name: "AMD64" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));

    const input = await payloadAfterSubmit();
    expect(input.bundledExtensions).toEqual([
      { name: "tailscale", type: "sysext", order: 0 },
    ]);
  });

  // Omitted rather than sent empty: the handler treats a present
  // extensionHierarchies as a request to bake the drop-in, so an untouched
  // form must not ask for one.
  it("omits both when the operator selected neither", async () => {
    renderBuilder();
    await gotoExtensionsStep();

    const input = await payloadAfterSubmit();
    expect(input.bundledExtensions).toBeUndefined();
    expect(input.extensionHierarchies).toBeUndefined();
  });
});
