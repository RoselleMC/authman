import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AdvancedList,
  Button,
  Card,
  Copyable,
  Dialog,
  EmptyState,
  ErrorState,
  Icon,
  IPLocation,
  PageHeader,
  PageShell,
  StatusBadge,
  TypeBadge,
  formatRelativeTime,
  useI18n,
  useListState,
  useToast,
  type ListColumn,
} from "@authman/shared";
import { createPassportBan, fetchPassports, revokeBan, updatePassportStatus, type IdentityListFilters, type PassportRow } from "../api/admin";
import { useSession } from "../auth/SessionContext";
import { BanDurationFields, BanStateCell, LockUntilCell, durationSeconds, isValidDuration, type DurationUnit } from "../components/PlayerStateCells";

const PAGE_SIZE_OPTIONS = [10, 25, 50, 100] as const;

export function PassportsPage() {
  const { t } = useI18n();
  const { user } = useSession();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [bulkBanRows, setBulkBanRows] = useState<PassportRow[]>([]);
  const [banReason, setBanReason] = useState("");
  const [banDurationValue, setBanDurationValue] = useState("1");
  const [banDurationUnit, setBanDurationUnit] = useState<DurationUnit>("d");
  const list = useListState({ urlPrefix: "pa", defaults: { pageSize: 25, hidden: ["uuid"] }, storageScope: user?.id });
  const filters = useMemo<IdentityListFilters>(() => {
    const next: IdentityListFilters = { page: list.state.page, page_size: list.state.pageSize };
    const q = (list.state.filters.username ?? "").trim();
    if (q) next.q = q;
    const kind = list.state.filters.kind;
    if (kind === "premium" || kind === "offline") next.kind = kind;
    const status = list.state.filters.status;
    if (status) next.status = status;
    if (list.state.sortKey) {
      next.sort = list.state.sortKey;
      next.dir = list.state.sortDir;
    }
    return next;
  }, [list.state]);
  const q = useQuery({ queryKey: ["admin.passports", filters], queryFn: ({ signal }) => fetchPassports(filters, signal) });
  const statusMut = useMutation({
    mutationFn: async ({ rows, status }: { rows: PassportRow[]; status: PassportRow["status"] }) => {
      await Promise.all(rows.map((row) => updatePassportStatus(row.id, status)));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.saved") });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const revokeMut = useMutation({
    mutationFn: async (rows: PassportRow[]) => {
      await Promise.all(rows.map((row) => row.active_ban ? revokeBan(row.active_ban.id, "") : Promise.resolve()));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.bans.revoked") });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const banMut = useMutation({
    mutationFn: async () => {
      await Promise.all(bulkBanRows.map((row) => createPassportBan(row.id, { reason: banReason, expires_in_seconds: durationSeconds(banDurationValue, banDurationUnit) })));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.bans.created") });
      setBulkBanRows([]);
      setBanReason("");
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });

  const columns: ListColumn<PassportRow>[] = [
    {
      key: "username",
      header: t("admin.passports.col.username"),
      mandatory: true,
      sortable: true,
      sortValue: (r) => r.username,
      filter: { type: "text", placeholder: t("admin.passports.searchPlaceholder") },
      render: (r) => (
        <div className="player-cell">
          <span className={r.avatar_url ? "pa-avatar has-image" : "pa-avatar"}>
            {r.avatar_url ? <img src={r.avatar_url} alt="" aria-hidden="true" /> : (r.username || "?")[0]}
          </span>
          <span className="player-name">{r.username}</span>
        </div>
      ),
    },
    {
      key: "profile",
      header: t("admin.passports.col.profile"),
      minWidth: "170px",
      render: (r) => <span>{r.primary_profile?.protocol_name ?? "—"}</span>,
    },
    {
      key: "profiles",
      header: t("admin.passports.col.profiles"),
      sortable: true,
      sortValue: (r) => r.profile_count,
      render: (r) => <span>{r.profile_count}</span>,
    },
    {
      key: "kind",
      header: t("admin.players.col.type"),
      sortable: true,
      sortValue: (r) => r.kind,
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "premium", label: t("admin.players.filter.premium") },
          { value: "offline", label: t("admin.players.filter.offline") },
        ],
      },
      render: (r) => <TypeBadge kind={r.kind} />,
    },
    {
      key: "status",
      header: t("admin.players.col.status"),
      sortable: true,
      sortValue: (r) => r.status,
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "active", label: t("status.active") },
          { value: "locked", label: t("status.locked") },
          { value: "pending_verification", label: t("status.pending") },
          { value: "deleted", label: t("status.deleted") },
        ],
      },
      render: (r) => <StatusBadge status={r.status} />,
    },
    { key: "online", header: t("admin.presences.onlineState"), minWidth: "120px", render: (r) => <StatusBadge status={r.online ? "online" : "offline_status"} />, sortable: true, sortValue: (r) => r.online },
    { key: "ban", header: t("admin.bans.heading"), minWidth: "180px", defaultVisible: false, render: (r) => <BanStateCell ban={r.active_ban} />, sortable: true, sortValue: (r) => r.ban_expires_at ?? "" },
    { key: "lockedUntil", header: t("admin.player.lockedUntil"), minWidth: "180px", defaultVisible: false, render: (r) => <LockUntilCell lockedUntil={r.locked_until} />, sortable: true, sortValue: (r) => r.locked_until ?? "" },
    { key: "uuid", header: "UUID", minWidth: "300px", defaultVisible: false, render: (r) => <Copyable value={r.uuid} />, sortable: true, sortValue: (r) => r.uuid },
    { key: "lastSeen", header: t("admin.players.col.lastSeen"), minWidth: "150px", render: (r) => <span className="muted-cell">{formatRelativeTime(r.last_seen_at)}</span>, sortable: true, sortValue: (r) => r.last_seen_at ?? "" },
    { key: "lastSeenIp", header: t("admin.players.col.lastSeenIp"), minWidth: "220px", defaultVisible: false, render: (r) => <IPLocation ip={r.last_seen_ip} geo={r.last_seen_geo} /> },
    { key: "open", header: "", mandatory: true, width: "44px", minWidth: "44px", align: "right", sticky: "right", render: () => <Icon name="chevronRight" size={16} /> },
  ];

  return (
    <PageShell>
      <PageHeader title={t("admin.passports.heading")} desc={t("admin.passports.desc")} />
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      <Card noBody className="table-card">
        <AdvancedList
          columns={columns}
          rowKey={(r) => r.id}
          mode="server"
          rows={q.data?.rows ?? []}
          total={(q.data?.meta as { total?: number } | undefined)?.total ?? 0}
          loading={q.isLoading}
          state={list.state}
          onStateChange={list.setState}
          pageSizeOptions={PAGE_SIZE_OPTIONS}
          onRowClick={(r) => navigate(`/passports/${r.id}`)}
          selectionActions={(rows) => (
            <>
              {rows.every((row) => row.status !== "locked") ? <Button size="sm" variant="secondary" icon="lock" loading={statusMut.isPending} onClick={() => statusMut.mutate({ rows, status: "locked" })}>{t("common.lock")}</Button> : null}
              {rows.every((row) => row.status === "locked") ? <Button size="sm" variant="secondary" icon="unlock" loading={statusMut.isPending} onClick={() => statusMut.mutate({ rows, status: "active" })}>{t("common.unlock")}</Button> : null}
              {rows.every((row) => !row.active_ban) ? <Button size="sm" variant="danger-soft" icon="alert" onClick={() => setBulkBanRows(rows)}>{t("common.ban")}</Button> : null}
              {rows.every((row) => !!row.active_ban) ? <Button size="sm" variant="secondary" icon="unlock" loading={revokeMut.isPending} onClick={() => revokeMut.mutate(rows)}>{t("common.unban")}</Button> : null}
              {rows.every((row) => row.status !== "deleted") ? <Button size="sm" variant="danger" icon="trash" loading={statusMut.isPending} onClick={() => statusMut.mutate({ rows, status: "deleted" })}>{t("common.delete")}</Button> : null}
            </>
          )}
          empty={<EmptyState icon="key" title={t("admin.passports.empty")} />}
          testId="passports"
        />
      </Card>
      <Dialog
        open={bulkBanRows.length > 0}
        onClose={() => !banMut.isPending && setBulkBanRows([])}
        icon="alert"
        iconTone="danger"
        title={t("admin.bans.banPassport")}
        footer={<><Button variant="ghost" onClick={() => setBulkBanRows([])}>{t("common.cancel")}</Button><Button variant="danger" loading={banMut.isPending} disabled={!banReason.trim() || !isValidDuration(banDurationValue)} onClick={() => banMut.mutate()}>{t("common.confirm")}</Button></>}
      >
        <BanDurationFields reason={banReason} value={banDurationValue} unit={banDurationUnit} onReasonChange={setBanReason} onValueChange={setBanDurationValue} onUnitChange={setBanDurationUnit} />
      </Dialog>
    </PageShell>
  );
}
