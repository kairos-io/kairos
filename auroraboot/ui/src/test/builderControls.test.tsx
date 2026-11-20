import { describe, it, expect, vi, beforeEach } from "vitest";
import { useState } from "react";
import { render, screen, fireEvent } from "@testing-library/react";
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

import { SegmentedControl } from "@/components/wizard/SegmentedControl";
import {
  CommandPresets,
  FULL_COMMANDS,
  SAFE_COMMANDS,
  presetFor,
} from "@/components/wizard/CommandPresets";
import { ArtifactBuilder } from "@/pages/ArtifactBuilder";

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

function Segmented() {
  const [v, setV] = useState<"a" | "b" | "c">("a");
  return (
    <SegmentedControl
      ariaLabel="Letters"
      value={v}
      onChange={setV}
      options={[
        { value: "a", label: "Alpha" },
        { value: "b", label: "Bravo", hint: "second" },
        { value: "c", label: "Charlie" },
      ]}
    />
  );
}

describe("SegmentedControl", () => {
  it("renders a radiogroup and moves the selection with arrow keys", () => {
    render(<Segmented />);
    const group = screen.getByRole("radiogroup", { name: "Letters" });
    expect(group).toBeInTheDocument();
    const alpha = screen.getByRole("radio", { name: /Alpha/ });
    expect(alpha).toHaveAttribute("aria-checked", "true");

    fireEvent.keyDown(alpha, { key: "ArrowRight" });
    expect(screen.getByRole("radio", { name: /Bravo/ })).toHaveAttribute("aria-checked", "true");
    expect(alpha).toHaveAttribute("aria-checked", "false");

    fireEvent.keyDown(screen.getByRole("radio", { name: /Bravo/ }), { key: "ArrowRight" });
    expect(screen.getByRole("radio", { name: /Charlie/ })).toHaveAttribute("aria-checked", "true");

    // Wraps around.
    fireEvent.keyDown(screen.getByRole("radio", { name: /Charlie/ }), { key: "ArrowRight" });
    expect(alpha).toHaveAttribute("aria-checked", "true");

    fireEvent.keyDown(alpha, { key: "ArrowLeft" });
    expect(screen.getByRole("radio", { name: /Charlie/ })).toHaveAttribute("aria-checked", "true");
  });
});

describe("presetFor", () => {
  it("detects safe, full and custom selections", () => {
    expect(presetFor([...SAFE_COMMANDS])).toBe("safe");
    expect(presetFor([...SAFE_COMMANDS].reverse())).toBe("safe");
    expect(presetFor([...FULL_COMMANDS])).toBe("full");
    expect(presetFor(["upgrade", "exec"])).toBe("custom");
    expect(presetFor([])).toBe("custom");
  });
});

function Presets() {
  const [v, setV] = useState<string[]>([...SAFE_COMMANDS]);
  return (
    <>
      <CommandPresets value={v} onChange={setV} renderCustom={() => <div>custom picker</div>} />
      <output data-testid="value">{v.join(",")}</output>
    </>
  );
}

describe("CommandPresets", () => {
  it("sets the eight commands when Full is chosen", () => {
    render(<Presets />);
    expect(screen.getByRole("radio", { name: /Safe/ })).toHaveAttribute("aria-checked", "true");
    fireEvent.click(screen.getByRole("radio", { name: /Full/ }));
    expect(FULL_COMMANDS).toHaveLength(8);
    expect(screen.getByTestId("value").textContent).toBe(FULL_COMMANDS.join(","));
    expect(screen.queryByText("custom picker")).not.toBeInTheDocument();
  });

  it("renders the custom picker only for Custom", () => {
    render(<Presets />);
    expect(screen.queryByText("custom picker")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Custom" }));
    expect(screen.getByText("custom picker")).toBeInTheDocument();
  });
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

async function goToAccess() {
  renderBuilder();
  fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
  fireEvent.change(screen.getByPlaceholderText(/Production v4\.0\.3/), { target: { value: "edge" } });
  fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
  fireEvent.click(screen.getByRole("button", { name: "Next: Extensions" }));
  fireEvent.click(screen.getByRole("button", { name: "Next: Access" }));
}

describe("ArtifactBuilder controls", () => {
  it("uses segmented controls for architecture and variant", async () => {
    renderBuilder();
    fireEvent.click(await screen.findByText(/^Ubuntu \d+\.\d+$/));
    fireEvent.change(screen.getByPlaceholderText(/Production v4\.0\.3/), { target: { value: "edge" } });
    fireEvent.click(screen.getByRole("button", { name: "Next: System" }));
    expect(screen.getByRole("radiogroup", { name: "Architecture" })).toBeInTheDocument();
    expect(screen.getByRole("radiogroup", { name: "Variant" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: /Standard/ }));
    expect(screen.getByRole("radio", { name: /Standard/ })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByText("Enable Kubernetes on first boot")).toBeInTheDocument();
  });

  it("shows the Custom picker and the default-password warning on the Access step", async () => {
    await goToAccess();
    expect(screen.getByRole("radiogroup", { name: "User setup" })).toBeInTheDocument();
    expect(screen.getByText(/password is/i)).toHaveTextContent(/kairos/);

    fireEvent.click(screen.getByRole("radio", { name: /Custom user/ }));
    expect(screen.queryByText(/password is/i)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: /No user/ }));
    expect(screen.queryByText(/password is/i)).not.toBeInTheDocument();

    const presets = screen.getByRole("radiogroup", { name: "Remote command preset" });
    expect(presets).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "exec" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Custom" }));
    expect(screen.getByRole("checkbox", { name: "exec" })).toBeInTheDocument();
  });
});
