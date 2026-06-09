import { useNavigate } from "react-router-dom";
import { PageHeader, PageShell, Tabs, useI18n } from "@authman/shared";
import { LimboBlueprintsPage } from "./LimboBlueprintsPage";
import { NodesPage } from "./NodesPage";
import { PortalSettingsPage } from "./PortalSettingsPage";

type LoginPortalTab = "instances" | "blueprints" | "settings";

interface LoginPortalsPageProps {
  tab: LoginPortalTab;
}

export function LoginPortalsPage({ tab }: LoginPortalsPageProps) {
  const { t } = useI18n();
  const navigate = useNavigate();

  return (
    <PageShell testId="login-portals-page">
      <PageHeader title={t("admin.loginPortals.heading")} desc={t("admin.loginPortals.workspace.desc")} />
      <Tabs<LoginPortalTab>
        value={tab}
        onChange={(next) => navigate(next === "instances" ? "/login-portals" : `/login-portals/${next}`)}
        tabs={[
          { value: "instances", label: t("admin.loginPortals.tab.instances"), icon: "layers" },
          { value: "blueprints", label: t("admin.loginPortals.tab.blueprints"), icon: "box" },
          { value: "settings", label: t("admin.loginPortals.tab.settings"), icon: "settings" },
        ]}
      />
      <div className="tab-panel">
        {tab === "instances" ? <NodesPage kind="limbo_portal" embedded /> : null}
        {tab === "blueprints" ? <LimboBlueprintsPage embedded basePath="/login-portals/blueprints" /> : null}
        {tab === "settings" ? <PortalSettingsPage embedded /> : null}
      </div>
    </PageShell>
  );
}
