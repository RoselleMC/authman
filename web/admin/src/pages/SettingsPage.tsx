import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Button,
  Card,
  ConfigGrid,
  ConfigRow,
  DataTable,
  EmptyState,
  ErrorState,
  Icon,
  LoadingState,
  PageHeader,
  PageShell,
  PlaceholderCard,
  PlaceholderGrid,
  SettingsStack,
  coerceAdminUser,
  coerceSystemSummary,
  formatAbsTime,
  useI18n,
  type DataColumn,
  type SafeAdminUser,
} from "@authman/shared";
import { fetchAdminUsers, fetchSystemSummary } from "../api/admin";
import { useSession } from "../auth/SessionContext";

const ROLE_TONE: Record<SafeAdminUser["role"], "info" | "success" | "neutral"> = {
  owner: "info",
  admin: "success",
  auditor: "neutral",
};

export function SettingsPage() {
  const { t } = useI18n();
  const { hasPermission } = useSession();
  const usersQ = useQuery({ queryKey: ["admin.users"], queryFn: fetchAdminUsers });
  const sysQ = useQuery({ queryKey: ["admin.system"], queryFn: fetchSystemSummary });

  const adminUsers: SafeAdminUser[] = (usersQ.data ?? []).map(coerceAdminUser);
  const columns: DataColumn<SafeAdminUser>[] = [
    {
      key: "name",
      header: t("admin.settings.col.admin"),
      render: (u) => (
        <div className="admin-user">
          <span className="acct-avatar" style={{ width: 30, height: 30 }}>
            {u.display_name[0]?.toUpperCase() ?? "?"}
          </span>
          <div>
            <div className="admin-user-name">{u.display_name}</div>
            <code className="mono admin-user-email">{u.email}</code>
          </div>
        </div>
      ),
    },
    { key: "role", header: t("admin.settings.col.role"), render: (u) => <Badge tone={ROLE_TONE[u.role]}>{t(`admin.settings.role.${u.role}`, u.role)}</Badge> },
    {
      key: "status",
      header: t("admin.settings.col.status"),
      render: (u) =>
        u.status === "active" ? (
          <Badge tone="success" dot>
            {t("status.active")}
          </Badge>
        ) : (
          <Badge tone="neutral" dot>
            {t("status.disabled")}
          </Badge>
        ),
    },
    { key: "created", header: t("admin.settings.col.created"), render: (u) => (u.created_at ? formatAbsTime(u.created_at) : "—") },
  ];

  return (
    <PageShell>
      <PageHeader
        title={t("admin.settings.heading")}
        desc={t("admin.settings.desc")}
      />
      <SettingsStack>
        <Card
          title={t("admin.settings.admins")}
          noBody
          actions={
            <Button
              variant="secondary"
              size="sm"
              icon="plus"
              disabled={!hasPermission("admin_users.write")}
              title={hasPermission("admin_users.write") ? undefined : t("admin.settings.readOnly")}
            >
              {t("admin.settings.addAdmin")}
            </Button>
          }
        >
          {usersQ.error ? (
            <ErrorState error={usersQ.error} onRetry={() => usersQ.refetch()} />
          ) : (
            <DataTable
              loading={usersQ.isLoading}
              rows={adminUsers}
              columns={columns}
              rowKey={(r) => r.id}
              empty={<EmptyState icon="users" title={t("admin.settings.emptyAdmins")} />}
              testId="admin-users-table"
            />
          )}
          <div className="card-foot-note">
            <Icon name="info" size={13} /> {t("admin.settings.footnote")}
          </div>
        </Card>

        <Card title={t("admin.settings.system")}>
          {sysQ.error ? (
            <ErrorState error={sysQ.error} onRetry={() => sysQ.refetch()} />
          ) : sysQ.isLoading || !sysQ.data ? (
            <LoadingState />
          ) : (
            <SystemSummaryGrid raw={sysQ.data} />
          )}
        </Card>

        <PlaceholderGrid>
          <PlaceholderCard
            icon="external"
            title={t("admin.settings.smtp")}
            desc={t("admin.settings.smtp.placeholder")}
            testId="placeholder-smtp"
          />
          <PlaceholderCard
            icon="key"
            title={t("admin.settings.2fa")}
            desc={t("admin.settings.2fa.placeholder")}
            testId="placeholder-2fa"
          />
        </PlaceholderGrid>
      </SettingsStack>
    </PageShell>
  );
}

function SystemSummaryGrid({ raw }: { raw: unknown }) {
  const { t } = useI18n();
  const sys = coerceSystemSummary(raw);
  const flagsLabel =
    Object.keys(sys.feature_flags).length === 0
      ? "—"
      : Object.entries(sys.feature_flags)
          .map(([k, v]) => `${k}: ${v}`)
          .join(", ");
  return (
    <ConfigGrid testId="system-summary">
      <ConfigRow k={t("admin.settings.system.version")} v={sys.version} mono />
      <ConfigRow k={t("admin.settings.system.environment")} v={sys.environment} mono />
      <ConfigRow k={t("admin.settings.system.database")} v={sys.database} ok={sys.database !== "unknown"} />
      <ConfigRow k={t("admin.settings.system.uptime")} v={sys.uptime_seconds == null ? "—" : `${Math.round(sys.uptime_seconds / 60)} ${t("common.minutesShort")}`} />
      <ConfigRow k={t("admin.settings.system.featureFlags")} v={flagsLabel} />
      {sys.extra_rows.map((row) => (
        <ConfigRow key={row.k} k={row.k} v={row.v} />
      ))}
    </ConfigGrid>
  );
}
