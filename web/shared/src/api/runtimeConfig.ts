/*
 * Runtime configuration is injected via /config.js in production containers
 * and via Vite env vars in development. Hostnames are never hardcoded.
 */

export interface RuntimeConfig {
  apiBase: string;
  appKind: "admin" | "player";
  defaultLocale: string;
}

declare global {
  // eslint-disable-next-line no-var
  var __AUTHMAN_RUNTIME_CONFIG__: Partial<RuntimeConfig> | undefined;
}

function readViteEnv(key: string): string | undefined {
  try {
    // import.meta.env is available in Vite-built bundles.
    const env = (import.meta as { env?: Record<string, string | undefined> }).env;
    return env?.[key];
  } catch {
    return undefined;
  }
}

let cached: RuntimeConfig | null = null;

export function getRuntimeConfig(): RuntimeConfig {
  if (cached) return cached;

  const injected = typeof globalThis !== "undefined" ? globalThis.__AUTHMAN_RUNTIME_CONFIG__ : undefined;

  const apiBase =
    injected?.apiBase ??
    readViteEnv("VITE_AUTHMAN_API_BASE") ??
    "/api";

  const appKindRaw =
    injected?.appKind ??
    readViteEnv("VITE_AUTHMAN_APP_KIND") ??
    "player";
  const appKind: RuntimeConfig["appKind"] = appKindRaw === "admin" ? "admin" : "player";

  const defaultLocale =
    injected?.defaultLocale ??
    readViteEnv("VITE_AUTHMAN_DEFAULT_LOCALE") ??
    "en";

  cached = { apiBase: stripTrailingSlash(apiBase), appKind, defaultLocale };
  return cached;
}

function stripTrailingSlash(s: string): string {
  return s.endsWith("/") ? s.slice(0, -1) : s;
}

// Test/dev only.
export function _resetRuntimeConfigForTests(): void {
  cached = null;
}
