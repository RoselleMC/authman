import { ApiError, apiFetch, getCsrfToken, getRuntimeConfig, setCsrfToken } from "@authman/shared";

export interface AdminMe {
  id: string;
  username: string;
  email: string;
  avatar_url?: string;
  display_name?: string;
  role: string;
  role_id?: string;
  role_alias?: string;
  permissions: string[];
}

export interface AdminBootstrapStatus {
  owner_exists: boolean;
}

interface LoginResponse {
  user: AdminMe;
  csrf_token: string;
  mfa_required?: boolean;
  methods?: Array<"totp" | "passkey">;
  expires_at?: string;
}

export type AdminLoginResult =
  | { kind: "ok"; user: AdminMe }
  | { kind: "mfa"; user: AdminMe; methods: Array<"totp" | "passkey">; expires_at?: string };

export async function adminLogin(identifier: string, password: string): Promise<AdminLoginResult> {
  const res = await apiFetch<LoginResponse>("/admin/session/login", {
    method: "POST",
    body: { username: identifier, email: identifier.includes("@") ? identifier : undefined, password },
    skipCsrf: true,
  });
  if (res.data.mfa_required) {
    return { kind: "mfa", user: res.data.user, methods: res.data.methods ?? [], expires_at: res.data.expires_at };
  }
  setCsrfToken(res.data.csrf_token);
  return { kind: "ok", user: res.data.user };
}

export async function requestAdminPasswordReset(identifier: string): Promise<void> {
  await apiFetch<null>("/admin/session/password-reset/request", {
    method: "POST",
    body: { identifier },
    skipCsrf: true,
  });
}

export async function confirmAdminPasswordReset(token: string, newPassword: string): Promise<void> {
  await apiFetch<null>("/admin/session/password-reset/confirm", {
    method: "POST",
    body: { token, new_password: newPassword },
    skipCsrf: true,
  });
}

export async function adminMFATOTP(code: string, trustDevice: boolean): Promise<AdminMe> {
  const res = await apiFetch<LoginResponse>("/admin/session/mfa/totp", {
    method: "POST",
    body: { code, trust_device: trustDevice },
    skipCsrf: true,
  });
  setCsrfToken(res.data.csrf_token);
  return res.data.user;
}

export async function adminMFAPasskey(trustDevice: boolean): Promise<AdminMe> {
  const optionsRes = await apiFetch<{ options: CredentialRequestOptions }>("/admin/session/mfa/passkey/options", {
    method: "POST",
    skipCsrf: true,
  });
  const credential = await navigator.credentials.get(normalizeCredentialRequestOptions(optionsRes.data.options));
  const res = await apiFetch<LoginResponse>("/admin/session/mfa/passkey/finish", {
    method: "POST",
    query: { trust_device: trustDevice },
    body: publicKeyCredentialToJSON(credential),
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

export interface PlayerListMeta {
  total: number;
  page: number;
  page_size: number;
}

export interface ListFilters extends Record<string, string | number | undefined> {
  q?: string;
  kind?: string;
  status?: string;
  state?: string;
  binding?: string;
  page?: number;
  page_size?: number;
  sort?: string;
  dir?: "asc" | "desc";
}

export interface ListResult<T> {
  rows: T[];
  meta: PlayerListMeta;
}

function listResult<T>(data: T[], meta: unknown): ListResult<T> {
  const m = (meta as Partial<PlayerListMeta> | undefined) ?? {};
  return { rows: data, meta: { total: m.total ?? data.length, page: m.page ?? 1, page_size: m.page_size ?? data.length } };
}

export interface ProfileSummary {
  id: string;
  uuid: string;
  avatar_url?: string;
  protocol_name: string;
  normalized_name: string;
  display_name: string;
  status: "active" | "locked" | "archived";
  online: boolean;
  presence_count: number;
  last_seen_ip?: string | null;
  last_seen_geo?: import("@authman/shared").IPGeo | null;
  active_ban?: PlayerBan | null;
  ban_expires_at?: string | null;
  locked_until?: string | null;
}

export interface PlayerPresence {
  id: string;
  passport_id: string;
  profile_id: string;
  server_id: string;
  node_id: string;
  protocol_name: string;
  uuid: string;
  remote_addr: string;
  connected_at: string;
  last_seen_at: string;
}

export interface PlayerBan {
  id: string;
  scope: "passport" | "profile";
  target_id: string;
  reason: string;
  created_by: string;
  created_at: string;
  expires_at: string | null;
  revoked_by: string;
  revoked_at: string | null;
  revoke_reason: string;
}

export interface PassportRow {
  id: string;
  kind: "premium" | "offline";
  uuid: string;
  avatar_url?: string;
  username: string;
  username_normalized: string;
  raw_offline_name: string;
  status: "active" | "locked" | "pending_verification" | "deleted";
  profile_count: number;
  online: boolean;
  presence_count: number;
  primary_profile: ProfileSummary | null;
  last_seen_at: string | null;
  last_seen_ip: string | null;
  last_seen_geo: import("@authman/shared").IPGeo | null;
  active_ban: PlayerBan | null;
  ban_expires_at: string | null;
  locked_until: string | null;
  created_at: string;
}

export interface PassportDetail extends PassportRow {
  skin: PassportSkinInfo;
  profiles: ProfileSummary[];
  credential: {
    password_updated_at: string | null;
    failed_attempts: number;
    locked_until: string | null;
  } | null;
  presences: PlayerPresence[];
  bans: PlayerBan[];
  audit_events: AuditEventSummary[];
}

export interface ProfileRow extends ProfileSummary {
  uuid_compact?: string;
  skin_source: "mojang" | "custom" | "passport" | "offline_custom" | "none";
  passport: { id: string; kind: "premium" | "offline"; username: string; status: string } | null;
  last_seen_at: string | null;
  last_seen_ip: string | null;
  last_seen_geo: import("@authman/shared").IPGeo | null;
  created_at: string;
}

export interface ProfileDetail extends ProfileRow {
  skin: ProfileSkinInfo;
  properties: Array<{ name: string; value: string; signature?: string }>;
  presences: PlayerPresence[];
  bans: PlayerBan[];
  audit_events: AuditEventSummary[];
  extension_data: Array<Record<string, unknown>>;
}

export interface ProfileSkinInfo {
  source: string;
  use_passport_skin?: boolean;
  effective_source: "custom" | "passport" | "mojang" | "textures" | "default";
  model: "slim" | "wide" | string;
  default_variant: string;
  default_model: "slim" | "wide" | string;
  skin_url: string;
  cape_url?: string | null;
  elytra_url?: string | null;
  avatar_url: string;
  has_custom_skin: boolean;
  has_custom_cape: boolean;
  has_custom_elytra: boolean;
  updated_at?: string | null;
  passport_skin?: PassportSkinInfo | null;
}

export interface PassportSkinInfo {
  source: string;
  use_upstream_skin?: boolean;
  effective_source: "custom" | "mojang" | "textures" | "default";
  model: "slim" | "wide" | string;
  default_variant: string;
  default_model: "slim" | "wide" | string;
  skin_url: string;
  cape_url?: string | null;
  elytra_url?: string | null;
  avatar_url: string;
  has_custom_skin: boolean;
  has_custom_cape: boolean;
  has_custom_elytra: boolean;
  updated_at?: string | null;
}

export interface IdentityListFilters extends Record<string, string | number | undefined> {
  q?: string;
  kind?: "premium" | "offline";
  status?: string;
  binding?: "bound" | "unbound";
  page?: number;
  page_size?: number;
  sort?: string;
  dir?: "asc" | "desc";
}

export async function fetchPassports(filters: IdentityListFilters, signal?: AbortSignal) {
  const res = await apiFetch<PassportRow[]>("/admin/passports", { method: "GET", query: filters, signal });
  return listResult(res.data, res.meta);
}

export async function createOfflinePassport(input: { username: string; password: string }): Promise<PassportRow> {
  const res = await apiFetch<PassportRow>("/admin/passports", { method: "POST", body: input });
  return res.data;
}

export async function fetchPassport(id: string): Promise<PassportDetail> {
  const res = await apiFetch<PassportDetail>(`/admin/passports/${encodeURIComponent(id)}`);
  return res.data;
}

export async function updatePassportStatus(id: string, status: PassportRow["status"]): Promise<PassportRow> {
  const res = await apiFetch<PassportRow>(`/admin/passports/${encodeURIComponent(id)}`, { method: "PATCH", body: { status } });
  return res.data;
}

export async function createPassportBan(id: string, input: { reason: string; expires_at?: string | null; expires_in_seconds?: number }): Promise<{ ban: PlayerBan; ended_presences: number }> {
  const res = await apiFetch<{ ban: PlayerBan; ended_presences: number }>(`/admin/passports/${encodeURIComponent(id)}/bans`, { method: "POST", body: input });
  return res.data;
}

export async function kickPassport(id: string, reason: string): Promise<{ ended_presences: number }> {
  const res = await apiFetch<{ ended_presences: number }>(`/admin/passports/${encodeURIComponent(id)}/kick`, { method: "POST", body: { reason } });
  return res.data;
}

export async function fetchProfiles(filters: IdentityListFilters, signal?: AbortSignal) {
  const res = await apiFetch<ProfileRow[]>("/admin/profiles", { method: "GET", query: filters, signal });
  return listResult(res.data, res.meta);
}

export async function fetchProfile(id: string): Promise<ProfileDetail> {
  const res = await apiFetch<ProfileDetail>(`/admin/profiles/${encodeURIComponent(id)}`);
  return res.data;
}

export async function createProfile(input: { protocol_name: string; passport_id?: string }): Promise<ProfileRow> {
  const res = await apiFetch<ProfileRow>("/admin/profiles", { method: "POST", body: input });
  return res.data;
}

export async function updateProfileStatus(id: string, status: ProfileRow["status"]): Promise<ProfileRow> {
  const res = await apiFetch<ProfileRow>(`/admin/profiles/${encodeURIComponent(id)}`, { method: "PATCH", body: { status } });
  return res.data;
}

export async function uploadProfileSkin(id: string, input: { skin?: File | null; cape?: File | null; elytra?: File | null; model: "slim" | "wide" }): Promise<ProfileSkinInfo> {
  const form = new FormData();
  form.set("model", input.model);
  if (input.skin) form.set("skin", input.skin);
  if (input.cape) form.set("cape", input.cape);
  if (input.elytra) form.set("elytra", input.elytra);
  return multipartFetch<ProfileSkinInfo>(`/admin/profiles/${encodeURIComponent(id)}/skin`, { method: "POST", body: form });
}

export async function updateProfileSkinSource(id: string, input: { use_passport_skin: boolean }): Promise<ProfileSkinInfo> {
  const res = await apiFetch<ProfileSkinInfo>(`/admin/profiles/${encodeURIComponent(id)}/skin/source`, { method: "POST", body: input });
  return res.data;
}

export async function deleteProfileSkin(id: string): Promise<ProfileSkinInfo> {
  return multipartFetch<ProfileSkinInfo>(`/admin/profiles/${encodeURIComponent(id)}/skin`, { method: "DELETE" });
}

export async function uploadPassportSkin(id: string, input: { skin?: File | null; cape?: File | null; elytra?: File | null; model: "slim" | "wide" }): Promise<PassportSkinInfo> {
  const form = new FormData();
  form.set("model", input.model);
  if (input.skin) form.set("skin", input.skin);
  if (input.cape) form.set("cape", input.cape);
  if (input.elytra) form.set("elytra", input.elytra);
  return multipartFetch<PassportSkinInfo>(`/admin/passports/${encodeURIComponent(id)}/skin`, { method: "POST", body: form });
}

export async function updatePassportSkinSource(id: string, input: { use_upstream_skin: boolean }): Promise<PassportSkinInfo> {
  const res = await apiFetch<PassportSkinInfo>(`/admin/passports/${encodeURIComponent(id)}/skin/source`, { method: "POST", body: input });
  return res.data;
}

export async function deletePassportSkin(id: string): Promise<PassportSkinInfo> {
  return multipartFetch<PassportSkinInfo>(`/admin/passports/${encodeURIComponent(id)}/skin`, { method: "DELETE" });
}

export async function createProfileBan(id: string, input: { reason: string; expires_at?: string | null; expires_in_seconds?: number }): Promise<{ ban: PlayerBan; ended_presences: number }> {
  const res = await apiFetch<{ ban: PlayerBan; ended_presences: number }>(`/admin/profiles/${encodeURIComponent(id)}/bans`, { method: "POST", body: input });
  return res.data;
}

export async function kickProfile(id: string, reason: string): Promise<{ ended_presences: number }> {
  const res = await apiFetch<{ ended_presences: number }>(`/admin/profiles/${encodeURIComponent(id)}/kick`, { method: "POST", body: { reason } });
  return res.data;
}

export async function kickPresence(id: string, reason: string): Promise<PlayerPresence> {
  const res = await apiFetch<PlayerPresence>(`/admin/presences/${encodeURIComponent(id)}/kick`, { method: "POST", body: { reason } });
  return res.data;
}

export async function revokeBan(id: string, reason: string): Promise<PlayerBan> {
  const res = await apiFetch<PlayerBan>(`/admin/bans/${encodeURIComponent(id)}`, { method: "DELETE", body: { reason } });
  return res.data;
}

export async function extendBan(id: string, input: { expires_in_seconds: number; reason?: string }): Promise<PlayerBan> {
  const res = await apiFetch<PlayerBan>(`/admin/bans/${encodeURIComponent(id)}/extend`, { method: "POST", body: input });
  return res.data;
}

export async function bindProfile(id: string, passport_id: string, primary = true): Promise<ProfileRow> {
  const res = await apiFetch<ProfileRow>(`/admin/profiles/${encodeURIComponent(id)}/bind`, { method: "POST", body: { passport_id, primary } });
  return res.data;
}

export async function unbindProfile(id: string): Promise<void> {
  await apiFetch<null>(`/admin/profiles/${encodeURIComponent(id)}/unbind`, { method: "POST" });
}

export interface VelocityNode {
  id: string;
  name: string;
  mode: "limbo_portal" | "downstream_velocity";
  kind: "limbo_portal" | "downstream_velocity";
  server_id: string;
  server_label: string;
  runtime_config?: PortalRuntimeConfig;
  status: "active" | "disabled" | "stale";
  last_seen_at: string | null;
  token_fingerprint: string;
  instance_fingerprint: string;
  plugin_version: string;
  velocity_version: string;
  created_at: string;
}

export async function fetchNodes(kind: "limbo_portal" | "downstream_velocity" | "all" = "all", filters: ListFilters = {}): Promise<ListResult<VelocityNode>> {
  const path = kind === "limbo_portal" ? "/admin/login-portals" : kind === "downstream_velocity" ? "/admin/downstream/nodes" : "/admin/nodes";
  const res = await apiFetch<VelocityNode[]>(path, { query: filters });
  return listResult(res.data, res.meta);
}

export async function fetchNode(id: string): Promise<VelocityNode> {
  const res = await apiFetch<VelocityNode>(`/admin/nodes/${encodeURIComponent(id)}`);
  return res.data;
}

export async function updateNode(id: string, input: { name: string; runtime_config: Record<string, unknown> }): Promise<VelocityNode> {
  const res = await apiFetch<VelocityNode>(`/admin/nodes/${encodeURIComponent(id)}`, { method: "PUT", body: input });
  return res.data;
}

export async function rotateNodeToken(id: string): Promise<{ token_once: string; token_fingerprint: string }> {
  const res = await apiFetch<{ token_once: string; token_fingerprint: string }>(`/admin/nodes/${encodeURIComponent(id)}/rotate`, { method: "POST" });
  return res.data;
}
export async function disableNode(id: string): Promise<void> {
  await apiFetch<null>(`/admin/velocity/nodes/${encodeURIComponent(id)}/disable`, { method: "POST" });
}
export async function deleteNode(id: string): Promise<void> {
  await apiFetch<null>(`/admin/nodes/${encodeURIComponent(id)}`, { method: "DELETE" });
}
export async function createNode(input: { name: string; kind?: "limbo_portal" | "downstream_velocity"; server_id?: string }): Promise<{ token_once: string; token_fingerprint: string; node: VelocityNode }> {
  const path = input.kind === "limbo_portal" ? "/admin/login-portals" : input.kind === "downstream_velocity" ? "/admin/downstream/nodes" : "/admin/nodes";
  const res = await apiFetch<{ token_once: string; token_fingerprint: string; node: VelocityNode }>(path, { method: "POST", body: input });
  return res.data;
}

export interface PortalRuntimeConfig {
  node_name?: string;
  server_id?: string;
  heartbeat_interval_seconds?: number;
  resolve_raw_offline_names?: boolean;
  max_password_attempts?: number;
  chat_cooldown_millis?: number;
  auth_timeout_seconds?: number;
  completion_delay_seconds?: number;
  transfer_cookie_key?: string;
  gate_initial_server?: string;
  gate_holding_server?: string;
  gate_validation_timeout_seconds?: number;
  email_verification_mode?: string;
}

export interface PortalSettings {
  transfer_cookie_key: string;
  fallback_server_id: string;
  max_profiles_per_passport?: number;
  auto_join_single_profile?: boolean;
  available_servers?: Array<{
    id: string;
    slug: string;
    display_name: string;
    status: DownstreamServer["status"];
  }>;
}

export async function fetchPortalSettings(): Promise<PortalSettings> {
  const res = await apiFetch<PortalSettings>("/admin/portal-settings");
  return res.data;
}

export async function updatePortalSettings(input: PortalSettings): Promise<PortalSettings> {
  // Strip the read-only echo field; the Go decoder rejects unknown fields.
  const { available_servers: _availableServers, ...body } = input;
  const res = await apiFetch<PortalSettings>("/admin/portal-settings", { method: "PUT", body });
  return res.data;
}

export type PlayerMessageDialogScreen = "login" | "register" | "profile_create" | "profile_select";

export const PLAYER_DIALOG_SCREENS: PlayerMessageDialogScreen[] = ["login", "register", "profile_create", "profile_select"];

export type PlayerMessageDialogWhen = "always" | "auth_required" | "premium_passthrough" | "premium_unverified" | "error";

export type PlayerDialogBodyKind = "text" | "item";
export type PlayerDialogInputKind = "text" | "boolean" | "option" | "range";
export type PlayerDialogInputRole = "" | "password" | "confirm" | "profile_name" | "profile_choice";
export type PlayerDialogActionKind = "submit" | "open_url" | "copy_to_clipboard" | "open_screen";
export type PlayerDialogAfterAction = "wait_for_response" | "none";

export interface PlayerDialogBody {
  id: string;
  kind: PlayerDialogBodyKind;
  when?: PlayerMessageDialogWhen;
  text?: string;
  width?: number;
  item?: string;
  count?: number;
  description?: string;
  show_tooltip?: boolean;
  show_decorations?: boolean;
  height?: number;
}

export interface PlayerDialogOption {
  id: string;
  display: string;
  initial?: boolean;
}

export interface PlayerDialogInput {
  id: string;
  kind: PlayerDialogInputKind;
  role?: PlayerDialogInputRole;
  key: string;
  label: string;
  label_visible?: boolean;
  when?: PlayerMessageDialogWhen;
  width?: number;
  initial?: string;
  max_length?: number;
  multiline?: boolean;
  multiline_lines?: number;
  initial_bool?: boolean;
  on_true?: string;
  on_false?: string;
  options?: PlayerDialogOption[];
  start?: number;
  end?: number;
  step?: number | null;
  initial_num?: number | null;
  label_format?: string;
}

export interface PlayerDialogAction {
  kind: PlayerDialogActionKind;
  url?: string;
  value?: string;
  screen?: string;
}

export interface PlayerDialogButton {
  id: string;
  label: string;
  tooltip?: string;
  width?: number;
  when?: PlayerMessageDialogWhen;
  action: PlayerDialogAction;
}

export interface PlayerMessageDialogDoc {
  version: number;
  title: string;
  external_title?: string;
  can_close_with_escape?: boolean;
  pause?: boolean;
  after_action?: PlayerDialogAfterAction;
  columns?: number;
  body: PlayerDialogBody[];
  inputs: PlayerDialogInput[];
  buttons: PlayerDialogButton[];
}

export interface PlayerMessagesData {
  messages: {
    defaults: Record<string, string>;
    overrides: Record<string, string>;
    placeholders: Record<string, string[]>;
  };
  dialogs: Record<PlayerMessageDialogScreen, {
    default: PlayerMessageDialogDoc;
    override: PlayerMessageDialogDoc | null;
  }>;
}

export interface PlayerMessagesUpdateInput {
  messages: Record<string, string>;
  dialogs: Partial<Record<PlayerMessageDialogScreen, PlayerMessageDialogDoc | null>>;
}

export async function fetchPlayerMessages(): Promise<PlayerMessagesData> {
  const res = await apiFetch<PlayerMessagesData>("/admin/settings/player-messages");
  return res.data;
}

export async function updatePlayerMessages(input: PlayerMessagesUpdateInput): Promise<PlayerMessagesData> {
  const res = await apiFetch<PlayerMessagesData>("/admin/settings/player-messages", { method: "PUT", body: input });
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

export async function fetchMojang(filters: ListFilters = {}): Promise<{ status: MojangStatus; meta: PlayerListMeta }> {
  const res = await apiFetch<MojangStatus>("/admin/mojang/upstream/status", { query: filters });
  return { status: res.data, meta: listResult(res.data.proxies ?? [], res.meta).meta };
}

export interface CreateMojangRouteInput {
  id?: string;
  kind: "http" | "socks5";
  url: string;
  weight?: number;
}

export async function createMojangRoute(input: CreateMojangRouteInput): Promise<MojangProxy> {
  const res = await apiFetch<MojangProxy>("/admin/mojang/routes", {
    method: "POST",
    body: input,
  });
  return res.data;
}
export async function updateMojangRoute(id: string, input: CreateMojangRouteInput & { disabled?: boolean }): Promise<MojangProxy> {
  const res = await apiFetch<MojangProxy>(`/admin/mojang/routes/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: input,
  });
  return res.data;
}
export async function deleteMojangRoute(id: string): Promise<void> {
  await apiFetch<null>(`/admin/mojang/routes/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export interface RouteChoice {
  id: string;
  kind: "direct" | "http" | "socks5";
  url_masked: string;
  weight: number;
  disabled?: boolean;
}

export interface MojangRuntimeSettings {
  enabled_route_ids: string[];
  load_balance_strategy: string;
  request_timeout_seconds: number;
  failure_cooldown_seconds: number;
  cache_fresh_seconds: number;
  cache_stale_seconds: number;
  available_routes: RouteChoice[];
}

export async function fetchMojangSettings(): Promise<MojangRuntimeSettings> {
  const res = await apiFetch<MojangRuntimeSettings>("/admin/settings/mojang");
  return res.data;
}

export async function updateMojangSettings(input: MojangRuntimeSettings): Promise<MojangRuntimeSettings> {
  const { available_routes: _availableRoutes, ...body } = input;
  const res = await apiFetch<MojangRuntimeSettings>("/admin/settings/mojang", { method: "PUT", body });
  return res.data;
}

export interface IPGeoSettings {
  enabled_route_ids: string[];
  cache_ttl_seconds: number;
  request_timeout_seconds: number;
  provider: string;
  available_routes: RouteChoice[];
}

export async function fetchIPGeoSettings(): Promise<IPGeoSettings> {
  const res = await apiFetch<IPGeoSettings>("/admin/settings/ip-geo");
  return res.data;
}

export async function updateIPGeoSettings(input: IPGeoSettings): Promise<IPGeoSettings> {
  const { available_routes: _availableRoutes, ...body } = input;
  const res = await apiFetch<IPGeoSettings>("/admin/settings/ip-geo", { method: "PUT", body });
  return res.data;
}

export interface NodeCommunicationSettings {
  websocket_enabled: boolean;
  heartbeat_interval_seconds: number;
  websocket_reconnect_min_seconds: number;
  websocket_reconnect_max_seconds: number;
  websocket_ping_interval_seconds: number;
}

export async function fetchNodeCommunicationSettings(): Promise<NodeCommunicationSettings> {
  const res = await apiFetch<NodeCommunicationSettings>("/admin/settings/communication");
  return res.data;
}

export async function updateNodeCommunicationSettings(input: NodeCommunicationSettings): Promise<NodeCommunicationSettings> {
  const res = await apiFetch<NodeCommunicationSettings>("/admin/settings/communication", { method: "PUT", body: input });
  return res.data;
}

export interface BrandingSettings {
  product_name: string;
  core_label: string;
  title_suffix: string;
}

export async function fetchBrandingSettings(): Promise<BrandingSettings> {
  const res = await apiFetch<BrandingSettings>("/admin/settings/branding");
  return res.data;
}

export async function updateBrandingSettings(input: BrandingSettings): Promise<BrandingSettings> {
  const res = await apiFetch<BrandingSettings>("/admin/settings/branding", { method: "PUT", body: input });
  return res.data;
}

export interface SMTPSettings {
  enabled: boolean;
  delivery_mode: "smtp" | "log";
  host: string;
  port: number;
  security: "none" | "starttls" | "tls";
  username: string;
  password?: string;
  password_set: boolean;
  clear_password?: boolean;
  from_name: string;
  from_email: string;
  reply_to: string;
  timeout_seconds: number;
  reset_token_ttl_minutes: number;
  last_message?: {
    to?: string;
    subject?: string;
    body?: string;
    created_at?: string;
  };
}

export async function fetchSMTPSettings(): Promise<SMTPSettings> {
  const res = await apiFetch<SMTPSettings>("/admin/settings/smtp");
  return res.data;
}

export async function updateSMTPSettings(input: SMTPSettings): Promise<SMTPSettings> {
  const body = { ...input };
  delete body.last_message;
  const res = await apiFetch<SMTPSettings>("/admin/settings/smtp", { method: "PUT", body });
  return res.data;
}

export async function sendSMTPTest(to: string): Promise<{ ok: boolean; delivery: "smtp" | "log" }> {
  const res = await apiFetch<{ ok: boolean; delivery: "smtp" | "log" }>("/admin/settings/smtp/test", { method: "POST", body: { to } });
  return res.data;
}

export interface PasswordRecoveryKeyStatus {
  algorithm: string;
  public_key_pem: string;
  fingerprint: string;
  size_bits: number;
  created_at: string;
  downloaded_at?: string;
  private_key_available: boolean;
}

export interface PasswordRecoveryPrivateKeyDownload {
  private_key_pem: string;
  fingerprint: string;
  algorithm: string;
  filename: string;
  destroyed_at: string;
}

export async function fetchPasswordRecoveryKeyStatus(): Promise<PasswordRecoveryKeyStatus> {
  const res = await apiFetch<PasswordRecoveryKeyStatus>("/admin/settings/security/password-recovery-key");
  return res.data;
}

export async function downloadPasswordRecoveryPrivateKey(): Promise<PasswordRecoveryPrivateKeyDownload> {
  const res = await apiFetch<PasswordRecoveryPrivateKeyDownload>("/admin/settings/security/password-recovery-key/download", { method: "POST" });
  return res.data;
}

export async function factoryResetSystem(confirm: string): Promise<PasswordRecoveryKeyStatus> {
  const res = await apiFetch<PasswordRecoveryKeyStatus>("/admin/settings/system/factory-reset", { method: "POST", body: { confirm } });
  return res.data;
}

export interface ExternalAPIToken {
  id: string;
  name: string;
  token_fingerprint: string;
  status: "active" | "disabled" | "revoked";
  created_by: string;
  call_count: number;
  last_used_at: string | null;
  last_used_ip: string;
  last_used_path: string;
  created_at: string;
  updated_at: string;
}

export interface ExternalAPITokenCreated extends ExternalAPIToken {
  token_once: string;
}

export async function fetchExternalAPITokens(filters: ListFilters = {}): Promise<ListResult<ExternalAPIToken>> {
  const res = await apiFetch<ExternalAPIToken[]>("/admin/external-tokens", { query: filters });
  return listResult(res.data, res.meta);
}

export async function fetchExternalAPIToken(id: string): Promise<ExternalAPIToken> {
  const res = await apiFetch<ExternalAPIToken>(`/admin/external-tokens/${encodeURIComponent(id)}`);
  return res.data;
}

export async function createExternalAPIToken(name: string): Promise<ExternalAPITokenCreated> {
  const res = await apiFetch<ExternalAPITokenCreated>("/admin/external-tokens", { method: "POST", body: { name } });
  return res.data;
}

export async function updateExternalAPIToken(id: string, input: { name?: string; status?: ExternalAPIToken["status"] }): Promise<ExternalAPIToken> {
  const res = await apiFetch<ExternalAPIToken>(`/admin/external-tokens/${encodeURIComponent(id)}`, { method: "PUT", body: input });
  return res.data;
}

export async function revokeExternalAPIToken(id: string): Promise<ExternalAPIToken> {
  const res = await apiFetch<ExternalAPIToken>(`/admin/external-tokens/${encodeURIComponent(id)}`, { method: "DELETE" });
  return res.data;
}

export async function deleteExternalAPITokenRecord(id: string): Promise<ExternalAPIToken> {
  const res = await apiFetch<ExternalAPIToken>(`/admin/external-tokens/${encodeURIComponent(id)}/record`, { method: "DELETE" });
  return res.data;
}

export interface DownstreamServer {
  id: string;
  slug: string;
  display_name: string;
  status: "active" | "hidden" | "disabled";
  enabled: boolean;
  visible: boolean;
  registration_open: boolean;
  routing_config: {
    registration_strategy?: "open" | "closed" | "invite";
    show_in_global: boolean;
    host?: string;
    port?: number;
    transfer_host?: string;
    transfer_port?: number;
    motd?: string;
    server_icon?: string;
    grant_required?: boolean;
    gate_enabled?: boolean;
    grant_ttl_seconds?: number;
    allowed_portal_sources?: string[];
    portal_hosts?: string[];
    limbo_blueprint_id?: string;
    min_protocol_version?: number;
    max_protocol_version?: number;
    resource_pack_enabled?: boolean;
    resource_pack_required?: boolean;
    resource_packs?: DownstreamResourcePack[];
  };
  target: {
    server_id: string;
    slug: string;
    display_name: string;
    status: DownstreamServer["status"];
    host: string;
    port: number;
    transfer_host: string;
    transfer_port: number;
    motd: string;
    server_icon?: string;
    grant_required: boolean;
    gate_enabled: boolean;
    grant_ttl_seconds: number;
    allowed_portal_sources: string[];
    registration_open: boolean;
    extension_providers: string[];
    min_protocol_version?: number;
    max_protocol_version?: number;
    resource_pack_enabled?: boolean;
    resource_pack_required?: boolean;
    resource_packs?: DownstreamResourcePack[];
  };
  extension_providers: string[];
  created_at?: string;
  updated_at?: string;
}

export interface DownstreamResourcePack {
  id?: string;
  name?: string;
  url: string;
  hash?: string;
  prompt?: string;
}

export interface DownstreamServerPrivilegedPassport extends PassportRow {
  server_id: string;
  passport_id: string;
  privileges: string[];
  allowed_at: string;
  created_by: string;
}

export interface DownstreamServerInput {
  display_name: string;
  enabled: boolean;
  visible: boolean;
  registration_open: boolean;
  routing_config: DownstreamServer["routing_config"];
  extension_providers: string[];
}

export async function fetchDownstreamServers(filters: ListFilters = {}): Promise<ListResult<DownstreamServer>> {
  const res = await apiFetch<DownstreamServer[]>("/admin/downstream-servers", { query: filters });
  return listResult(res.data, res.meta);
}
export async function fetchDownstreamServer(id: string): Promise<DownstreamServer> {
  const res = await apiFetch<DownstreamServer>(`/admin/downstream-servers/${encodeURIComponent(id)}`);
  return res.data;
}
export async function createDownstreamServer(input: DownstreamServerInput): Promise<DownstreamServer> {
  const res = await apiFetch<DownstreamServer>("/admin/downstream-servers", { method: "POST", body: input });
  return res.data;
}
export async function updateDownstreamServer(id: string, input: DownstreamServerInput): Promise<DownstreamServer> {
  const res = await apiFetch<DownstreamServer>(`/admin/downstream-servers/${encodeURIComponent(id)}`, { method: "PUT", body: input });
  return res.data;
}
export async function uploadDownstreamServerIcon(id: string, file: File): Promise<DownstreamServer> {
  const form = new FormData();
  form.set("icon", file);
  return multipartFetch<DownstreamServer>(`/admin/downstream-servers/${encodeURIComponent(id)}/icon`, { method: "POST", body: form });
}
export async function deleteDownstreamServerIcon(id: string): Promise<DownstreamServer> {
  return multipartFetch<DownstreamServer>(`/admin/downstream-servers/${encodeURIComponent(id)}/icon`, { method: "DELETE" });
}
export async function deleteDownstreamServer(id: string): Promise<void> {
  await apiFetch<null>(`/admin/downstream-servers/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function fetchDownstreamServerPrivilegedPassports(id: string, filters: IdentityListFilters = {}): Promise<ListResult<DownstreamServerPrivilegedPassport>> {
  const res = await apiFetch<DownstreamServerPrivilegedPassport[]>(`/admin/downstream-servers/${encodeURIComponent(id)}/privileged-passports`, { query: filters });
  return listResult(res.data, res.meta);
}

export async function addDownstreamServerPrivilegedPassport(id: string, passportID: string): Promise<DownstreamServerPrivilegedPassport> {
  const res = await apiFetch<DownstreamServerPrivilegedPassport>(`/admin/downstream-servers/${encodeURIComponent(id)}/privileged-passports`, { method: "POST", body: { passport_id: passportID } });
  return res.data;
}

export async function removeDownstreamServerPrivilegedPassport(id: string, passportID: string): Promise<void> {
  await apiFetch<null>(`/admin/downstream-servers/${encodeURIComponent(id)}/privileged-passports/${encodeURIComponent(passportID)}`, { method: "DELETE" });
}

export interface LimboBlueprintPreviewBlock {
  x: number;
  y: number;
  z: number;
  p: number;
  name?: string;
}

export interface LimboBlueprintPreview {
  bounds?: {
    min_x: number;
    min_y: number;
    min_z: number;
    max_x: number;
    max_y: number;
    max_z: number;
    width: number;
    height: number;
    length: number;
  };
  block_count?: number;
  sampled?: number;
  palette?: Array<{ id: number; name: string }>;
  blocks?: LimboBlueprintPreviewBlock[];
}

export interface LimboBlueprintConfig {
  world_id?: string;
  dimension?: "overworld" | "nether" | "end";
  spawn?: { x: number; y: number; z: number; yaw: number; pitch: number };
  [key: string]: unknown;
}

export interface LimboBlueprint {
  id: string;
  name: string;
  description: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  sha256: string;
  preview: LimboBlueprintPreview;
  config: LimboBlueprintConfig;
  created_at: string;
  updated_at: string;
}

export async function fetchLimboBlueprints(filters: ListFilters = {}): Promise<ListResult<LimboBlueprint>> {
  const res = await apiFetch<LimboBlueprint[]>("/admin/limbo-blueprints", { query: filters });
  return listResult(res.data, res.meta);
}

export async function fetchLimboBlueprint(id: string): Promise<LimboBlueprint> {
  const res = await apiFetch<LimboBlueprint>(`/admin/limbo-blueprints/${encodeURIComponent(id)}`);
  return res.data;
}

export async function updateLimboBlueprint(id: string, input: { name: string; description: string; config: LimboBlueprintConfig }): Promise<LimboBlueprint> {
  const res = await apiFetch<LimboBlueprint>(`/admin/limbo-blueprints/${encodeURIComponent(id)}`, { method: "PUT", body: input });
  return res.data;
}

export async function deleteLimboBlueprint(id: string): Promise<void> {
  await apiFetch<null>(`/admin/limbo-blueprints/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function uploadLimboBlueprint(input: { file: File; name?: string; description?: string; config?: LimboBlueprintConfig }): Promise<LimboBlueprint> {
  const cfg = getRuntimeConfig();
  const form = new FormData();
  form.append("file", input.file);
  if (input.name) form.append("name", input.name);
  if (input.description) form.append("description", input.description);
  if (input.config) form.append("config", JSON.stringify(input.config));
  const headers: Record<string, string> = { Accept: "application/json" };
  const csrf = getCsrfToken();
  if (csrf) headers["X-CSRF-Token"] = csrf;
  const res = await fetch(`${cfg.apiBase}/admin/limbo-blueprints/upload`, {
    method: "POST",
    credentials: "include",
    headers,
    body: form,
  });
  const envelope = await res.json().catch(() => null) as { data?: LimboBlueprint; error?: { code: string; message: string } } | null;
  if (!res.ok || envelope?.error || !envelope?.data) {
    throw new ApiError(res.status, envelope?.error ?? null, res.statusText);
  }
  return envelope.data;
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
  schema_version?: number;
  category?: string;
  outcome?: string;
  source?: string;
  session_id?: string;
  correlation_id?: string;
  actor_type: string;
  actor_id?: string;
  actor_label: string;
  target_type: string;
  target_id?: string;
  target_label: string;
  client_ip?: string | null;
  client_geo?: import("@authman/shared").IPGeo | null;
  details: Record<string, unknown>;
  created_at: string;
}

export interface AuditFilters extends Record<string, string | number | undefined> {
  actor_type?: string;
  target_type?: string;
  event_type?: string;
  since?: string;
  until?: string;
  related_id?: string;
  page?: number;
  page_size?: number;
}

export async function fetchAuditEvents(filters: AuditFilters) {
  const res = await apiFetch<AuditEvent[]>("/admin/audit-events", { query: filters });
  return res;
}

export async function fetchAuditEvent(id: string): Promise<AuditEvent> {
  const res = await apiFetch<AuditEvent>(`/admin/audit-events/${encodeURIComponent(id)}`);
  return res.data;
}

export interface AdminUser {
  id: string;
  username: string;
  email: string;
  display_name?: string;
  role: string;
  role_id?: string;
  role_alias?: string;
  status?: "active" | "disabled";
  created_at?: string;
  security?: AdminAccountSecurity;
}

export async function fetchAdminUsers(filters: ListFilters = {}): Promise<ListResult<AdminUser>> {
  const res = await apiFetch<AdminUser[]>("/admin/users", { query: filters });
  return listResult(res.data, res.meta);
}

export interface CreateAdminUserInput {
  username: string;
  email?: string;
  password: string;
  role: string;
}

export async function createAdminUser(input: CreateAdminUserInput): Promise<AdminUser> {
  const res = await apiFetch<AdminUser>("/admin/users", {
    method: "POST",
    body: input,
  });
  return res.data;
}

export interface UpdateAdminUserInput {
  username: string;
  email?: string;
  role: string;
  status: "active" | "disabled";
}

export async function updateAdminUser(id: string, input: UpdateAdminUserInput): Promise<AdminUser> {
  const res = await apiFetch<AdminUser>(`/admin/users/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: input,
  });
  return res.data;
}

export async function disableAdminUserTOTP(id: string): Promise<AdminAccountSecurity> {
  const res = await apiFetch<AdminAccountSecurity>(`/admin/users/${encodeURIComponent(id)}/totp/disable`, { method: "POST" });
  return res.data;
}

export async function deleteAdminUserPasskey(id: string, passkeyID: string): Promise<AdminAccountSecurity> {
  const res = await apiFetch<AdminAccountSecurity>(`/admin/users/${encodeURIComponent(id)}/passkeys/${encodeURIComponent(passkeyID)}`, { method: "DELETE" });
  return res.data;
}

export interface AdminPermission {
  key: string;
  group: string;
  label: string;
  description: string;
}

export interface AdminRole {
  id: string;
  role_id?: string;
  alias?: string;
  name: string;
  description: string;
  permissions: string[];
  system: boolean;
  created_at?: string;
  updated_at?: string;
}

export async function fetchAdminPermissions(): Promise<AdminPermission[]> {
  const res = await apiFetch<AdminPermission[]>("/admin/permissions");
  return res.data;
}

export async function fetchAdminRoles(filters: ListFilters = {}): Promise<ListResult<AdminRole>> {
  const res = await apiFetch<AdminRole[]>("/admin/roles", { query: filters });
  return listResult(res.data, res.meta);
}

export interface CreateAdminRoleInput {
  role_id: string;
  alias?: string;
  description?: string;
  permissions: string[];
}

export async function createAdminRole(input: CreateAdminRoleInput): Promise<AdminRole> {
  const res = await apiFetch<AdminRole>("/admin/roles", {
    method: "POST",
    body: input,
  });
  return res.data;
}

export async function updateAdminRole(id: string, permissions: string[], alias?: string, description?: string): Promise<AdminRole> {
  const res = await apiFetch<AdminRole>(`/admin/roles/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: { permissions, alias, description },
  });
  return res.data;
}

export async function deleteAdminRole(id: string): Promise<void> {
  await apiFetch<null>(`/admin/roles/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export interface AdminAccountSecurity {
  totp_enabled: boolean;
  mfa_requirement: "new_device" | "always";
  preferred_locale: "system" | "en" | "zh";
  preferred_theme: "system" | "light" | "dark";
  passkeys: Array<{
    id: string;
    name: string;
    created_at: string;
    last_used_at: string | null;
  }>;
}

export interface AdminAccount {
  user: AdminMe;
  security: AdminAccountSecurity;
  webauthn: { enabled: boolean };
}

export async function fetchAdminAccount(): Promise<AdminAccount> {
  const res = await apiFetch<AdminAccount>("/admin/account");
  return res.data;
}

export async function updateAdminAccountProfile(input: {
  username: string;
  email?: string;
  avatar_url?: string;
}): Promise<AdminMe> {
  const res = await apiFetch<AdminMe>("/admin/account/profile", { method: "PUT", body: input });
  return res.data;
}

export async function updateAdminAccountPreferences(input: {
  mfa_requirement: "new_device" | "always";
  preferred_locale: "system" | "en" | "zh";
  preferred_theme: "system" | "light" | "dark";
}): Promise<AdminAccountSecurity> {
  const res = await apiFetch<AdminAccountSecurity>("/admin/account/preferences", { method: "PUT", body: input });
  return res.data;
}

export async function updateAdminAccountPassword(input: {
  current_password: string;
  new_password: string;
}): Promise<void> {
  await apiFetch<null>("/admin/account/password", { method: "POST", body: input });
}

export async function startAdminTOTP(): Promise<{ secret: string; otpauth_url: string }> {
  const res = await apiFetch<{ secret: string; otpauth_url: string }>("/admin/account/totp/start", { method: "POST" });
  return res.data;
}

export async function confirmAdminTOTP(code: string): Promise<AdminAccountSecurity> {
  const res = await apiFetch<AdminAccountSecurity>("/admin/account/totp/confirm", { method: "POST", body: { code } });
  return res.data;
}

export async function disableAdminTOTP(): Promise<AdminAccountSecurity> {
  const res = await apiFetch<AdminAccountSecurity>("/admin/account/totp/disable", { method: "POST", body: { code: "" } });
  return res.data;
}

export async function registerAdminPasskey(name: string): Promise<AdminAccountSecurity> {
  const optionsRes = await apiFetch<{ challenge_id: string; name: string; options: CredentialCreationOptions }>("/admin/account/passkeys/options", {
    method: "POST",
    body: { name },
  });
  const credential = await navigator.credentials.create(normalizeCredentialCreationOptions(optionsRes.data.options));
  const res = await apiFetch<AdminAccountSecurity>("/admin/account/passkeys/finish", {
    method: "POST",
    body: {
      challenge_id: optionsRes.data.challenge_id,
      name: optionsRes.data.name,
      credential: publicKeyCredentialToJSON(credential),
    },
  });
  return res.data;
}

export async function deleteAdminPasskey(id: string): Promise<void> {
  await apiFetch<null>(`/admin/account/passkeys/${encodeURIComponent(id)}`, { method: "DELETE" });
}

function normalizeCredentialCreationOptions(options: CredentialCreationOptions): CredentialCreationOptions {
  const publicKey = { ...(options.publicKey ?? {}) } as PublicKeyCredentialCreationOptions;
  publicKey.challenge = base64URLToBuffer(publicKey.challenge as unknown as string);
  publicKey.user = { ...publicKey.user, id: base64URLToBuffer(publicKey.user.id as unknown as string) };
  publicKey.excludeCredentials = publicKey.excludeCredentials?.map((credential) => ({
    ...credential,
    id: base64URLToBuffer(credential.id as unknown as string),
  }));
  return { publicKey };
}

function normalizeCredentialRequestOptions(options: CredentialRequestOptions): CredentialRequestOptions {
  const publicKey = { ...(options.publicKey ?? {}) } as PublicKeyCredentialRequestOptions;
  publicKey.challenge = base64URLToBuffer(publicKey.challenge as unknown as string);
  publicKey.allowCredentials = publicKey.allowCredentials?.map((credential) => ({
    ...credential,
    id: base64URLToBuffer(credential.id as unknown as string),
  }));
  return { publicKey };
}

function publicKeyCredentialToJSON(credential: Credential | null): Record<string, unknown> {
  if (!(credential instanceof PublicKeyCredential)) throw new Error("No passkey credential returned");
  const response = credential.response as AuthenticatorAttestationResponse | AuthenticatorAssertionResponse;
  const out: Record<string, unknown> = {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64URL(response.clientDataJSON),
    },
    clientExtensionResults: credential.getClientExtensionResults(),
    authenticatorAttachment: credential.authenticatorAttachment,
  };
  if ("attestationObject" in response) {
    (out.response as Record<string, unknown>).attestationObject = bufferToBase64URL(response.attestationObject);
    if ("getTransports" in response) {
      (out.response as Record<string, unknown>).transports = response.getTransports();
    }
  } else {
    (out.response as Record<string, unknown>).authenticatorData = bufferToBase64URL(response.authenticatorData);
    (out.response as Record<string, unknown>).signature = bufferToBase64URL(response.signature);
    (out.response as Record<string, unknown>).userHandle = response.userHandle ? bufferToBase64URL(response.userHandle) : null;
  }
  return out;
}

function base64URLToBuffer(value: string): ArrayBuffer {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized.padEnd(normalized.length + ((4 - (normalized.length % 4)) % 4), "=");
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes.buffer;
}

function bufferToBase64URL(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

async function multipartFetch<T>(path: string, opts: { method: "POST" | "DELETE"; body?: FormData }): Promise<T> {
  const base = getRuntimeConfig().apiBase;
  const url = path.startsWith("/") ? `${base}${path}` : `${base}/${path}`;
  const headers: Record<string, string> = { Accept: "application/json" };
  const csrf = getCsrfToken();
  if (csrf) headers["X-CSRF-Token"] = csrf;
  const res = await fetch(url, {
    method: opts.method,
    headers,
    credentials: "include",
    body: opts.body,
  });
  const text = await res.text();
  const envelope = text ? (JSON.parse(text) as { data?: T; error?: { code: string; message: string; details?: Record<string, unknown> } }) : null;
  if (!res.ok || envelope?.error) {
    throw new ApiError(res.status, envelope?.error ?? null, res.statusText);
  }
  return envelope?.data as T;
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
