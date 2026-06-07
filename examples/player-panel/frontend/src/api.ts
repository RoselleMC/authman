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

export interface PortalPlayer {
  id: string;
  uuid: string;
  raw_name: string;
  raw_offline_name?: string;
  protocol_name: string;
  kind: "premium" | "offline";
  registration_server_label: string | null;
  last_seen_server_label: string | null;
  connected_servers: Array<{ slug: string; display_name: string }>;
}

export interface PortalSession {
  player: PortalPlayer;
  csrf_token: string;
  expires_at?: string;
}

export interface PortalServer {
  slug: string;
  display_name: string;
  description?: string;
  primary_color?: string;
  accent_color?: string;
  logo_url?: string;
  portal_message?: string;
  registration_open: boolean;
  prefer_dark?: boolean;
}

export interface CheckNameResult {
  available: boolean;
  reason?: string;
}

export interface ExtensionData {
  id?: string;
  provider: string;
  label?: string;
  visibility?: string;
  data?: unknown;
  schema?: unknown;
  server_slug?: string;
  updated_at?: string;
}

export interface ExampleStatus {
  core_url_configured: boolean;
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
  return apiFetch<ExampleStatus>("/api/_example/status");
}

export async function getPortalConfig() {
  return apiFetch<PortalConfig>("/api/portal/config", { skipCSRF: true });
}

export async function getServers() {
  return apiFetch<PortalServer[]>("/api/portal/servers", { skipCSRF: true });
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

export async function getExtensionData(serverSlug?: string) {
  const path = serverSlug
    ? `/api/portal/player/extension-data/${encodeURIComponent(serverSlug)}`
    : "/api/portal/player/extension-data";
  return apiFetch<ExtensionData[]>(path);
}
