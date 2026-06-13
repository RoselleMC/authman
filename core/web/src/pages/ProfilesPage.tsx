import { useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AdvancedList,
  Button,
  Card,
  ConfirmDialog,
  Copyable,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  Icon,
  IPLocation,
  Input,
  PageHeader,
  PageShell,
  StatusBadge,
  useI18n,
  useListState,
  useToast,
  navigateWithBack,
  type ListColumn,
} from "@authman/shared";
import { createProfile, deleteProfile, fetchProfiles, type IdentityListFilters, type ProfileRow } from "../api/admin";
import { createProfileBan, revokeBan, updateProfileStatus } from "../api/admin";
import { useSession } from "../auth/SessionContext";
import { BanDurationFields, BanStateCell, LockUntilCell, durationSeconds, isValidDuration, type DurationUnit } from "../components/PlayerStateCells";

const PAGE_SIZE_OPTIONS = [10, 25, 50, 100] as const;

export function ProfilesPage() {
  const { t } = useI18n();
  const { user } = useSession();
  const navigate = useNavigate();
  const location = useLocation();
  const toast = useToast();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [bulkBanRows, setBulkBanRows] = useState<ProfileRow[]>([]);
  const [bulkDeleteRows, setBulkDeleteRows] = useState<ProfileRow[]>([]);
  const [banReason, setBanReason] = useState("");
  const [banDurationValue, setBanDurationValue] = useState("1");
  const [banDurationUnit, setBanDurationUnit] = useState<DurationUnit>("d");
  const [protocolName, setProtocolName] = useState("");
  const list = useListState({ urlPrefix: "pr", defaults: { pageSize: 25, hidden: ["uuid"] }, storageScope: user?.id });
  const filters = useMemo<IdentityListFilters>(() => {
    const next: IdentityListFilters = { page: list.state.page, page_size: list.state.pageSize };
    const q = (list.state.filters.protocol ?? "").trim();
    if (q) next.q = q;
    const status = list.state.filters.status;
    if (status) next.status = status;
    if (list.state.sortKey) {
      next.sort = list.state.sortKey;
      next.dir = list.state.sortDir;
    }
    return next;
  }, [list.state]);
  const q = useQuery({ queryKey: ["admin.profiles", filters], queryFn: ({ signal }) => fetchProfiles(filters, signal) });
  const createMut = useMutation({
    mutationFn: () => createProfile({ protocol_name: protocolName }),
    onSuccess: (profile) => {
      toast.push({ tone: "success", title: t("admin.profiles.created") });
      setOpen(false);
      setProtocolName("");
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
      navigateWithBack(navigate, `/profiles/${profile.id}`, location);
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const statusMut = useMutation({
    mutationFn: async ({ rows, status }: { rows: ProfileRow[]; status: ProfileRow["status"] }) => {
      await Promise.all(rows.map((row) => updateProfileStatus(row.id, status)));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.saved") });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const deleteMut = useMutation({
    mutationFn: async (rows: ProfileRow[]) => {
      await Promise.all(rows.map((row) => deleteProfile(row.id)));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.deleted") });
      setBulkDeleteRows([]);
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const revokeMut = useMutation({
    mutationFn: async (rows: ProfileRow[]) => {
      await Promise.all(rows.map((row) => row.active_ban ? revokeBan(row.active_ban.id, "") : Promise.resolve()));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.bans.revoked") });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const banMut = useMutation({
    mutationFn: async () => {
      await Promise.all(bulkBanRows.map((row) => createProfileBan(row.id, { reason: banReason, expires_in_seconds: durationSeconds(banDurationValue, banDurationUnit) })));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.bans.created") });
      setBulkBanRows([]);
      setBanReason("");
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });

  const columns: ListColumn<ProfileRow>[] = [
    {
      key: "protocol",
      header: t("admin.profiles.col.protocol"),
      mandatory: true,
      sortable: true,
      sortValue: (r) => r.protocol_name,
      filter: { type: "text", placeholder: t("admin.profiles.searchPlaceholder") },
      render: (r) => (
        <div className="player-cell">
          <span className={r.avatar_url ? "pa-avatar has-image" : "pa-avatar"}>
            {r.avatar_url ? <img src={r.avatar_url} alt="" aria-hidden="true" /> : (r.protocol_name || "?")[0]}
          </span>
          <span className="player-name">{r.protocol_name}</span>
        </div>
      ),
    },
    { key: "uuid", header: "UUID", minWidth: "300px", defaultVisible: false, render: (r) => <Copyable value={r.uuid} />, sortable: true, sortValue: (r) => r.uuid },
    { key: "passport", header: t("admin.profiles.col.passport"), minWidth: "180px", render: (r) => r.passport ? r.passport.username : "—" },
    {
      key: "status",
      header: t("admin.players.col.status"),
      sortable: true,
      sortValue: (r) => r.status,
      filter: { type: "select", options: [
        { value: "", label: t("common.all") },
        { value: "active", label: t("status.active") },
        { value: "locked", label: t("status.locked") },
        { value: "archived", label: t("status.archived") },
      ] },
      render: (r) => <StatusBadge status={r.status} />,
    },
    { key: "online", header: t("admin.presences.onlineState"), minWidth: "120px", render: (r) => <StatusBadge status={r.online ? "online" : "offline_status"} />, sortable: true, sortValue: (r) => r.online },
    { key: "ban", header: t("admin.bans.heading"), minWidth: "180px", defaultVisible: false, render: (r) => <BanStateCell ban={r.active_ban} />, sortable: true, sortValue: (r) => r.ban_expires_at ?? "" },
    { key: "lockedUntil", header: t("admin.player.lockedUntil"), minWidth: "180px", defaultVisible: false, render: (r) => <LockUntilCell lockedUntil={r.locked_until} />, sortable: true, sortValue: (r) => r.locked_until ?? "" },
    { key: "lastSeenIp", header: t("admin.players.col.lastSeenIp"), minWidth: "220px", defaultVisible: false, render: (r) => <IPLocation ip={r.last_seen_ip} geo={r.last_seen_geo} /> },
    { key: "open", header: "", mandatory: true, width: "44px", minWidth: "44px", align: "right", sticky: "right", render: () => <Icon name="chevronRight" size={16} /> },
  ];

  return (
    <PageShell>
      <PageHeader
        title={t("admin.profiles.heading")}
        desc={t("admin.profiles.desc")}
        action={<Button icon="plus" onClick={() => setOpen(true)}>{t("admin.profiles.create")}</Button>}
      />
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
          onRowClick={(r) => navigateWithBack(navigate, `/profiles/${r.id}`, location)}
          selectionActions={(rows) => (
            <>
              {rows.every((row) => row.status === "active") ? <Button size="sm" variant="secondary" icon="lock" loading={statusMut.isPending} onClick={() => statusMut.mutate({ rows, status: "locked" })}>{t("common.lock")}</Button> : null}
              {rows.every((row) => row.status === "locked") ? <Button size="sm" variant="secondary" icon="unlock" loading={statusMut.isPending} onClick={() => statusMut.mutate({ rows, status: "active" })}>{t("common.unlock")}</Button> : null}
              {rows.every((row) => !row.active_ban) ? <Button size="sm" variant="danger-soft" icon="alert" onClick={() => setBulkBanRows(rows)}>{t("common.ban")}</Button> : null}
              {rows.every((row) => !!row.active_ban) ? <Button size="sm" variant="secondary" icon="unlock" loading={revokeMut.isPending} onClick={() => revokeMut.mutate(rows)}>{t("common.unban")}</Button> : null}
              {rows.every((row) => row.status !== "archived") ? <Button size="sm" variant="secondary" icon="box" loading={statusMut.isPending} onClick={() => statusMut.mutate({ rows, status: "archived" })}>{t("common.archive")}</Button> : null}
              {rows.every((row) => row.status === "archived") ? <Button size="sm" variant="secondary" icon="refresh" loading={statusMut.isPending} onClick={() => statusMut.mutate({ rows, status: "active" })}>{t("common.restore")}</Button> : null}
              <Button size="sm" variant="danger" icon="trash" loading={deleteMut.isPending} onClick={() => setBulkDeleteRows(rows)}>{t("common.delete")}</Button>
            </>
          )}
          empty={<EmptyState icon="user" title={t("admin.profiles.empty")} />}
          testId="profiles"
        />
      </Card>
      <Dialog
        open={open}
        onClose={() => !createMut.isPending && setOpen(false)}
        icon="plus"
        title={t("admin.profiles.create")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)}>{t("common.cancel")}</Button>
            <Button variant="primary" loading={createMut.isPending} disabled={!protocolName.trim()} onClick={() => createMut.mutate()}>{t("common.confirm")}</Button>
          </>
        }
      >
        <Field label={t("admin.profiles.col.protocol")}>
          <Input value={protocolName} onChange={(e) => setProtocolName(e.target.value)} autoFocus />
        </Field>
      </Dialog>
      <Dialog
        open={bulkBanRows.length > 0}
        onClose={() => !banMut.isPending && setBulkBanRows([])}
        icon="alert"
        iconTone="danger"
        title={t("admin.bans.banProfile")}
        footer={<><Button variant="ghost" onClick={() => setBulkBanRows([])}>{t("common.cancel")}</Button><Button variant="danger" loading={banMut.isPending} disabled={!banReason.trim() || !isValidDuration(banDurationValue)} onClick={() => banMut.mutate()}>{t("common.confirm")}</Button></>}
      >
        <BanDurationFields reason={banReason} value={banDurationValue} unit={banDurationUnit} onReasonChange={setBanReason} onValueChange={setBanDurationValue} onUnitChange={setBanDurationUnit} />
      </Dialog>
      <ConfirmDialog
        open={bulkDeleteRows.length > 0}
        title={t("admin.profiles.deleteDialog")}
        body={t("admin.profiles.deleteDialogBody")}
        confirmLabel={t("common.delete")}
        destructive
        loading={deleteMut.isPending}
        onCancel={() => setBulkDeleteRows([])}
        onConfirm={() => deleteMut.mutate(bulkDeleteRows)}
      />
    </PageShell>
  );
}
