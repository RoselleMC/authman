import React, { FormEvent, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  archiveProfile,
  changePassword,
  checkName,
  createProfile,
  deletePassportSkin,
  deleteProfileSkin,
  getExampleStatus,
  getPassportSkin,
  getProfileSkin,
  getPortalConfig,
  getSession,
  login,
  loginWithLink,
  logout,
  register,
  restoreProfile,
  selectProfile,
  setProfileSkinSource,
  setPassportSkinSource,
  uploadPassportSkin,
  uploadProfileSkin,
  type ExampleStatus,
  type PortalConfig,
  type PortalPassportSkin,
  type PortalProfileSkin,
  type PortalProfile,
  type PortalSession
} from "./api";
import { SkinPreview } from "./SkinPreview";
import "./styles.css";

type Mode = "login" | "register" | "link";
type Tab = "profile" | "skin" | "passport";

function App() {
  const [status, setStatus] = useState<ExampleStatus | null>(null);
  const [config, setConfig] = useState<PortalConfig | null>(null);
  const [session, setSession] = useState<PortalSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState("");
  const [tab, setTab] = useState<Tab>("profile");

  useEffect(() => {
    let active = true;
    Promise.all([getExampleStatus(), getPortalConfig(), getSession()])
      .then(([nextStatus, nextConfig, nextSession]) => {
        if (!active) {
          return;
        }
        setStatus(nextStatus);
        setConfig(nextConfig);
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

  async function handleLogout() {
    await logout();
    setSession(null);
    setTab("profile");
    setMessage("Signed out.");
  }

  async function switchProfile(profileID: string) {
    try {
      const next = await selectProfile(profileID);
      setSession(next);
      setMessage(`Switched profile to ${next.profile.protocol_name}.`);
    } catch (err) {
      setMessage(errorMessage(err));
    }
  }

  return (
    <div className="app-shell">
      <div className="terrain" aria-hidden="true">
        <span className="block block-a" />
        <span className="block block-b" />
        <span className="block block-c" />
      </div>
      <main className="page">
        <header className="topbar">
          <div className="brand">
            <p className="eyebrow">Authman downstream example</p>
            <h1>Player Camp</h1>
          </div>
          <div className="topbar-right">
            <CoreStatus status={status} loading={loading} />
            {session ? (
              <AccountChip
                session={session}
                onSwitchProfile={switchProfile}
                onLogout={handleLogout}
              />
            ) : null}
          </div>
        </header>

        {message ? (
          <div className="notice" role="status">
            <span>{message}</span>
            <button
              type="button"
              className="notice-clear"
              aria-label="Dismiss message"
              onClick={() => setMessage("")}
            >
              ×
            </button>
          </div>
        ) : null}

        {loading ? (
          <section className="stone-panel loading-panel">Loading camp inventory...</section>
        ) : session ? (
          <AuthenticatedPanel
            session={session}
            config={config}
            tab={tab}
            onTab={setTab}
            onMessage={setMessage}
            onSession={setSession}
          />
        ) : (
          <GatewayPanel
            config={config}
            onSession={(nextSession) => {
              setSession(nextSession);
              setTab("profile");
            }}
            onMessage={setMessage}
          />
        )}
      </main>
    </div>
  );
}

function AccountChip({
  session,
  onSwitchProfile,
  onLogout
}: {
  session: PortalSession;
  onSwitchProfile: (profileID: string) => void | Promise<void>;
  onLogout: () => void | Promise<void>;
}) {
  const passportAvatar = session.passport.avatar_url || session.profile.avatar_url;
  const selectableProfiles = session.profiles.filter((profile) => profile.status === "active");
  const optionList = selectableProfiles.length > 0 ? selectableProfiles : session.profiles;

  return (
    <div className="account-chip" aria-label="Signed-in passport">
      <div className="account-identity">
        <div className="account-avatar">
          {passportAvatar ? <img src={passportAvatar} alt="" /> : <span className="avatar-fallback" />}
        </div>
        <div className="account-text">
          <span className="account-label">
            {session.passport.kind === "premium" ? "Premium passport" : "Offline passport"}
          </span>
          <strong className="account-name" title={session.passport.username}>
            {session.passport.username}
          </strong>
        </div>
      </div>
      <label className="profile-picker">
        <span>Active profile</span>
        <select value={session.profile.id} onChange={(event) => onSwitchProfile(event.target.value)}>
          {optionList.map((profile) => (
            <option key={profile.id} value={profile.id}>
              {profile.protocol_name}
            </option>
          ))}
        </select>
      </label>
      <button type="button" className="ghost logout-button" onClick={onLogout}>
        Logout
      </button>
    </div>
  );
}

function GatewayPanel({
  config,
  onSession,
  onMessage
}: {
  config: PortalConfig | null;
  onSession: (session: PortalSession) => void;
  onMessage: (message: string) => void;
}) {
  const [mode, setMode] = useState<Mode>(tokenFromLocation() ? "link" : "login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState(tokenFromLocation());
  const [busy, setBusy] = useState(false);
  const [nameState, setNameState] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    onMessage("");
    try {
      const next = mode === "login"
        ? await login(username, password)
        : mode === "register"
          ? await register(username, password)
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
      const result = await checkName(username);
      setNameState(result.available ? "Name is available." : `Unavailable: ${result.reason || "already used"}`);
    } catch (err) {
      setNameState(errorMessage(err));
    }
  }

  return (
    <section className="gateway-grid">
      <form className="stone-panel auth-panel" onSubmit={submit}>
        <p className="hero-copy">
          A standalone RPG styled player panel that talks to Authman Core through its own backend proxy.
        </p>
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
  tab,
  onTab,
  onMessage,
  onSession
}: {
  session: PortalSession;
  config: PortalConfig | null;
  tab: Tab;
  onTab: (tab: Tab) => void;
  onMessage: (message: string) => void;
  onSession: (session: PortalSession) => void;
}) {
  return (
    <section className="camp-stack">
      <nav className="tab-row" aria-label="Panel tabs">
        {(["profile", "skin", "passport"] as Tab[]).map((item) => (
          <button
            key={item}
            type="button"
            className={tab === item ? "tab active" : "tab"}
            onClick={() => onTab(item)}
            aria-pressed={tab === item}
          >
            {labelForTab(item)}
          </button>
        ))}
      </nav>
      <section className="stone-panel main-panel">
        {tab === "profile" ? (
          <ProfileView session={session} onMessage={onMessage} onSession={onSession} />
        ) : null}
        {tab === "skin" ? <SkinView session={session} onMessage={onMessage} onSession={onSession} /> : null}
        {tab === "passport" ? (
          <PassportView session={session} config={config} onMessage={onMessage} />
        ) : null}
      </section>
    </section>
  );
}

function ProfileView({
  session,
  onMessage,
  onSession
}: {
  session: PortalSession;
  onMessage: (message: string) => void;
  onSession: (session: PortalSession) => void;
}) {
  const [newProfileName, setNewProfileName] = useState("");
  const [busyProfileID, setBusyProfileID] = useState("");
  const [creating, setCreating] = useState(false);
  const activeProfileCount = session.profiles.filter((profile) => profile.status === "active").length;
  const profile = session.profile;
  const profileAvatar = profile.avatar_url || session.passport.avatar_url;

  const profileFacts = useMemo(
    () => [
      ["Protocol name", profile.protocol_name || "n/a"],
      ["Profile UUID", profile.uuid],
      ["Status", profileStateLabel(profile)],
      ["Presence", presenceLabel(profile)],
      ["Last seen IP", profile.last_seen_ip || "n/a"],
      ["Location", geoLabel(profile.last_seen_geo)]
    ],
    [profile]
  );

  async function createOwnedProfile(event: FormEvent) {
    event.preventDefault();
    if (!newProfileName.trim()) {
      onMessage("Enter a profile name first.");
      return;
    }
    setCreating(true);
    onMessage("");
    try {
      const next = await createProfile(newProfileName);
      onSession(next);
      setNewProfileName("");
      onMessage(`Profile created: ${next.profile.protocol_name}.`);
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setCreating(false);
    }
  }

  async function selectOwnedProfile(target: PortalProfile) {
    if (target.id === profile.id || target.status !== "active") {
      return;
    }
    setBusyProfileID(target.id);
    onMessage("");
    try {
      const next = await selectProfile(target.id);
      onSession(next);
      onMessage(`Switched profile to ${next.profile.protocol_name}.`);
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusyProfileID("");
    }
  }

  async function archiveOwnedProfile(target: PortalProfile) {
    setBusyProfileID(target.id);
    onMessage("");
    try {
      const next = await archiveProfile(target.id);
      onSession(next);
      onMessage(`Profile archived: ${target.protocol_name}.`);
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusyProfileID("");
    }
  }

  async function restoreOwnedProfile(target: PortalProfile) {
    setBusyProfileID(target.id);
    onMessage("");
    try {
      const next = await restoreProfile(target.id);
      onSession(next);
      onMessage(`Profile restored: ${target.protocol_name}.`);
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusyProfileID("");
    }
  }

  return (
    <div className="profile-view">
      <header className="view-heading">
        <div className="view-heading-text">
          <span className="eyebrow">Selected profile</span>
          <h2>{profile.protocol_name || "Unnamed profile"}</h2>
        </div>
        <div className="profile-headline-avatar">
          {profileAvatar ? <img src={profileAvatar} alt="" /> : <span className="avatar-fallback" />}
        </div>
      </header>

      <div className="state-strip">
        <StatusBadge tone={profile.active_ban ? "danger" : profile.status === "active" ? "online" : "warning"}>
          {profile.active_ban ? `Banned: ${banLabel(profile.active_ban)}` : profile.status || "active"}
        </StatusBadge>
        <StatusBadge tone={profile.online ? "online" : "neutral"}>
          {presenceLabel(profile)}
        </StatusBadge>
        {profile.locked_until ? (
          <StatusBadge tone="warning">Locked until {formatDate(profile.locked_until)}</StatusBadge>
        ) : null}
      </div>

      <div className="inventory-grid">
        {profileFacts.map(([label, value]) => (
          <article className="inventory-slot" key={label}>
            <span>{label}</span>
            <strong>{value}</strong>
          </article>
        ))}
      </div>

      <section className="roster-panel" aria-label="Your profiles">
        <div className="section-heading">
          <h3>Your profiles</h3>
          <small>{session.profiles.length} profile(s) available to this passport</small>
        </div>
        <form className="profile-create-form" onSubmit={createOwnedProfile}>
          <label>
            New profile name
            <input value={newProfileName} onChange={(event) => setNewProfileName(event.target.value)} autoComplete="off" />
          </label>
          <button className="primary" disabled={creating} type="submit">
            {creating ? "Creating..." : "Create Profile"}
          </button>
        </form>
        <div className="profile-list">
          {session.profiles.map((row) => (
            <article className={row.id === profile.id ? "profile-row active" : "profile-row"} key={row.id}>
              {row.avatar_url || session.passport.avatar_url ? (
                <img src={row.avatar_url || session.passport.avatar_url} alt="" />
              ) : (
                <span className="mini-avatar" />
              )}
              <div className="profile-row-text">
                <strong>{row.protocol_name}</strong>
                <span>{row.uuid}</span>
              </div>
              <div className="row-badges">
                <StatusBadge tone={row.online ? "online" : "neutral"}>{presenceLabel(row)}</StatusBadge>
                <StatusBadge tone={row.active_ban ? "danger" : row.status === "active" ? "online" : "warning"}>
                  {row.active_ban ? "Banned" : row.status}
                </StatusBadge>
              </div>
              <div className="profile-actions">
                {row.status === "active" ? (
                  <>
                    <button
                      className="secondary"
                      type="button"
                      disabled={busyProfileID === row.id || row.id === profile.id}
                      onClick={() => selectOwnedProfile(row)}
                    >
                      Select
                    </button>
                    <button
                      className="secondary"
                      type="button"
                      disabled={busyProfileID === row.id || activeProfileCount <= 1}
                      onClick={() => archiveOwnedProfile(row)}
                      aria-label={`Archive ${row.protocol_name}`}
                    >
                      Archive
                    </button>
                  </>
                ) : (
                  <button
                    className="secondary"
                    type="button"
                    disabled={busyProfileID === row.id}
                    onClick={() => restoreOwnedProfile(row)}
                    aria-label={`Restore ${row.protocol_name}`}
                  >
                    Restore
                  </button>
                )}
              </div>
            </article>
          ))}
        </div>
      </section>

      <section className="boundary-note">
        <h3>Player portal boundary</h3>
        <p>
          You can create new profiles, archive or restore your own profiles, switch the active one, and edit the
          selected profile&apos;s skin. Deletion, passport binding, bans, audit logs, and server administration stay
          in Authman Core.
        </p>
      </section>
    </div>
  );
}

function SkinView({
  session,
  onMessage,
  onSession
}: {
  session: PortalSession;
  onMessage: (message: string) => void;
  onSession: (session: PortalSession) => void;
}) {
  const [skin, setSkin] = useState<PortalProfileSkin | null>(null);
  const [passportSkin, setPassportSkin] = useState<PortalPassportSkin | null>(null);
  const [skinFile, setSkinFile] = useState<File | null>(null);
  const [capeFile, setCapeFile] = useState<File | null>(null);
  const [elytraFile, setElytraFile] = useState<File | null>(null);
  const [passportSkinFile, setPassportSkinFile] = useState<File | null>(null);
  const [passportCapeFile, setPassportCapeFile] = useState<File | null>(null);
  const [passportElytraFile, setPassportElytraFile] = useState<File | null>(null);
  const [model, setModel] = useState<"wide" | "slim">("wide");
  const [passportModel, setPassportModel] = useState<"wide" | "slim">("wide");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const skinPreviewURL = useObjectURL(skinFile);
  const capePreviewURL = useObjectURL(capeFile);
  const elytraPreviewURL = useObjectURL(elytraFile);
  const passportSkinPreviewURL = useObjectURL(passportSkinFile);
  const passportCapePreviewURL = useObjectURL(passportCapeFile);
  const passportElytraPreviewURL = useObjectURL(passportElytraFile);

  useEffect(() => {
    let active = true;
    setLoading(true);
    Promise.all([getProfileSkin(), getPassportSkin()])
      .then(([nextSkin, nextPassportSkin]) => {
        if (!active) return;
        setSkin(nextSkin);
        setPassportSkin(nextPassportSkin);
        setModel(nextSkin.model === "slim" ? "slim" : "wide");
        setPassportModel(nextPassportSkin.model === "slim" ? "slim" : "wide");
        setSkinFile(null);
        setCapeFile(null);
        setElytraFile(null);
        setPassportSkinFile(null);
        setPassportCapeFile(null);
        setPassportElytraFile(null);
      })
      .catch((err) => {
        if (active) onMessage(errorMessage(err));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [session.profile.id, onMessage]);

  const usingPassportSkin = Boolean(skin?.use_passport_skin);
  const usingUpstreamSkin = Boolean(passportSkin?.use_upstream_skin);
  const hasChanges = !usingPassportSkin && Boolean(skinFile || capeFile || elytraFile || (skin?.has_custom_skin && normalizeModel(skin.model) !== model));
  const hasPassportChanges = !usingUpstreamSkin && Boolean(
    passportSkinFile ||
      passportCapeFile ||
      passportElytraFile ||
      (passportSkin?.has_custom_skin && normalizeModel(passportSkin.model) !== passportModel)
  );
  const previewSkin = skinPreviewURL || versionedAsset(skin?.skin_url, skin?.updated_at);
  const previewCape = capePreviewURL || versionedAsset(skin?.cape_url, skin?.updated_at);
  const previewElytra = elytraPreviewURL || versionedAsset(skin?.elytra_url, skin?.updated_at);
  const passportPreviewSkin = passportSkinPreviewURL || versionedAsset(passportSkin?.skin_url, passportSkin?.updated_at);
  const passportPreviewCape = passportCapePreviewURL || versionedAsset(passportSkin?.cape_url, passportSkin?.updated_at);
  const passportPreviewElytra = passportElytraPreviewURL || versionedAsset(passportSkin?.elytra_url, passportSkin?.updated_at);

  async function refreshSession() {
    const nextSession = await getSession();
    if (nextSession) {
      onSession(nextSession);
    }
  }

  async function save() {
    setBusy(true);
    onMessage("");
    try {
      const nextSkin = await uploadProfileSkin({ skin: skinFile, cape: capeFile, elytra: elytraFile, model });
      setSkin(nextSkin);
      setModel(nextSkin.model === "slim" ? "slim" : "wide");
      setSkinFile(null);
      setCapeFile(null);
      setElytraFile(null);
      await refreshSession();
      onMessage("Skin settings saved.");
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function reset() {
    setBusy(true);
    onMessage("");
    try {
      const nextSkin = await deleteProfileSkin();
      setSkin(nextSkin);
      setModel(nextSkin.model === "slim" ? "slim" : "wide");
      setSkinFile(null);
      setCapeFile(null);
      setElytraFile(null);
      await refreshSession();
      onMessage("Custom skin reset.");
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function togglePassportSource(usePassportSkin: boolean) {
    setBusy(true);
    onMessage("");
    try {
      const nextSkin = await setProfileSkinSource(usePassportSkin);
      setSkin(nextSkin);
      setModel(nextSkin.model === "slim" ? "slim" : "wide");
      setSkinFile(null);
      setCapeFile(null);
      setElytraFile(null);
      await refreshSession();
      onMessage(usePassportSkin ? "Profile now inherits the passport skin." : "Profile now uses its own skin priority.");
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function savePassportSkin() {
    setBusy(true);
    onMessage("");
    try {
      const nextSkin = await uploadPassportSkin({
        skin: passportSkinFile,
        cape: passportCapeFile,
        elytra: passportElytraFile,
        model: passportModel
      });
      setPassportSkin(nextSkin);
      setPassportModel(nextSkin.model === "slim" ? "slim" : "wide");
      setPassportSkinFile(null);
      setPassportCapeFile(null);
      setPassportElytraFile(null);
      const nextProfileSkin = await getProfileSkin();
      setSkin(nextProfileSkin);
      await refreshSession();
      onMessage("Passport skin settings saved.");
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function toggleUpstreamSource(useUpstreamSkin: boolean) {
    setBusy(true);
    onMessage("");
    try {
      const nextSkin = await setPassportSkinSource(useUpstreamSkin);
      setPassportSkin(nextSkin);
      setPassportModel(nextSkin.model === "slim" ? "slim" : "wide");
      setPassportSkinFile(null);
      setPassportCapeFile(null);
      setPassportElytraFile(null);
      const nextProfileSkin = await getProfileSkin();
      setSkin(nextProfileSkin);
      setModel(nextProfileSkin.model === "slim" ? "slim" : "wide");
      await refreshSession();
      onMessage(useUpstreamSkin ? "Passport now uses upstream skin." : "Passport now allows custom texture priority.");
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function resetPassportSkin() {
    setBusy(true);
    onMessage("");
    try {
      const nextSkin = await deletePassportSkin();
      setPassportSkin(nextSkin);
      setPassportModel(nextSkin.model === "slim" ? "slim" : "wide");
      setPassportSkinFile(null);
      setPassportCapeFile(null);
      setPassportElytraFile(null);
      const nextProfileSkin = await getProfileSkin();
      setSkin(nextProfileSkin);
      await refreshSession();
      onMessage("Passport custom skin reset.");
    } catch (err) {
      onMessage(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  if (loading || !skin || !passportSkin) {
    return <div className="empty">Loading skin workshop...</div>;
  }

  return (
    <div className="skin-view">
      <header className="view-heading">
        <div className="view-heading-text">
          <span className="eyebrow">Skin editor</span>
          <h2>{session.profile.protocol_name}</h2>
        </div>
      </header>
      <div className="section-heading skin-section-title">
        <h3>Selected profile skin</h3>
        <small>Choose whether this profile inherits the passport skin or uses its own custom texture.</small>
      </div>
      <div className="skin-workshop">
        <section className="skin-preview-panel">
          {previewSkin ? (
            <SkinPreview
              skinUrl={previewSkin}
              capeUrl={previewCape}
              elytraUrl={previewElytra}
              model={model}
            />
          ) : (
            <div className="empty">No skin preview available.</div>
          )}
        </section>

        <section className="skin-editor-panel">
          <div className="inventory-grid compact">
            <article className="inventory-slot">
              <span>Effective source</span>
              <strong>{skin.effective_source}</strong>
            </article>
            <article className="inventory-slot">
              <span>Default skin</span>
              <strong>{skin.default_variant} · {skin.default_model}</strong>
            </article>
            <article className="inventory-slot">
              <span>Custom skin</span>
              <strong>{skin.has_custom_skin ? "Yes" : "No"}</strong>
            </article>
            <article className="inventory-slot">
              <span>Custom cape / elytra</span>
              <strong>{skin.has_custom_cape || skin.has_custom_elytra ? "Yes" : "No"}</strong>
            </article>
            <article className="inventory-slot">
              <span>Uses passport skin</span>
              <strong>{usingPassportSkin ? "Yes" : "No"}</strong>
            </article>
          </div>

          <div className="field-stack">
            <label className="toggle-line">
              <input
                type="checkbox"
                checked={usingPassportSkin}
                disabled={busy}
                onChange={(event) => togglePassportSource(event.currentTarget.checked)}
              />
              <span>Inherit passport skin when this profile has no custom override</span>
            </label>
            {!usingPassportSkin ? (
              <>
                <label>
                  Arm model
                  <select value={model} onChange={(event) => setModel(event.target.value === "slim" ? "slim" : "wide")}>
                    <option value="wide">Wide arms</option>
                    <option value="slim">Slim arms</option>
                  </select>
                </label>
                <SkinFileInput id="player-skin-file" label="Skin PNG" file={skinFile} onChange={setSkinFile} />
                <SkinFileInput id="player-cape-file" label="Cape PNG" file={capeFile} onChange={setCapeFile} />
                <SkinFileInput id="player-elytra-file" label="Elytra PNG" file={elytraFile} onChange={setElytraFile} />
              </>
            ) : null}
          </div>

          {!usingPassportSkin ? (
            <>
              <div className="button-row">
                <button className="primary" type="button" disabled={busy || !hasChanges} onClick={save}>
                  {busy ? "Saving..." : "Save Skin"}
                </button>
                <button className="secondary" type="button" disabled={busy || !skin.has_custom_skin} onClick={reset}>
                  Reset Custom
                </button>
              </div>
              <p className="inline-status">
                Profile skin edits apply only to the currently selected profile. Uploading a custom skin switches this
                profile back to its own custom texture priority.
              </p>
            </>
          ) : (
            <p className="inline-status">Custom profile texture controls are hidden while this profile inherits the passport skin.</p>
          )}
        </section>
      </div>
      <div className="section-heading skin-section-title">
        <h3>Passport global skin</h3>
        <small>Used by this passport and by profiles that inherit passport skin.</small>
      </div>
      <div className="skin-workshop">
        <section className="skin-preview-panel">
          {passportPreviewSkin ? (
            <SkinPreview
              skinUrl={passportPreviewSkin}
              capeUrl={passportPreviewCape}
              elytraUrl={passportPreviewElytra}
              model={passportModel}
            />
          ) : (
            <div className="empty">No passport skin preview available.</div>
          )}
        </section>

        <section className="skin-editor-panel">
          <div className="inventory-grid compact">
            <article className="inventory-slot">
              <span>Effective source</span>
              <strong>{passportSkin.effective_source}</strong>
            </article>
            <article className="inventory-slot">
              <span>Default skin</span>
              <strong>{passportSkin.default_variant} · {passportSkin.default_model}</strong>
            </article>
            <article className="inventory-slot">
              <span>Custom skin</span>
              <strong>{passportSkin.has_custom_skin ? "Yes" : "No"}</strong>
            </article>
            <article className="inventory-slot">
              <span>Custom cape / elytra</span>
              <strong>{passportSkin.has_custom_cape || passportSkin.has_custom_elytra ? "Yes" : "No"}</strong>
            </article>
            <article className="inventory-slot">
              <span>Uses upstream skin</span>
              <strong>{usingUpstreamSkin ? "Yes" : "No"}</strong>
            </article>
          </div>

          <div className="field-stack">
            <label className="toggle-line">
              <input
                type="checkbox"
                checked={usingUpstreamSkin}
                disabled={busy}
                onChange={(event) => toggleUpstreamSource(event.currentTarget.checked)}
              />
              <span>Use upstream passport skin from Mojang or UUID default.</span>
            </label>
            {!usingUpstreamSkin ? (
              <>
                <label>
                  Arm model
                  <select value={passportModel} onChange={(event) => setPassportModel(event.target.value === "slim" ? "slim" : "wide")}>
                    <option value="wide">Wide arms</option>
                    <option value="slim">Slim arms</option>
                  </select>
                </label>
                <SkinFileInput id="passport-skin-file" label="Passport skin PNG" file={passportSkinFile} onChange={setPassportSkinFile} />
                <SkinFileInput id="passport-cape-file" label="Passport cape PNG" file={passportCapeFile} onChange={setPassportCapeFile} />
                <SkinFileInput id="passport-elytra-file" label="Passport elytra PNG" file={passportElytraFile} onChange={setPassportElytraFile} />
              </>
            ) : null}
          </div>

          {!usingUpstreamSkin ? (
            <>
              <div className="button-row">
                <button className="primary" type="button" disabled={busy || !hasPassportChanges} onClick={savePassportSkin}>
                  {busy ? "Saving..." : "Save Passport Skin"}
                </button>
                <button className="secondary" type="button" disabled={busy || !passportSkin.has_custom_skin} onClick={resetPassportSkin}>
                  Reset Passport Custom
                </button>
              </div>
              <p className="inline-status">
                Uploading here makes Authman use this custom passport skin until upstream skin is enabled again.
              </p>
            </>
          ) : (
            <p className="inline-status">Custom passport texture controls are hidden while upstream skin is enabled.</p>
          )}
        </section>
      </div>
    </div>
  );
}

function PassportView({
  session,
  config,
  onMessage
}: {
  session: PortalSession;
  config: PortalConfig | null;
  onMessage: (message: string) => void;
}) {
  const passport = session.passport;
  const player = session.player;
  const passportAvatar = passport.avatar_url || session.profile.avatar_url;

  const passportFacts = useMemo(
    () => [
      ["Kind", passport.kind === "premium" ? "Premium" : "Offline"],
      ["Username", passport.username],
      ["Passport UUID", passport.uuid],
      ["Status", passportStateLabel(passport)],
      ["Profile count", String(passport.profile_count ?? session.profiles.length)],
      ["Presence", passport.online ? `${passport.presence_count || 1} online` : "Offline"],
      ["Registered at", player.registration_server_label || passport.registration_server || "n/a"],
      ["Last seen", player.last_seen_server_label || passport.last_seen_server || "n/a"],
      ["Last IP", player.last_seen_ip || passport.last_seen_ip || "n/a"],
      ["Location", geoLabel(player.last_seen_geo || passport.last_seen_geo)]
    ],
    [passport, player, session.profiles.length]
  );

  return (
    <div className="passport-view">
      <header className="view-heading">
        <div className="view-heading-text">
          <span className="eyebrow">Passport</span>
          <h2>{passport.username}</h2>
        </div>
        <div className="profile-headline-avatar">
          {passportAvatar ? <img src={passportAvatar} alt="" /> : <span className="avatar-fallback" />}
        </div>
      </header>

      <div className="state-strip">
        <StatusBadge tone={passport.kind === "premium" ? "premium" : "offline"}>
          {passport.kind === "premium" ? "Premium passport" : "Offline passport"}
        </StatusBadge>
        <StatusBadge tone={passport.active_ban ? "danger" : "online"}>
          {passport.active_ban ? `Banned: ${banLabel(passport.active_ban)}` : "Passport usable"}
        </StatusBadge>
        {passport.locked_until ? (
          <StatusBadge tone="warning">Locked until {formatDate(passport.locked_until)}</StatusBadge>
        ) : null}
      </div>

      <div className="inventory-grid">
        {passportFacts.map(([label, value]) => (
          <article className="inventory-slot" key={label}>
            <span>{label}</span>
            <strong>{value}</strong>
          </article>
        ))}
      </div>

      <section className="security-panel">
        <div className="section-heading">
          <h3>Passport security</h3>
          <small>Account-level actions stay here</small>
        </div>
        {passport.kind === "premium" ? (
          <div className="security-note">
            <p>
              Premium passports authenticate through the Mojang session or a trusted portal link, so there is no
              password to manage here. Microsoft account controls remain with Mojang.
            </p>
          </div>
        ) : (
          <OfflinePasswordForm config={config} onMessage={onMessage} />
        )}
      </section>

      <section className="boundary-note">
        <h3>Out of scope for this example</h3>
        <p>
          Binding or unbinding passports, deleting profiles, viewing audit logs, and managing bans or server settings
          are Core/admin responsibilities. This panel exposes only the player&apos;s self-service surface.
        </p>
      </section>
    </div>
  );
}

function OfflinePasswordForm({
  config,
  onMessage
}: {
  config: PortalConfig | null;
  onMessage: (message: string) => void;
}) {
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
        <input
          type="password"
          value={currentPassword}
          autoComplete="current-password"
          onChange={(event) => setCurrentPassword(event.target.value)}
        />
      </label>
      <label>
        New password
        <input
          type="password"
          value={newPassword}
          autoComplete="new-password"
          onChange={(event) => setNewPassword(event.target.value)}
        />
      </label>
      {config?.password_policy_hints?.length ? (
        <ul className="policy-list" aria-label="Password policy">
          {config.password_policy_hints.map((hint) => (
            <li key={hint}>{hint}</li>
          ))}
        </ul>
      ) : null}
      <button className="primary" disabled={busy} type="submit">
        {busy ? "Saving..." : "Change Password"}
      </button>
    </form>
  );
}

function StatusBadge({
  children,
  tone = "neutral"
}: {
  children: React.ReactNode;
  tone?: "premium" | "offline" | "online" | "neutral" | "warning" | "danger";
}) {
  return <span className={`status-badge ${tone}`}>{children}</span>;
}

function CoreStatus({ status, loading }: { status: ExampleStatus | null; loading: boolean }) {
  const checking = loading && !status;
  const reachable = Boolean(status?.core_reachable);
  const label = checking ? "Checking Core" : reachable ? "Core Online" : "Core Unreachable";
  const detail = checking
    ? "health pending"
    : status?.core_health_status
      ? `HTTP ${status.core_health_status}`
      : "health unavailable";
  const tone = checking ? "pending" : reachable ? "online" : "offline";
  return (
    <div className={`core-rune ${tone}`} title="Core connection" aria-live="polite">
      <span className={`pulse ${tone}`} />
      <div className="core-rune-text">
        <strong>{label}</strong>
        <small>{detail}</small>
      </div>
    </div>
  );
}

function profileStateLabel(profile: PortalProfile) {
  if (profile.active_ban) {
    return `Banned ${banLabel(profile.active_ban)}`;
  }
  if (profile.locked_until) {
    return `Locked until ${formatDate(profile.locked_until)}`;
  }
  return profile.status || "active";
}

function passportStateLabel(passport: PortalSession["passport"]) {
  if (passport.active_ban) {
    return `Banned ${banLabel(passport.active_ban)}`;
  }
  if (passport.locked_until) {
    return `Locked until ${formatDate(passport.locked_until)}`;
  }
  return passport.status || "active";
}

function presenceLabel(profile: PortalProfile) {
  return profile.online ? `${profile.presence_count || 1} online` : "Offline";
}

function banLabel(ban: { reason?: string; expires_at?: string | null }) {
  const reason = ban.reason?.trim() || "No reason";
  return ban.expires_at ? `${reason}, until ${formatDate(ban.expires_at)}` : `${reason}, permanent`;
}

function geoLabel(geo: { country?: string; regionName?: string; city?: string } | null | undefined) {
  if (!geo) {
    return "n/a";
  }
  return [geo.country, geo.regionName, geo.city].filter(Boolean).join(" / ") || "n/a";
}

function formatDate(value?: string | null) {
  if (!value) {
    return "n/a";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function labelForTab(tab: Tab) {
  switch (tab) {
    case "profile":
      return "Profile";
    case "skin":
      return "Skin";
    case "passport":
      return "Passport";
  }
}

function SkinFileInput({
  id,
  label,
  file,
  onChange
}: {
  id: string;
  label: string;
  file: File | null;
  onChange: (file: File | null) => void;
}) {
  return (
    <div className="skin-file-control">
      <input
        id={id}
        className="skin-file-native"
        type="file"
        accept="image/png"
        onChange={(event) => onChange(event.target.files?.[0] ?? null)}
      />
      <label className="skin-file-button" htmlFor={id}>
        {label}
      </label>
      <span>{file ? file.name : "No file selected"}</span>
      {file ? (
        <button type="button" className="skin-file-clear" onClick={() => onChange(null)} aria-label={`Clear ${label}`}>
          Clear
        </button>
      ) : null}
    </div>
  );
}

function useObjectURL(file: File | null) {
  const [url, setURL] = useState<string | null>(null);
  useEffect(() => {
    if (!file) {
      setURL(null);
      return undefined;
    }
    const next = URL.createObjectURL(file);
    setURL(next);
    return () => URL.revokeObjectURL(next);
  }, [file]);
  return url;
}

function normalizeModel(model: string | undefined) {
  return model === "slim" ? "slim" : "wide";
}

function versionedAsset(url: string | null | undefined, version: string | null | undefined) {
  if (!url) return "";
  if (!version || url.startsWith("blob:") || url.startsWith("data:")) return url;
  try {
    const parsed = new URL(url, window.location.origin);
    if (parsed.searchParams.has("v")) return url;
  } catch {
    if (/[?&]v=/.test(url)) return url;
  }
  return `${url}${url.includes("?") ? "&" : "?"}v=${encodeURIComponent(version)}`;
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
