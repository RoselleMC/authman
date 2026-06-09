import { useNavigate, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Card,
  Copyable,
  DefList,
  DefRow,
  ErrorState,
  Icon,
  IPLocation,
  PageShell,
  coerceAuditEvent,
  formatAbsTime,
  useI18n,
} from "@authman/shared";
import { fetchAuditEvent } from "../api/admin";

export function AuditDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { t } = useI18n();
  const q = useQuery({ queryKey: ["admin.audit.detail", id], queryFn: () => fetchAuditEvent(id), enabled: !!id });

  if (q.isLoading) return <div className="page"><Card>{t("common.loading")}</Card></div>;
  if (q.error || !q.data) return <div className="page"><ErrorState error={q.error} onRetry={() => q.refetch()} /></div>;

  const event = coerceAuditEvent(q.data);
  const details = event.details;
  const contextKeys = ["method", "path", "client_ip", "user_agent", "request_id"];
  const structuredContext = [
    ["category", event.category],
    ["outcome", event.outcome],
    ["source", event.source],
    ["session_id", event.session_id],
    ["correlation_id", event.correlation_id],
    ["schema_version", event.schema_version],
  ] as const;
  const structuredKeys = new Set<string>(structuredContext.map(([key]) => key));
  const detailEntries = Object.entries(details).filter(([key]) => !contextKeys.includes(key) && !structuredKeys.has(key) && key !== "client_geo");

  return (
    <PageShell>
      <button type="button" className="back-link" onClick={() => navigate("/audit")}>
        <Icon name="arrowLeft" size={15} />
        {t("admin.audit.heading")}
      </button>
      <div className="detail-body">
        <Card title={event.event_type}>
          <DefList>
            <DefRow k="ID"><Copyable value={event.id} /></DefRow>
            <DefRow k={t("admin.audit.col.time")}>{formatAbsTime(event.created_at)}</DefRow>
            <DefRow k={t("admin.audit.col.event")}>
              <span className="audit-event-cell">
                <span>{t(`audit.event.${event.event_type}`, event.event_type)}</span>
                <code className="mono event-type">{event.event_type}</code>
              </span>
            </DefRow>
            <DefRow k={t("admin.audit.filter.actor")}>
              <span className="audit-detail-principal">
                <span>{t(`admin.audit.actor.${event.actor_type}`, event.actor_type)}</span>
                <Copyable value={event.actor_id || event.actor_label} truncate={18} />
              </span>
            </DefRow>
            <DefRow k={t("admin.audit.filter.target")}>
              <span className="audit-detail-principal">
                <span>{t(`admin.audit.target.${event.target_type}`, event.target_type)}</span>
                <Copyable value={event.target_id || event.target_label} truncate={18} />
              </span>
            </DefRow>
          </DefList>
        </Card>
        <Card title={t("admin.audit.context")}>
          <DefList>
            {structuredContext.map(([key, value]) => (
              <DefRow key={key} k={t(`admin.audit.field.${key}`, key)}>{renderAuditValue(value)}</DefRow>
            ))}
            {contextKeys.map((key) => (
              <DefRow key={key} k={t(`admin.audit.field.${key}`, key)}>
                {key === "client_ip" ? <IPLocation ip={event.client_ip} geo={event.client_geo} /> : renderAuditValue(details[key])}
              </DefRow>
            ))}
          </DefList>
        </Card>
        <Card title={t("admin.audit.details")}>
          {detailEntries.length ? (
            <DefList>
              {detailEntries.map(([key, value]) => (
                <DefRow key={key} k={key}>{renderAuditValue(value)}</DefRow>
              ))}
            </DefList>
          ) : (
            <p className="muted-cell">{t("common.none")}</p>
          )}
        </Card>
      </div>
    </PageShell>
  );
}

function renderAuditValue(value: unknown) {
  if (value == null || value === "") return <span className="muted-cell">—</span>;
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return <Copyable value={String(value)} truncate={48} />;
  }
  return <pre className="json-block">{JSON.stringify(value, null, 2)}</pre>;
}
