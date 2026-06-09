/*
 * Constrained downstream-server theme application.
 * Only a narrow allowlist of tokens may be overridden, and we clamp
 * obviously-unreadable color choices back to defaults.
 */

export interface DownstreamPortalTheme {
  primary_color?: string;
  accent_color?: string;
  prefer_dark?: boolean;
  portal_message?: string;
  display_name?: string;
  description?: string;
  logo_url?: string;
  registration_open?: boolean;
}

const HEX = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i;

function parseHex(s: string): { r: number; g: number; b: number } | null {
  if (!HEX.test(s)) return null;
  let hex = s.slice(1);
  if (hex.length === 3) hex = hex.split("").map((c) => c + c).join("");
  return {
    r: parseInt(hex.slice(0, 2), 16),
    g: parseInt(hex.slice(2, 4), 16),
    b: parseInt(hex.slice(4, 6), 16),
  };
}

function luminance(c: { r: number; g: number; b: number }): number {
  const f = (v: number) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
}

function isSafeAsPrimary(hex: string): boolean {
  const c = parseHex(hex);
  if (!c) return false;
  const l = luminance(c);
  // Require some color away from pure white/black.
  return l > 0.04 && l < 0.85;
}

function clampPrimary(hex: string | undefined): string | undefined {
  if (!hex || !isSafeAsPrimary(hex)) return undefined;
  return hex;
}

/**
 * Returns CSS variables to apply to a container scope. Caller is responsible
 * for setting --color-primary etc. inline on a wrapper element so it does not
 * pollute global tokens.
 */
export function downstreamThemeVars(theme: DownstreamPortalTheme | null | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  if (!theme) return out;
  const primary = clampPrimary(theme.primary_color);
  if (primary) {
    out["--color-primary"] = primary;
    out["--color-primary-hover"] = primary;
    out["--color-primary-active"] = primary;
    out["--color-on-primary"] = onPrimaryFor(primary);
  }
  return out;
}

function onPrimaryFor(hex: string): string {
  const c = parseHex(hex);
  if (!c) return "#ffffff";
  return luminance(c) > 0.55 ? "#102015" : "#ffffff";
}
