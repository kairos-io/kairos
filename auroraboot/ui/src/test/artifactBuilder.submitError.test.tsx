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

import { ArtifactBuilder } from "@/pages/ArtifactBuilder";
import { ApiError } from "@/api/client";
import { createArtifact } from "@/api/artifacts";

beforeEach(() => {
  vi.mocked(createArtifact).mockReset();
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
        <Route path="/artifacts/:id" element={<div>artifact detail</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

// The Base step wants a name before Next moves on. Everything after it is
// valid with the defaults, so the wizard walks straight through to Review.
async function gotoReview() {
  fireEvent.click(await screen.findByText(/^Hadron v/));
  fireEvent.change(screen.getByPlaceholderText(/Production v4\.0\.3/), {
    target: { value: "edge" },
  });
  for (const label of [
    "Next: System",
    "Next: Extensions",
    "Next: Access",
    "Next: Output",
    "Next: Review",
  ]) {
    fireEvent.click(screen.getByRole("button", { name: label }));
  }
}

async function startBuild() {
  fireEvent.click(await screen.findByRole("button", { name: /Start build/i }));
  await waitFor(() => expect(createArtifact).toHaveBeenCalled());
}

// handleSubmit used to call createArtifact with no try/catch, so a 400 from
// the create handler escaped the form's onSubmit as an unhandled rejection:
// the page stayed on Review, rendered nothing, and the button looked as if it
// had never been clicked. Every message pkg/handlers/artifacts.go writes for
// the operator reaches them through this path.
describe("ArtifactBuilder: a rejected build says so", () => {
  it("renders the handler's message and keeps the operator on Review", async () => {
    vi.mocked(createArtifact).mockRejectedValue(
      new ApiError(
        400,
        { error: 'bundledExtensions[0]: arch "amd64" != artifact arch "arm64"' },
        "",
      ),
    );
    renderBuilder();
    await gotoReview();
    await startBuild();

    expect(
      await screen.findByText(
        /Build was not started: bundledExtensions\[0\]: arch "amd64" != artifact arch "arm64"/,
      ),
    ).toBeTruthy();
    // Still on the wizard, with the build button ready for another try.
    expect(screen.getByRole("button", { name: /Start build/i })).toBeTruthy();
    expect(screen.queryByText("artifact detail")).toBeNull();
  });

  it("reports a failure that carries no error body", async () => {
    vi.mocked(createArtifact).mockRejectedValue(new Error("Failed to fetch"));
    renderBuilder();
    await gotoReview();
    await startBuild();

    expect(
      await screen.findByText(/Build was not started: Failed to fetch/),
    ).toBeTruthy();
  });

  it("drops the message when the next build is accepted", async () => {
    vi.mocked(createArtifact)
      .mockRejectedValueOnce(new ApiError(400, { error: "bundle name is empty" }, ""))
      .mockResolvedValueOnce({
        id: "build-1",
        phase: "Pending",
        message: "",
        artifacts: [],
      } as Awaited<ReturnType<typeof createArtifact>>);
    renderBuilder();
    await gotoReview();
    await startBuild();

    expect(
      await screen.findByText(/Build was not started: bundle name is empty/),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Start build/i }));
    expect(await screen.findByText("artifact detail")).toBeTruthy();
  });
});
