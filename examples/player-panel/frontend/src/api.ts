export interface APIEnvelope<T> {
  data: T;
  meta: unknown;
  error: null | {
    code: string;
    message: string;
    details?: Record<string, unknown>;
  };
}

export interface PortalConfig {
  registration_open: boolean;
  password_policy_hints?: string[];
  message?: string;
}

export interface PortalIPGeo {
  status?: string;
  country?: string;
  countryCode?: string;
  regionName?: string;
  city?: string;
  query?: string;
}

export interface PortalBan {
  id: string;
  scope: "passport" | "profile" | string;
  reason?: string;
  created_at?: string;
  expires_at?: string | null;
  revoked_at?: string | null;
}

export interface PortalPlayer {
  id: string;
  uuid: string;
  raw_name: string;
  raw_offline_name?: string;
  protocol_name: string;
  kind: "premium" | "offline";
  registration_server_label: string | null;
  last_seen_server_label: string | null;
  last_seen_at?: string | null;
  last_seen_ip?: string | null;
  last_seen_geo?: PortalIPGeo | null;
  connected_servers: Array<{ slug: string; display_name: string }>;
}

export interface PortalPassport {
  id: string;
  uuid: string;
  uuid_compact?: string;
  username: string;
  kind: "premium" | "offline";
  status: string;
  avatar_url?: string;
  profile_count: number;
  online?: boolean;
  presence_count?: number;
  raw_offline_name?: string;
  registration_server?: string | null;
  last_seen_server?: string | null;
  last_seen_at?: string | null;
  last_seen_ip?: string | null;
  last_seen_geo?: PortalIPGeo | null;
  active_ban?: PortalBan | null;
  ban_expires_at?: string | null;
  locked_until?: string | null;
}

export interface PortalProfile {
  id: string;
  uuid: string;
  avatar_url?: string;
  protocol_name: string;
  normalized_name: string;
  display_name: string;
  status: "active" | "locked" | "archived";
  online?: boolean;
  presence_count?: number;
  last_seen_ip?: string | null;
  last_seen_geo?: PortalIPGeo | null;
  active_ban?: PortalBan | null;
  ban_expires_at?: string | null;
  locked_until?: string | null;
}

export interface PortalSession {
  player: PortalPlayer;
  passport: PortalPassport;
  profiles: PortalProfile[];
  profile: PortalProfile;
  csrf_token: string;
  expires_at?: string;
}

export interface PortalTextureSkin {
  source: string;
  effective_source: string;
  use_upstream_skin?: boolean;
  model: "wide" | "slim" | string;
  default_variant: string;
  default_model: string;
  skin_url: string;
  cape_url?: string | null;
  elytra_url?: string | null;
  avatar_url: string;
  has_custom_skin: boolean;
  has_custom_cape: boolean;
  has_custom_elytra: boolean;
  updated_at?: string | null;
}

export interface PortalPassportSkin extends PortalTextureSkin {}

export interface PortalProfileSkin extends PortalTextureSkin {
  use_passport_skin?: boolean;
  passport_skin?: PortalPassportSkin | null;
}

export interface CheckNameResult {
  available: boolean;
  reason?: string;
}

export interface ExampleStatus {
  core_url_configured: boolean;
  external_token_configured?: boolean;
  core_health_status: number | null;
  core_reachable: boolean;
}

let csrfToken: string | null = null;

export function setCSRF(token: string | null) {
  csrfToken = token;
}

export async function apiFetch<T>(path: string, options: RequestInit & { bodyJSON?: unknown; skipCSRF?: boolean } = {}) {
  const headers = new Headers(options.headers);
  if (options.bodyJSON !== undefined) {
    headers.set("Content-Type", "application/json");
  }
  if (!options.skipCSRF && csrfToken) {
    headers.set("X-CSRF-Token", csrfToken);
  }
  const response = await fetch(path, {
    ...options,
    headers,
    credentials: "include",
    body: options.bodyJSON === undefined ? options.body : JSON.stringify(options.bodyJSON)
  });
  const envelope = (await response.json()) as APIEnvelope<T>;
  if (!response.ok || envelope.error) {
    const error = new Error(envelope.error?.message || `HTTP ${response.status}`) as Error & {
      status?: number;
      code?: string;
    };
    error.status = response.status;
    error.code = envelope.error?.code;
    throw error;
  }
  return envelope.data;
}

export async function getExampleStatus() {
  const response = await fetch("/api/_example/status", { credentials: "include" });
  if (!response.ok) {
    return {
      core_url_configured: true,
      core_health_status: response.status,
      core_reachable: false
    };
  }
  return (await response.json()) as ExampleStatus;
}

export async function getPortalConfig() {
  return apiFetch<PortalConfig>("/api/portal/config", { skipCSRF: true });
}

export async function getSession() {
  try {
    const session = await apiFetch<PortalSession>("/api/portal/session/me", { skipCSRF: true });
    setCSRF(session.csrf_token);
    return session;
  } catch (err) {
    if ((err as { status?: number }).status === 401) {
      setCSRF(null);
      return null;
    }
    throw err;
  }
}

export async function login(username: string, password: string, serverSlug?: string) {
  const session = await apiFetch<PortalSession>("/api/portal/session/login", {
    method: "POST",
    bodyJSON: { username, password, server_slug: serverSlug || undefined },
    skipCSRF: true
  });
  setCSRF(session.csrf_token);
  return session;
}

export async function register(rawUsername: string, password: string, serverSlug?: string) {
  const session = await apiFetch<PortalSession>("/api/portal/offline/register", {
    method: "POST",
    bodyJSON: { raw_username: rawUsername, password, server_slug: serverSlug || undefined },
    skipCSRF: true
  });
  setCSRF(session.csrf_token);
  return session;
}

export async function loginWithLink(token: string) {
  const session = await apiFetch<PortalSession>("/api/portal/session/login-with-link", {
    method: "POST",
    bodyJSON: { token },
    skipCSRF: true
  });
  setCSRF(session.csrf_token);
  return session;
}

export async function selectProfile(profileID: string) {
  const session = await apiFetch<PortalSession>("/api/portal/session/select-profile", {
    method: "POST",
    bodyJSON: { profile_id: profileID }
  });
  if (session.csrf_token) {
    setCSRF(session.csrf_token);
  }
  return session;
}

export async function createProfile(protocolName: string) {
  return apiFetch<PortalSession>("/api/portal/profiles", {
    method: "POST",
    bodyJSON: { protocol_name: protocolName }
  });
}

export async function archiveProfile(profileID: string) {
  return apiFetch<PortalSession>(`/api/portal/profiles/${encodeURIComponent(profileID)}/archive`, {
    method: "POST"
  });
}

export async function restoreProfile(profileID: string) {
  return apiFetch<PortalSession>(`/api/portal/profiles/${encodeURIComponent(profileID)}/restore`, {
    method: "POST"
  });
}

export async function checkName(rawUsername: string, serverSlug?: string) {
  return apiFetch<CheckNameResult>("/api/portal/offline/check-name", {
    method: "POST",
    bodyJSON: { raw_username: rawUsername, server_slug: serverSlug || undefined },
    skipCSRF: true
  });
}

export async function logout() {
  try {
    await apiFetch<{ ok: boolean }>("/api/portal/session/logout", { method: "POST" });
  } finally {
    setCSRF(null);
  }
}

export async function changePassword(currentPassword: string, newPassword: string) {
  await apiFetch<null>("/api/portal/security/password", {
    method: "POST",
    bodyJSON: { current_password: currentPassword, new_password: newPassword }
  });
}

export async function getProfileSkin() {
  return apiFetch<PortalProfileSkin>("/api/portal/profile/skin");
}

export async function getPassportSkin() {
  return apiFetch<PortalPassportSkin>("/api/portal/passport/skin");
}

export async function uploadProfileSkin(input: {
  skin?: File | null;
  cape?: File | null;
  elytra?: File | null;
  model: "wide" | "slim";
}) {
  const form = new FormData();
  form.set("model", input.model);
  if (input.skin) form.set("skin", input.skin);
  if (input.cape) form.set("cape", input.cape);
  if (input.elytra) form.set("elytra", input.elytra);
  return apiFetch<PortalProfileSkin>("/api/portal/profile/skin", {
    method: "POST",
    body: form
  });
}

export async function setProfileSkinSource(usePassportSkin: boolean) {
  return apiFetch<PortalProfileSkin>("/api/portal/profile/skin/source", {
    method: "POST",
    bodyJSON: { use_passport_skin: usePassportSkin }
  });
}

export async function setPassportSkinSource(useUpstreamSkin: boolean) {
  return apiFetch<PortalPassportSkin>("/api/portal/passport/skin/source", {
    method: "POST",
    bodyJSON: { use_upstream_skin: useUpstreamSkin }
  });
}

export async function deleteProfileSkin() {
  return apiFetch<PortalProfileSkin>("/api/portal/profile/skin", { method: "DELETE" });
}

export async function uploadPassportSkin(input: {
  skin?: File | null;
  cape?: File | null;
  elytra?: File | null;
  model: "wide" | "slim";
}) {
  const form = new FormData();
  form.set("model", input.model);
  if (input.skin) form.set("skin", input.skin);
  if (input.cape) form.set("cape", input.cape);
  if (input.elytra) form.set("elytra", input.elytra);
  return apiFetch<PortalPassportSkin>("/api/portal/passport/skin", {
    method: "POST",
    body: form
  });
}

export async function deletePassportSkin() {
  return apiFetch<PortalPassportSkin>("/api/portal/passport/skin", { method: "DELETE" });
}
