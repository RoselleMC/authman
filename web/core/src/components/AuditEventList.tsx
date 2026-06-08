import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  AdvancedList,
  EmptyState,
  Icon,
  IPLocation,
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
import { useSession } from "../auth/SessionContext";

const PAGE_SIZE_OPTIONS = [25, 50, 100] as const;

interface Props {
  baseFilters?: AuditFilters;
  filterable?: boolean;
  testId?: string;
  urlPrefix?: string;
}

export function AuditEventList({ baseFilters, filterable = true, testId = "audit", urlPrefix = "a" }: Props) {
  const { t } = useI18n();
  const { user } = useSession();
  const navigate = useNavigate();
  const list = useListState({ urlPrefix, defaults: { pageSize: 25 }, storageScope: user?.id });

  const apiFilters = useMemo<AuditFilters>(() => {
    const f: AuditFilters = {
      ...(baseFilters ?? {}),
      page: list.state.page,
      page_size: list.state.pageSize,
    };
    if (filterable) {
      const actor = list.state.filters.actor;
      const target = list.state.filters.target;
      const evt = (list.state.filters.event ?? "").trim();
      if (actor) f.actor_type = actor;
      if (target) f.target_type = target;
      if (evt) f.event_type = evt;
    }
    return f;
  }, [baseFilters, filterable, list.state]);

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
      minWidth: "230px",
      filter: filterable ? { type: "text", placeholder: t("admin.audit.event.placeholder") } : undefined,
      render: (ev) => (
        <div className="audit-event-cell">
          <span>{t(`audit.event.${ev.event_type}`, ev.event_type)}</span>
          <code className="mono event-type">{ev.event_type}</code>
          <span className="muted-cell">{auditEventHint(ev)}</span>
        </div>
      ),
    },
    {
      key: "actor",
      header: t("admin.audit.filter.actor"),
      minWidth: "220px",
      filter: filterable ? { type: "select", options: actorOptions } : undefined,
      render: (ev) => (
        <div className="audit-actor">
          <span className={cx("actor-tag", `actor-tag--${ev.actor_type}`)}>{t(`admin.audit.actor.${ev.actor_type}`, ev.actor_type)}</span>
          <span className="audit-actor-name mono" title={ev.actor_id || ev.actor_label}>{auditPrincipalLabel(ev.actor_label, ev.actor_id)}</span>
        </div>
      ),
    },
    {
      key: "target",
      header: t("admin.audit.filter.target"),
      minWidth: "240px",
      filter: filterable ? { type: "select", options: targetOptions } : undefined,
      render: (ev) => (
        <div className="audit-target-cell">
          <span className="target-type mono">{t(`admin.audit.target.${ev.target_type}`, ev.target_type)}</span>
          <span className="mono" title={ev.target_id || ev.target_label}>{auditPrincipalLabel(ev.target_label, ev.target_id)}</span>
        </div>
      ),
    },
    {
      key: "ip",
      header: t("admin.audit.col.ip"),
      minWidth: "220px",
      defaultVisible: true,
      render: (ev) => <IPLocation ip={ev.client_ip} geo={ev.client_geo} />,
    },
    {
      key: "details",
      header: t("admin.audit.col.details"),
      minWidth: "300px",
      defaultVisible: true,
      render: (ev) => <span className="audit-details-summary">{auditDetailsSummary(ev.details)}</span>,
    },
    {
      key: "time",
      header: t("admin.audit.col.time"),
      align: "right",
      minWidth: "160px",
      defaultVisible: true,
      render: (ev) => (
        <span className="muted-cell" title={formatAbsTime(ev.created_at)}>
          {formatRelativeTime(ev.created_at)}
        </span>
      ),
    },
    {
      key: "open",
      header: "",
      mandatory: true,
      width: "44px",
      minWidth: "44px",
      align: "right",
      sticky: "right",
      render: () => <Icon name="chevronRight" size={16} />,
    },
  ];

  return (
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
      onRowClick={(ev) => navigate(`/audit/${encodeURIComponent(ev.id)}`)}
      empty={<EmptyState icon="list" title={t("admin.audit.empty")} description={t("admin.audit.empty.desc")} testId={`${testId}-empty`} />}
      testId={testId}
    />
  );
}

function auditPrincipalLabel(label: string, id: string) {
  const value = label && label !== "—" ? label : id;
  if (!value) return "—";
  if (value.length <= 18) return value;
  return `${value.slice(0, 8)}...${value.slice(-6)}`;
}

function auditEventHint(ev: SafeAuditEvent) {
  const details = ev.details;
  const reason = stringValue(details.reason);
  const path = stringValue(details.path);
  const method = stringValue(details.method);
  if (reason) return reason;
  if (method || path) return [method, path].filter(Boolean).join(" ");
  return ev.target_type;
}

function auditDetailsSummary(details: Record<string, unknown>) {
  const keys = ["protocol_name", "username", "raw_offline_name", "server_id", "server_slug", "reason", "path", "client_ip"];
  const parts: string[] = [];
  for (const key of keys) {
    const value = stringValue(details[key]);
    if (value) parts.push(`${key}: ${value}`);
    if (parts.length >= 3) break;
  }
  return parts.length ? parts.join(" · ") : "—";
}

function stringValue(value: unknown) {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}
