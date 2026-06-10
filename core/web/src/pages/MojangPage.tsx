import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import {
  AdvancedList,
  ApiError,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  Icon,
  IconButton,
  Input,
  PageHeader,
  PageShell,
  Select,
  coerceMojangStatus,
  useI18n,
  useListState,
  useToast,
  type ListColumn,
  type SafeMojangProxy,
} from "@authman/shared";
import { createMojangRoute, deleteMojangRoute, fetchMojang, updateMojangRoute, type CreateMojangRouteInput, type ListFilters } from "../api/admin";
import { useSession } from "../auth/SessionContext";

function StateBadge({ state }: { state: string }) {
  const { t } = useI18n();
  const tone: "success" | "danger" | "neutral" | "warning" = state === "healthy" ? "success" : state === "failed" ? "danger" : state === "disabled" ? "neutral" : "warning";
  const key = state === "cooling_down" ? "cooldown" : state;
  return <Badge tone={tone} dot>{t(`admin.mojang.state.${key}`, state)}</Badge>;
}

const PROXY_TYPE: Record<Exclude<SafeMojangProxy["kind"], "direct">, string> = { http: "HTTP", socks5: "SOCKS5" };

export function ProxyPoolPage() {
  const { t, tError } = useI18n();
  const { user } = useSession();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const list = useListState({ urlPrefix: "px", defaults: { pageSize: 25 }, storageScope: user?.id });
  const [dialogOpen, setDialogOpen] = useState(false);
  const [kind, setKind] = useState<CreateMojangRouteInput["kind"]>("http");
  const [routeID, setRouteID] = useState("");
  const [routeURL, setRouteURL] = useState("");
  const [weight, setWeight] = useState(1);
  const [disabled, setDisabled] = useState(false);
  const [authEnabled, setAuthEnabled] = useState(false);
  const [authUsername, setAuthUsername] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [editTarget, setEditTarget] = useState<SafeMojangProxy | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SafeMojangProxy | null>(null);
  const [bulkDeleteRows, setBulkDeleteRows] = useState<SafeMojangProxy[]>([]);
  const filters = useMemo<ListFilters>(() => {
    const next: ListFilters = { page: list.state.page, page_size: list.state.pageSize };
    const q = (list.state.filters.route ?? "").trim();
    if (q) next.q = q;
    const kind = list.state.filters.kind;
    if (kind) next.kind = kind;
    const state = list.state.filters.state;
    if (state) next.state = state;
    if (list.state.sortKey) {
      next.sort = list.state.sortKey;
      next.dir = list.state.sortDir;
    }
    return next;
  }, [list.state]);
  const q = useQuery({
    queryKey: ["admin.mojang", filters],
    queryFn: () => fetchMojang(filters),
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  });

  const createMut = useMutation({
    mutationFn: () => createMojangRoute({
      id: routeID.trim() || undefined,
      kind,
      url: buildRouteURL(routeURL, authEnabled, authUsername, authPassword),
      weight,
    }),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.mojang.add.toast") });
      resetDialog();
      void qc.invalidateQueries({ queryKey: ["admin.mojang"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });
  const updateMut = useMutation({
    mutationFn: () => {
      if (!editTarget) throw new Error("missing edit target");
      return updateMojangRoute(editTarget.id, {
        id: editTarget.id,
        kind,
        url: routeURL.trim() ? buildRouteURL(routeURL, authEnabled, authUsername, authPassword) : "",
        weight,
        disabled,
      });
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.saved") });
      resetDialog();
      void qc.invalidateQueries({ queryKey: ["admin.mojang"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });
  const deleteMut = useMutation({
    mutationFn: (route: SafeMojangProxy) => deleteMojangRoute(route.id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.mojang.delete.toast") });
      setDeleteTarget(null);
      void qc.invalidateQueries({ queryKey: ["admin.mojang"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });
  const bulkDeleteMut = useMutation({
    mutationFn: async (routes: SafeMojangProxy[]) => {
      await Promise.all(routes.map((route) => deleteMojangRoute(route.id)));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.mojang.delete.toast") });
      setBulkDeleteRows([]);
      void qc.invalidateQueries({ queryKey: ["admin.mojang"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });
  const dialogPending = createMut.isPending || updateMut.isPending;

  const status = q.data ? coerceMojangStatus(q.data.status) : null;
  const columns: ListColumn<SafeMojangProxy>[] = [
    {
      key: "route",
      header: t("admin.mojang.col.route"),
      mandatory: true,
      sortable: true,
      sortValue: (p) => p.id,
      filter: { type: "text" },
      render: (p) => (
        <div className="proxy-route">
          <code className="mono" style={{ fontWeight: 560 }}>{p.id}</code>
          {p.url_masked ? <code className="mono proxy-masked">{p.url_masked}</code> : null}
        </div>
      ),
    },
    {
      key: "kind",
      header: t("admin.mojang.col.type"),
      sortable: true,
      sortValue: (p) => p.kind,
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "direct", label: t("admin.mojang.route.direct") },
          { value: "http", label: "HTTP" },
          { value: "socks5", label: "SOCKS5" },
        ],
      },
      render: (p) => <span className="type-pill">{p.kind === "direct" ? t("admin.mojang.route.direct") : PROXY_TYPE[p.kind]}</span>,
    },
    {
      key: "state",
      header: t("admin.mojang.col.state"),
      sortable: true,
      sortValue: (p) => p.state,
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "healthy", label: t("admin.mojang.state.healthy") },
          { value: "unknown", label: t("admin.mojang.state.unknown") },
          { value: "failed", label: t("admin.mojang.state.failed") },
          { value: "disabled", label: t("admin.mojang.state.disabled") },
          { value: "cooling_down", label: t("admin.mojang.state.cooldown") },
        ],
      },
      render: (p) => <StateBadge state={p.state} />,
    },
    {
      key: "count",
      header: t("admin.mojang.col.requests"),
      align: "right",
      sortable: true,
      sortValue: (p) => p.request_count,
      render: (p) => <span className="mono">{p.request_count.toLocaleString()}</span>,
    },
    {
      key: "cooldown",
      header: t("admin.mojang.col.cooldown"),
      sortable: true,
      sortValue: (p) => p.cooldown_remaining_seconds,
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
    {
      key: "actions",
      header: "",
      align: "right",
      width: "52px",
      minWidth: "52px",
      sticky: "right",
      render: (p) => (
        <div className="row-actions" onClick={(event) => event.stopPropagation()}>
          {p.kind !== "direct" ? (
            <>
              <IconButton
                name="settings"
                size={16}
                label={t("common.edit")}
                onClick={() => openEditDialog(p)}
                data-testid={`edit-mojang-${p.id}`}
              />
              <IconButton
                name="close"
                size={16}
                label={t("admin.mojang.delete")}
                onClick={() => setDeleteTarget(p)}
                data-testid={`delete-mojang-${p.id}`}
              />
            </>
          ) : null}
        </div>
      ),
    },
  ];

  function openCreateDialog() {
    setEditTarget(null);
    setDialogOpen(true);
    setKind("http");
    setRouteID("");
    setRouteURL("");
    setWeight(1);
    setDisabled(false);
    setAuthEnabled(false);
    setAuthUsername("");
    setAuthPassword("");
  }

  function openEditDialog(route: SafeMojangProxy) {
    if (route.kind === "direct") return;
    setEditTarget(route);
    setDialogOpen(true);
    setKind(route.kind);
    setRouteID(route.id);
    setRouteURL("");
    setWeight(route.weight || 1);
    setDisabled(route.state === "disabled");
    setAuthEnabled(false);
    setAuthUsername("");
    setAuthPassword("");
  }

  function resetDialog() {
    setDialogOpen(false);
    setEditTarget(null);
    setRouteID("");
    setRouteURL("");
    setWeight(1);
    setDisabled(false);
    setAuthEnabled(false);
    setAuthUsername("");
    setAuthPassword("");
  }

  return (
    <PageShell>
      <PageHeader
        title={t("admin.proxies.heading")}
        desc={t("admin.proxies.desc")}
        action={
          <div className="row-actions">
            <Button variant="primary" icon="plus" onClick={openCreateDialog} data-testid="mojang-add-proxy">
              {t("admin.mojang.add")}
            </Button>
            <Button variant="secondary" icon="list" onClick={() => navigate("/audit")} data-testid="mojang-audit">
              {t("admin.mojang.openAudit")}
            </Button>
            <Button variant="secondary" icon="refresh" loading={q.isLoading} onClick={() => q.refetch()} data-testid="mojang-refresh">
              {t("common.refresh")}
            </Button>
          </div>
        }
      />
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}

      <Card noBody className="table-card">
        <AdvancedList
          title={t("admin.mojang.proxies")}
          loading={q.isLoading}
          rows={status?.proxies ?? []}
          mode="server"
          total={q.data?.meta.total ?? 0}
          columns={columns}
          rowKey={(r) => r.id}
          state={list.state}
          onStateChange={list.setState}
          selectable={(row) => row.kind !== "direct"}
          selectionActions={(rows) => (
            <Button size="sm" variant="danger-soft" icon="close" onClick={() => setBulkDeleteRows(rows)}>
              {t("common.delete")}
            </Button>
          )}
          empty={<EmptyState icon="activity" title={t("admin.mojang.empty.proxies")} />}
          testId="mojang-proxies"
        />
        <div className="card-foot-note">
          <Icon name="lock" size={13} /> {t("admin.mojang.footnote")}
        </div>
      </Card>

      <Dialog
        open={dialogOpen}
        onClose={() => !dialogPending && resetDialog()}
        icon="activity"
        iconTone="primary"
        title={editTarget ? t("admin.mojang.edit") : t("admin.mojang.add")}
        desc={editTarget ? t("admin.mojang.edit.desc") : t("admin.mojang.add.desc")}
        testId="dialog-mojang-route"
        footer={
          <>
            <Button variant="ghost" onClick={resetDialog} disabled={dialogPending} data-testid="confirm-cancel">
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              icon={editTarget ? "check" : "plus"}
              loading={dialogPending}
              onClick={() => (editTarget ? updateMut.mutate() : createMut.mutate())}
              data-testid="confirm-confirm"
            >
              {editTarget ? t("common.save") : t("admin.mojang.add.submit")}
            </Button>
          </>
        }
      >
        <Field label={t("admin.mojang.field.kind")}>
          <Select<CreateMojangRouteInput["kind"]>
            value={kind}
            onChange={setKind}
            options={[
              { value: "http", label: "HTTP" },
              { value: "socks5", label: "SOCKS5" },
            ]}
            testId="mojang-route-kind"
          />
        </Field>
        <Field label={t("admin.mojang.field.url")} hint={editTarget ? t("admin.mojang.field.url.editHint") : (kind === "http" ? t("admin.mojang.field.url.httpHint") : t("admin.mojang.field.url.socksHint"))}>
          <Input
            value={routeURL}
            onChange={(e) => setRouteURL(e.target.value)}
            placeholder={kind === "http" ? "http://user:pass@proxy.example:8080" : "socks5://user:pass@proxy.example:1080"}
            mono
            data-testid="mojang-route-url"
          />
        </Field>
        <label className="toggle-row">
          <input type="checkbox" checked={authEnabled} onChange={(event) => setAuthEnabled(event.target.checked)} />
          <span>{t("admin.proxies.auth.enabled")}</span>
        </label>
        {authEnabled ? (
          <div className="form-grid two">
            <Field label={t("common.username")}>
              <Input value={authUsername} onChange={(e) => setAuthUsername(e.target.value)} autoComplete="off" data-testid="mojang-route-auth-user" />
            </Field>
            <Field label={t("common.password")}>
              <Input value={authPassword} type="password" onChange={(e) => setAuthPassword(e.target.value)} autoComplete="new-password" data-testid="mojang-route-auth-password" />
            </Field>
          </div>
        ) : null}
        <Field label={t("admin.mojang.field.id")} hint={t("admin.mojang.field.id.hint")}>
          <Input
            value={routeID}
            onChange={(e) => setRouteID(e.target.value)}
            placeholder={`${kind}-edge-1`}
            mono
            data-testid="mojang-route-id"
            disabled={!!editTarget}
          />
        </Field>
        <Field label={t("admin.mojang.field.weight")} hint={t("admin.mojang.field.weight.hint")}>
          <Input
            type="number"
            min={1}
            max={100}
            value={weight}
            onChange={(e) => setWeight(Number(e.target.value) || 1)}
            data-testid="mojang-route-weight"
          />
        </Field>
        {editTarget ? (
          <label className="toggle-row">
            <input type="checkbox" checked={disabled} onChange={(event) => setDisabled(event.target.checked)} />
            <span>{t("admin.proxies.disabled")}</span>
          </label>
        ) : null}
      </Dialog>
      <Dialog
        open={!!deleteTarget}
        onClose={() => !deleteMut.isPending && setDeleteTarget(null)}
        icon="close"
        iconTone="danger"
        title={t("admin.mojang.delete")}
        desc={t("admin.mojang.delete.desc")}
        testId="dialog-delete-mojang-route"
        footer={
          <>
            <Button variant="ghost" onClick={() => setDeleteTarget(null)} disabled={deleteMut.isPending} data-testid="delete-cancel">
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              icon="close"
              loading={deleteMut.isPending}
              onClick={() => deleteTarget && deleteMut.mutate(deleteTarget)}
              data-testid="delete-confirm"
            >
              {t("admin.mojang.delete")}
            </Button>
          </>
        }
      >
        <p className="dialog-note" style={{ marginTop: 0 }}>
          {deleteTarget?.id} · {deleteTarget?.url_masked || deleteTarget?.kind}
        </p>
      </Dialog>
      <ConfirmDialog
        open={bulkDeleteRows.length > 0}
        onCancel={() => setBulkDeleteRows([])}
        onConfirm={() => bulkDeleteMut.mutate(bulkDeleteRows)}
        title={t("admin.mojang.delete")}
        body={t("admin.mojang.delete.desc")}
        confirmLabel={t("admin.mojang.delete")}
        destructive
        loading={bulkDeleteMut.isPending}
        testId="dialog-bulk-delete-mojang-route"
      />
    </PageShell>
  );
}

function buildRouteURL(rawURL: string, authEnabled: boolean, username: string, password: string) {
  const trimmed = rawURL.trim();
  if (!authEnabled) return trimmed;
  try {
    const parsed = new URL(trimmed);
    parsed.username = username.trim();
    parsed.password = password;
    return parsed.toString();
  } catch {
    return trimmed;
  }
}
