import { describe, it, expect } from "vitest";
import { timeAgo, formatBytes } from "@/lib/time";

describe("timeAgo", () => {
  const now = Date.parse("2026-09-29T12:00:00Z");
  const ago = (ms: number) => new Date(now - ms).toISOString();

  it("returns Never for an empty date", () => {
    expect(timeAgo(null, now)).toBe("Never");
    expect(timeAgo(undefined, now)).toBe("Never");
    expect(timeAgo("", now)).toBe("Never");
  });

  it("returns Never for an unparseable date", () => {
    expect(timeAgo("not a date", now)).toBe("Never");
  });

  it("uses seconds below one minute", () => {
    expect(timeAgo(ago(0), now)).toBe("0s ago");
    expect(timeAgo(ago(12_000), now)).toBe("12s ago");
    expect(timeAgo(ago(59_999), now)).toBe("59s ago");
  });

  it("uses minutes below one hour", () => {
    expect(timeAgo(ago(60_000), now)).toBe("1m ago");
    expect(timeAgo(ago(5 * 60_000), now)).toBe("5m ago");
    expect(timeAgo(ago(3_599_999), now)).toBe("59m ago");
  });

  it("uses hours below one day", () => {
    expect(timeAgo(ago(3_600_000), now)).toBe("1h ago");
    expect(timeAgo(ago(3 * 3_600_000), now)).toBe("3h ago");
    expect(timeAgo(ago(86_399_999), now)).toBe("23h ago");
  });

  it("uses days from one day", () => {
    expect(timeAgo(ago(86_400_000), now)).toBe("1d ago");
    expect(timeAgo(ago(2 * 86_400_000), now)).toBe("2d ago");
  });

  it("treats a date in the future as now", () => {
    expect(timeAgo(new Date(now + 5000).toISOString(), now)).toBe("0s ago");
  });
});

describe("formatBytes", () => {
  it("formats bytes in binary units with one decimal", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1024)).toBe("1.0 KiB");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(8024512 * 1024)).toBe("7.7 GiB");
    expect(formatBytes(1024 ** 4)).toBe("1.0 TiB");
  });
});
