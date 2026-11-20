import { describe, it, expect } from "vitest";
import {
  phaseTone,
  isOnline,
  isOffline,
  isBuilding,
  isReady,
  isFailed,
  isRunning,
} from "@/lib/phase";

describe("phaseTone", () => {
  const cases: Array<[string | undefined | null, string]> = [
    ["online", "success"],
    ["Ready", "success"],
    ["COMPLETED", "success"],
    ["active", "success"],
    ["Pending", "warning"],
    ["queued", "warning"],
    ["Delivered", "warning"],
    ["Offline", "danger"],
    ["failed", "danger"],
    ["Error", "danger"],
    ["Running", "info"],
    ["Building", "info"],
    ["upgrading", "info"],
    ["Installing", "info"],
    ["Registered", "neutral"],
    ["expired", "neutral"],
    ["unknown", "neutral"],
    ["", "neutral"],
    [undefined, "neutral"],
    [null, "neutral"],
    ["something-else", "neutral"],
  ];
  it.each(cases)("maps %s to %s", (phase, tone) => {
    expect(phaseTone(phase)).toBe(tone);
  });
});

describe("phase predicates", () => {
  it("compare case-insensitively", () => {
    expect(isOnline("Online")).toBe(true);
    expect(isOnline("online")).toBe(true);
    expect(isOnline("Pending")).toBe(false);
    expect(isOffline("OFFLINE")).toBe(true);
    expect(isOffline("Registered")).toBe(false);
    expect(isOffline("Pending")).toBe(false);
    expect(isBuilding("Building")).toBe(true);
    expect(isBuilding("building")).toBe(true);
    expect(isBuilding("Ready")).toBe(false);
    expect(isReady("Ready")).toBe(true);
    expect(isReady("ready")).toBe(true);
  });

  it("treat failed and error as failed", () => {
    expect(isFailed("Failed")).toBe(true);
    expect(isFailed("error")).toBe(true);
    expect(isFailed("Offline")).toBe(false);
  });

  it("treat running, building, upgrading and installing as running", () => {
    for (const p of ["Running", "building", "Upgrading", "installing"]) {
      expect(isRunning(p)).toBe(true);
    }
    expect(isRunning("Pending")).toBe(false);
  });

  it("return false for missing phases", () => {
    for (const fn of [isOnline, isOffline, isBuilding, isReady, isFailed, isRunning]) {
      expect(fn(undefined)).toBe(false);
      expect(fn(null)).toBe(false);
      expect(fn("")).toBe(false);
    }
  });
});
