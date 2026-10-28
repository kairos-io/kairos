// Theme mode helpers. No React: used by the useTheme hook and by pages that
// only need to apply the stored mode (the login screen).

export type ThemeMode = "system" | "light" | "dark";

export const THEME_KEY = "auroraboot_theme";

export const DARK_MEDIA_QUERY = "(prefers-color-scheme: dark)";

function isThemeMode(v: unknown): v is ThemeMode {
  return v === "system" || v === "light" || v === "dark";
}

// getStoredTheme returns the stored mode, or "system" when nothing valid is
// stored or storage cannot be read.
export function getStoredTheme(): ThemeMode {
  try {
    const v = window.localStorage.getItem(THEME_KEY);
    return isThemeMode(v) ? v : "system";
  } catch {
    return "system";
  }
}

export function setStoredTheme(m: ThemeMode): void {
  try {
    window.localStorage.setItem(THEME_KEY, m);
  } catch {
    // Storage blocked: the mode still applies for this page load.
  }
}

export function systemPrefersDark(): boolean {
  try {
    return typeof window.matchMedia === "function" && window.matchMedia(DARK_MEDIA_QUERY).matches;
  } catch {
    return false;
  }
}

export function resolveTheme(m: ThemeMode): "light" | "dark" {
  if (m === "system") return systemPrefersDark() ? "dark" : "light";
  return m;
}

// applyTheme toggles the `dark` class on <html> for the given mode.
export function applyTheme(m: ThemeMode): void {
  document.documentElement.classList.toggle("dark", resolveTheme(m) === "dark");
}
