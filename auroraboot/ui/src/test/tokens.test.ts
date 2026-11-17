import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const css = readFileSync(resolve(__dirname, "../index.css"), "utf8");

function block(selector: string): string {
  const start = css.indexOf(`${selector} {`);
  if (start < 0) throw new Error(`no ${selector} block in index.css`);
  const end = css.indexOf("}", start);
  return css.slice(start, end);
}

function value(body: string, name: string): string | undefined {
  const m = body.match(new RegExp(`--${name}:\\s*([^;]+);`));
  return m?.[1].trim();
}

const tokens = [
  "primary",
  "primary-hover",
  "primary-soft",
  "primary-foreground",
  "accent",
  "accent-foreground",
  "navy",
  "success",
  "success-foreground",
  "warning",
  "warning-foreground",
  "danger",
  "danger-foreground",
  "info",
  "info-foreground",
  "neutral",
  "neutral-foreground",
];

describe("design tokens", () => {
  const root = block(":root");
  const dark = block(".dark");
  const theme = block("@theme inline");

  it.each(tokens)("defines --%s in :root, .dark and @theme", (name) => {
    expect(value(root, name)).toBeDefined();
    expect(value(dark, name)).toBeDefined();
    expect(value(theme, `color-${name}`)).toBe(`var(--${name})`);
  });

  it("uses the brand orange as the light primary", () => {
    expect(value(root, "primary")?.toLowerCase()).toBe("#ee5007");
  });

  it("uses a neutral surface as the light accent", () => {
    expect(value(root, "accent")?.toLowerCase()).toBe("#f1f5f9");
  });
});
