import { describe, it, expect, afterEach } from "vitest";

import {
  extensionDownloadUrl,
  extensionSourceUrl,
  type Extension,
} from "@/api/extensions";
import {
  PHONEHOME_ALL_COMMANDS,
  PHONEHOME_DESTRUCTIVE_COMMANDS,
  PHONEHOME_SAFE_DEFAULTS,
} from "./buildConfig";

const ADMIN = "s3cr3t-admin-pass";

afterEach(() => {
  localStorage.removeItem("auroraboot_token");
});

describe("extensionDownloadUrl", () => {
  it("is a plain path even when a legacy admin token is in localStorage", () => {
    localStorage.setItem("auroraboot_token", ADMIN);
    expect(extensionDownloadUrl("e-1", "x.raw")).toBe("/api/v1/extensions/e-1/download/x.raw");
  });

  it("encodes the id and filename so neither can inject a path or query separator", () => {
    expect(extensionDownloadUrl("../e-1", "a b?x=1.raw")).toBe(
      "/api/v1/extensions/..%2Fe-1/download/a%20b%3Fx%3D1.raw",
    );
  });
});

describe("extensionSourceUrl", () => {
  // The URL this builds becomes the `source` arg of an `extension` install
  // command: AuroraBoot pushes it to every node in the selector, stores it in
  // the commands table, hands it back to any node polling its own commands and
  // renders it in the dialog's payload preview. The admin password must not be
  // anywhere in it.
  const ready = (over: Partial<Extension> = {}): Extension =>
    ({
      id: "e-1",
      name: "agent-tools",
      type: "sysext",
      phase: "Ready",
      message: "",
      arch: "amd64",
      version: "1.0",
      sourceMode: "image",
      rawFilename: "agent-tools.sysext.raw",
      downloadToken: "ext-token-abcdef",
      createdAt: "",
      updatedAt: "",
      ...over,
    }) as Extension;

  it("carries the extension's own download token, never the admin password", () => {
    localStorage.setItem("auroraboot_token", ADMIN);
    const url = extensionSourceUrl(ready());
    expect(url).toBe(
      "/api/v1/extensions/e-1/download/agent-tools.sysext.raw?token=ext-token-abcdef",
    );
    expect(url).not.toContain(ADMIN);
  });

  it("encodes the id and filename so neither can inject a path or query separator", () => {
    expect(
      extensionSourceUrl(ready({ id: "../e-1", rawFilename: "a b?x=1.raw" })),
    ).toBe(
      "/api/v1/extensions/..%2Fe-1/download/a%20b%3Fx%3D1.raw?token=ext-token-abcdef",
    );
  });

  it("returns null when the extension has no download token", () => {
    // Falling back to the admin token here is exactly the leak, so the caller
    // must be told there is no usable source rather than handed one.
    localStorage.setItem("auroraboot_token", ADMIN);
    expect(extensionSourceUrl(ready({ downloadToken: undefined }))).toBeNull();
  });

  it("returns null when the build produced no .raw", () => {
    expect(extensionSourceUrl(ready({ rawFilename: undefined }))).toBeNull();
  });
});

describe("phonehome command catalogue", () => {
  // The Install Extension dialog sends the `extension` command; the agent
  // refuses any command absent from the baked phonehome.allowed_commands, and
  // an operator can only tick what this catalogue renders. Without the entry
  // the whole feature is unreachable on a provisioned node.
  it("offers `extension`, in the destructive set", () => {
    expect(PHONEHOME_ALL_COMMANDS).toContain("extension");
    expect(PHONEHOME_DESTRUCTIVE_COMMANDS).toContain("extension");
    expect(PHONEHOME_SAFE_DEFAULTS).not.toContain("extension");
  });
});
