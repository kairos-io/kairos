import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

import { PRODUCT_NAME, documentTitle } from "./branding";

describe("branding", () => {
  it("names the product Kairos", () => {
    expect(PRODUCT_NAME).toBe("Kairos");
  });

  it("uses the product name alone when no page is given", () => {
    expect(documentTitle()).toBe("Kairos");
    expect(documentTitle("")).toBe("Kairos");
    expect(documentTitle("   ")).toBe("Kairos");
  });

  it("prefixes the page name", () => {
    expect(documentTitle("Nodes")).toBe("Nodes · Kairos");
    expect(documentTitle("  Artifacts ")).toBe("Artifacts · Kairos");
  });

  it("keeps the static index.html title in sync", () => {
    const html = fs.readFileSync(path.resolve(__dirname, "../../index.html"), "utf8");
    expect(html).toContain(`<title>${documentTitle()}</title>`);
  });
});
