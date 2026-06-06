export interface ApiEnvelope<T = unknown> {
  data: T | null;
  meta: Record<string, unknown> | null;
  error: ApiErrorBody | null;
}

export interface ApiErrorBody {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

export class ApiError extends Error {
  public readonly status: number;
  public readonly code: string;
  public readonly details: Record<string, unknown>;
  public readonly raw: ApiErrorBody | null;

  constructor(status: number, body: ApiErrorBody | null, fallbackMessage?: string) {
    super(body?.message ?? fallbackMessage ?? "Request failed");
    this.name = "ApiError";
    this.status = status;
    this.code = body?.code ?? inferCodeFromStatus(status);
    this.details = body?.details ?? {};
    this.raw = body;
  }
}

function inferCodeFromStatus(status: number): string {
  if (status === 0) return "system.network";
  if (status === 401) return "auth.unauthenticated";
  if (status === 403) return "auth.forbidden";
  if (status === 404) return "system.not_found";
  if (status === 429) return "system.rate_limited";
  if (status >= 500) return "system.backend_unavailable";
  return "system.unknown";
}
