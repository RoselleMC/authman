import { apiFetch, setCsrfToken, type ExtensionData, type ExtensionSchema } from "@authman/shared";

export interface PortalMe {
  player: {
    id: string;
    uuid: string;
    raw_name: string;
    protocol_name: string;
    kind: "premium" | "offline";
    registration_server_label: string | null;
    last_seen_server_label: string | null;
    connected_servers: Array<{ slug: string; display_name: string }>;
  };
  csrf_token: string;
}

export interface PortalGlobalConfig {
  registration_open: boolean;
  password_policy_hints?: string[];
  message?: string;
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
  /** Set when the server is browsed in `/server/:slug`. */
  current_context?: boolean;
}

export async function portalMe(): Promise<PortalMe | null> {
  try {
    const res = await apiFetch<PortalMe>("/portal/session/me");
    setCsrfToken(res.data.csrf_token);
    return res.data;
  } catch (err) {
    // Treat 401 as not-signed-in rather than fatal.
    if ((err as { status?: number }).status === 401) {
      setCsrfToken(null);
      return null;
    }
    throw err;
  }
}

export async function portalLoginOffline(input: {
  username: string;
  password: string;
  server_slug?: string;
}): Promise<PortalMe> {
  const res = await apiFetch<PortalMe>("/portal/session/login", {
    method: "POST",
    body: input,
    skipCsrf: true,
  });
  setCsrfToken(res.data.csrf_token);
  return res.data;
}

export async function portalLoginWithLink(token: string): Promise<PortalMe & { server_slug?: string }> {
  const res = await apiFetch<PortalMe & { server_slug?: string }>("/portal/session/login-with-link", {
    method: "POST",
    body: { token },
    skipCsrf: true,
  });
  setCsrfToken(res.data.csrf_token);
  return res.data;
}

export async function portalLogout(): Promise<void> {
  try {
    await apiFetch<null>("/portal/session/logout", { method: "POST" });
  } finally {
    setCsrfToken(null);
  }
}

export async function portalRegister(input: {
  raw_username: string;
  password: string;
  server_slug?: string;
}): Promise<PortalMe> {
  const res = await apiFetch<PortalMe>("/portal/offline/register", {
    method: "POST",
    body: input,
    skipCsrf: true,
  });
  setCsrfToken(res.data.csrf_token);
  return res.data;
}

export interface CheckNameResult {
  available: boolean;
  reason?: string;
}

export async function portalCheckName(raw_username: string, server_slug?: string): Promise<CheckNameResult> {
  const res = await apiFetch<CheckNameResult>("/portal/offline/check-name", {
    method: "POST",
    body: { raw_username, server_slug },
    skipCsrf: true,
  });
  return res.data;
}

export async function portalChangePassword(input: { current_password: string; new_password: string }): Promise<void> {
  await apiFetch<null>("/portal/offline/password/change", {
    method: "POST",
    body: input,
  });
}

export async function portalGlobalConfig(): Promise<PortalGlobalConfig> {
  const res = await apiFetch<PortalGlobalConfig>("/portal/config");
  return res.data;
}

export async function portalServers(): Promise<PortalServer[]> {
  const res = await apiFetch<PortalServer[]>("/portal/servers");
  return res.data;
}

export async function portalServer(slug: string): Promise<PortalServer> {
  const res = await apiFetch<PortalServer>(`/portal/servers/${encodeURIComponent(slug)}/config`);
  return res.data;
}

export async function portalExtensionData(serverSlug?: string): Promise<ExtensionData[]> {
  if (serverSlug) {
    const res = await apiFetch<ExtensionData[]>(`/portal/player/extension-data/${encodeURIComponent(serverSlug)}`);
    return res.data;
  }
  const res = await apiFetch<ExtensionData[]>("/portal/player/extension-data");
  return res.data;
}

export type { ExtensionData, ExtensionSchema };
