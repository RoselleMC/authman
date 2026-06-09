import { useState } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import {
  AccountMenu,
  BrandMark,
  Icon,
  LocaleSelect,
  ThemeToggle,
  cx,
  useI18n,
  useToast,
} from "@authman/shared";
import { useSession } from "../auth/SessionContext";

const NAV: Array<{ to: string; key: string; icon: string; permission?: string }> = [
  { to: "/", key: "nav.admin.overview", icon: "gauge", permission: "admin.overview.read" },
  { to: "/passports", key: "nav.admin.passports", icon: "key", permission: "players.read" },
  { to: "/profiles", key: "nav.admin.profiles", icon: "users", permission: "players.read" },
  { to: "/login-portals", key: "nav.admin.loginPortals", icon: "layers", permission: "nodes.read" },
  { to: "/nodes", key: "nav.admin.nodes", icon: "server", permission: "servers.read" },
  { to: "/proxies", key: "nav.admin.proxies", icon: "activity", permission: "mojang.read" },
  { to: "/audit", key: "nav.admin.audit", icon: "list", permission: "audit.read" },
  { to: "/settings", key: "nav.admin.settings", icon: "settings", permission: "settings.read" },
];

function titleFor(pathname: string, t: (k: string) => string): string {
  if (pathname.startsWith("/passports/")) return t("admin.passports.identity");
  if (pathname.startsWith("/profiles/")) return t("admin.profiles.identity");
  if (pathname.startsWith("/audit/")) return t("admin.audit.detail");
  if (pathname.startsWith("/login-portals")) return t("nav.admin.loginPortals");
  if (pathname.startsWith("/nodes/")) return t("nav.admin.nodes");
  if (pathname.startsWith("/servers/")) return t("nav.admin.nodes");
  for (const item of NAV) {
    if (item.to === "/" ? pathname === "/" : pathname.startsWith(item.to)) return t(item.key);
  }
  return t("app.admin.title");
}

export function CoreShell() {
  const { t } = useI18n();
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
            <LocaleSelect />
            <ThemeToggle />
            <AccountMenu
              name={user?.username ?? "—"}
              secondary={user?.email || undefined}
              avatarUrl={user?.avatar_url || undefined}
              badge={user?.role ? t(`admin.settings.role.${user.role}`, user.role) : undefined}
              primaryActionLabel={t("account.settings")}
              primaryActionIcon="user"
              onPrimaryAction={() => navigate("/settings/account")}
              onSignOut={handleLogout}
            />
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
