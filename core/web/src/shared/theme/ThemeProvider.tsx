import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

export type ThemeMode = "light" | "dark";
export type ThemePreference = ThemeMode | "system";

const STORAGE_KEY = "authman.theme";

function systemMode(): ThemeMode {
  if (typeof window === "undefined") return "light";
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function detectInitial(): ThemePreference {
  if (typeof window === "undefined") return "system";
  const saved = window.localStorage.getItem(STORAGE_KEY);
  if (saved === "light" || saved === "dark" || saved === "system") return saved;
  return "system";
}

interface ThemeContextValue {
  preference: ThemePreference;
  effectiveMode: ThemeMode;
  mode: ThemeMode;
  toggle: () => void;
  setMode: (mode: ThemePreference) => void;
  setPreference: (mode: ThemePreference) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<ThemePreference>(detectInitial);
  const [system, setSystem] = useState<ThemeMode>(systemMode);
  const effectiveMode: ThemeMode = preference === "system" ? system : preference;

  useEffect(() => {
    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    if (!media) return undefined;
    function onChange() {
      setSystem(media.matches ? "dark" : "light");
    }
    onChange();
    media.addEventListener?.("change", onChange);
    return () => media.removeEventListener?.("change", onChange);
  }, []);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", effectiveMode);
    try {
      window.localStorage.setItem(STORAGE_KEY, preference);
    } catch {
      // localStorage may be unavailable; ignore.
    }
  }, [effectiveMode, preference]);

  const setPreference = useCallback((next: ThemePreference) => setPreferenceState(next), []);
  const setMode = setPreference;
  const toggle = useCallback(() => setPreferenceState((m) => (m === "dark" ? "light" : "dark")), []);

  const value = useMemo(
    () => ({ preference, effectiveMode, mode: effectiveMode, toggle, setMode, setPreference }),
    [preference, effectiveMode, toggle, setMode, setPreference],
  );
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used inside ThemeProvider");
  return ctx;
}
