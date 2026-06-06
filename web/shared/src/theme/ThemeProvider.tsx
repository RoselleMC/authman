import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

export type ThemeMode = "light" | "dark";

const STORAGE_KEY = "authman.theme";

function detectInitial(): ThemeMode {
  if (typeof window === "undefined") return "light";
  const saved = window.localStorage.getItem(STORAGE_KEY);
  if (saved === "light" || saved === "dark") return saved;
  const prefersDark = window.matchMedia?.("(prefers-color-scheme: dark)").matches;
  return prefersDark ? "dark" : "light";
}

interface ThemeContextValue {
  mode: ThemeMode;
  toggle: () => void;
  setMode: (mode: ThemeMode) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(detectInitial);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", mode);
    try {
      window.localStorage.setItem(STORAGE_KEY, mode);
    } catch {
      // localStorage may be unavailable; ignore.
    }
  }, [mode]);

  const setMode = useCallback((next: ThemeMode) => setModeState(next), []);
  const toggle = useCallback(() => setModeState((m) => (m === "dark" ? "light" : "dark")), []);

  const value = useMemo(() => ({ mode, toggle, setMode }), [mode, toggle, setMode]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used inside ThemeProvider");
  return ctx;
}
