import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import {
  Alert,
  ApiError,
  Badge,
  BackLink,
  Button,
  Card,
  DefList,
  DefRow,
  DetailAside,
  DetailBody,
  DetailGrid,
  Dialog,
  Field,
  Icon,
  Input,
  PageHeader,
  PageShell,
  Select,
  useI18n,
  useToast,
} from "@authman/shared";
import {
  deleteDownstreamServer,
  fetchDownstreamServer,
  updateDownstreamServer,
  type DownstreamServer,
  type DownstreamServerInput,
} from "../api/admin";
import { ErrorBlock } from "../components/ErrorBlock";

interface FormState {
  display_name: string;
  status: "active" | "hidden" | "disabled";
  registration_open: boolean;
  host: string;
  port: string;
  transfer_host: string;
  transfer_port: string;
  motd: string;
  gate_enabled: boolean;
  grant_ttl_seconds: string;
  portal_hosts: string;
  allowed_portal_sources: string;
}

function toFormState(s: DownstreamServer): FormState {
  const cfg = s.portal_config ?? {};
  return {
    display_name: s.display_name,
    status: s.status,
    registration_open: s.registration_open,
    host: cfg.host ?? s.target.host ?? "",
    port: String(cfg.port ?? s.target.port ?? 25565),
    transfer_host: cfg.transfer_host ?? s.target.transfer_host ?? "",
    transfer_port: String(cfg.transfer_port ?? s.target.transfer_port ?? 25565),
    motd: cfg.motd ?? s.target.motd ?? "",
    gate_enabled: cfg.gate_enabled ?? s.target.gate_enabled ?? true,
    grant_ttl_seconds: String(cfg.grant_ttl_seconds ?? s.target.grant_ttl_seconds ?? 45),
    portal_hosts: (cfg.portal_hosts ?? []).join(", "),
    allowed_portal_sources: (cfg.allowed_portal_sources ?? s.target.allowed_portal_sources ?? []).join(", "),
  };
}

function toInput(s: DownstreamServer, form: FormState): DownstreamServerInput {
  const parseList = (value: string) =>
    value
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean);
  return {
    slug: s.slug,
    display_name: form.display_name,
    status: form.status,
    registration_open: form.registration_open,
    portal_theme: {
      ...s.portal_theme,
      display_name: form.display_name,
    },
    portal_config: {
      ...s.portal_config,
      registration_strategy: form.registration_open ? "open" : "closed",
      host: form.host,
      port: Number(form.port),
      transfer_host: form.transfer_host,
      transfer_port: Number(form.transfer_port),
      motd: form.motd,
      gate_enabled: form.gate_enabled,
      grant_ttl_seconds: Number(form.grant_ttl_seconds),
      portal_hosts: parseList(form.portal_hosts),
      allowed_portal_sources: parseList(form.allowed_portal_sources),
    },
    extension_providers: s.extension_providers ?? [],
  };
}

function formEquals(a: FormState, b: FormState): boolean {
  return (
    a.display_name === b.display_name &&
    a.status === b.status &&
    a.registration_open === b.registration_open &&
    a.host === b.host &&
    a.port === b.port &&
    a.transfer_host === b.transfer_host &&
    a.transfer_port === b.transfer_port &&
    a.motd === b.motd &&
    a.gate_enabled === b.gate_enabled &&
    a.grant_ttl_seconds === b.grant_ttl_seconds &&
    a.portal_hosts === b.portal_hosts &&
    a.allowed_portal_sources === b.allowed_portal_sources
  );
}

function statusTone(status: DownstreamServer["status"]): "success" | "warning" | "neutral" {
  if (status === "active") return "success";
  if (status === "hidden") return "warning";
  return "neutral";
}

export function ServerDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t, tError } = useI18n();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [deleteOpen, setDeleteOpen] = useState(false);

  const q = useQuery({
    queryKey: ["admin.server", id],
    queryFn: () => fetchDownstreamServer(id),
    enabled: !!id,
  });

  const initial = useMemo(() => (q.data ? toFormState(q.data) : null), [q.data]);
  const [form, setForm] = useState<FormState | null>(null);

  useEffect(() => {
    if (initial) setForm(initial);
  }, [initial]);

  const updateMut = useMutation({
    mutationFn: (input: DownstreamServerInput) => updateDownstreamServer(id, input),
    onSuccess: (saved) => {
      toast.push({ tone: "success", title: t("admin.servers.saved.toast") });
      void qc.invalidateQueries({ queryKey: ["admin.server", id] });
      void qc.invalidateQueries({ queryKey: ["admin.servers"] });
      setForm(toFormState(saved));
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const deleteMut = useMutation({
    mutationFn: () => deleteDownstreamServer(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.servers.deleted.toast") });
      setDeleteOpen(false);
      void qc.invalidateQueries({ queryKey: ["admin.servers"] });
      navigate("/servers", { replace: true });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  if (q.error) {
    return (
      <PageShell>
        <ErrorBlock error={q.error} onRetry={() => q.refetch()} />
      </PageShell>
    );
  }
  if (q.isLoading || !q.data || !form || !initial) {
    return (
      <PageShell>
        <Card>{t("common.loading")}</Card>
      </PageShell>
    );
  }

  const s = q.data;
  const dirty = !formEquals(form, initial);
  const isDefault = s.id === "default";
  const portalHostList = s.portal_config?.portal_hosts ?? [];
  const allowedSourceList = s.portal_config?.allowed_portal_sources ?? s.target.allowed_portal_sources ?? [];

  function patch<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((prev) => (prev ? { ...prev, [key]: value } : prev));
  }

  function submit() {
    if (!form) return;
    updateMut.mutate(toInput(s, form));
  }

  return (
    <PageShell>
      <BackLink onClick={() => navigate("/servers")} testId="back-to-servers">
        {t("admin.servers.heading")}
      </BackLink>
      <PageHeader
        eyebrow={(
          <code className="mono" style={{ color: "var(--color-text-muted)", fontSize: 12.5 }}>
            /server/{s.slug}
          </code>
        )}
        title={s.display_name}
        desc={t("admin.servers.detail.desc")}
        action={(
          <div style={{ display: "flex", gap: 8 }}>
            {!isDefault ? (
              <Button variant="ghost" icon="close" onClick={() => setDeleteOpen(true)} data-testid="server-detail-delete">
                {t("admin.servers.delete")}
              </Button>
            ) : null}
            <Button
              variant="primary"
              icon="check"
              loading={updateMut.isPending}
              disabled={!dirty || updateMut.isPending}
              onClick={submit}
              data-testid="server-detail-save"
            >
              {t("common.save")}
            </Button>
          </div>
        )}
      />

      <DetailGrid>
        <DetailAside>
          <Card title={t("admin.servers.detail.summary")}>
            <DefList>
              <DefRow k={t("admin.servers.detail.displayName")}>{s.display_name}</DefRow>
              <DefRow k={t("admin.servers.field.slug")}>
                <code className="mono" style={{ fontSize: 12.5 }}>{s.slug}</code>
              </DefRow>
              <DefRow k={t("admin.servers.col.status")}>
                <Badge tone={statusTone(s.status)} dot>
                  {t(`admin.servers.status.${s.status}`)}
                </Badge>
              </DefRow>
              <DefRow k={t("admin.servers.col.registration")}>
                <Badge tone={s.registration_open ? "success" : "neutral"} dot>
                  {s.registration_open ? t("common.open") : t("common.closed")}
                </Badge>
              </DefRow>
              <DefRow k={t("admin.servers.detail.transferTarget")}>
                <code className="mono" style={{ fontSize: 12.5 }} data-testid="detail-transfer-target">
                  {s.target.transfer_host}:{s.target.transfer_port}
                </code>
              </DefRow>
              <DefRow k={t("admin.servers.detail.portalContext")}>
                <code className="mono" style={{ fontSize: 12.5 }}>
                  {s.target.host}:{s.target.port}
                </code>
              </DefRow>
              <DefRow k={t("admin.servers.col.gate")}>
                <Badge tone={s.target.gate_enabled ? "success" : "neutral"} dot>
                  {s.target.gate_enabled ? t("common.enabled") : t("common.disabled")}
                </Badge>
              </DefRow>
              <DefRow k={t("admin.servers.field.grantTtl")}>{s.target.grant_ttl_seconds}s</DefRow>
              <DefRow k={t("admin.servers.detail.motd")}>
                <span data-testid="detail-motd">{s.target.motd || "—"}</span>
              </DefRow>
            </DefList>
          </Card>
          <Card title={t("admin.servers.detail.aliases")}>
            {portalHostList.length === 0 ? (
              <p className="muted-cell" data-testid="detail-aliases-empty">
                {t("admin.servers.detail.aliases.empty")}
              </p>
            ) : (
              <ul className="alias-list" data-testid="detail-aliases-list">
                {portalHostList.map((host) => (
                  <li key={host}>
                    <code className="mono" style={{ fontSize: 12.5 }}>{host}</code>
                  </li>
                ))}
              </ul>
            )}
            <p className="muted-cell" style={{ marginTop: 12, fontSize: 12.5 }}>
              {t("admin.servers.detail.aliases.hint")}
            </p>
          </Card>
          <Card title={t("admin.servers.detail.allowedSources")}>
            {allowedSourceList.length === 0 ? (
              <p className="muted-cell">{t("admin.servers.detail.allowedSources.empty")}</p>
            ) : (
              <ul className="alias-list" data-testid="detail-sources-list">
                {allowedSourceList.map((src) => (
                  <li key={src}>
                    <code className="mono" style={{ fontSize: 12.5 }}>{src}</code>
                  </li>
                ))}
              </ul>
            )}
            <p className="muted-cell" style={{ marginTop: 12, fontSize: 12.5 }}>
              {t("admin.servers.detail.allowedSources.hint")}
            </p>
          </Card>
        </DetailAside>
        <DetailBody>
          <Card title={t("admin.servers.detail.identity")}>
            <div className="form-grid">
              <Field label={t("admin.servers.detail.displayName")}>
                <Input
                  value={form.display_name}
                  onChange={(e) => patch("display_name", e.target.value)}
                  data-testid="edit-display-name"
                />
              </Field>
              <Field label={t("admin.servers.col.status")}>
                <Select
                  value={form.status}
                  onChange={(v) => patch("status", v as FormState["status"])}
                  options={[
                    { value: "active", label: t("admin.servers.status.active") },
                    { value: "hidden", label: t("admin.servers.status.hidden") },
                    { value: "disabled", label: t("admin.servers.status.disabled") },
                  ]}
                  testId="edit-status"
                />
              </Field>
              <Field label={t("admin.servers.field.motd")} hint={t("admin.servers.field.motd.hint")}>
                <Input
                  value={form.motd}
                  onChange={(e) => patch("motd", e.target.value)}
                  data-testid="edit-motd"
                />
              </Field>
              <label className="check-row">
                <input
                  type="checkbox"
                  checked={form.registration_open}
                  onChange={(e) => patch("registration_open", e.target.checked)}
                  data-testid="edit-registration"
                />
                <span>{t("admin.servers.field.registrationOpen")}</span>
              </label>
            </div>
          </Card>

          <Card title={t("admin.servers.detail.gate")}>
            <Alert tone="neutral">
              <p>{t("admin.servers.detail.gate.explain")}</p>
            </Alert>
            <div className="form-grid" style={{ marginTop: 16 }}>
              <Field label={t("admin.servers.field.transferHost")} hint={t("admin.servers.field.transferHost.hint")}>
                <Input
                  value={form.transfer_host}
                  onChange={(e) => patch("transfer_host", e.target.value)}
                  mono
                  data-testid="edit-transfer-host"
                />
              </Field>
              <Field label={t("admin.servers.field.transferPort")}>
                <Input
                  value={form.transfer_port}
                  onChange={(e) => patch("transfer_port", e.target.value)}
                  mono
                  data-testid="edit-transfer-port"
                />
              </Field>
              <Field label={t("admin.servers.field.grantTtl")} hint={t("admin.servers.field.grantTtl.hint")}>
                <Input
                  value={form.grant_ttl_seconds}
                  onChange={(e) => patch("grant_ttl_seconds", e.target.value)}
                  mono
                  data-testid="edit-grant-ttl"
                />
              </Field>
              <label className="check-row">
                <input
                  type="checkbox"
                  checked={form.gate_enabled}
                  onChange={(e) => patch("gate_enabled", e.target.checked)}
                  data-testid="edit-gate-enabled"
                />
                <span>{t("admin.servers.field.gateEnabled")}</span>
              </label>
            </div>
          </Card>

          <Card title={t("admin.servers.detail.portal")}>
            <Alert tone="neutral">
              <p>{t("admin.servers.detail.portal.explain")}</p>
            </Alert>
            <div className="form-grid" style={{ marginTop: 16 }}>
              <Field label={t("admin.servers.field.portalHosts")} hint={t("admin.servers.field.portalHosts.hint")}>
                <Input
                  value={form.portal_hosts}
                  onChange={(e) => patch("portal_hosts", e.target.value)}
                  mono
                  placeholder="play.example.com, us.example.com"
                  data-testid="edit-portal-hosts"
                />
              </Field>
              <Field label={t("admin.servers.field.host")} hint={t("admin.servers.field.host.hint")}>
                <Input
                  value={form.host}
                  onChange={(e) => patch("host", e.target.value)}
                  mono
                  data-testid="edit-host"
                />
              </Field>
              <Field label={t("admin.servers.field.port")}>
                <Input
                  value={form.port}
                  onChange={(e) => patch("port", e.target.value)}
                  mono
                  data-testid="edit-port"
                />
              </Field>
              <Field label={t("admin.servers.field.allowedSources")} hint={t("admin.servers.field.allowedSources.hint")}>
                <Input
                  value={form.allowed_portal_sources}
                  onChange={(e) => patch("allowed_portal_sources", e.target.value)}
                  mono
                  placeholder="portal-us, portal-eu"
                  data-testid="edit-allowed-sources"
                />
              </Field>
            </div>
          </Card>
        </DetailBody>
      </DetailGrid>

      <Dialog
        open={deleteOpen}
        onClose={() => !deleteMut.isPending && setDeleteOpen(false)}
        icon="close"
        iconTone="danger"
        title={t("admin.servers.delete")}
        desc={t("admin.servers.delete.desc")}
        testId="dialog-detail-delete-server"
        footer={(
          <>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)} disabled={deleteMut.isPending}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              icon="close"
              loading={deleteMut.isPending}
              onClick={() => deleteMut.mutate()}
              data-testid="confirm-detail-delete"
            >
              {t("admin.servers.delete")}
            </Button>
          </>
        )}
      >
        <p className="dialog-note" style={{ marginTop: 0 }}>
          {s.display_name} · {s.slug}
        </p>
      </Dialog>
    </PageShell>
  );
}
