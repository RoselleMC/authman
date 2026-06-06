export function formatRelativeTime(input: string | number | Date | null | undefined): string {
  if (input === null || input === undefined) return "—";
  const d = new Date(input);
  if (Number.isNaN(d.getTime())) return String(input);
  const now = Date.now();
  const diff = (d.getTime() - now) / 1000;
  const abs = Math.abs(diff);
  if (abs < 60) return rtf(currentLocale()).format(Math.round(diff), "second");
  if (abs < 3600) return rtf(currentLocale()).format(Math.round(diff / 60), "minute");
  if (abs < 86400) return rtf(currentLocale()).format(Math.round(diff / 3600), "hour");
  if (abs < 86400 * 30) return rtf(currentLocale()).format(Math.round(diff / 86400), "day");
  return d.toLocaleString(currentLocale());
}

let _rtf: Intl.RelativeTimeFormat | null = null;
let _rtfLocale = "";

function currentLocale(): string | undefined {
  if (typeof document === "undefined") return undefined;
  return document.documentElement.lang || undefined;
}

function rtf(locale: string | undefined): Intl.RelativeTimeFormat {
  const key = locale ?? "";
  if (!_rtf || _rtfLocale !== key) {
    _rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
    _rtfLocale = key;
  }
  return _rtf;
}

export function formatAbsTime(input: string | number | Date | null | undefined): string {
  if (input === null || input === undefined) return "—";
  const d = new Date(input);
  if (Number.isNaN(d.getTime())) return String(input);
  return d.toLocaleString(currentLocale());
}
