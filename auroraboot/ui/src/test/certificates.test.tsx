import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, within, cleanup } from "@testing-library/react";
import { MemoryRouter } from "react-router";

import { Certificates } from "@/pages/Certificates";

const keySets = [
  { id: "ks-1", name: "with-tpm", hasTpmPcrKey: true, secureBootEnroll: "if-safe", createdAt: "2026-10-01T00:00:00Z" },
  { id: "ks-2", name: "without-tpm", hasTpmPcrKey: false, secureBootEnroll: "if-safe", createdAt: "2026-10-01T00:00:00Z" },
];

function json(body: unknown) {
  return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("Certificates", () => {
  it("shows the TPM badge only for key sets that have a TPM PCR key", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = new URL(String(input), "http://localhost");
        if (url.pathname === "/api/v1/secureboot-keys") return json(keySets);
        return json([]);
      }),
    );

    render(
      <MemoryRouter>
        <Certificates />
      </MemoryRouter>,
    );

    const withRow = (await screen.findByText("with-tpm")).closest("tr")!;
    const withoutRow = (await screen.findByText("without-tpm")).closest("tr")!;
    expect(within(withRow).queryByText("TPM")).not.toBeNull();
    expect(within(withoutRow).queryByText("TPM")).toBeNull();
  });
});
