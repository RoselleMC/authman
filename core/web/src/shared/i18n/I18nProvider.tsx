import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { STRING_TABLES, SUPPORTED_LOCALES, type Locale } from "./strings";

const STORAGE_KEY = "authman.locale";

export type LocalePreference = Locale | "system";

interface I18nContextValue {
  locale: Locale;
  localePreference: LocalePreference;
  setLocale: (l: Locale) => void;
  setLocalePreference: (l: LocalePreference) => void;
  t: (key: string, fallback?: string) => string;
  tError: (code: string | undefined) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

function isLocale(value: string | null | undefined): value is Locale {
  return !!value && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}

function pickSystemLocale(defaultLocale: string): Locale {
  if (typeof window !== "undefined") {
    const nav = window.navigator?.language?.slice(0, 2).toLowerCase();
    if (isLocale(nav)) return nav;
  }
  return isLocale(defaultLocale) ? defaultLocale : "en";
}

function pickInitialPreference(): LocalePreference {
  if (typeof window === "undefined") return "system";
  const saved = window.localStorage.getItem(STORAGE_KEY);
  if (saved === "system") return "system";
  if (isLocale(saved)) return saved;
  return "system";
}

export function I18nProvider({ defaultLocale, children }: { defaultLocale: string; children: ReactNode }) {
  const [localePreference, setLocalePreferenceState] = useState<LocalePreference>(() => pickInitialPreference());
  const [systemLocale, setSystemLocale] = useState<Locale>(() => pickSystemLocale(defaultLocale));
  const locale = localePreference === "system" ? systemLocale : localePreference;

  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, localePreference);
    } catch {
      // ignore
    }
    document.documentElement.lang = locale;
  }, [locale, localePreference]);

  useEffect(() => {
    function syncSystemLocale() {
      setSystemLocale(pickSystemLocale(defaultLocale));
    }
    syncSystemLocale();
    window.addEventListener("languagechange", syncSystemLocale);
    return () => window.removeEventListener("languagechange", syncSystemLocale);
  }, [defaultLocale]);

  const setLocale = useCallback((l: Locale) => setLocalePreferenceState(l), []);
  const setLocalePreference = useCallback((l: LocalePreference) => setLocalePreferenceState(l), []);

  const t = useCallback(
    (key: string, fallback?: string): string => {
      const table = STRING_TABLES[locale];
      const fallbackTable = STRING_TABLES.en;
      return table[key] ?? fallbackTable[key] ?? fallback ?? key;
    },
    [locale],
  );

  const tError = useCallback(
    (code: string | undefined): string => {
      if (!code) return t("common.unknown");
      const key = `errors.${code}`;
      const direct = STRING_TABLES[locale][key] ?? STRING_TABLES.en[key];
      if (direct) return direct;
      // try namespace fallback like errors.auth.unknown
      const ns = code.split(".")[0];
      const nsKey = `errors.${ns}.unknown`;
      const nsHit = STRING_TABLES[locale][nsKey] ?? STRING_TABLES.en[nsKey];
      if (nsHit) return nsHit;
      return t("common.unknown");
    },
    [locale, t],
  );

  const value = useMemo(
    () => ({ locale, localePreference, setLocale, setLocalePreference, t, tError }),
    [locale, localePreference, setLocale, setLocalePreference, t, tError],
  );
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used inside I18nProvider");
  return ctx;
}
