import { useState } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import {
  BrandMark,
  IconButton,
  Icon,
  ThemeToggle,
  cx,
  useI18n,
  useToast,
} from "@authman/shared";
import { useSession } from "../auth/SessionContext";

const NAV: Array<{ to: string; key: string; icon: string; permission?: string }> = [
  { to: "/", key: "nav.admin.overview", icon: "gauge", permission: "admin.overview.read" },
  { to: "/players", key: "nav.admin.players", icon: "users", permission: "players.read" },
  { to: "/nodes", key: "nav.admin.nodes", icon: "server", permission: "nodes.read" },
  { to: "/mojang", key: "nav.admin.mojang", icon: "activity", permission: "mojang.read" },
  { to: "/servers", key: "nav.admin.servers", icon: "layers", permission: "servers.read" },
  { to: "/extensions", key: "nav.admin.extensions", icon: "box", permission: "extensions.read" },
  { to: "/audit", key: "nav.admin.audit", icon: "list", permission: "audit.read" },
  { to: "/settings", key: "nav.admin.settings", icon: "settings", permission: "settings.read" },
];

function titleFor(pathname: string, t: (k: string) => string): string {
  if (pathname.startsWith("/players/")) return t("admin.player.identity");
  if (pathname.startsWith("/servers/")) return t("admin.servers.heading");
  for (const item of NAV) {
    if (item.to === "/" ? pathname === "/" : pathname.startsWith(item.to)) return t(item.key);
  }
  return t("app.admin.title");
}

export function AdminShell() {
  const { t, locale, setLocale } = useI18n();
  const { user, hasPermission, logout } = useSession();
  const [collapsed, setCollapsed] = useState(false);
  const toast = useToast();
  const navigate = useNavigate();
  const location = useLocation();
  const items = NAV.filter((n) => !n.permission || hasPermission(n.permission));
  const title = titleFor(location.pathname, t);

  async function handleLogout() {
    try {
      await logout();
      navigate("/login", { replace: true });
    } catch {
      toast.danger(t("common.unknown"));
    }
  }

  const avatarInitial = user?.display_name?.[0]?.toUpperCase() ?? "?";

  return (
    <div className={cx("admin-shell", collapsed && "is-collapsed")}>
      <aside className="sidebar" data-testid="admin-sidebar">
        <div className="sidebar-brand">
          {collapsed ? <BrandMark markOnly /> : <BrandMark sub={t("brand.adminSub")} />}
        </div>
        <nav className="sidebar-nav">
          {items.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === "/"}
              data-testid={`nav-${item.key.split(".").pop()}`}
              className={({ isActive }) => cx("nav-item", isActive && "is-active")}
              title={collapsed ? t(item.key) : undefined}
            >
              <Icon name={item.icon} size={18} />
              {collapsed ? null : <span>{t(item.key)}</span>}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-foot">
          <button
            type="button"
            className="nav-item"
            onClick={() => setCollapsed((c) => !c)}
            data-testid="sidebar-toggle"
          >
            <Icon name={collapsed ? "chevronRight" : "chevronLeft"} size={18} />
            {collapsed ? null : <span>{t("nav.admin.collapse")}</span>}
          </button>
        </div>
      </aside>
      <div className="admin-main">
        <header className="topbar">
          <div className="topbar-title">
            <h1>{title}</h1>
          </div>
          <div className="topbar-actions">
            <span className="env-badge">
              <span className="env-dot" /> {(import.meta.env.MODE ?? "dev") === "production" ? "prod" : "dev"}
            </span>
            <button
              type="button"
              className="iconbtn iconbtn--bordered"
              onClick={() => setLocale(locale === "en" ? "zh" : "en")}
              data-testid="locale-toggle"
              aria-label={t("common.locale.toggle")}
              title={t("common.locale.toggle")}
              style={{ width: "auto", padding: "0 10px", fontSize: 12, fontWeight: 540 }}
            >
              {locale.toUpperCase()}
            </button>
            <ThemeToggle />
            <div className="acct-menu">
              <div className="acct-avatar">{avatarInitial}</div>
              <div className="acct-info">
                <span className="acct-name">{user?.display_name ?? "—"}</span>
                <span className="acct-role">{user?.role ? t(`admin.settings.role.${user.role}`, user.role) : ""}</span>
              </div>
              <IconButton
                name="logout"
                size={16}
                label={t("common.signOut")}
                onClick={handleLogout}
                data-testid="logout-button"
              />
            </div>
          </div>
        </header>
        <main className="admin-content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

// PageHeader / PageShell live in @authman/shared/framework now. The legacy
// re-export below keeps existing page imports compiling so a refactor can
// roll out gradually; new pages should import directly from @authman/shared.
export { PageHeader, PageShell } from "@authman/shared";
