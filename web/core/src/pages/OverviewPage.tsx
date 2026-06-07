import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorState,
  HealthBanner,
  PageHeader,
  PageShell,
  Skeleton,
  StatCard,
  StatGrid,
  asArray,
  asNumber,
  asString,
  coerceAuditEvent,
  cx,
  formatRelativeTime,
  isObject,
  mapEnum,
  useI18n,
  type HealthTone,
  type SafeAuditEvent,
} from "@authman/shared";
import { fetchOverview } from "../api/admin";

type OverviewState = "healthy" | "partial" | "critical" | "empty";

const STATE_TO_TONE: Record<OverviewState, HealthTone> = {
  healthy: "success",
  partial: "warning",
  critical: "danger",
  empty: "neutral",
};

interface SafeOverview {
  total_players: number;
  premium_players: number;
  offline_players: number;
  recent_offline_login_failures: number;
  active_nodes: number;
  mojang_status: OverviewState;
  audit_events: SafeAuditEvent[];
}

function coerceOverview(raw: unknown): SafeOverview {
  const r = isObject(raw) ? raw : {};
  return {
    total_players: asNumber(r.total_players, 0),
    premium_players: asNumber(r.premium_players, 0),
    offline_players: asNumber(r.offline_players, 0),
    recent_offline_login_failures: asNumber(r.recent_offline_login_failures, 0),
    active_nodes: asNumber(r.active_nodes, 0),
    mojang_status: mapEnum<OverviewState>(asString(r.mojang_status, "empty"), ["healthy", "partial", "critical", "empty"], "empty"),
    audit_events: asArray(r.audit_events, coerceAuditEvent),
  };
}

export function OverviewPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const q = useQuery({
    queryKey: ["admin.overview"],
    queryFn: fetchOverview,
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  });

  const overview = q.data ? coerceOverview(q.data) : null;
  const state: OverviewState = overview?.mojang_status ?? "empty";
  const tone = STATE_TO_TONE[state];
  const mojangBadge =
    state === "healthy" ? <Badge tone="success" dot>{t("admin.overview.health.healthy")}</Badge>
    : state === "partial" ? <Badge tone="warning" dot>{t("admin.overview.health.degraded")}</Badge>
    : state === "critical" ? <Badge tone="danger" dot>{t("admin.overview.health.unavailable")}</Badge>
    : <Badge tone="neutral" dot>{t("admin.overview.health.empty")}</Badge>;

  return (
    <PageShell>
      <PageHeader
        title={t("admin.overview.heading")}
        desc={t("admin.overview.desc")}
      />
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      {overview && state !== "empty" ? (
        <HealthBanner
          tone={tone}
          title={t(`admin.overview.health.${state}`, "Mojang upstream healthy")}
          desc={t(`admin.overview.state.${state}.desc`)}
        />
      ) : null}

      <StatGrid>
        <StatCard icon="users" label={t("admin.overview.totalPlayers")} value={overview?.total_players} loading={q.isLoading} />
        <StatCard icon="shield" label={t("admin.overview.premium")} value={overview?.premium_players} tone="info" loading={q.isLoading} />
        <StatCard icon="user" label={t("admin.overview.offline")} value={overview?.offline_players} loading={q.isLoading} />
        <StatCard
          icon="alert"
          label={t("admin.overview.failures")}
          value={overview?.recent_offline_login_failures}
          sub={t("admin.overview.last24h")}
          tone={(overview?.recent_offline_login_failures ?? 0) > 0 ? "warning" : "neutral"}
          loading={q.isLoading}
        />
        <StatCard icon="server" label={t("admin.overview.nodes")} value={overview?.active_nodes} tone="success" loading={q.isLoading} />
        <StatCard
          icon="activity"
          label={t("admin.overview.mojang")}
          value={mojangBadge}
          tone={tone === "neutral" ? "neutral" : tone === "success" ? "success" : tone === "warning" ? "warning" : "danger"}
        />
      </StatGrid>

      <Card
        testId="overview-audit-card"
        title={t("admin.overview.audit")}
        actions={
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigate("/audit")}
            data-testid="overview-audit-viewAll"
          >
            {t("admin.overview.viewAll")}
          </Button>
        }
        noBody
      >
        {q.isLoading ? (
          <div style={{ padding: 16 }}>
            <Skeleton height={16} />
            <div style={{ height: 8 }} />
            <Skeleton height={16} />
          </div>
        ) : !overview || overview.audit_events.length === 0 ? (
          <EmptyState icon="list" title={t("common.empty")} testId="overview-audit-empty" />
        ) : (
          <ul className="mini-audit">
            {overview.audit_events.slice(0, 6).map((ev) => (
              <li key={ev.id || `${ev.event_type}-${ev.created_at}`} className="mini-audit-item" data-testid={`overview-audit-${ev.id || ev.event_type}`}>
                <span className={cx("actor-tag", `actor-tag--${ev.actor_type}`)}>{t(`admin.audit.actor.${ev.actor_type}`, ev.actor_type)}</span>
                <code className="mono mini-audit-event">{ev.event_type}</code>
                <span className="mini-audit-target">{ev.target_label}</span>
                <span className="mini-audit-time">{formatRelativeTime(ev.created_at)}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </PageShell>
  );
}
