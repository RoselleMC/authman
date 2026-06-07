/*
 * API compatibility helpers.
 *
 * Pages must consume API responses through these adapters rather than indexing
 * raw types directly. This prevents white-screens when the backend ships a new
 * field name, drops an optional field, or returns a value in a slightly
 * different shape than the TypeScript type advertises.
 *
 * Rules of thumb:
 * - Treat every backend field as potentially missing or of an unexpected type.
 * - Provide a sane default for every consumed field.
 * - Never throw from a coercer; return a usable, partial value instead.
 * - Coercers MUST be pure and synchronous.
 */

export function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

export function asString(v: unknown, fallback = ""): string {
  if (typeof v === "string") return v;
  if (typeof v === "number" && Number.isFinite(v)) return String(v);
  if (typeof v === "boolean") return v ? "true" : "false";
  return fallback;
}

export function asNumber(v: unknown, fallback = 0): number {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string") {
    const parsed = Number(v);
    if (Number.isFinite(parsed)) return parsed;
  }
  return fallback;
}

export function asBoolean(v: unknown, fallback = false): boolean {
  if (typeof v === "boolean") return v;
  if (v === "true" || v === 1) return true;
  if (v === "false" || v === 0) return false;
  return fallback;
}

export function asArray<T>(v: unknown, mapItem?: (item: unknown, index: number) => T): T[] {
  if (!Array.isArray(v)) return [];
  if (!mapItem) return v as T[];
  return v.map(mapItem);
}

export function asRecord(v: unknown): Record<string, unknown> {
  return isObject(v) ? v : {};
}

/**
 * Return the first non-null/undefined/empty-string value from a candidate list.
 * Useful for accepting either snake_case or camelCase field names from a backend
 * still settling on a convention.
 */
export function firstDefined<T>(...candidates: Array<T | null | undefined>): T | undefined {
  for (const candidate of candidates) {
    if (candidate !== null && candidate !== undefined && candidate !== "") return candidate;
  }
  return undefined;
}

/**
 * Map an unknown enum-ish value to a known set; fall back when unknown.
 */
export function mapEnum<T extends string>(
  v: unknown,
  known: ReadonlyArray<T>,
  fallback: T,
): T {
  if (typeof v === "string" && (known as readonly string[]).includes(v)) {
    return v as T;
  }
  return fallback;
}

/**
 * Try to read a property from a record-ish value, returning undefined cleanly
 * even when the input is null or not an object.
 */
export function pick(record: unknown, key: string): unknown {
  if (!isObject(record)) return undefined;
  return record[key];
}

/* --------------------------------------------------------------------------
 * Per-resource coercers.
 *
 * Each page in admin-web / player-web should consume the output of these,
 * not the raw API type. The raw TypeScript type stays as documentation but
 * pages must treat fields as potentially missing.
 * -------------------------------------------------------------------------- */

export interface SafeSystemSummary {
  version: string;
  environment: string;
  database: string;
  uptime_seconds: number | null;
  feature_flags: Record<string, boolean>;
  extra_rows: Array<{ k: string; v: string }>;
}

export function coerceSystemSummary(raw: unknown): SafeSystemSummary {
  const r = asRecord(raw);
  const version = asString(firstDefined(r.version, r.app_version, r.build_version), "0.0.0-dev");
  const environment = asString(
    firstDefined(r.environment, r.env, r.runtime_environment),
    "unknown",
  );
  const database = asString(firstDefined(r.database, r.db, r.database_kind), environment);
  const uptimeRaw = firstDefined(r.uptime_seconds, r.uptime);
  const uptime = typeof uptimeRaw === "number" && Number.isFinite(uptimeRaw) ? uptimeRaw : null;
  const flagsRaw = asRecord(r.feature_flags ?? r.flags);
  const feature_flags: Record<string, boolean> = {};
  for (const [k, v] of Object.entries(flagsRaw)) feature_flags[k] = asBoolean(v, false);

  // Collect unknown scalar fields under extra_rows so the operator can still
  // see them when the backend ships new keys before the frontend knows them.
  const KNOWN = new Set([
    "version",
    "app_version",
    "build_version",
    "environment",
    "env",
    "runtime_environment",
    "database",
    "db",
    "database_kind",
    "uptime_seconds",
    "uptime",
    "feature_flags",
    "flags",
  ]);
  const extra_rows: Array<{ k: string; v: string }> = [];
  for (const [k, v] of Object.entries(r)) {
    if (KNOWN.has(k)) continue;
    if (typeof v === "string" || typeof v === "number" || typeof v === "boolean") {
      extra_rows.push({ k, v: String(v) });
    }
  }
  return { version, environment, database, uptime_seconds: uptime, feature_flags, extra_rows };
}

const MOJANG_OVERALL_TONES = {
  direct_healthy: "success",
  proxy_healthy: "success",
  mojang_healthy: "success",
  proxy_rate_limited: "warning",
  mojang_degraded: "warning",
  mojang_unavailable_stale: "warning",
  mojang_disabled: "warning",
  proxy_failed: "danger",
  mojang_unavailable: "danger",
  mojang_unavailable_nocache: "danger",
} as const;

export type MojangOverallTone = "success" | "warning" | "danger";

export function coerceMojangOverall(value: unknown): {
  key: string;
  tone: MojangOverallTone;
} {
  const key = asString(value, "mojang_degraded");
  const tone: MojangOverallTone =
    (MOJANG_OVERALL_TONES as Record<string, MojangOverallTone | undefined>)[key] ?? "warning";
  return { key, tone };
}

export interface SafeMojangProxy {
  id: string;
  kind: "direct" | "http" | "socks5";
  url_masked: string;
  state: string;
  weight: number;
  request_count: number;
  cooldown_remaining_seconds: number;
  last_error_at: string | null;
}

export function coerceMojangProxy(raw: unknown): SafeMojangProxy {
  const r = asRecord(raw);
  const kind = mapEnum(r.kind, ["direct", "http", "socks5"] as const, "direct");
  return {
    id: asString(r.id, "unknown"),
    kind,
    url_masked: asString(r.url_masked, kind === "direct" ? "(direct)" : ""),
    state: asString(r.state, "healthy"),
    weight: asNumber(r.weight, 1),
    request_count: asNumber(firstDefined(r.recent_request_count, r.failure_count, r.rate_limit_count, r.request_count), 0),
    cooldown_remaining_seconds: asNumber(r.cooldown_remaining_seconds, 0),
    last_error_at: typeof r.last_error_at === "string" ? r.last_error_at : null,
  };
}

export interface SafeMojangEvent {
  id: string;
  proxy_id: string;
  event_type: string;
  status_code: number | null;
  retry_after: string | null;
  created_at: string;
}

export function coerceMojangEvent(raw: unknown): SafeMojangEvent {
  const r = asRecord(raw);
  return {
    id: asString(r.id, ""),
    proxy_id: asString(r.proxy_id, "—"),
    event_type: asString(r.event_type, "unknown"),
    status_code: typeof r.status_code === "number" ? r.status_code : null,
    retry_after: typeof r.retry_after === "string" ? r.retry_after : null,
    created_at: asString(r.created_at, ""),
  };
}

export interface SafeMojangStatus {
  overall: { key: string; tone: MojangOverallTone };
  proxies: SafeMojangProxy[];
  events: SafeMojangEvent[];
}

export function coerceMojangStatus(raw: unknown): SafeMojangStatus {
  const r = asRecord(raw);
  return {
    overall: coerceMojangOverall(r.overall),
    proxies: asArray(r.proxies, coerceMojangProxy),
    events: asArray(r.events, coerceMojangEvent),
  };
}

export interface SafeAdminUser {
  id: string;
  email: string;
  display_name: string;
  role: "owner" | "admin" | "auditor";
  status: "active" | "disabled";
  created_at: string | null;
}

export function coerceAdminUser(raw: unknown): SafeAdminUser {
  const r = asRecord(raw);
  return {
    id: asString(r.id, ""),
    email: asString(r.email, ""),
    display_name: asString(firstDefined(r.display_name, r.name), asString(r.email, "—")),
    role: mapEnum(r.role, ["owner", "admin", "auditor"] as const, "admin"),
    status: mapEnum(r.status, ["active", "disabled"] as const, "active"),
    created_at: typeof r.created_at === "string" ? r.created_at : null,
  };
}

export interface SafeVelocityNode {
  id: string;
  name: string;
  server_id: string;
  server_label: string;
  status: "active" | "disabled" | "stale";
  last_seen_at: string | null;
  token_fingerprint: string;
  instance_fingerprint: string;
  plugin_version: string;
  velocity_version: string;
  created_at: string | null;
}

export function coerceVelocityNode(raw: unknown): SafeVelocityNode {
  const r = asRecord(raw);
  return {
    id: asString(r.id, ""),
    name: asString(r.name, "—"),
    server_id: asString(r.server_id, ""),
    server_label: asString(firstDefined(r.server_label, r.server_name), "—"),
    status: mapEnum(r.status, ["active", "disabled", "stale"] as const, "stale"),
    last_seen_at: typeof r.last_seen_at === "string" ? r.last_seen_at : null,
    token_fingerprint: asString(firstDefined(r.token_fingerprint, r.fingerprint, r.token_hint), "—"),
    instance_fingerprint: asString(r.instance_fingerprint, ""),
    plugin_version: asString(r.plugin_version, ""),
    velocity_version: asString(r.velocity_version, ""),
    created_at: typeof r.created_at === "string" ? r.created_at : null,
  };
}

export interface SafeAuditEvent {
  id: string;
  event_type: string;
  actor_type: string;
  actor_label: string;
  target_type: string;
  target_label: string;
  created_at: string;
}

export function coerceAuditEvent(raw: unknown): SafeAuditEvent {
  const r = asRecord(raw);
  return {
    id: asString(r.id, ""),
    event_type: asString(r.event_type, "unknown"),
    actor_type: asString(r.actor_type, "system"),
    actor_label: asString(firstDefined(r.actor_label, r.actor), "—"),
    target_type: asString(r.target_type, "—"),
    target_label: asString(firstDefined(r.target_label, r.target), "—"),
    created_at: asString(r.created_at, ""),
  };
}
