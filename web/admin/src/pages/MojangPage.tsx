import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Button,
  Card,
  DataTable,
  EmptyState,
  ErrorState,
  HealthBanner,
  Icon,
  PageHeader,
  PageShell,
  coerceMojangStatus,
  cx,
  formatRelativeTime,
  useI18n,
  type DataColumn,
  type SafeMojangProxy,
} from "@authman/shared";
import { fetchMojang } from "../api/admin";

function StateBadge({ state }: { state: string }) {
  const { t } = useI18n();
  const tone: "success" | "danger" | "neutral" | "warning" = state === "healthy" ? "success" : state === "failed" ? "danger" : state === "disabled" ? "neutral" : "warning";
  const key = state === "cooling_down" ? "cooldown" : state;
  return <Badge tone={tone} dot>{t(`admin.mojang.state.${key}`, state)}</Badge>;
}

const PROXY_TYPE: Record<Exclude<SafeMojangProxy["kind"], "direct">, string> = { http: "HTTP", socks5: "SOCKS5" };

const EVENT_TONE: Record<string, "success" | "warning" | "danger"> = {
  rate_limited: "warning",
  timeout: "warning",
  network_error: "danger",
  bad_status: "danger",
  recovered: "success",
};

export function MojangPage() {
  const { t } = useI18n();
  const q = useQuery({
    queryKey: ["admin.mojang"],
    queryFn: fetchMojang,
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  });

  const status = q.data ? coerceMojangStatus(q.data) : null;
  const overall = status?.overall;
  const overallText = overall
    ? {
        title: t(`admin.mojang.overall.${overall.key}.title`, t("admin.mojang.overall.unknown.title")),
        desc: t(`admin.mojang.overall.${overall.key}.desc`, t("admin.mojang.overall.unknown.desc").replace("{state}", overall.key)),
      }
    : null;
  const columns: DataColumn<SafeMojangProxy>[] = [
    {
      key: "route",
      header: t("admin.mojang.col.route"),
      render: (p) => (
        <div className="proxy-route">
          <code className="mono" style={{ fontWeight: 560 }}>{p.id}</code>
          {p.url_masked ? <code className="mono proxy-masked">{p.url_masked}</code> : null}
        </div>
      ),
    },
    { key: "kind", header: t("admin.mojang.col.type"), render: (p) => <span className="type-pill">{p.kind === "direct" ? t("admin.mojang.route.direct") : PROXY_TYPE[p.kind]}</span> },
    { key: "state", header: t("admin.mojang.col.state"), render: (p) => <StateBadge state={p.state} /> },
    {
      key: "count",
      header: t("admin.mojang.col.requests"),
      align: "right",
      render: (p) => <span className="mono">{p.request_count.toLocaleString()}</span>,
    },
    {
      key: "cooldown",
      header: t("admin.mojang.col.cooldown"),
      render: (p) =>
        p.cooldown_remaining_seconds > 0 ? (
          <span className="cooldown-pill">
            <Icon name="clock" size={12} />
            {p.cooldown_remaining_seconds}s
          </span>
        ) : (
          <span className="muted-cell">—</span>
        ),
    },
  ];

  return (
    <PageShell>
      <PageHeader
        title={t("admin.mojang.heading")}
        desc={t("admin.mojang.desc")}
        action={
          <Button variant="secondary" icon="refresh" loading={q.isLoading} onClick={() => q.refetch()} data-testid="mojang-refresh">
            {t("common.refresh")}
          </Button>
        }
      />
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}

      {overall && overallText ? (
        <HealthBanner
          tone={overall.tone}
          title={overallText.title}
          desc={overallText.desc}
          testId="mojang-overall"
        />
      ) : null}

      <Card title={t("admin.mojang.proxies")} noBody className="table-card">
        <DataTable
          loading={q.isLoading}
          rows={status?.proxies ?? []}
          columns={columns}
          rowKey={(r) => r.id}
          empty={<EmptyState icon="activity" title={t("admin.mojang.empty.proxies")} />}
          testId="mojang-proxies"
        />
        <div className="card-foot-note">
          <Icon name="lock" size={13} /> {t("admin.mojang.footnote")}
        </div>
      </Card>

      <Card title={t("admin.mojang.events")} noBody>
        {(status?.events ?? []).length === 0 ? (
          <EmptyState icon="list" title={t("common.empty")} />
        ) : (
          <ul className="event-list">
            {(status?.events ?? []).slice(0, 12).map((ev) => {
              const tone = EVENT_TONE[ev.event_type] ?? "warning";
              return (
                <li key={ev.id || `${ev.proxy_id}-${ev.created_at}`} className="event-item">
                  <span className={cx("event-dot", `event-dot--${tone}`)} />
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div className="event-row">
                      <code className="mono event-type">mojang.{ev.event_type}</code>
                      <span className="event-proxy">{ev.proxy_id}</span>
                      {ev.status_code !== null ? (
                        <span className={cx("status-code", ev.status_code >= 400 ? "status-code--bad" : "status-code--ok")}>
                          {ev.status_code}
                        </span>
                      ) : null}
                      {ev.retry_after ? <span className="retry-after">{t("admin.mojang.retryAfter").replace("{value}", ev.retry_after)}</span> : null}
                    </div>
                    <div className="event-meta">{formatRelativeTime(ev.created_at)}</div>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </Card>
    </PageShell>
  );
}
