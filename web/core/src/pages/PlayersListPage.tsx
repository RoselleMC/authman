import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  AdvancedList,
  Card,
  Copyable,
  EmptyState,
  ErrorState,
  Icon,
  PageHeader,
  PageShell,
  ProtocolName,
  StatusBadge,
  TypeBadge,
  cx,
  formatRelativeTime,
  useI18n,
  useListState,
  type ListColumn,
} from "@authman/shared";
import { fetchPlayers, type PlayerRow, type PlayerListFilters } from "../api/admin";

const PAGE_SIZE_OPTIONS = [10, 25, 50, 100] as const;

/**
 * Players list — fully driven by the AdvancedList primitive:
 *   - column visibility (UUID + protocol hidden by default to keep the
 *     table scannable on smaller screens)
 *   - per-column filters mapped onto the backend's `q`, `kind`, `status` params
 *   - server-backed pagination via `page` + `page_size`
 *   - URL sync under the `p.*` prefix so list state is share-able / bookmark-able
 */
export function PlayersListPage() {
  const { t } = useI18n();
  const navigate = useNavigate();

  const list = useListState({
    urlPrefix: "p",
    defaults: { pageSize: 25, hidden: ["uuid", "protocol"] },
  });

  // Map the framework's neutral filter values onto the backend's API shape.
  // The Players API supports `q` (substring across name+protocol+UUID),
  // `kind`, and `status` along with `page` + `page_size`.
  const apiFilters = useMemo<PlayerListFilters>(() => {
    const f: PlayerListFilters = {
      page: list.state.page,
      page_size: list.state.pageSize,
    };
    const q = (list.state.filters.name ?? "").trim();
    if (q) f.q = q;
    const kind = list.state.filters.kind;
    if (kind === "premium" || kind === "offline") f.kind = kind;
    const status = list.state.filters.status;
    if (status === "active" || status === "locked" || status === "pending_verification") f.status = status;
    return f;
  }, [list.state]);

  const q = useQuery({
    queryKey: ["admin.players", apiFilters],
    queryFn: ({ signal }) => fetchPlayers(apiFilters, signal),
  });

  const columns: ListColumn<PlayerRow>[] = [
    {
      key: "name",
      header: t("admin.players.col.name"),
      mandatory: true,
      filter: { type: "text", placeholder: t("admin.players.searchPlaceholder") },
      render: (r) => (
        <div className="player-cell">
          <span className={cx("pa-avatar", r.kind === "premium" ? "pa-premium" : "pa-offline")}>{r.raw_name[0]}</span>
          <span className="player-name">{r.raw_name}</span>
        </div>
      ),
    },
    {
      key: "protocol",
      header: t("admin.players.col.protocol"),
      defaultVisible: false,
      render: (r) => <ProtocolName rawName={r.raw_name} kind={r.kind} />,
    },
    {
      key: "uuid",
      header: t("admin.players.col.uuid"),
      defaultVisible: false,
      render: (r) => <Copyable value={r.uuid} truncate={13} />,
    },
    {
      key: "kind",
      header: t("admin.players.col.type"),
      filter: {
        type: "select",
        options: [
          { value: "", label: t("admin.players.filter.all") },
          { value: "premium", label: t("admin.players.filter.premium") },
          { value: "offline", label: t("admin.players.filter.offline") },
        ],
      },
      render: (r) => <TypeBadge kind={r.kind} />,
    },
    {
      key: "status",
      header: t("admin.players.col.status"),
      filter: {
        type: "select",
        options: [
          { value: "", label: t("admin.players.filter.all") },
          { value: "active", label: t("status.active") },
          { value: "locked", label: t("status.locked") },
          { value: "pending_verification", label: t("status.pending") },
        ],
      },
      render: (r) => <StatusBadge status={r.status} />,
    },
    {
      key: "lastSeen",
      header: t("admin.players.col.lastSeen"),
      render: (r) => <span className="muted-cell">{formatRelativeTime(r.last_seen_at)}</span>,
    },
    {
      key: "lastSeenServer",
      header: t("admin.players.col.lastSeenServer"),
      render: (r) => <span className="muted-cell">{r.last_seen_server_label ?? "—"}</span>,
    },
    {
      key: "open",
      header: "",
      mandatory: true,
      width: "32px",
      align: "right",
      render: () => <Icon name="chevronRight" size={16} style={{ color: "var(--color-text-subtle)" }} />,
    },
  ];

  const totalRaw = (q.data?.meta as { total?: number } | undefined)?.total ?? q.data?.rows.length ?? 0;

  return (
    <PageShell>
      <PageHeader title={t("admin.players.heading")} />
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      <Card noBody className="table-card">
        <AdvancedList
          columns={columns}
          rowKey={(r) => r.id}
          mode="server"
          rows={q.data?.rows ?? []}
          total={totalRaw}
          loading={q.isLoading}
          state={list.state}
          onStateChange={list.setState}
          pageSizeOptions={PAGE_SIZE_OPTIONS}
          onRowClick={(r) => navigate(`/players/${r.id}`)}
          empty={
            <EmptyState
              icon="users"
              title={t("admin.players.empty")}
              description={t("admin.players.empty.desc")}
              testId="players-empty"
            />
          }
          testId="players"
        />
      </Card>
    </PageShell>
  );
}
