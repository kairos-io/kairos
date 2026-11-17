import { describe, it, expect, vi } from "vitest";

import { uploadOverlayFiles } from "@/api/artifacts";
import { ApiError } from "@/api/client";

const OVERLAY_ID = "6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a";

describe("uploadOverlayFiles", () => {
  it("posts the files and returns the overlay ID", async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ id: OVERLAY_ID }), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const id = await uploadOverlayFiles([new File(["hello\n"], "motd")]);

    expect(id).toBe(OVERLAY_ID);
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe("/api/v1/artifacts/upload-overlay");
    expect(init.method).toBe("POST");
    expect((init.body as FormData).getAll("files")).toHaveLength(1);
    expect(new Headers(init.headers).has("Authorization")).toBe(false);
  });

  it("throws the server's error message", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(JSON.stringify({ error: "no files uploaded" }), { status: 400 }),
      ),
    );

    const err = await uploadOverlayFiles([new File(["x"], "motd")]).catch((e) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(400);
    expect(err.message).toBe("no files uploaded");
  });

  it("throws the raw body when it is not JSON", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("request entity too large", { status: 413 })),
    );

    const err = await uploadOverlayFiles([new File(["x"], "motd")]).catch((e) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect(err.message).toBe("request entity too large");
  });

  it("refuses a response without an overlay ID", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("{}", { status: 200 })));

    await expect(uploadOverlayFiles([new File(["x"], "motd")])).rejects.toThrow(
      "Upload returned no overlay ID",
    );
  });
});
