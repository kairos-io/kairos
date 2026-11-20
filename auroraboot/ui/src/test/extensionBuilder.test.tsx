import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { ExtensionBuilder } from "@/pages/ExtensionBuilder";

const keySets = [
  { id: "ks-1", name: "prod-keys", hasTpmPcrKey: false, secureBootEnroll: "", createdAt: "" },
  { id: "ks-2", name: "lab-keys", hasTpmPcrKey: false, secureBootEnroll: "", createdAt: "" },
];

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

let posted: unknown[] = [];

function mockFetch() {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.includes("/api/v1/secureboot-keys")) return json(keySets);
    if (url.includes("/api/v1/artifacts")) return json([]);
    if (url.includes("/api/v1/extensions") && init?.method === "POST") {
      posted.push(JSON.parse(String(init.body)));
      return json({ id: "ext-1" });
    }
    return json({});
  });
}

function renderPage() {
  return render(
    <MemoryRouter>
      <ExtensionBuilder />
    </MemoryRouter>,
  );
}

function primary() {
  const footer = document.querySelector("footer") as HTMLElement;
  const buttons = within(footer).getAllByRole("button");
  return buttons[buttons.length - 1];
}

describe("ExtensionBuilder wizard", () => {
  beforeEach(() => {
    posted = [];
    vi.stubGlobal("fetch", mockFetch());
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("requires a name on Source and stays on the step", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByLabelText("Base image")).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText("Base image"), { target: { value: "ubuntu:24.04" } });
    fireEvent.click(primary());
    expect(await screen.findByText("Name is required")).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(screen.queryByLabelText("Version")).not.toBeInTheDocument();
  });

  it("blocks Next in image mode when the image is empty", async () => {
    renderPage();
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "tailscale" } });
    fireEvent.click(primary());
    expect(await screen.findByText("Base image is required")).toBeInTheDocument();
    expect(screen.queryByLabelText("Version")).not.toBeInTheDocument();
  });

  it("lists key sets and Unsigned in the signing select, and reviews every field", async () => {
    renderPage();
    fireEvent.change(await screen.findByLabelText("Name"), { target: { value: "tailscale" } });
    fireEvent.change(screen.getByLabelText("Base image"), { target: { value: "ubuntu:24.04" } });
    expect(primary().textContent).not.toContain("→");
    fireEvent.click(primary());

    const select = (await screen.findByLabelText("Signing key set")) as HTMLSelectElement;
    await waitFor(() => expect(select.options.length).toBe(3));
    const labels = Array.from(select.options).map((o) => o.textContent);
    expect(labels[0]).toBe("Unsigned");
    expect(labels).toContain("prod-keys");
    expect(labels).toContain("lab-keys");
    fireEvent.change(select, { target: { value: "ks-2" } });
    expect(primary().textContent).not.toContain("→");

    fireEvent.click(primary());
    expect(await screen.findByText("Hierarchies")).toBeInTheDocument();
    expect(screen.getByText("Signing")).toBeInTheDocument();
    expect(screen.getByText("lab-keys")).toBeInTheDocument();
    expect(screen.getByText("Service reload")).toBeInTheDocument();
    expect(primary().textContent).not.toContain("→");

    fireEvent.click(primary());
    await waitFor(() => expect(posted.length).toBe(1));
    expect(posted[0]).toMatchObject({
      name: "tailscale",
      type: "sysext",
      arch: "amd64",
      version: "v1.0",
      source: { mode: "image", baseImage: "ubuntu:24.04" },
      signingKeySetId: "ks-2",
      serviceReload: false,
    });
  });
});
