import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, fireEvent, render, renderHook, screen } from "@testing-library/react";

import { THEME_KEY, getStoredTheme, resolveTheme } from "@/lib/theme";
import { useTheme } from "@/hooks/useTheme";
import { ThemeToggle } from "@/components/ThemeToggle";

type Listener = (e: { matches: boolean }) => void;

let prefersDark = false;
let listeners: Listener[] = [];

function stubMatchMedia() {
  listeners = [];
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      get matches() {
        return prefersDark;
      },
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn((_: string, fn: Listener) => listeners.push(fn)),
      removeEventListener: vi.fn((_: string, fn: Listener) => {
        listeners = listeners.filter((l) => l !== fn);
      }),
      dispatchEvent: vi.fn(),
    })),
  });
}

function isDark() {
  return document.documentElement.classList.contains("dark");
}

beforeEach(() => {
  prefersDark = false;
  window.localStorage.clear();
  document.documentElement.classList.remove("dark");
  stubMatchMedia();
});

describe("theme helpers", () => {
  it("defaults to system and resolves it from matchMedia", () => {
    expect(getStoredTheme()).toBe("system");
    expect(resolveTheme("system")).toBe("light");
    prefersDark = true;
    expect(resolveTheme("system")).toBe("dark");
    expect(resolveTheme("light")).toBe("light");
  });

  it("falls back to system when storage throws", () => {
    const spy = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(getStoredTheme()).toBe("system");
    spy.mockRestore();
  });
});

describe("useTheme", () => {
  it("defaults to system and follows a dark matchMedia", () => {
    prefersDark = true;
    const { result } = renderHook(() => useTheme());
    expect(result.current.mode).toBe("system");
    expect(result.current.resolved).toBe("dark");
    expect(isDark()).toBe(true);
  });

  it("updates when the OS setting changes in system mode", () => {
    const { result } = renderHook(() => useTheme());
    expect(isDark()).toBe(false);
    prefersDark = true;
    act(() => listeners.forEach((l) => l({ matches: true })));
    expect(result.current.resolved).toBe("dark");
    expect(isDark()).toBe(true);
  });

  it("setMode('light') removes the class and persists", () => {
    prefersDark = true;
    const { result } = renderHook(() => useTheme());
    expect(isDark()).toBe(true);
    act(() => result.current.setMode("light"));
    expect(result.current.mode).toBe("light");
    expect(isDark()).toBe(false);
    expect(window.localStorage.getItem(THEME_KEY)).toBe("light");
  });

  it("applies a stored dark mode on mount", () => {
    window.localStorage.setItem(THEME_KEY, "dark");
    const { result } = renderHook(() => useTheme());
    expect(result.current.mode).toBe("dark");
    expect(isDark()).toBe(true);
  });
});

describe("ThemeToggle", () => {
  it("sets the modes from its three buttons", () => {
    render(<ThemeToggle />);
    const dark = screen.getByRole("button", { name: "Dark theme" });
    fireEvent.click(dark);
    expect(isDark()).toBe(true);
    expect(dark).toHaveAttribute("aria-pressed", "true");
    expect(window.localStorage.getItem(THEME_KEY)).toBe("dark");

    fireEvent.click(screen.getByRole("button", { name: "Light theme" }));
    expect(isDark()).toBe(false);
    expect(window.localStorage.getItem(THEME_KEY)).toBe("light");

    fireEvent.click(screen.getByRole("button", { name: "System theme" }));
    expect(window.localStorage.getItem(THEME_KEY)).toBe("system");
    expect(screen.getByRole("button", { name: "System theme" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("cycles through the modes when compact", () => {
    render(<ThemeToggle compact />);
    fireEvent.click(screen.getByRole("button", { name: "Theme: system" }));
    expect(window.localStorage.getItem(THEME_KEY)).toBe("light");
    fireEvent.click(screen.getByRole("button", { name: "Theme: light" }));
    expect(isDark()).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Theme: dark" }));
    expect(window.localStorage.getItem(THEME_KEY)).toBe("system");
  });
});
