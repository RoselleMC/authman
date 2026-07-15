import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import {
  ApiError,
  BackLink,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  ConfigGrid,
  ConfigRow,
  DefList,
  DefRow,
  DetailActions,
  DetailAside,
  DetailBody,
  DetailGrid,
  DetailIdentifier,
  DetailSummary,
  ErrorState,
  Field,
  Icon,
  Input,
  PageShell,
  coerceVelocityNode,
  coerceLimboProtocolPackState,
  formatRelativeTime,
  useI18n,
  useBackTarget,
  useToast,
  type SafeVelocityNode,
} from "@authman/shared";
import {
  downloadLimboProtocolPack,
  fetchLimboProtocolPack,
  fetchNode,
  resetLimboProtocolPack,
  updateNode,
  uploadLimboProtocolPack,
} from "../api/admin";

interface FormState {
  name: string;
  server_id: string;
  heartbeat_interval_seconds: string;
  proxy_protocol_enabled: boolean;
  proxy_protocol_restrict_trusted_proxies: boolean;
  proxy_protocol_trusted_proxies: string;
  proxy_protocol_header_timeout_millis: string;
  resolve_raw_offline_names: boolean;
  transfer_cookie_key: string;
  downstream_initial_server: string;
  downstream_holding_server: string;
  downstream_validation_timeout_seconds: string;
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
    server_id: text(cfg.server_id, n.server_id || ""),
    heartbeat_interval_seconds: numberText(cfg.heartbeat_interval_seconds, 60),
    proxy_protocol_enabled: boolValue(cfg.proxy_protocol_enabled, false),
    proxy_protocol_restrict_trusted_proxies: boolValue(cfg.proxy_protocol_restrict_trusted_proxies, false),
    proxy_protocol_trusted_proxies: text(cfg.proxy_protocol_trusted_proxies),
    proxy_protocol_header_timeout_millis: numberText(cfg.proxy_protocol_header_timeout_millis, 5000),
    resolve_raw_offline_names: boolValue(cfg.resolve_raw_offline_names, true),
    transfer_cookie_key: text(cfg.transfer_cookie_key, "authman:transfer_grant"),
    downstream_initial_server: text(cfg.downstream_initial_server, text(cfg.gate_initial_server)),
    downstream_holding_server: text(cfg.downstream_holding_server, text(cfg.gate_holding_server)),
    downstream_validation_timeout_seconds: numberText(cfg.downstream_validation_timeout_seconds, 10),
  };
}

function toRuntime(form: FormState): Record<string, unknown> {
  return {
    node_name: form.name.trim(),
    server_id: form.server_id.trim(),
    heartbeat_interval_seconds: Number(form.heartbeat_interval_seconds) || 60,
    resolve_raw_offline_names: form.resolve_raw_offline_names,
    transfer_cookie_key: form.transfer_cookie_key.trim() || "authman:transfer_grant",
    downstream_initial_server: form.downstream_initial_server.trim(),
    downstream_holding_server: form.downstream_holding_server.trim(),
    downstream_validation_timeout_seconds: Number(form.downstream_validation_timeout_seconds) || 10,
  };
}

function toLimboRuntime(form: FormState): Record<string, unknown> {
  return {
    proxy_protocol_enabled: form.proxy_protocol_enabled,
    proxy_protocol_restrict_trusted_proxies: form.proxy_protocol_restrict_trusted_proxies,
    proxy_protocol_trusted_proxies: form.proxy_protocol_trusted_proxies.trim(),
    proxy_protocol_header_timeout_millis: Number(form.proxy_protocol_header_timeout_millis) || 5000,
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

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "—";
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
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
  const protocolPackQ = useQuery({
    queryKey: ["admin.limboProtocolPack", id],
    queryFn: () => fetchLimboProtocolPack(id),
    enabled: !!id && node?.kind === "limbo_portal",
    refetchInterval: 5_000,
  });
  const protocolPack = useMemo(
    () => (protocolPackQ.data ? coerceLimboProtocolPackState(protocolPackQ.data) : node?.protocol_pack ?? null),
    [node?.protocol_pack, protocolPackQ.data],
  );
  const initial = useMemo(() => (node ? toForm(node) : null), [node]);
  const [form, setForm] = useState<FormState | null>(null);
  const [protocolFile, setProtocolFile] = useState<File | null>(null);
  const [protocolFileKey, setProtocolFileKey] = useState(0);
  const [resetProtocolOpen, setResetProtocolOpen] = useState(false);
  const backPath = node?.kind === "limbo_portal" ? "/login-portals" : "/nodes";
  const backTarget = useBackTarget(backPath);

  useEffect(() => {
    if (initial) setForm(initial);
  }, [initial]);

  const save = useMutation({
    mutationFn: (current: FormState) =>
      updateNode(id, {
        name: current.name.trim(),
        runtime_config: node?.kind === "downstream_velocity" ? toRuntime(current) : toLimboRuntime(current),
      }),
    onSuccess: (saved) => {
      const next = coerceVelocityNode(saved);
      setForm(toForm(next));
      toast.push({ tone: "success", title: t("admin.nodes.detail.saved") });
      void qc.invalidateQueries({ queryKey: ["admin.node", id] });
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const refreshProtocolPack = () => {
    void qc.invalidateQueries({ queryKey: ["admin.limboProtocolPack", id] });
    void qc.invalidateQueries({ queryKey: ["admin.node", id] });
    void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
  };

  const uploadProtocolPack = useMutation({
    mutationFn: (file: File) => uploadLimboProtocolPack(id, file),
    onSuccess: () => {
      setProtocolFile(null);
      setProtocolFileKey((value) => value + 1);
      toast.push({ tone: "success", title: t("admin.nodes.protocolPack.uploaded") });
      refreshProtocolPack();
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const resetProtocolPack = useMutation({
    mutationFn: () => resetLimboProtocolPack(id),
    onSuccess: () => {
      setResetProtocolOpen(false);
      toast.push({ tone: "success", title: t("admin.nodes.protocolPack.resetDone") });
      refreshProtocolPack();
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const downloadProtocolPack = useMutation({
    mutationFn: () => downloadLimboProtocolPack(id),
    onSuccess: ({ blob, filename }) => {
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = filename;
      anchor.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
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
  const configuredPack = protocolPack?.configured ?? null;
  const activePack = protocolPack?.active ?? null;
  const backLabel = node.kind === "limbo_portal" ? t("admin.loginPortals.heading") : t("admin.nodes.heading");

  function patch<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((prev) => (prev ? { ...prev, [key]: value } : prev));
  }

  return (
    <PageShell>
      <div className="detail-toolbar">
        <BackLink onClick={() => navigate(backTarget)} testId="back-to-nodes">
          {backLabel}
        </BackLink>
      </div>

      <DetailGrid>
        <DetailAside>
          <DetailSummary
            title={node.name}
            icon={node.kind === "limbo_portal" ? "layers" : "server"}
            titleMeta={<Badge tone={statusTone(node.status)} dot>{t(`admin.nodes.status.${node.status}`, node.status)}</Badge>}
            meta={<><span className="muted-cell">{t("admin.nodes.col.mode")}</span><strong>{t(`admin.nodes.mode.${node.kind}`)}</strong></>}
          >
            <DetailIdentifier label={t("admin.nodes.detail.nodeId")} value={node.id} />
            <DetailIdentifier label={t("admin.nodes.detail.instance")} value={node.instance_fingerprint} />
            <DetailIdentifier label={t("admin.nodes.detail.token")} value={node.token_fingerprint} />
            <DefList>
              <DefRow k={t("admin.nodes.col.heartbeat")}>{formatRelativeTime(node.last_seen_at)}</DefRow>
              <DefRow k={t("admin.nodes.col.version")}>
                <code className="mono">{node.plugin_version || "—"}{node.velocity_version ? ` / ${node.velocity_version}` : ""}</code>
              </DefRow>
            </DefList>
          </DetailSummary>
          <DetailActions title={t("common.actions")}>
            <Button
              variant="primary"
              icon="save"
              block
              loading={save.isPending}
              disabled={!dirty || save.isPending}
              onClick={() => save.mutate(form)}
              data-testid="node-detail-save"
            >
              {t("common.save")}
            </Button>
            {node.kind === "limbo_portal" ? (
              <Button variant="secondary" icon="settings" block onClick={() => navigate("/login-portals/settings")} data-testid="node-open-portal-settings">
                {t("admin.nodes.detail.openPortal")}
              </Button>
            ) : null}
          </DetailActions>
        </DetailAside>

        <DetailBody>
          <Card title={t("admin.nodes.detail.identity")}>
            <div className="settings-form-grid settings-form-grid--single">
              <Field label={t("admin.nodes.field.name")} hint={t("admin.nodes.detail.name.hint")}>
                <Input value={form.name} onChange={(e) => patch("name", e.target.value)} mono data-testid="node-detail-name" />
              </Field>
            </div>
            <DefList>
              <DefRow k={t("admin.nodes.detail.instance")}>
                <code className="mono fingerprint">{node.instance_fingerprint || "—"}</code>
              </DefRow>
              <DefRow k={t("admin.nodes.detail.token")}>
                <code className="mono fingerprint">{node.token_fingerprint || "—"}</code>
              </DefRow>
            </DefList>
          </Card>

          {node.kind === "downstream_velocity" ? (
            <Card title={t("admin.nodes.detail.downstreamRuntime")}>
              <div className="settings-form-grid">
                <Field label={t("admin.nodes.field.serverId")} hint={t("admin.nodes.field.serverId.hint")}>
                  <Input value={form.server_id} onChange={(e) => patch("server_id", e.target.value)} mono data-testid="node-gate-server-id" />
                </Field>
                <Field label={t("admin.nodes.detail.field.initial")} hint={t("admin.nodes.detail.field.initial.hint")}>
                  <Input value={form.downstream_initial_server} onChange={(e) => patch("downstream_initial_server", e.target.value)} placeholder="survival" mono data-testid="node-downstream-initial" />
                </Field>
                <Field label={t("admin.nodes.detail.field.holding")} hint={t("admin.nodes.detail.field.holding.hint")}>
                  <Input value={form.downstream_holding_server} onChange={(e) => patch("downstream_holding_server", e.target.value)} placeholder="auth-hold" mono data-testid="node-downstream-holding" />
                </Field>
                <Field label={t("admin.nodes.detail.field.cookie")} hint={t("admin.nodes.detail.field.cookie.hint")}>
                  <Input value={form.transfer_cookie_key} onChange={(e) => patch("transfer_cookie_key", e.target.value)} mono data-testid="node-gate-cookie" />
                </Field>
                <Field label={t("admin.nodes.detail.field.timeout")} hint={t("admin.nodes.detail.field.timeout.hint")}>
                  <Input type="number" min={3} value={form.downstream_validation_timeout_seconds} onChange={(e) => patch("downstream_validation_timeout_seconds", e.target.value)} data-testid="node-downstream-timeout" />
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
            <>
            <Card
              title={t("admin.nodes.protocolPack.heading")}
              actions={
                <div className="row-actions">
                  <Badge tone={protocolPack?.source === "custom" ? "info" : "neutral"}>
                    {t(`admin.nodes.protocolPack.source.${protocolPack?.source ?? "unavailable"}`)}
                  </Badge>
                  <Badge tone={protocolPack?.in_sync ? "success" : "warning"} dot>
                    {t(protocolPack?.in_sync ? "admin.nodes.protocolPack.synced" : "admin.nodes.protocolPack.pending")}
                  </Badge>
                </div>
              }
              testId="node-protocol-pack"
            >
              <ConfigGrid>
                <ConfigRow k={t("admin.nodes.protocolPack.configured")} v={configuredPack ? `${configuredPack.name} / ${configuredPack.version}` : "—"} mono />
                <ConfigRow k={t("admin.nodes.protocolPack.active")} v={activePack ? `${activePack.name} / ${activePack.version}` : "—"} mono />
                <ConfigRow k={t("admin.nodes.protocolPack.archive")} v={configuredPack ? `${configuredPack.filename || "authman-protocols.zip"} · ${formatBytes(configuredPack.size_bytes)}` : "—"} mono />
                <ConfigRow k="SHA-256" v={configuredPack?.sha256 ? configuredPack.sha256.slice(0, 16) : "—"} mono />
                <ConfigRow k={t("admin.nodes.protocolPack.protocols")} v={configuredPack?.protocols.join(", ") || "—"} mono />
                <ConfigRow k={t("admin.nodes.protocolPack.reported")} v={activePack?.reported_at ? formatRelativeTime(activePack.reported_at) : "—"} />
              </ConfigGrid>

              <div className="protocol-version-section">
                <span className="protocol-version-label">{t("admin.nodes.protocolPack.minecraftVersions")}</span>
                <div className="protocol-version-list" data-testid="node-protocol-pack-versions">
                  {(activePack?.minecraft_versions.length ? activePack.minecraft_versions : configuredPack?.minecraft_versions ?? []).map((version) => (
                    <span className="protocol-version-tag" key={version}>{version}</span>
                  ))}
                  {!activePack?.minecraft_versions.length && !configuredPack?.minecraft_versions.length ? <span className="muted-cell">—</span> : null}
                </div>
              </div>

              {activePack?.last_error ? <p className="protocol-pack-error"><Icon name="alert" size={14} /> {activePack.last_error}</p> : null}
              {protocolPack?.error ? <p className="protocol-pack-error"><Icon name="alert" size={14} /> {protocolPack.error}</p> : null}

              <div className="protocol-pack-controls">
                <input
                  key={protocolFileKey}
                  className="input protocol-pack-file"
                  type="file"
                  accept=".zip,application/zip"
                  onChange={(event) => setProtocolFile(event.target.files?.[0] ?? null)}
                  data-testid="node-protocol-pack-file"
                />
                <div className="protocol-pack-actions">
                  <Button
                    variant="primary"
                    icon="upload"
                    loading={uploadProtocolPack.isPending}
                    disabled={!protocolFile || uploadProtocolPack.isPending}
                    onClick={() => protocolFile && uploadProtocolPack.mutate(protocolFile)}
                    data-testid="node-protocol-pack-upload"
                  >
                    {t("admin.nodes.protocolPack.upload")}
                  </Button>
                  <Button
                    variant="secondary"
                    icon="download"
                    loading={downloadProtocolPack.isPending}
                    onClick={() => downloadProtocolPack.mutate()}
                    data-testid="node-protocol-pack-download"
                  >
                    {t("admin.nodes.protocolPack.download")}
                  </Button>
                  <Button
                    variant="secondary"
                    icon="refresh"
                    disabled={protocolPack?.source !== "custom"}
                    onClick={() => setResetProtocolOpen(true)}
                    data-testid="node-protocol-pack-reset"
                  >
                    {t("admin.nodes.protocolPack.reset")}
                  </Button>
                </div>
              </div>
            </Card>

            <Card title={t("admin.nodes.detail.limboRuntime")}>
              <p className="card-foot-note" style={{ marginTop: 0 }}>
                <Icon name="info" size={13} /> {t("admin.nodes.detail.portalRuntime")}
              </p>
              <label className="toggle-row" style={{ marginBottom: 16 }}>
                <input
                  type="checkbox"
                  checked={form.proxy_protocol_enabled}
                  onChange={(e) => patch("proxy_protocol_enabled", e.target.checked)}
                  data-testid="node-limbo-proxy-protocol"
                />
                <span>
                  <strong>{t("admin.nodes.detail.field.proxyProtocol")}</strong>
                  <small>{t("admin.nodes.detail.field.proxyProtocol.hint")}</small>
                </span>
              </label>
              <label className="toggle-row" style={{ marginBottom: 16 }}>
                <input
                  type="checkbox"
                  checked={form.proxy_protocol_restrict_trusted_proxies}
                  onChange={(e) => patch("proxy_protocol_restrict_trusted_proxies", e.target.checked)}
                  disabled={!form.proxy_protocol_enabled}
                  data-testid="node-limbo-proxy-restrict"
                />
                <span>
                  <strong>{t("admin.nodes.detail.field.proxyRestrict")}</strong>
                  <small>{t("admin.nodes.detail.field.proxyRestrict.hint")}</small>
                </span>
              </label>
              <div className="settings-form-grid settings-form-grid--single">
                <Field label={t("admin.nodes.detail.field.proxyTrusted")} hint={t("admin.nodes.detail.field.proxyTrusted.hint")}>
                  <Input
                    value={form.proxy_protocol_trusted_proxies}
                    onChange={(e) => patch("proxy_protocol_trusted_proxies", e.target.value)}
                    placeholder="127.0.0.1,192.0.2.0/24"
                    mono
                    data-testid="node-limbo-proxy-trusted"
                  />
                </Field>
                <Field label={t("admin.nodes.detail.field.proxyHeaderTimeout")} hint={t("admin.nodes.detail.field.proxyHeaderTimeout.hint")}>
                  <Input
                    type="number"
                    min={0}
                    value={form.proxy_protocol_header_timeout_millis}
                    onChange={(e) => patch("proxy_protocol_header_timeout_millis", e.target.value)}
                    data-testid="node-limbo-proxy-timeout"
                  />
                </Field>
              </div>
              <ConfigGrid testId="node-portal-runtime">
                <ConfigRow k={t("admin.portal.field.cookie")} v={text(cfg.transfer_cookie_key, "authman:transfer_grant")} mono />
                <ConfigRow k={t("admin.nodes.detail.field.proxyProtocol.current")} v={boolValue(cfg.proxy_protocol_enabled, false) ? t("status.enabled") : t("status.disabled")} />
                <ConfigRow k={t("admin.nodes.detail.field.proxyRestrict.current")} v={boolValue(cfg.proxy_protocol_restrict_trusted_proxies, false) ? t("status.enabled") : t("status.disabled")} />
                <ConfigRow
                  k={t("admin.nodes.detail.field.proxyTrusted.current")}
                  v={boolValue(cfg.proxy_protocol_restrict_trusted_proxies, false) ? text(cfg.proxy_protocol_trusted_proxies, "—") || "—" : t("admin.nodes.detail.field.proxyTrusted.all")}
                  mono
                />
                <ConfigRow k={t("admin.portal.field.protocol")} v={t("admin.portal.protocolRange")} />
              </ConfigGrid>
            </Card>
            </>
          )}
        </DetailBody>
      </DetailGrid>
      <ConfirmDialog
        open={resetProtocolOpen}
        title={t("admin.nodes.protocolPack.resetConfirm")}
        body={t("admin.nodes.protocolPack.resetConfirm.body")}
        confirmLabel={t("admin.nodes.protocolPack.reset")}
        loading={resetProtocolPack.isPending}
        icon="refresh"
        onConfirm={() => resetProtocolPack.mutate()}
        onCancel={() => setResetProtocolOpen(false)}
        testId="node-protocol-pack-reset-dialog"
      />
    </PageShell>
  );
}
