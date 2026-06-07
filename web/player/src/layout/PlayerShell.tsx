import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import {
  BrandMark,
  IconButton,
  LocaleSelect,
  ThemeToggle,
  cx,
  useI18n,
  useToast,
} from "@authman/shared";
import { useSession } from "../auth/SessionContext";
import { useServerContext } from "../server-context/ServerContextProvider";

export function PlayerShell() {
  const { t } = useI18n();
  const { me, logout } = useSession();
  const { slug, server } = useServerContext();
  const toast = useToast();
  const navigate = useNavigate();

  const prefix = slug ? `/server/${slug}` : "";
  const links = me
    ? [
        { to: `${prefix}/account`, key: "nav.player.account" },
        { to: `${prefix}/servers`, key: "nav.player.servers" },
        { to: `${prefix}/extensions`, key: "nav.player.extensions" },
        { to: `${prefix}/security`, key: "nav.player.security" },
      ]
    : [];

  async function handleLogout() {
    try {
      await logout();
      navigate(slug ? `/server/${slug}/login` : "/login", { replace: true });
    } catch {
      toast.danger(t("common.unknown"));
    }
  }

  const isAuthed = !!me;
  const ctxColor = server?.primary_color;
  const initial = me?.player.raw_name?.[0]?.toUpperCase() ?? "?";

  // When unauthenticated, the page-level auth screens render their own .pauth-top
  // chrome (brand + theme toggle), so the shell suppresses its pnav to avoid a
  // duplicate header. The handoff design uses one or the other, never both.
  if (!isAuthed) {
    return (
      <div className="player-app" style={ctxColor ? ({ ["--ctx-primary" as string]: ctxColor } as React.CSSProperties) : undefined}>
        <main className="player-main">
          <Outlet />
        </main>
      </div>
    );
  }

  return (
    <div className="player-app" style={ctxColor ? ({ ["--ctx-primary" as string]: ctxColor } as React.CSSProperties) : undefined}>
      <header className="pnav" data-testid="player-header">
        <div className="pnav-inner">
          <Link
            to={slug ? `/server/${slug}` : "/"}
            className="pnav-brand"
            style={{ textDecoration: "none", color: "var(--color-text)" }}
          >
            <BrandMark sub={server ? undefined : undefined} markOnly={!!server} />
            {server ? <span className="brand-name">{server.display_name}</span> : null}
            {server ? (
              <span className="pnav-ctx" data-testid="server-context-banner">
                <span className="ctx-dot" style={{ background: ctxColor ?? "var(--color-primary)" }} />
                {t("common.server")}
              </span>
            ) : null}
          </Link>
          {isAuthed ? (
            <nav className="pnav-links">
              {links.map((l) => (
                <NavLink
                  key={l.to}
                  to={l.to}
                  data-testid={`nav-${l.key.split(".").pop()}`}
                  className={({ isActive }) => cx("pnav-link", isActive && "is-active")}
                >
                  {t(l.key)}
                </NavLink>
              ))}
            </nav>
          ) : (
            <div style={{ flex: 1 }} />
          )}
          <div className="pnav-right">
            <LocaleSelect />
            <ThemeToggle />
            {isAuthed ? (
              <div className="pnav-acct">
                <span className="pa-avatar pa-offline" style={{ width: 28, height: 28 }}>
                  {initial}
                </span>
                <IconButton
                  name="logout"
                  size={15}
                  label={t("common.signOut")}
                  onClick={handleLogout}
                  data-testid="logout-button"
                />
              </div>
            ) : null}
          </div>
        </div>
      </header>
      <main className="player-main">
        <Outlet />
      </main>
    </div>
  );
}
