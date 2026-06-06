/*
 * Offline username validation. Backend remains the source of truth;
 * this is only a fast preview to surface obvious problems before submit.
 *
 * Rules from docs/frontend.md §3:
 * - length 3..15
 * - allowed [A-Za-z0-9_]
 * - '#', whitespace, control, URL-special chars are invalid
 * - reserved names are rejected
 */

export const RESERVED_OFFLINE_NAMES: ReadonlyArray<string> = [
  "admin",
  "root",
  "system",
  "mojang",
  "minecraft",
  "authman",
  "console",
];

export type UsernameValidationCode =
  | "ok"
  | "too_short"
  | "too_long"
  | "invalid_chars"
  | "reserved";

export interface UsernameValidationResult {
  ok: boolean;
  code: UsernameValidationCode;
  protocolName: string;
}

const ALLOWED = /^[A-Za-z0-9_]+$/;

export function validateOfflineUsername(raw: string): UsernameValidationResult {
  const trimmed = raw.trim();
  const protocolName = trimmed ? `#${trimmed}` : "";

  if (trimmed.length < 3) return { ok: false, code: "too_short", protocolName };
  if (trimmed.length > 15) return { ok: false, code: "too_long", protocolName };
  if (!ALLOWED.test(trimmed)) return { ok: false, code: "invalid_chars", protocolName };
  if (RESERVED_OFFLINE_NAMES.includes(trimmed.toLowerCase())) {
    return { ok: false, code: "reserved", protocolName };
  }
  return { ok: true, code: "ok", protocolName };
}

export function protocolNameOf(raw: string): string {
  const trimmed = raw.trim();
  return trimmed ? `#${trimmed}` : "";
}
