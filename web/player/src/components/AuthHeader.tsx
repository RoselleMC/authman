import { BrandMark, LocaleSelect, ThemeToggle } from "@authman/shared";

interface Props {
  /** Sub-label shown next to the wordmark; usually the server display name. */
  sub?: string;
}

/**
 * Compact top bar shown above unauthenticated portal cards.
 * Provides the locale + theme toggles the Playwright suite and accessibility tooling expect.
 */
export function AuthHeader({ sub }: Props) {
  return (
    <div className="pauth-top">
      <BrandMark sub={sub} />
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <LocaleSelect />
        <ThemeToggle />
      </div>
    </div>
  );
}
