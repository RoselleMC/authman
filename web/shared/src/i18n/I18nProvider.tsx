import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { STRING_TABLES, SUPPORTED_LOCALES, type Locale } from "./strings";

const STORAGE_KEY = "authman.locale";

interface I18nContextValue {
  locale: Locale;
  setLocale: (l: Locale) => void;
  t: (key: string, fallback?: string) => string;
  tError: (code: string | undefined) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

function pickInitial(defaultLocale: string): Locale {
  if (typeof window !== "undefined") {
    const saved = window.localStorage.getItem(STORAGE_KEY);
    if (saved && (SUPPORTED_LOCALES as readonly string[]).includes(saved)) return saved as Locale;
    const nav = window.navigator?.language?.slice(0, 2).toLowerCase();
    if (nav && (SUPPORTED_LOCALES as readonly string[]).includes(nav)) return nav as Locale;
  }
  return (SUPPORTED_LOCALES as readonly string[]).includes(defaultLocale)
    ? (defaultLocale as Locale)
    : "en";
}

export function I18nProvider({ defaultLocale, children }: { defaultLocale: string; children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => pickInitial(defaultLocale));

  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, locale);
    } catch {
      // ignore
    }
    document.documentElement.lang = locale;
  }, [locale]);

  const setLocale = useCallback((l: Locale) => setLocaleState(l), []);

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

  const value = useMemo(() => ({ locale, setLocale, t, tError }), [locale, setLocale, t, tError]);
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used inside I18nProvider");
  return ctx;
}
