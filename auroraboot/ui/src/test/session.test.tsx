import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";

import { login, logout, hasSession, apiFetch } from "@/api/client";
import { Login } from "@/pages/Login";
import App from "@/App";
import { Layout } from "@/components/Layout";

function stubMatchMedia() {
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  );
}

describe("session", () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
  });

  it("logs in by posting the password to /api/v1/session and stores nothing", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    expect(await login("pw")).toEqual({ ok: true });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/v1/session");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ password: "pw" });
    expect(localStorage.length).toBe(0);
  });

  it("reports a wrong password", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 401 }));
    expect(await login("bad")).toEqual({ ok: false, status: 401 });
  });

  it("reports a refused origin", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 403 }));
    expect(await login("pw")).toEqual({ ok: false, status: 403 });
  });

  it("logs out with DELETE /api/v1/session", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    expect(await logout()).toBe(true);
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/session", expect.objectContaining({ method: "DELETE" }));
  });

  it("reports a sign-out the server did not confirm", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 500 }));
    expect(await logout()).toBe(false);
    fetchMock.mockRejectedValue(new TypeError("network"));
    expect(await logout()).toBe(false);
  });

  it("leaves the page only once the server has signed the browser out", async () => {
    stubMatchMedia();
    let deleteStatus = 500;
    fetchMock.mockImplementation(async (_url: string, init?: RequestInit) =>
      init?.method === "DELETE"
        ? new Response(null, { status: deleteStatus })
        : new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    render(
      <MemoryRouter initialEntries={["/"]}>
        <Routes>
          <Route element={<Layout />}>
            <Route path="/" element={<div>home page</div>} />
          </Route>
          <Route path="/login" element={<div>login page</div>} />
        </Routes>
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Logout" }));
    expect(await screen.findByText(/Sign-out failed/)).toBeInTheDocument();
    expect(screen.getByText("home page")).toBeInTheDocument();

    deleteStatus = 204;
    fireEvent.click(screen.getByRole("button", { name: "Logout" }));
    expect(await screen.findByText("login page")).toBeInTheDocument();
  });

  it("checks the session with GET /api/v1/session", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    expect(await hasSession()).toBe(true);
    fetchMock.mockResolvedValue(new Response(null, { status: 401 }));
    expect(await hasSession()).toBe(false);
  });

  it("sends no Authorization header on API calls", async () => {
    fetchMock.mockResolvedValue(new Response("[]", { status: 200 }));
    await apiFetch("/api/v1/nodes");
    const init = fetchMock.mock.calls[0][1];
    expect(new Headers(init.headers).has("Authorization")).toBe(false);
  });

  it("removes a password left in localStorage by an earlier version", async () => {
    localStorage.setItem("auroraboot_token", "old-password");
    fetchMock.mockResolvedValue(new Response(null, { status: 401 }));
    render(
      <MemoryRouter initialEntries={["/login"]}>
        <App />
      </MemoryRouter>,
    );
    await waitFor(() => expect(localStorage.getItem("auroraboot_token")).toBeNull());
  });

  it("sends a signed-out visitor to the login page", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 401 }));
    render(
      <MemoryRouter initialEntries={["/"]}>
        <App />
      </MemoryRouter>,
    );
    expect((await screen.findAllByText("Sign In")).length).toBeGreaterThan(0);
  });

  function renderLogin() {
    render(
      <MemoryRouter initialEntries={["/login"]}>
        <Routes>
          <Route path="/login" element={<Login />} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "bad" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
  }

  it("shows an error for a wrong password on the login page", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 401 }));
    renderLogin();
    expect(await screen.findByText("Invalid password")).toBeInTheDocument();
  });

  it("tells the operator the origin was refused on a 403", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 403 }));
    renderLogin();
    expect(await screen.findByText(/does not match the server's public URL/)).toBeInTheDocument();
    expect(screen.queryByText("Invalid password")).not.toBeInTheDocument();
  });

  it("shows a generic failure for other statuses and network errors", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 500 }));
    renderLogin();
    expect(await screen.findByText("Sign-in failed")).toBeInTheDocument();
  });

  it("shows a generic failure when the request cannot be sent", async () => {
    fetchMock.mockRejectedValue(new TypeError("network"));
    renderLogin();
    expect(await screen.findByText("Sign-in failed")).toBeInTheDocument();
  });
});
