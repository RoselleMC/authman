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

import type { IPGeo } from "../components/IPLocation";

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
 * Each page in core-web should consume the output of these,
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
  username: string;
  email: string;
  display_name: string;
  role: string;
  role_alias: string;
  status: "active" | "disabled";
  created_at: string | null;
  security: {
    totp_enabled: boolean;
    passkeys: Array<{
      id: string;
      name: string;
      created_at: string | null;
      last_used_at: string | null;
    }>;
  };
}

export function coerceAdminUser(raw: unknown): SafeAdminUser {
  const r = asRecord(raw);
  return {
    id: asString(r.id, ""),
    username: asString(r.username, ""),
    email: asString(r.email, ""),
    display_name: asString(firstDefined(r.username, r.display_name, r.name), asString(r.email, "—")),
    role: asString(firstDefined(r.role_id, r.role), "admin"),
    role_alias: asString(firstDefined(r.role_alias, r.alias), ""),
    status: mapEnum(r.status, ["active", "disabled"] as const, "active"),
    created_at: typeof r.created_at === "string" ? r.created_at : null,
    security: {
      totp_enabled: asBoolean(asRecord(r.security).totp_enabled, false),
      passkeys: asArray(asRecord(r.security).passkeys, (item) => {
        const p = asRecord(item);
        return {
          id: asString(p.id, ""),
          name: asString(p.name, "Passkey"),
          created_at: typeof p.created_at === "string" ? p.created_at : null,
          last_used_at: typeof p.last_used_at === "string" ? p.last_used_at : null,
        };
      }).filter((p) => p.id !== ""),
    },
  };
}

export interface SafeVelocityNode {
  id: string;
  name: string;
  mode: "limbo_portal" | "downstream_velocity";
  kind: "limbo_portal" | "downstream_velocity";
  server_id: string;
  server_label: string;
  runtime_config: Record<string, unknown>;
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
    mode: mapEnum(firstDefined(r.kind, r.mode), ["limbo_portal", "downstream_velocity"] as const, "downstream_velocity"),
    kind: mapEnum(firstDefined(r.kind, r.mode), ["limbo_portal", "downstream_velocity"] as const, "downstream_velocity"),
    server_id: asString(r.server_id, ""),
    server_label: asString(firstDefined(r.server_label, r.server_name), "—"),
    runtime_config: asRecord(r.runtime_config),
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
  schema_version: number;
  category: string;
  outcome: string;
  source: string;
  session_id: string;
  correlation_id: string;
  actor_type: string;
  actor_id: string;
  actor_label: string;
  target_type: string;
  target_id: string;
  target_label: string;
  client_ip: string;
  client_geo: IPGeo | null;
  details: Record<string, unknown>;
  created_at: string;
}

export function coerceAuditEvent(raw: unknown): SafeAuditEvent {
  const r = asRecord(raw);
  return {
    id: asString(r.id, ""),
    event_type: asString(r.event_type, "unknown"),
    schema_version: asNumber(r.schema_version, 1),
    category: asString(firstDefined(r.category, pick(r.details, "category")), ""),
    outcome: asString(firstDefined(r.outcome, pick(r.details, "outcome")), ""),
    source: asString(firstDefined(r.source, pick(r.details, "source")), ""),
    session_id: asString(firstDefined(r.session_id, pick(r.details, "session_id")), ""),
    correlation_id: asString(firstDefined(r.correlation_id, pick(r.details, "correlation_id")), ""),
    actor_type: asString(r.actor_type, "system"),
    actor_id: asString(r.actor_id, ""),
    actor_label: asString(firstDefined(r.actor_label, r.actor_id, r.actor), "—"),
    target_type: asString(r.target_type, "—"),
    target_id: asString(r.target_id, ""),
    target_label: asString(firstDefined(r.target_label, r.target_id, r.target), "—"),
    client_ip: asString(firstDefined(r.client_ip, pick(r.details, "client_ip")), ""),
    client_geo: coerceIPGeo(firstDefined(r.client_geo, pick(r.details, "client_geo"))),
    details: asRecord(firstDefined(r.details, r.metadata)),
    created_at: asString(r.created_at, ""),
  };
}

function coerceIPGeo(raw: unknown): IPGeo | null {
  const r = asRecord(raw);
  const ip = asString(r.ip, "");
  const countryCode = asString(r.country_code, "");
  if (!ip && !countryCode) return null;
  const localesRaw = asRecord(r.locales);
  const locales: IPGeo["locales"] = {};
  for (const [key, value] of Object.entries(localesRaw)) {
    const loc = asRecord(value);
    locales[key] = {
      country: asString(loc.country, ""),
      region: asString(loc.region, ""),
      city: asString(loc.city, ""),
    };
  }
  return {
    ip,
    country_code: countryCode,
    isp: asString(r.isp, ""),
    asn: asString(r.asn, ""),
    locales,
  };
}
