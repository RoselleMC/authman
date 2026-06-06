import { BrandMark, ThemeToggle, useI18n } from "@authman/shared";

interface Props {
  /** Sub-label shown next to the wordmark; usually the server display name. */
  sub?: string;
}

/**
 * Compact top bar shown above unauthenticated portal cards.
 * Provides the locale + theme toggles the Playwright suite and accessibility tooling expect.
 */
export function AuthHeader({ sub }: Props) {
  const { t, locale, setLocale } = useI18n();
  return (
    <div className="pauth-top">
      <BrandMark sub={sub} />
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <button
          type="button"
          className="iconbtn iconbtn--bordered"
          onClick={() => setLocale(locale === "en" ? "zh" : "en")}
          data-testid="locale-toggle"
          aria-label={t("common.locale.toggle")}
          style={{ width: "auto", padding: "0 10px", fontSize: 12, fontWeight: 540 }}
        >
          {locale.toUpperCase()}
        </button>
        <ThemeToggle />
      </div>
    </div>
  );
}
