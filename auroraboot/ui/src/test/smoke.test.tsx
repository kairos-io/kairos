import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import App from "@/App";
import { PRODUCT_NAME } from "@/lib/branding";

// jsdom doesn't ship matchMedia; shadcn's theme provider touches it on mount.
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
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("UI smoke", () => {
  it("renders the product login screen for unauthenticated users", async () => {
    // No live session: the auth guard sends `/` to `/login`.
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 401 })));

    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>,
    );

    expect(await screen.findByRole("heading", { name: PRODUCT_NAME })).toBeInTheDocument();
    expect(screen.queryByText(/AuroraBoot/)).not.toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/admin password/i)).toBeInTheDocument();
  });
});
