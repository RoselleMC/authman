import { apiFetch, setCsrfToken } from "@authman/shared";

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
  instance_fingerprint: string;
  plugin_version: string;
  velocity_version: string;
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
export async function deleteNode(id: string): Promise<void> {
  await apiFetch<null>(`/admin/velocity/nodes/${encodeURIComponent(id)}`, { method: "DELETE" });
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
export async function deleteMojangRoute(id: string): Promise<void> {
  await apiFetch<null>(`/admin/mojang/routes/${encodeURIComponent(id)}`, { method: "DELETE" });
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
    host?: string;
    port?: number;
    transfer_host?: string;
    transfer_port?: number;
    motd?: string;
    gate_enabled?: boolean;
    grant_ttl_seconds?: number;
    allowed_portal_sources?: string[];
    portal_hosts?: string[];
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
    gate_enabled: boolean;
    grant_ttl_seconds: number;
    allowed_portal_sources: string[];
    registration_open: boolean;
    extension_providers: string[];
  };
  extension_providers: string[];
}

export interface DownstreamServerInput {
  slug: string;
  display_name: string;
  status: "active" | "hidden" | "disabled";
  registration_open: boolean;
  portal_theme: DownstreamServer["portal_theme"];
  portal_config: DownstreamServer["portal_config"];
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
export async function createDownstreamServer(input: DownstreamServerInput): Promise<DownstreamServer> {
  const res = await apiFetch<DownstreamServer>("/admin/downstream-servers", { method: "POST", body: input });
  return res.data;
}
export async function updateDownstreamServer(id: string, input: DownstreamServerInput): Promise<DownstreamServer> {
  const res = await apiFetch<DownstreamServer>(`/admin/downstream-servers/${encodeURIComponent(id)}`, { method: "PUT", body: input });
  return res.data;
}
export async function deleteDownstreamServer(id: string): Promise<void> {
  await apiFetch<null>(`/admin/downstream-servers/${encodeURIComponent(id)}`, { method: "DELETE" });
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

export async function fetchAdminUsers(): Promise<AdminUser[]> {
  const res = await apiFetch<AdminUser[]>("/admin/users");
  return res.data;
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

export async function fetchAdminRoles(): Promise<AdminRole[]> {
  const res = await apiFetch<AdminRole[]>("/admin/roles");
  return res.data;
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
