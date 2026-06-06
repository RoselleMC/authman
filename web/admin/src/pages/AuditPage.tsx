import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AdvancedList,
  Card,
  EmptyState,
  ErrorState,
  PageHeader,
  PageShell,
  coerceAuditEvent,
  cx,
  formatAbsTime,
  formatRelativeTime,
  useI18n,
  useListState,
  type ListColumn,
  type SafeAuditEvent,
} from "@authman/shared";
import { fetchAuditEvents, type AuditFilters } from "../api/admin";

const PAGE_SIZE_OPTIONS = [25, 50, 100] as const;

export function AuditPage() {
  const { t } = useI18n();
  const list = useListState({ urlPrefix: "a", defaults: { pageSize: 25 } });

  // Backend supports actor_type, target_type, event_type substring +
  // page/page_size. Map AdvancedList state onto exactly those params.
  const apiFilters = useMemo<AuditFilters>(() => {
    const f: AuditFilters = { page: list.state.page, page_size: list.state.pageSize };
    const actor = list.state.filters.actor;
    const target = list.state.filters.target;
    const evt = (list.state.filters.event ?? "").trim();
    if (actor) f.actor_type = actor;
    if (target) f.target_type = target;
    if (evt) f.event_type = evt;
    return f;
  }, [list.state]);

  const q = useQuery({
    queryKey: ["admin.audit", apiFilters],
    queryFn: () => fetchAuditEvents(apiFilters),
  });

  const rows: SafeAuditEvent[] = useMemo(
    () => ((q.data?.data ?? []) as unknown[]).map(coerceAuditEvent),
    [q.data?.data],
  );
  const total = (q.data?.meta as { total?: number } | undefined)?.total ?? rows.length;
  const actorOptions = [
    { value: "", label: t("admin.players.filter.all") },
    { value: "admin", label: t("admin.audit.actor.admin") },
    { value: "node", label: t("admin.audit.actor.node") },
    { value: "player", label: t("admin.audit.actor.player") },
    { value: "system", label: t("admin.audit.actor.system") },
  ];
  const targetOptions = [
    { value: "", label: t("admin.players.filter.all") },
    { value: "player", label: t("admin.audit.target.player") },
    { value: "node", label: t("admin.audit.target.node") },
    { value: "downstream_server", label: t("admin.audit.target.downstream_server") },
    { value: "mojang_proxy", label: t("admin.audit.target.mojang_proxy") },
    { value: "portal_session", label: t("admin.audit.target.portal_session") },
    { value: "extension_data", label: t("admin.audit.target.extension_data") },
    { value: "system", label: t("admin.audit.target.system") },
  ];

  const columns: ListColumn<SafeAuditEvent>[] = [
    {
      key: "event",
      header: t("admin.audit.filter.event"),
      mandatory: true,
      filter: { type: "text", placeholder: t("admin.audit.event.placeholder") },
      render: (ev) => <code className="mono event-type">{ev.event_type}</code>,
    },
    {
      key: "actor",
      header: t("admin.audit.filter.actor"),
      filter: { type: "select", options: actorOptions },
      render: (ev) => (
        <div className="audit-actor">
          <span className={cx("actor-tag", `actor-tag--${ev.actor_type}`)}>{t(`admin.audit.actor.${ev.actor_type}`, ev.actor_type)}</span>
          <span className="audit-actor-name">{ev.actor_label}</span>
        </div>
      ),
    },
    {
      key: "target",
      header: t("admin.audit.filter.target"),
      filter: { type: "select", options: targetOptions },
      render: (ev) => (
        <div className="audit-target-cell">
          <span className="target-type mono">{t(`admin.audit.target.${ev.target_type}`, ev.target_type)}</span>
          <span>{ev.target_label}</span>
        </div>
      ),
    },
    {
      key: "time",
      header: t("admin.audit.col.time"),
      align: "right",
      defaultVisible: true,
      render: (ev) => (
        <span className="muted-cell" title={formatAbsTime(ev.created_at)}>
          {formatRelativeTime(ev.created_at)}
        </span>
      ),
    },
  ];

  return (
    <PageShell>
      <PageHeader title={t("admin.audit.heading")} desc={t("admin.audit.desc")} />
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      <Card noBody className="table-card">
        <AdvancedList
          columns={columns}
          rowKey={(ev) => ev.id || `${ev.event_type}-${ev.created_at}`}
          mode="server"
          rows={rows}
          total={total}
          loading={q.isLoading}
          state={list.state}
          onStateChange={list.setState}
          pageSizeOptions={PAGE_SIZE_OPTIONS}
          empty={
            <EmptyState
              icon="list"
              title={t("admin.audit.empty")}
              description={t("admin.audit.empty.desc")}
              testId="audit-empty"
            />
          }
          testId="audit"
        />
      </Card>
    </PageShell>
  );
}
