import React, { FormEvent, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  changePassword,
  checkName,
  getExampleStatus,
  getExtensionData,
  getPortalConfig,
  getServers,
  getSession,
  login,
  loginWithLink,
  logout,
  register,
  type ExampleStatus,
  type ExtensionData,
  type PortalConfig,
  type PortalServer,
  type PortalSession
} from "./api";
import "./styles.css";

type Mode = "login" | "register" | "link";
type Tab = "profile" | "servers" | "extensions" | "security";

function App() {
  const [status, setStatus] = useState<ExampleStatus | null>(null);
  const [config, setConfig] = useState<PortalConfig | null>(null);
  const [servers, setServers] = useState<PortalServer[]>([]);
  const [session, setSession] = useState<PortalSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [tab, setTab] = useState<Tab>("profile");

  useEffect(() => {
    let active = true;
    Promise.all([getExampleStatus(), getPortalConfig(), getServers(), getSession()])
      .then(([nextStatus, nextConfig, nextServers, nextSession]) => {
        if (!active) {
          return;
        }
        setStatus(nextStatus);
        setConfig(nextConfig);
        setServers(nextServers);
        setSession(nextSession);
      })
      .catch((err) => {
        if (active) {
          setMessage(errorMessage(err));
        }
      })
      .finally(() => {
        if (active) {
          setLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const selectedServer = servers[0]?.slug;

  return (
    <div className="app-shell">
      <div className="terrain" aria-hidden="true">
        <span className="block block-a" />
        <span className="block block-b" />
        <span className="block block-c" />
      </div>
      <main className="page">
        <header className="hero">
          <div>
            <p className="eyebrow">Authman downstream example</p>
            <h1>Player Camp</h1>
            <p className="hero-copy">
              A standalone RPG styled player panel that talks to Authman Core through its own backend proxy.
            </p>
          </div>
          <div className="core-rune" title="Core connection">
            <span className={status?.core_reachable ? "pulse online" : "pulse offline"} />
            <strong>{status?.core_reachable ? "Core Online" : "Core Unknown"}</strong>
            <small>{status?.core_health_status ? `HTTP ${status.core_health_status}` : "waiting"}</small>
          </div>
        </header>

        {message ? <div className="notice">{message}</div> : null}
        {loading ? (
          <section className="stone-panel loading-panel">Loading camp inventory...</section>
        ) : session ? (
          <AuthenticatedPanel
            session={session}
            config={config}
            servers={servers}
            tab={tab}
            onTab={setTab}
            onMessage={setMessage}
            onLogout={async () => {
              await logout();
              setSession(null);
              setMessage("Signed out.");
            }}
          />
        ) : (
          <GatewayPanel
            config={config}
            servers={servers}
            defaultServerSlug={selectedServer}
            onSession={setSession}
            onMessage={setMessage}
          />
        )}
      </main>
    </div>
  );
}

function GatewayPanel({
  config,
  servers,
  defaultServerSlug,
  onSession,
  onMessage
}: {
  config: PortalConfig | null;
  servers: PortalServer[];
  defaultServerSlug?: string;
  onSession: (session: PortalSession) => void;
  onMessage: (message: string) => void;
}) {
  const [mode, setMode] = useState<Mode>(tokenFromLocation() ? "link" : "login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState(tokenFromLocation());
  const [serverSlug, setServerSlug] = useState(defaultServerSlug || "");
  const [busy, setBusy] = useState(false);
  const [nameState, setNameState] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    onMessage("");
    try {
      const next = mode === "login"
        ? await login(username, password, serverSlug)
        : mode === "register"
          ? await register(username, password, serverSlug)
          : await loginWithLink(token);
      onSession(next);
      onMessage("Session opened through Authman Core.");
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function runCheckName() {
    if (!username.trim()) {
      setNameState("Enter a name first.");
      return;
    }
    try {
      const result = await checkName(username, serverSlug);
      setNameState(result.available ? "Name is available." : `Unavailable: ${result.reason || "already used"}`);
    } catch (err) {
      setNameState(errorMessage(err));
    }
  }

  return (
    <section className="gateway-grid">
      <form className="stone-panel auth-panel" onSubmit={submit}>
        <div className="mode-row" role="tablist" aria-label="Auth mode">
          {(["login", "register", "link"] as Mode[]).map((item) => (
            <button
              key={item}
              type="button"
              className={mode === item ? "mode active" : "mode"}
              onClick={() => setMode(item)}
            >
              {item === "login" ? "Login" : item === "register" ? "Register" : "Portal Link"}
            </button>
          ))}
        </div>

        <div className="field-stack">
          {mode === "link" ? (
            <label>
              Link token
              <input value={token} onChange={(event) => setToken(event.target.value)} autoComplete="one-time-code" />
            </label>
          ) : (
            <>
              <label>
                Player name
                <input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
              </label>
              <label>
                Password
                <input
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  type="password"
                  autoComplete={mode === "login" ? "current-password" : "new-password"}
                />
              </label>
              <label>
                Server context
                <select value={serverSlug} onChange={(event) => setServerSlug(event.target.value)}>
                  <option value="">Global portal</option>
                  {servers.map((server) => (
                    <option key={server.slug} value={server.slug}>
                      {server.display_name}
                    </option>
                  ))}
                </select>
              </label>
            </>
          )}
        </div>

        <div className="button-row">
          <button className="primary" disabled={busy} type="submit">
            {busy ? "Working..." : mode === "register" ? "Create Player" : "Enter Camp"}
          </button>
          {mode === "register" ? (
            <button className="secondary" type="button" onClick={runCheckName}>
              Check Name
            </button>
          ) : null}
        </div>
        {nameState ? <p className="inline-status">{nameState}</p> : null}
      </form>

      <aside className="stone-panel quest-panel">
        <div className="totem">
          <span />
          <span />
          <span />
        </div>
        <h2>{config?.message || "Authman central portal"}</h2>
        <p>{config?.registration_open ? "Offline registration is open." : "Offline registration is closed."}</p>
        <div className="api-scroll">
          <code>POST /api/portal/session/login</code>
          <code>POST /api/portal/offline/register</code>
          <code>GET /api/portal/session/me</code>
        </div>
      </aside>
    </section>
  );
}

function AuthenticatedPanel({
  session,
  config,
  servers,
  tab,
  onTab,
  onMessage,
  onLogout
}: {
  session: PortalSession;
  config: PortalConfig | null;
  servers: PortalServer[];
  tab: Tab;
  onTab: (tab: Tab) => void;
  onMessage: (message: string) => void;
  onLogout: () => Promise<void>;
}) {
  return (
    <section className="camp-grid">
      <aside className="stone-panel player-card">
        <div className="skin">
          <span className="head" />
          <span className="body" />
          <span className="arm left" />
          <span className="arm right" />
          <span className="leg left" />
          <span className="leg right" />
        </div>
        <h2>{displayName(session)}</h2>
        <p>{session.player.kind === "premium" ? "Premium profile" : "Offline profile"}</p>
        <button className="secondary full" onClick={onLogout}>
          Logout
        </button>
      </aside>

      <section className="stone-panel main-panel">
        <nav className="tab-row" aria-label="Panel tabs">
          {(["profile", "servers", "extensions", "security"] as Tab[]).map((item) => (
            <button key={item} className={tab === item ? "tab active" : "tab"} onClick={() => onTab(item)}>
              {labelForTab(item)}
            </button>
          ))}
        </nav>
        {tab === "profile" ? <ProfileView session={session} config={config} /> : null}
        {tab === "servers" ? <ServersView servers={servers} /> : null}
        {tab === "extensions" ? <ExtensionsView servers={servers} onMessage={onMessage} /> : null}
        {tab === "security" ? <SecurityView onMessage={onMessage} /> : null}
      </section>
    </section>
  );
}

function ProfileView({ session, config }: { session: PortalSession; config: PortalConfig | null }) {
  const inventory = useMemo(
    () => [
      ["Player ID", session.player.id],
      ["UUID", session.player.uuid],
      ["Protocol name", session.player.protocol_name || "n/a"],
      ["Registered at", session.player.registration_server_label || "n/a"],
      ["Last seen", session.player.last_seen_server_label || "n/a"],
      ["Password policy", config?.password_policy_hints?.join(", ") || "n/a"]
    ],
    [session, config]
  );

  return (
    <div className="inventory-grid">
      {inventory.map(([label, value]) => (
        <article className="inventory-slot" key={label}>
          <span>{label}</span>
          <strong>{value}</strong>
        </article>
      ))}
    </div>
  );
}

function ServersView({ servers }: { servers: PortalServer[] }) {
  if (servers.length === 0) {
    return <div className="empty">No downstream servers are visible to the player portal.</div>;
  }
  return (
    <div className="server-list">
      {servers.map((server) => (
        <article className="server-card" key={server.slug} style={server.primary_color ? { borderColor: server.primary_color } : undefined}>
          <span className="server-gem" style={server.accent_color ? { background: server.accent_color } : undefined} />
          <div>
            <h3>{server.display_name}</h3>
            <p>{server.description || server.portal_message || "Downstream server"}</p>
            <small>{server.registration_open ? "Registration open" : "Registration closed"}</small>
          </div>
        </article>
      ))}
    </div>
  );
}

function ExtensionsView({ servers, onMessage }: { servers: PortalServer[]; onMessage: (message: string) => void }) {
  const [serverSlug, setServerSlug] = useState("");
  const [rows, setRows] = useState<ExtensionData[]>([]);
  const [busy, setBusy] = useState(false);

  async function load() {
    setBusy(true);
    onMessage("");
    try {
      setRows(await getExtensionData(serverSlug || undefined));
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="extension-view">
      <div className="toolbar">
        <select value={serverSlug} onChange={(event) => setServerSlug(event.target.value)}>
          <option value="">All servers</option>
          {servers.map((server) => (
            <option key={server.slug} value={server.slug}>
              {server.display_name}
            </option>
          ))}
        </select>
        <button className="primary" onClick={load} disabled={busy}>
          {busy ? "Loading..." : "Load Data"}
        </button>
      </div>
      {rows.length === 0 ? (
        <div className="empty">No player-visible extension data loaded.</div>
      ) : (
        <div className="loot-list">
          {rows.map((row, index) => (
            <article className="loot-row" key={`${row.provider}-${row.id || index}`}>
              <strong>{row.label || row.provider}</strong>
              <pre>{JSON.stringify(row.data ?? row, null, 2)}</pre>
            </article>
          ))}
        </div>
      )}
    </div>
  );
}

function SecurityView({ onMessage }: { onMessage: (message: string) => void }) {
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    onMessage("");
    try {
      await changePassword(currentPassword, newPassword);
      setCurrentPassword("");
      setNewPassword("");
      onMessage("Password changed.");
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="security-form" onSubmit={submit}>
      <label>
        Current password
        <input type="password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} />
      </label>
      <label>
        New password
        <input type="password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} />
      </label>
      <button className="primary" disabled={busy} type="submit">
        {busy ? "Saving..." : "Change Password"}
      </button>
    </form>
  );
}

function displayName(session: PortalSession) {
  return session.player.protocol_name || session.player.raw_name || session.player.raw_offline_name || "Player";
}

function labelForTab(tab: Tab) {
  switch (tab) {
    case "profile":
      return "Profile";
    case "servers":
      return "Servers";
    case "extensions":
      return "Extensions";
    case "security":
      return "Security";
  }
}

function tokenFromLocation() {
  const hash = new URLSearchParams(window.location.hash.replace(/^#/, ""));
  const query = new URLSearchParams(window.location.search);
  return hash.get("token") || query.get("token") || "";
}

function errorMessage(err: unknown) {
  if (err instanceof Error) {
    const withCode = err as Error & { code?: string };
    return withCode.code ? `${withCode.code}: ${err.message}` : err.message;
  }
  return "Unexpected error.";
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
