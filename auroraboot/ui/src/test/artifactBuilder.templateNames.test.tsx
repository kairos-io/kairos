import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
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

describe("ArtifactBuilder template names", () => {
  it("names the openSUSE and Fedora tiles after the version they submit", () => {
    render(
      <MemoryRouter initialEntries={["/artifacts/new"]}>
        <Routes>
          <Route path="/artifacts/new" element={<ArtifactBuilder />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByText("openSUSE Leap 16.1")).toBeInTheDocument();
    expect(screen.getByText("Fedora 44")).toBeInTheDocument();
  });
});
