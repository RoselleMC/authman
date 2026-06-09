import { ApiError, type ApiEnvelope } from "./envelope";
import { getRuntimeConfig } from "./runtimeConfig";

const CSRF_HEADER = "X-CSRF-Token";

let csrfToken: string | null = null;

export function setCsrfToken(token: string | null): void {
  csrfToken = token;
}

export function getCsrfToken(): string | null {
  return csrfToken;
}

interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined | null>;
  signal?: AbortSignal;
  skipCsrf?: boolean;
}

export interface ApiResponse<T> {
  data: T;
  meta: Record<string, unknown> | null;
}

function buildUrl(path: string, query?: RequestOptions["query"]): string {
  const cfg = getRuntimeConfig();
  const base = cfg.apiBase;
  const url = path.startsWith("/") ? `${base}${path}` : `${base}/${path}`;
  if (!query) return url;
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === "") continue;
    params.append(k, String(v));
  }
  const qs = params.toString();
  return qs ? `${url}?${qs}` : url;
}

export async function apiFetch<T>(path: string, opts: RequestOptions = {}): Promise<ApiResponse<T>> {
  const method = opts.method ?? "GET";
  const headers: Record<string, string> = {
    Accept: "application/json",
  };

  if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }

  const mutating = method !== "GET";
  if (mutating && !opts.skipCsrf && csrfToken) {
    headers[CSRF_HEADER] = csrfToken;
  }

  let res: Response;
  try {
    res = await fetch(buildUrl(path, opts.query), {
      method,
      headers,
      credentials: "include",
      body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      signal: opts.signal,
    });
  } catch (err) {
    if ((err as { name?: string }).name === "AbortError") throw err;
    throw new ApiError(0, null, "Network error");
  }

  const text = await res.text();
  let envelope: ApiEnvelope<T> | null = null;
  if (text.length > 0) {
    try {
      envelope = JSON.parse(text) as ApiEnvelope<T>;
    } catch {
      envelope = null;
    }
  }

  if (!res.ok || envelope?.error) {
    throw new ApiError(res.status, envelope?.error ?? null, res.statusText);
  }

  return {
    data: (envelope?.data ?? null) as T,
    meta: envelope?.meta ?? null,
  };
}
