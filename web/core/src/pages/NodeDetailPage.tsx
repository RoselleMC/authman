import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import {
  ApiError,
  BackLink,
  Badge,
  Button,
  Card,
  ConfigGrid,
  ConfigRow,
  DefList,
  DefRow,
  ErrorState,
  Field,
  Icon,
  Input,
  PageHeader,
  PageShell,
  coerceVelocityNode,
  formatRelativeTime,
  useI18n,
  useToast,
  type SafeVelocityNode,
} from "@authman/shared";
import { fetchNode, updateNode } from "../api/admin";

interface FormState {
  name: string;
  server_id: string;
  heartbeat_interval_seconds: string;
  resolve_raw_offline_names: boolean;
  transfer_cookie_key: string;
  gate_initial_server: string;
  gate_holding_server: string;
  gate_validation_timeout_seconds: string;
}

function text(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function numberText(value: unknown, fallback: number): string {
  return typeof value === "number" && Number.isFinite(value) ? String(value) : String(fallback);
}

function boolValue(value: unknown, fallback: boolean): boolean {
  return typeof value === "boolean" ? value : fallback;
}

function toForm(n: SafeVelocityNode): FormState {
  const cfg = n.runtime_config ?? {};
  return {
    name: n.name,
    server_id: text(cfg.server_id, n.server_id || "default"),
    heartbeat_interval_seconds: numberText(cfg.heartbeat_interval_seconds, 60),
    resolve_raw_offline_names: boolValue(cfg.resolve_raw_offline_names, true),
    transfer_cookie_key: text(cfg.transfer_cookie_key, "authman:transfer_grant"),
    gate_initial_server: text(cfg.gate_initial_server),
    gate_holding_server: text(cfg.gate_holding_server),
    gate_validation_timeout_seconds: numberText(cfg.gate_validation_timeout_seconds, 10),
  };
}

function toRuntime(form: FormState): Record<string, unknown> {
  return {
    node_name: form.name.trim(),
    server_id: form.server_id.trim() || "default",
    heartbeat_interval_seconds: Number(form.heartbeat_interval_seconds) || 60,
    resolve_raw_offline_names: form.resolve_raw_offline_names,
    transfer_cookie_key: form.transfer_cookie_key.trim() || "authman:transfer_grant",
    gate_initial_server: form.gate_initial_server.trim(),
    gate_holding_server: form.gate_holding_server.trim(),
    gate_validation_timeout_seconds: Number(form.gate_validation_timeout_seconds) || 10,
  };
}

function formEquals(a: FormState, b: FormState): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

function statusTone(status: SafeVelocityNode["status"]): "success" | "warning" | "neutral" {
  if (status === "active") return "success";
  if (status === "stale") return "warning";
  return "neutral";
}

export function NodeDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t, tError } = useI18n();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["admin.node", id],
    queryFn: () => fetchNode(id),
    enabled: !!id,
    refetchInterval: 30_000,
  });

  const node = useMemo(() => (q.data ? coerceVelocityNode(q.data) : null), [q.data]);
  const initial = useMemo(() => (node ? toForm(node) : null), [node]);
  const [form, setForm] = useState<FormState | null>(null);

  useEffect(() => {
    if (initial) setForm(initial);
  }, [initial]);

  const save = useMutation({
    mutationFn: (current: FormState) => updateNode(id, { name: current.name.trim(), runtime_config: node?.mode === "gate" ? toRuntime(current) : {} }),
    onSuccess: (saved) => {
      const next = coerceVelocityNode(saved);
      setForm(toForm(next));
      toast.push({ tone: "success", title: t("admin.nodes.detail.saved") });
      void qc.invalidateQueries({ queryKey: ["admin.node", id] });
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  if (q.error) {
    return (
      <PageShell>
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      </PageShell>
    );
  }
  if (q.isLoading || !node || !form || !initial) {
    return (
      <PageShell>
        <Card>{t("common.loading")}</Card>
      </PageShell>
    );
  }

  const dirty = !formEquals(form, initial);
  const cfg = node.runtime_config ?? {};

  function patch<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((prev) => (prev ? { ...prev, [key]: value } : prev));
  }

  return (
    <PageShell>
      <BackLink onClick={() => navigate("/nodes")} testId="back-to-nodes">
        {t("admin.nodes.heading")}
      </BackLink>
      <PageHeader
        eyebrow={<Badge tone={node.mode === "gate" ? "warning" : "info"} dot>{t(`admin.nodes.mode.${node.mode}`)}</Badge>}
        title={node.name}
        desc={node.mode === "gate" ? t("admin.nodes.detail.gateDesc") : t("admin.nodes.detail.portalDesc")}
        action={(
          <Button
            variant="primary"
            icon="save"
            loading={save.isPending}
            disabled={!dirty || save.isPending}
            onClick={() => save.mutate(form)}
            data-testid="node-detail-save"
          >
            {t("common.save")}
          </Button>
        )}
      />

      <div className="detail-grid">
        <div className="detail-aside">
          <Card title={t("admin.nodes.detail.summary")}>
            <DefList>
              <DefRow k={t("admin.nodes.col.status")}>
                <Badge tone={statusTone(node.status)} dot>{t(`admin.nodes.status.${node.status}`, node.status)}</Badge>
              </DefRow>
              <DefRow k={t("admin.nodes.col.mode")}>{t(`admin.nodes.mode.${node.mode}`)}</DefRow>
              <DefRow k={t("admin.nodes.col.heartbeat")}>{formatRelativeTime(node.last_seen_at)}</DefRow>
              <DefRow k={t("admin.nodes.col.version")}>
                <code className="mono">{node.plugin_version || "—"}{node.velocity_version ? ` / ${node.velocity_version}` : ""}</code>
              </DefRow>
              <DefRow k={t("admin.nodes.detail.instance")}>
                <code className="mono fingerprint">{node.instance_fingerprint || "—"}</code>
              </DefRow>
              <DefRow k={t("admin.nodes.detail.token")}>
                <code className="mono fingerprint">{node.token_fingerprint || "—"}</code>
              </DefRow>
            </DefList>
          </Card>
        </div>

        <div className="detail-body">
          <Card title={t("admin.nodes.detail.identity")}>
            <div className="settings-form-grid settings-form-grid--single">
              <Field label={t("admin.nodes.field.name")} hint={t("admin.nodes.detail.name.hint")}>
                <Input value={form.name} onChange={(e) => patch("name", e.target.value)} mono data-testid="node-detail-name" />
              </Field>
            </div>
          </Card>

          {node.mode === "gate" ? (
            <Card title={t("admin.nodes.detail.gateRuntime")}>
              <div className="settings-form-grid">
                <Field label={t("admin.nodes.field.serverId")} hint={t("admin.nodes.field.serverId.hint")}>
                  <Input value={form.server_id} onChange={(e) => patch("server_id", e.target.value)} mono data-testid="node-gate-server-id" />
                </Field>
                <Field label={t("admin.nodes.detail.field.initial")} hint={t("admin.nodes.detail.field.initial.hint")}>
                  <Input value={form.gate_initial_server} onChange={(e) => patch("gate_initial_server", e.target.value)} placeholder="survival" mono data-testid="node-gate-initial" />
                </Field>
                <Field label={t("admin.nodes.detail.field.holding")} hint={t("admin.nodes.detail.field.holding.hint")}>
                  <Input value={form.gate_holding_server} onChange={(e) => patch("gate_holding_server", e.target.value)} placeholder="auth-hold" mono data-testid="node-gate-holding" />
                </Field>
                <Field label={t("admin.nodes.detail.field.cookie")} hint={t("admin.nodes.detail.field.cookie.hint")}>
                  <Input value={form.transfer_cookie_key} onChange={(e) => patch("transfer_cookie_key", e.target.value)} mono data-testid="node-gate-cookie" />
                </Field>
                <Field label={t("admin.nodes.detail.field.timeout")} hint={t("admin.nodes.detail.field.timeout.hint")}>
                  <Input type="number" min={3} value={form.gate_validation_timeout_seconds} onChange={(e) => patch("gate_validation_timeout_seconds", e.target.value)} data-testid="node-gate-timeout" />
                </Field>
                <Field label={t("admin.nodes.detail.field.heartbeat")} hint={t("admin.nodes.detail.field.heartbeat.hint")}>
                  <Input type="number" min={10} value={form.heartbeat_interval_seconds} onChange={(e) => patch("heartbeat_interval_seconds", e.target.value)} data-testid="node-gate-heartbeat" />
                </Field>
              </div>
              <label className="toggle-row" style={{ marginTop: 16 }}>
                <input
                  type="checkbox"
                  checked={form.resolve_raw_offline_names}
                  onChange={(e) => patch("resolve_raw_offline_names", e.target.checked)}
                  data-testid="node-gate-resolve-raw"
                />
                <span>
                  <strong>{t("admin.nodes.detail.field.resolveRaw")}</strong>
                  <small>{t("admin.nodes.detail.field.resolveRaw.hint")}</small>
                </span>
              </label>
            </Card>
          ) : (
            <Card title={t("admin.nodes.detail.portalRuntime")}>
              <p className="card-foot-note" style={{ marginTop: 0 }}>
                <Icon name="info" size={13} /> {t("admin.nodes.detail.portalGlobal")}
              </p>
              <ConfigGrid testId="node-portal-runtime">
                <ConfigRow k={t("admin.portal.field.defaultTarget")} v={text(cfg.default_target_server, "—")} mono />
                <ConfigRow k={t("admin.portal.field.holding")} v={text(cfg.holding_server, "—")} mono />
                <ConfigRow k={t("admin.portal.field.requestedHost")} v={text(cfg.portal_requested_host, "—")} mono />
                <ConfigRow k={t("admin.portal.field.source")} v={text(cfg.portal_source_id, "—")} mono />
                <ConfigRow k={t("admin.portal.field.cookie")} v={text(cfg.transfer_cookie_key, "authman:transfer_grant")} mono />
                <ConfigRow k={t("admin.portal.field.dialog")} v={boolValue(cfg.dialog_enabled, true) ? t("common.enabled") : t("common.disabled")} />
              </ConfigGrid>
              <Button variant="ghost" icon="settings" onClick={() => navigate("/portal")} style={{ marginTop: 16 }} data-testid="node-open-portal-settings">
                {t("admin.nodes.detail.openPortal")}
              </Button>
            </Card>
          )}
        </div>
      </div>
    </PageShell>
  );
}
