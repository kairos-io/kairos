import { useCallback, useEffect, useState } from "react";
import {
  DARK_MEDIA_QUERY,
  applyTheme,
  getStoredTheme,
  resolveTheme,
  setStoredTheme,
  type ThemeMode,
} from "@/lib/theme";

// useTheme owns the theme mode: it applies the mode to <html>, persists
// changes, and follows the OS setting while the mode is "system".
export function useTheme(): {
  mode: ThemeMode;
  resolved: "light" | "dark";
  setMode(m: ThemeMode): void;
} {
  const [mode, setModeState] = useState<ThemeMode>(() => getStoredTheme());
  const [resolved, setResolved] = useState<"light" | "dark">(() => resolveTheme(mode));

  useEffect(() => {
    applyTheme(mode);
    if (mode !== "system" || typeof window.matchMedia !== "function") return;
    const mql = window.matchMedia(DARK_MEDIA_QUERY);
    const onChange = () => {
      applyTheme("system");
      setResolved(resolveTheme("system"));
    };
    mql.addEventListener?.("change", onChange);
    return () => mql.removeEventListener?.("change", onChange);
  }, [mode]);

  const setMode = useCallback((m: ThemeMode) => {
    setStoredTheme(m);
    setModeState(m);
    setResolved(resolveTheme(m));
  }, []);

  return { mode, resolved, setMode };
}
