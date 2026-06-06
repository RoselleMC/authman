import { apiFetch, setCsrfToken } from "@authman/shared";

export interface AdminMe {
  id: string;
  username: string;
  email: string;
  display_name: string;
  role: "owner" | "admin" | "auditor";
  permissions: string[];
}

export interface AdminBootstrapStatus {
  owner_exists: boolean;
}

interface LoginResponse {
  user: AdminMe;
  csrf_token: string;
}

export async function adminLogin(username: string, password: string): Promise<AdminMe> {
  const res = await apiFetch<LoginResponse>("/admin/session/login", {
    method: "POST",
    body: { username, password },
    skipCsrf: true,
  });
  setCsrfToken(res.data.csrf_token);
  return res.data.user;
}

export async function adminLogout(): Promise<void> {
  try {
    await apiFetch<null>("/admin/session/logout", { method: "POST" });
  } finally {
    setCsrfToken(null);
  }
}

export async function adminMe(): Promise<{ user: AdminMe; csrf_token: string }> {
  const res = await apiFetch<{ user: AdminMe; csrf_token: string }>("/admin/me", { method: "GET" });
  setCsrfToken(res.data.csrf_token);
  return res.data;
}

export async function adminBootstrapStatus(): Promise<AdminBootstrapStatus> {
  const res = await apiFetch<AdminBootstrapStatus>("/admin/bootstrap/status", { method: "GET" });
  return res.data;
}

export interface AdminOverview {
  total_players: number;
  premium_players: number;
  offline_players: number;
  recent_offline_login_failures: number;
  active_nodes: number;
  mojang_status: "healthy" | "partial" | "critical" | "empty";
  audit_events: AuditEventSummary[];
}

export interface AuditEventSummary {
  id: string;
  event_type: string;
  actor_type: string;
  actor_label: string;
  target_type: string;
  target_label: string;
  created_at: string;
}

export async function fetchOverview(): Promise<AdminOverview> {
  const res = await apiFetch<AdminOverview>("/admin/overview");
  return res.data;
}

export interface PlayerRow {
  id: string;
  uuid: string;
  raw_name: string;
  protocol_name: string;
  kind: "premium" | "offline";
  status: "active" | "locked" | "pending_verification" | "deleted";
  last_seen_at: string | null;
  last_seen_server_label: string | null;
}

export interface PlayerListMeta {
  total: number;
  page: number;
  page_size: number;
}

export interface PlayerListFilters extends Record<string, string | number | undefined> {
  q?: string;
  kind?: "premium" | "offline";
  status?: "active" | "locked" | "pending_verification";
  page?: number;
  page_size?: number;
}

export async function fetchPlayers(filters: PlayerListFilters, signal?: AbortSignal) {
  const res = await apiFetch<PlayerRow[]>("/admin/players", {
    method: "GET",
    query: filters,
    signal,
  });
  return { rows: res.data, meta: (res.meta as unknown as PlayerListMeta) ?? { total: res.data.length, page: 1, page_size: res.data.length } };
}

export interface PlayerDetail {
  id: string;
  uuid: string;
  raw_name: string;
  protocol_name: string;
  kind: "premium" | "offline";
  status: PlayerRow["status"];
  registration_server_label: string | null;
  last_seen_server_label: string | null;
  last_seen_at: string | null;
  created_at: string;
  profile: {
    skin_source: "mojang" | "offline_custom" | "none";
    properties: Array<{ name: string; value: string }>;
  };
  offline_credentials: {
    password_updated_at: string | null;
    failed_attempts: number;
    locked_until: string | null;
  } | null;
  identities: Array<{
    id: string;
    provider: string;
    provider_subject: string;
    verified_at: string | null;
  }>;
  sessions: Array<{
    id: string;
    created_at: string;
    server_label: string | null;
    result: "success" | "failure";
    failure_reason?: string;
  }>;
  audit_events: AuditEventSummary[];
  extension_data: Array<{
    server_slug: string;
    server_display_name: string;
    provider: string;
    schema: import("@authman/shared").ExtensionSchema;
    values: Record<string, unknown>;
    updated_at: string;
  }>;
}

export async function fetchPlayer(id: string): Promise<PlayerDetail> {
  const res = await apiFetch<PlayerDetail>(`/admin/players/${encodeURIComponent(id)}`);
  return res.data;
}

export async function lockPlayer(id: string): Promise<void> {
  await apiFetch<null>(`/admin/players/${encodeURIComponent(id)}/lock`, { method: "POST" });
}
export async function unlockPlayer(id: string): Promise<void> {
  await apiFetch<null>(`/admin/players/${encodeURIComponent(id)}/unlock`, { method: "POST" });
}
export async function resetPlayerPassword(id: string): Promise<{ reset_token_hint: string }> {
  const res = await apiFetch<{ reset_token_hint: string }>(`/admin/players/${encodeURIComponent(id)}/reset-password`, { method: "POST" });
  return res.data;
}

export interface VelocityNode {
  id: string;
  name: string;
  server_id: string;
  server_label: string;
  status: "active" | "disabled" | "stale";
  last_seen_at: string | null;
  token_fingerprint: string;
  created_at: string;
}

export async function fetchNodes(): Promise<VelocityNode[]> {
  const res = await apiFetch<VelocityNode[]>("/admin/velocity/nodes");
  return res.data;
}

export async function rotateNodeToken(id: string): Promise<{ token_once: string; token_fingerprint: string }> {
  const res = await apiFetch<{ token_once: string; token_fingerprint: string }>(`/admin/velocity/nodes/${encodeURIComponent(id)}/rotate`, { method: "POST" });
  return res.data;
}
export async function disableNode(id: string): Promise<void> {
  await apiFetch<null>(`/admin/velocity/nodes/${encodeURIComponent(id)}/disable`, { method: "POST" });
}
export async function createNode(input: { name: string; server_id: string }): Promise<{ token_once: string; token_fingerprint: string; node: VelocityNode }> {
  const res = await apiFetch<{ token_once: string; token_fingerprint: string; node: VelocityNode }>("/admin/velocity/nodes", { method: "POST", body: input });
  return res.data;
}

export interface MojangProxy {
  id: string;
  kind: "direct" | "http" | "socks5";
  url_masked: string;
  state: "healthy" | "rate_limited" | "failed" | "cooldown" | "cooling_down" | "disabled";
  weight: number;
  recent_request_count?: number;
  failure_count?: number;
  rate_limit_count?: number;
  cooldown_remaining_seconds: number;
  last_error?: string;
  last_error_at?: string | null;
}

export interface MojangStatus {
  overall:
    | "direct_healthy"
    | "proxy_healthy"
    | "proxy_rate_limited"
    | "proxy_failed"
    | "mojang_healthy"
    | "mojang_degraded"
    | "mojang_unavailable"
    | "mojang_unavailable_stale"
    | "mojang_unavailable_nocache"
    | "mojang_disabled";
  proxies: MojangProxy[];
  cache?: {
    fresh: number;
    stale: number;
    expired: number;
  };
  events: Array<{
    id: string;
    proxy_id: string;
    event_type: string;
    status_code?: number;
    retry_after?: string;
    created_at: string;
  }>;
}

export async function fetchMojang(): Promise<MojangStatus> {
  const res = await apiFetch<MojangStatus>("/admin/mojang/upstream/status");
  return res.data;
}

export interface DownstreamServer {
  id: string;
  slug: string;
  display_name: string;
  status: "active" | "hidden" | "disabled";
  registration_open: boolean;
  portal_theme: {
    primary_color?: string;
    accent_color?: string;
    portal_message?: string;
    display_name?: string;
    description?: string;
  };
  portal_config: {
    registration_strategy: "open" | "closed" | "invite";
    show_in_global: boolean;
  };
  extension_providers: string[];
}

export async function fetchDownstreamServers(): Promise<DownstreamServer[]> {
  const res = await apiFetch<DownstreamServer[]>("/admin/downstream-servers");
  return res.data;
}
export async function fetchDownstreamServer(id: string): Promise<DownstreamServer> {
  const res = await apiFetch<DownstreamServer>(`/admin/downstream-servers/${encodeURIComponent(id)}`);
  return res.data;
}

export interface ExtensionRegistryEntry {
  provider: string;
  title: string;
  visibility: "private" | "player_visible" | "public";
  schema: import("@authman/shared").ExtensionSchema;
  last_update: string | null;
  preview_values: Record<string, unknown>;
}

export async function fetchExtensions(): Promise<ExtensionRegistryEntry[]> {
  const res = await apiFetch<ExtensionRegistryEntry[]>("/admin/extensions");
  return res.data;
}

export interface AuditEvent {
  id: string;
  event_type: string;
  actor_type: string;
  actor_label: string;
  target_type: string;
  target_label: string;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface AuditFilters extends Record<string, string | number | undefined> {
  actor_type?: string;
  target_type?: string;
  event_type?: string;
  since?: string;
  until?: string;
  page?: number;
  page_size?: number;
}

export async function fetchAuditEvents(filters: AuditFilters) {
  const res = await apiFetch<AuditEvent[]>("/admin/audit-events", { query: filters });
  return res;
}

export interface AdminUser {
  id: string;
  email: string;
  display_name: string;
  role: "owner" | "admin" | "auditor";
  status?: "active" | "disabled";
  created_at?: string;
}

export async function fetchAdminUsers(): Promise<AdminUser[]> {
  const res = await apiFetch<AdminUser[]>("/admin/users");
  return res.data;
}

export interface SystemSummary {
  service?: string;
  environment?: string;
  version: string;
  database?: string;
  uptime_seconds?: number;
  feature_flags?: Record<string, boolean>;
}

export async function fetchSystemSummary(): Promise<SystemSummary> {
  const res = await apiFetch<SystemSummary>("/admin/system/summary");
  return res.data;
}
