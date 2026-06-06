import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Badge, Card, EmptyState, Icon, useI18n } from "@authman/shared";
import { portalServers } from "../api/portal";
import { ErrorBlock } from "../components/ErrorBlock";

export function ServersPage() {
  const { t } = useI18n();
  const q = useQuery({ queryKey: ["portal.servers"], queryFn: portalServers });

  return (
    <div className="pcontent">
      <div className="pcontent-head">
        <h1>{t("servers.heading")}</h1>
        <p>{t("servers.desc")}</p>
      </div>
      <Card testId="public-servers">
        {q.error ? <ErrorBlock error={q.error} onRetry={() => q.refetch()} /> : null}
        {q.isLoading ? (
          <div>{t("common.loading")}</div>
        ) : (q.data ?? []).length === 0 ? (
          <EmptyState icon="layers" title={t("servers.empty")} testId="servers-empty" />
        ) : (
          <div className="conn-servers">
            {(q.data ?? []).map((s) => (
              <Link
                key={s.slug}
                to={`/server/${s.slug}`}
                className="conn-server"
                style={{ textDecoration: "none", color: "inherit" }}
                data-testid={`server-${s.slug}`}
              >
                <span className="ctx-badge" style={{ background: s.primary_color ?? "var(--color-primary)" }}>
                  {s.display_name[0]}
                </span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="conn-name">{s.display_name}</div>
                  {s.description ? <div className="muted-cell" style={{ fontSize: 12 }}>{s.description}</div> : null}
                </div>
                <Badge tone={s.registration_open ? "success" : "neutral"} dot>
                  {s.registration_open ? t("servers.registrationOpen") : t("servers.registrationClosed")}
                </Badge>
                <Icon name="chevronRight" size={16} style={{ color: "var(--color-text-subtle)" }} />
              </Link>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}
