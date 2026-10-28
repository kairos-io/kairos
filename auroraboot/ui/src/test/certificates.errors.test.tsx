import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

import { Certificates } from "@/pages/Certificates";
import { Toaster } from "@/components/ui/toaster";

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

const KEY_SET = {
  id: "ks-1",
  name: "production",
  hasTpmPcrKey: true,
  secureBootEnroll: "",
  createdAt: "2026-10-31T00:00:00Z",
};

function stubFetch(failures: { generate?: Response; remove?: Response }) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.toString();
      const method = (init?.method ?? "GET").toUpperCase();
      if (url === "/api/v1/secureboot-keys/generate" && method === "POST") {
        return failures.generate ?? json(KEY_SET, 201);
      }
      if (url === `/api/v1/secureboot-keys/${KEY_SET.id}` && method === "DELETE") {
        return failures.remove ?? new Response(null, { status: 204 });
      }
      return json([KEY_SET]);
    }),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Certificates page errors", () => {
  it("shows the server's error when generating a key set fails", async () => {
    stubFetch({
      generate: json({ error: "directory production already holds PK.key on disk" }, 409),
    });
    render(
      <>
        <Toaster />
        <Certificates />
      </>,
    );

    fireEvent.change(screen.getByPlaceholderText(/Name, e.g. production/), {
      target: { value: "production" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Generate" }));

    expect(
      await screen.findByText(/Generate failed: directory production already holds PK.key on disk/),
    ).toBeInTheDocument();
  });

  it("shows the server's error when removing a key set fails", async () => {
    stubFetch({ remove: json({ error: "database is locked" }, 500) });
    render(
      <>
        <Toaster />
        <Certificates />
      </>,
    );

    fireEvent.click(await screen.findByTitle("Delete"));
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));

    await waitFor(() =>
      expect(screen.getByText(/Remove failed: database is locked/)).toBeInTheDocument(),
    );
  });
});
