import { beforeEach } from "vitest";
import "@testing-library/jest-dom/vitest";

// Node 24 exposes `localStorage` as an experimental global that stays
// undefined unless the process was started with `--localstorage-file`, and
// jsdom v30 defers to that host global instead of shimming its own. Provide
// an in-memory Storage on both `window` and `globalThis` so app code that
// touches `localStorage`/`sessionStorage` works uniformly under vitest.
class MemoryStorage implements Storage {
  private store = new Map<string, string>();
  get length(): number {
    return this.store.size;
  }
  clear(): void {
    this.store.clear();
  }
  getItem(key: string): string | null {
    return this.store.has(key) ? this.store.get(key)! : null;
  }
  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null;
  }
  removeItem(key: string): void {
    this.store.delete(key);
  }
  setItem(key: string, value: string): void {
    this.store.set(key, String(value));
  }
}

function installStorage(name: "localStorage" | "sessionStorage"): void {
  const current = (globalThis as unknown as Record<string, unknown>)[name];
  if (current && typeof (current as Storage).clear === "function") return;
  const storage = new MemoryStorage();
  Object.defineProperty(globalThis, name, { value: storage, configurable: true });
  if (typeof window !== "undefined") {
    Object.defineProperty(window, name, { value: storage, configurable: true });
  }
}

installStorage("localStorage");
installStorage("sessionStorage");

beforeEach(() => {
  globalThis.localStorage?.clear();
  globalThis.sessionStorage?.clear();
});
