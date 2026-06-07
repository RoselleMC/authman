import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import {
  ApiError,
  Badge,
  Button,
  Card,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  HealthBanner,
  Icon,
  IconButton,
  Input,
  PageHeader,
  PageShell,
  coerceMojangStatus,
  useI18n,
  useToast,
  type DataColumn,
  type SafeMojangProxy,
} from "@authman/shared";
import { createMojangRoute, deleteMojangRoute, fetchMojang, type CreateMojangRouteInput } from "../api/admin";

function StateBadge({ state }: { state: string }) {
  const { t } = useI18n();
  const tone: "success" | "danger" | "neutral" | "warning" = state === "healthy" ? "success" : state === "failed" ? "danger" : state === "disabled" ? "neutral" : "warning";
  const key = state === "cooling_down" ? "cooldown" : state;
  return <Badge tone={tone} dot>{t(`admin.mojang.state.${key}`, state)}</Badge>;
}

const PROXY_TYPE: Record<Exclude<SafeMojangProxy["kind"], "direct">, string> = { http: "HTTP", socks5: "SOCKS5" };

export function MojangPage() {
  const { t, tError } = useI18n();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [kind, setKind] = useState<CreateMojangRouteInput["kind"]>("http");
  const [routeID, setRouteID] = useState("");
  const [routeURL, setRouteURL] = useState("");
  const [weight, setWeight] = useState(1);
  const [deleteTarget, setDeleteTarget] = useState<SafeMojangProxy | null>(null);
  const q = useQuery({
    queryKey: ["admin.mojang"],
    queryFn: fetchMojang,
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  });

  const createMut = useMutation({
    mutationFn: () => createMojangRoute({
      id: routeID.trim() || undefined,
      kind,
      url: routeURL.trim(),
      weight,
    }),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.mojang.add.toast") });
      setDialogOpen(false);
      setRouteID("");
      setRouteURL("");
      setWeight(1);
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
    {
      key: "actions",
      header: "",
      align: "right",
      render: (p) => (
        <div className="row-actions" onClick={(event) => event.stopPropagation()}>
          {p.kind !== "direct" ? (
            <IconButton
              name="close"
              size={16}
              label={t("admin.mojang.delete")}
              onClick={() => setDeleteTarget(p)}
              data-testid={`delete-mojang-${p.id}`}
            />
          ) : null}
        </div>
      ),
    },
  ];

  return (
    <PageShell>
      <PageHeader
        title={t("admin.mojang.heading")}
        desc={t("admin.mojang.desc")}
        action={
          <div className="row-actions">
            <Button variant="primary" icon="plus" onClick={() => setDialogOpen(true)} data-testid="mojang-add-proxy">
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

      <Dialog
        open={dialogOpen}
        onClose={() => !createMut.isPending && setDialogOpen(false)}
        icon="activity"
        iconTone="primary"
        title={t("admin.mojang.add")}
        desc={t("admin.mojang.add.desc")}
        testId="dialog-mojang-route"
        footer={
          <>
            <Button variant="ghost" onClick={() => setDialogOpen(false)} disabled={createMut.isPending} data-testid="confirm-cancel">
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              icon="plus"
              loading={createMut.isPending}
              onClick={() => createMut.mutate()}
              data-testid="confirm-confirm"
            >
              {t("admin.mojang.add.submit")}
            </Button>
          </>
        }
      >
        <Field label={t("admin.mojang.field.kind")}>
          <select
            className="input"
            value={kind}
            onChange={(e) => setKind(e.target.value as CreateMojangRouteInput["kind"])}
            data-testid="mojang-route-kind"
          >
            <option value="http">HTTP</option>
            <option value="socks5">SOCKS5</option>
          </select>
        </Field>
        <Field label={t("admin.mojang.field.url")} hint={kind === "http" ? t("admin.mojang.field.url.httpHint") : t("admin.mojang.field.url.socksHint")}>
          <Input
            value={routeURL}
            onChange={(e) => setRouteURL(e.target.value)}
            placeholder={kind === "http" ? "http://user:pass@proxy.example:8080" : "socks5://user:pass@proxy.example:1080"}
            mono
            data-testid="mojang-route-url"
          />
        </Field>
        <Field label={t("admin.mojang.field.id")} hint={t("admin.mojang.field.id.hint")}>
          <Input
            value={routeID}
            onChange={(e) => setRouteID(e.target.value)}
            placeholder={`${kind}-edge-1`}
            mono
            data-testid="mojang-route-id"
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
    </PageShell>
  );
}
