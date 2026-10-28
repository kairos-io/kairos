import { describe, it, expect } from "vitest";
import fs from "node:fs";
import path from "node:path";

// Every .tsx file under src/ (except src/test/) must use design tokens
// instead of hard-coded brand colors.
const SRC = path.resolve(__dirname, "..");
const SKIP_DIRS = new Set([path.join(SRC, "test")]);

// Files that may keep a brand hex literal. Keep this empty.
const ALLOWLIST: string[] = [];

const BRAND_HEX = /#(EE5007|FF7442|C73F00|03153A|FFB380|051d52)\b/i;

function collectTsx(dir: string): string[] {
  const out: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!SKIP_DIRS.has(full)) out.push(...collectTsx(full));
    } else if (entry.isFile() && entry.name.endsWith(".tsx")) {
      out.push(full);
    }
  }
  return out;
}

describe("no hard-coded brand colors", () => {
  it("scans the source tree", () => {
    const files = collectTsx(SRC).map((f) => path.relative(SRC, f));
    expect(files).toContain(path.join("pages", "Dashboard.tsx"));
    expect(files.some((f) => f.startsWith("test" + path.sep))).toBe(false);
  });

  it("uses design tokens instead of brand hex literals", () => {
    const hits: string[] = [];
    for (const full of collectTsx(SRC)) {
      const rel = path.relative(path.resolve(SRC, ".."), full);
      if (ALLOWLIST.includes(rel)) continue;
      const lines = fs.readFileSync(full, "utf8").split("\n");
      lines.forEach((line, i) => {
        if (BRAND_HEX.test(line)) hits.push(`${rel}:${i + 1}`);
      });
    }
    expect(hits, `hard-coded brand colors found:\n${hits.join("\n")}`).toEqual([]);
  });
});
