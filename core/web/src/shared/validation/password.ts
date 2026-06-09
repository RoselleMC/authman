/*
 * Frontend password preview only. Final policy is backend-defined.
 * docs/frontend.md §18: render backend-provided hints rather than hardcoding final rules.
 */

export interface PasswordPreview {
  acceptable: boolean;
  length: number;
  hints: string[];
}

export function previewPassword(pw: string): PasswordPreview {
  const length = pw.length;
  const hints: string[] = [];
  if (length < 8) hints.push("password.preview.length_min");
  if (!/[A-Za-z]/.test(pw)) hints.push("password.preview.need_letter");
  if (!/[0-9]/.test(pw)) hints.push("password.preview.need_digit");
  return { acceptable: hints.length === 0, length, hints };
}
