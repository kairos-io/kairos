import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, within, waitFor } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router";

vi.mock("@/api/artifacts", async () => {
  const actual = await vi.importActual<typeof import("@/api/artifacts")>("@/api/artifacts");
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

import { ArtifactBuilder, BUILDER_STEPS } from "@/pages/ArtifactBuilder";
import { PRODUCT_NAME } from "@/lib/branding";

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
  vi.stubGlobal("fetch", vi.fn(async () => new Response("[]", { status: 200 })));
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

function stepper() {
  return screen.getByRole("navigation", { name: "Steps" });
}

function currentStep() {
  return within(stepper())
    .getAllByRole("button")
    .find((b) => b.getAttribute("aria-current") === "step");
}

function nameInput() {
  return screen.getByPlaceholderText(/Production v4\.0\.3/);
}

async function startWithUbuntu() {
  renderBuilder();
  fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
  fireEvent.change(nameInput(), { target: { value: "edge" } });
}

describe("ArtifactBuilder: six steps", () => {
  it("exports the six step keys", () => {
    expect(BUILDER_STEPS).toEqual(["base", "system", "extensions", "access", "output", "review"]);
  });

  it("keeps the Base step when a template is clicked", async () => {
    renderBuilder();
    fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
    expect(currentStep()?.textContent).toMatch(/Base/);
    expect(nameInput()).toBeInTheDocument();
  });

  it("does not highlight the Custom card until it is selected", async () => {
    renderBuilder();
    const custom = (await screen.findByText("Custom")).closest("[aria-pressed]") as HTMLElement;
    expect(custom).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(custom);
    expect(custom).toHaveAttribute("aria-pressed", "true");
  });

  it("requires a name and clears the error once the name is set", async () => {
    renderBuilder();
    fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
    fireEvent.click(screen.getByRole("button", { name: "Next: System" }));

    expect(await screen.findByText(/Name is required/)).toBeInTheDocument();
    expect(currentStep()?.textContent).toMatch(/Base/);
    expect(screen.getByText("1 issue on this step")).toBeInTheDocument();

    fireEvent.change(nameInput(), { target: { value: "edge" } });
    expect(screen.queryByText(/Name is required/)).not.toBeInTheDocument();
    expect(screen.getByText("All required fields set")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
    expect(currentStep()?.textContent).toMatch(/System/);
  });

  it("shows the catalog picker and the bundled extensions on the Extensions step", async () => {
    await startWithUbuntu();
    fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));

    expect(currentStep()?.textContent).toMatch(/Extensions/);
    expect(screen.getByText("Extensions built on this instance")).toBeInTheDocument();
    expect(screen.getByLabelText("Extension catalog URL")).toBeInTheDocument();
    expect(screen.getByText("Pre-configure for system extensions")).toBeInTheDocument();
  });

  it("shows the provisioning checkboxes on the Access step", async () => {
    await startWithUbuntu();
    fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Access" }));

    expect(currentStep()?.textContent).toMatch(/Access/);
    expect(screen.getByLabelText("Auto-install")).toBeInTheDocument();
    expect(screen.getByLabelText(`Register with ${PRODUCT_NAME}`)).toBeInTheDocument();
    expect(screen.getByLabelText("FIPS")).toBeInTheDocument();
    expect(screen.getByText(/Allowed remote commands/)).toBeInTheDocument();
  });

  it("validates only the current step on Next", async () => {
    await startWithUbuntu();
    // The Output step has an error once no output is picked, but that must
    // not block the steps before it.
    fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Access" }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Output" }));
    expect(currentStep()?.textContent).toMatch(/Output/);
    fireEvent.click(screen.getByRole("button", { name: /^ISO/ }));
    fireEvent.click(screen.getByRole("button", { name: "Next: Review" }));
    expect(await screen.findByText(/At least one output format/)).toBeInTheDocument();
    expect(currentStep()?.textContent).toMatch(/Output/);
  });

  it("puts Import and Export in the header menu", async () => {
    renderBuilder();
    await screen.findByText(/^Ubuntu \d+\.\d+$/);
    expect(screen.queryByRole("button", { name: /^Export/ })).not.toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("button", { name: "More actions" }), { key: "Enter" });
    await waitFor(() => expect(screen.getByRole("menuitem", { name: /Import config/ })).toBeInTheDocument());
    expect(screen.getByRole("menuitem", { name: /Export config/ })).toBeInTheDocument();
  });
});
