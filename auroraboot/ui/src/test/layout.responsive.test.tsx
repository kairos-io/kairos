import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { Layout, SIDEBAR_KEY } from "@/components/Layout";

beforeEach(() => {
  window.localStorage.clear();
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
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
    vi.fn(async () =>
      new Response(JSON.stringify({ backend: "local", downloadSupported: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
});

function renderLayout() {
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<div>home page</div>} />
          <Route path="/groups" element={<div>groups page</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe("responsive layout", () => {
  it("collapses the sidebar to an icon rail and remembers it", () => {
    renderLayout();

    fireEvent.click(screen.getByRole("button", { name: "Collapse sidebar" }));

    expect(window.localStorage.getItem(SIDEBAR_KEY)).toBe("rail");
    const link = screen.getByRole("link", { name: "Nodes" });
    expect(link).toHaveAttribute("aria-label", "Nodes");
    expect(link).toHaveAttribute("title", "Nodes");
    const label = within(link).getByText("Nodes");
    expect(label).toHaveClass("sr-only");
    expect(label).not.toHaveClass("lg:not-sr-only");
    expect(screen.getByRole("button", { name: /^Theme: / })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Expand sidebar" }));
    expect(window.localStorage.getItem(SIDEBAR_KEY)).toBe("full");
    expect(within(screen.getByRole("link", { name: "Nodes" })).getByText("Nodes")).not.toHaveClass("sr-only");
  });

  it("opens the navigation drawer and closes it when a link is followed", async () => {
    renderLayout();

    fireEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("link", { name: "Groups" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(screen.getByText("groups page")).toBeInTheDocument();
  });
});
