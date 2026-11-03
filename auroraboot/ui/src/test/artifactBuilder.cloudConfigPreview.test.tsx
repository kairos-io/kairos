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
    createArtifact: vi.fn(),
    uploadOverlayFiles: vi.fn(),
  };
});

vi.mock("@/api/groups", () => ({
  listGroups: vi.fn(async () => []),
}));

import { ArtifactBuilder } from "@/pages/ArtifactBuilder";

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

// The base canonical cloud-config (install + phonehome + default user, no
// extra YAML) is ~275 characters -- see src/lib/cloudConfigPreview.ts. A 2000
// character filler value merged in as an extra top-level key lands at the
// tail of the stringified YAML (mergeYAML appends new keys), so TAIL_MARKER
// is guaranteed to fall well past the 500 character truncation boundary
// regardless of the exact base length.
const FILLER = "m".repeat(2000);
const TAIL_MARKER = "TAIL_MARKER_PAST_500";
const LONG_CONFIG = `filler: "${FILLER}${TAIL_MARKER}"`;
const SHORT_CONFIG = "hostname: short-host";

// The Output step is reached with Next from a template with a name, since
// the stepper does not jump ahead to a step not reached yet.
async function openAdvancedCloudConfig() {
  fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
  fireEvent.change(screen.getByPlaceholderText(/Production v4\.0\.3/), {
    target: { value: "edge" },
  });
  for (const next of ["System", "Extensions", "Access", "Output"]) {
    fireEvent.click(screen.getByRole("button", { name: `Next: ${next}` }));
  }
  fireEvent.click(
    await screen.findByRole("button", { name: /Advanced.*Cloud config/i }),
  );
  return screen.getByPlaceholderText(/Additional cloud-config/i);
}

describe("ArtifactBuilder: Review-step cloud config preview toggle", () => {
  it("truncates a long preview and shows a View full button", async () => {
    renderBuilder("/artifacts/new");
    const textarea = await openAdvancedCloudConfig();
    fireEvent.change(textarea, { target: { value: LONG_CONFIG } });

    fireEvent.click(screen.getByRole("button", { name: "Next: Review" }));

    const button = await screen.findByRole("button", { name: /View full/i });
    expect(button).toBeInTheDocument();

    const pre = document.querySelector("pre") as HTMLElement;
    expect(pre.textContent).not.toContain(TAIL_MARKER);
    expect(pre.textContent?.endsWith("...")).toBe(true);
  });

  it("expands to show content past character 500 when clicked, and collapses back", async () => {
    renderBuilder("/artifacts/new");
    const textarea = await openAdvancedCloudConfig();
    fireEvent.change(textarea, { target: { value: LONG_CONFIG } });
    fireEvent.click(screen.getByRole("button", { name: "Next: Review" }));

    const viewFull = await screen.findByRole("button", { name: /View full/i });
    fireEvent.click(viewFull);

    const pre = document.querySelector("pre") as HTMLElement;
    await waitFor(() => {
      expect(pre.textContent).toContain(TAIL_MARKER);
    });
    const showLess = screen.getByRole("button", { name: /Show less/i });
    expect(showLess).toHaveAttribute("aria-expanded", "true");

    fireEvent.click(showLess);
    await waitFor(() => {
      expect(pre.textContent).not.toContain(TAIL_MARKER);
    });
    expect(
      screen.getByRole("button", { name: /View full/i }),
    ).toHaveAttribute("aria-expanded", "false");
  });

  it("resets the toggle to View full when the underlying config changes", async () => {
    renderBuilder("/artifacts/new");
    const textarea = await openAdvancedCloudConfig();
    fireEvent.change(textarea, { target: { value: LONG_CONFIG } });
    fireEvent.click(screen.getByRole("button", { name: "Next: Review" }));

    fireEvent.click(await screen.findByRole("button", { name: /View full/i }));
    const pre = document.querySelector("pre") as HTMLElement;
    await waitFor(() => expect(pre.textContent).toContain(TAIL_MARKER));

    // Go back and change the config -- content differs, so the reset effect
    // (keyed on the computed preview, not on advancedConfig directly) should
    // fire and collapse the toggle again. The Output step's Advanced card
    // unmounts when the step changes away and remounts on return, so the
    // textarea needs re-querying rather than reusing the earlier handle.
    fireEvent.click(screen.getByRole("button", { name: /Output/i }));
    const textareaAgain = screen.getByPlaceholderText(/Additional cloud-config/i);
    fireEvent.change(textareaAgain, {
      target: { value: LONG_CONFIG.replace(TAIL_MARKER, "DIFFERENT_TAIL") },
    });
    fireEvent.click(screen.getByRole("button", { name: "Next: Review" }));

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /View full/i }),
      ).toBeInTheDocument();
    });
    const preAfter = document.querySelector("pre") as HTMLElement;
    expect(preAfter.textContent).not.toContain("DIFFERENT_TAIL");
  });

  it("renders no toggle for a short preview", async () => {
    renderBuilder("/artifacts/new");
    const textarea = await openAdvancedCloudConfig();
    fireEvent.change(textarea, { target: { value: SHORT_CONFIG } });
    fireEvent.click(screen.getByRole("button", { name: "Next: Review" }));

    await screen.findByText(/Cloud Config Preview/i);
    expect(
      screen.queryByRole("button", { name: /View full|Show less/i }),
    ).not.toBeInTheDocument();
  });
});
