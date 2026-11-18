import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { Settings } from "@/pages/Settings";
import { Import } from "@/pages/Import";

const TOKEN = "abcdefgh12345678wxyz";
const ROTATED = "zyxwvuts87654321abcd";

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

function mockFetch() {
  const calls: string[] = [];
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    const method = (init?.method ?? "GET").toUpperCase();
    calls.push(`${method} ${url}`);
    if (url === "/api/v1/settings/registration-token/rotate" && method === "POST") {
      return json({ registrationToken: ROTATED });
    }
    if (url === "/api/v1/settings/registration-token") {
      return json({ registrationToken: TOKEN });
    }
    return json([]);
  });
  return { calls, fn };
}

beforeEach(() => {
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Settings registration token", () => {
  it("shows the masked token from the server response", async () => {
    const m = mockFetch();
    vi.stubGlobal("fetch", m.fn);
    render(
      <MemoryRouter>
        <Settings />
      </MemoryRouter>,
    );
    expect(await screen.findByDisplayValue("abcdefgh...wxyz")).toBeInTheDocument();
  });

  it("rotates the token after confirming in the dialog", async () => {
    const m = mockFetch();
    vi.stubGlobal("fetch", m.fn);
    const confirmSpy = vi.fn(() => true);
    vi.stubGlobal("confirm", confirmSpy);
    render(
      <MemoryRouter>
        <Settings />
      </MemoryRouter>,
    );
    await screen.findByDisplayValue("abcdefgh...wxyz");

    fireEvent.click(screen.getByRole("button", { name: "Rotate" }));
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("Rotate registration token")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Rotate the registration token? Nodes that have not registered yet must use the new token.",
      ),
    ).toBeInTheDocument();
    expect(m.calls).not.toContain("POST /api/v1/settings/registration-token/rotate");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Rotate token" }));
    });

    await waitFor(() =>
      expect(m.calls).toContain("POST /api/v1/settings/registration-token/rotate"),
    );
    expect(await screen.findByDisplayValue("zyxwvuts...abcd")).toBeInTheDocument();
    expect(confirmSpy).not.toHaveBeenCalled();
  });
});

describe("Import registration token", () => {
  it("renders the install command with the registration token", async () => {
    const m = mockFetch();
    vi.stubGlobal("fetch", m.fn);
    render(
      <MemoryRouter>
        <Import />
      </MemoryRouter>,
    );
    expect(
      await screen.findByText(new RegExp(`REGISTRATION_TOKEN=${TOKEN}`)),
    ).toBeInTheDocument();
  });
});
