import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import {
  Badge,
  Card,
  DataTable,
  EmptyState,
  Icon,
  useI18n,
  type DataColumn,
} from "@authman/shared";
import { fetchDownstreamServers, type DownstreamServer } from "../api/admin";
import { PageHeader } from "../layout/AdminShell";
import { ErrorBlock } from "../components/ErrorBlock";

function RegBadge({ open }: { open: boolean }) {
  const { t } = useI18n();
  return open ? (
    <Badge tone="success" dot>{t("common.open")}</Badge>
  ) : (
    <Badge tone="neutral" dot>{t("common.closed")}</Badge>
  );
}

export function ServersPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const q = useQuery({ queryKey: ["admin.servers"], queryFn: fetchDownstreamServers });

  const columns: DataColumn<DownstreamServer>[] = [
    {
      key: "name",
      header: t("admin.servers.col.name"),
      render: (s) => (
        <div className="node-name">
          <span
            className="ctx-badge"
            style={{ background: s.portal_theme.primary_color ?? "var(--color-primary)", width: 26, height: 26, borderRadius: 6, fontSize: 12 }}
          >
            {s.display_name[0]}
          </span>
          {s.display_name}
        </div>
      ),
    },
    {
      key: "slug",
      header: t("admin.servers.col.slug"),
      render: (s) => <code className="mono" style={{ color: "var(--color-text-muted)", fontSize: 12.5 }}>{s.slug}</code>,
    },
    {
      key: "registration",
      header: t("admin.servers.col.registration"),
      render: (s) => <RegBadge open={s.registration_open} />,
    },
    {
      key: "extension",
      header: t("admin.servers.col.extension"),
      render: (s) => (s.extension_providers.length ? <Badge tone="info">{t("common.available")}</Badge> : <span className="muted-cell">—</span>),
    },
    {
      key: "open",
      header: "",
      align: "right",
      render: () => <Icon name="chevronRight" size={16} style={{ color: "var(--color-text-subtle)" }} />,
    },
  ];

  return (
    <div className="page">
      <PageHeader
        title={t("admin.servers.heading")}
        desc={t("admin.servers.desc")}
      />
      {q.error ? <ErrorBlock error={q.error} onRetry={() => q.refetch()} /> : null}
      <Card noBody className="table-card">
        <DataTable
          loading={q.isLoading}
          rows={q.data ?? []}
          columns={columns}
          rowKey={(r) => r.id}
          onRowClick={(r) => navigate(`/servers/${r.id}`)}
          empty={<EmptyState icon="layers" title={t("admin.servers.empty")} />}
          testId="servers-table"
        />
      </Card>
    </div>
  );
}
