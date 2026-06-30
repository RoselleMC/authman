const FLAG_CDN_BASE = "https://flagcdn.com";

export function countryFlagUrl(countryCode: string, width = 40) {
  const code = countryCode.trim().toLowerCase();
  const normalizedWidth = Math.max(20, Math.min(160, Math.round(width)));
  return `${FLAG_CDN_BASE}/w${normalizedWidth}/${code}.png`;
}
