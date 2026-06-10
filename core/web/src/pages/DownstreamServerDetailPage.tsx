import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ApiError,
  BackLink,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  DetailActions,
  DetailAside,
  DetailBody,
  DetailGrid,
  DetailSummary,
  Dialog,
  Field,
  Icon,
  Input,
  MinecraftMotdPreview,
  MiniMessageEditorDialog,
  PageShell,
  SecretReveal,
  Select,
  StatusBadge,
  coerceVelocityNode,
  formatRelativeTime,
  useI18n,
  useToast,
  type SafeVelocityNode,
} from "@authman/shared";
import {
  createNode,
  deleteDownstreamServerIcon,
  deleteDownstreamServer,
  deleteNode,
  fetchDownstreamServer,
  fetchLimboBlueprints,
  fetchNodes,
  updateDownstreamServer,
  uploadDownstreamServerIcon,
  type DownstreamServer,
  type DownstreamServerInput,
} from "../api/admin";

interface IssuedToken {
  token_once: string;
  token_fingerprint: string;
  name: string;
}

function toInput(server: DownstreamServer): DownstreamServerInput {
  return {
    display_name: server.display_name,
    enabled: server.enabled,
    visible: server.visible,
    registration_open: true,
    routing_config: { ...server.routing_config },
    extension_providers: [...server.extension_providers],
  };
}

function csv(value: string[] | undefined): string {
  return (value ?? []).join(", ");
}

function splitCSV(value: string): string[] {
  return value.split(",").map((item) => item.trim()).filter(Boolean);
}

function addressFromConfig(cfg: DownstreamServer["routing_config"]): string {
  const host = String(cfg.transfer_host || cfg.host || "127.0.0.1").trim();
  const port = Number(cfg.transfer_port || cfg.port || 25565);
  return `${host}:${Number.isFinite(port) && port > 0 ? port : 25565}`;
}

function parseAddress(value: string): { host: string; port: number } {
  const trimmed = value.trim();
  const lastColon = trimmed.lastIndexOf(":");
  if (lastColon > 0) {
    const host = trimmed.slice(0, lastColon).trim();
    const port = Number(trimmed.slice(lastColon + 1).trim());
    return { host: host || "127.0.0.1", port: Number.isFinite(port) && port > 0 ? port : 25565 };
  }
  return { host: trimmed || "127.0.0.1", port: 25565 };
}

function nodeBelongsToServer(n: SafeVelocityNode, server: DownstreamServer): boolean {
  return n.status !== "disabled" && (n.server_id === server.id || n.server_id === server.slug);
}

function NodeStatusBadge({ node }: { node: SafeVelocityNode }) {
  const { t } = useI18n();
  const tone: "success" | "warning" | "neutral" = node.status === "active" ? "success" : node.status === "stale" ? "warning" : "neutral";
  return <Badge tone={tone} dot>{t(`admin.nodes.status.${node.status}`, node.status)}</Badge>;
}

export function DownstreamServerDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t } = useI18n();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteNodeOpen, setDeleteNodeOpen] = useState(false);
  const [issueOpen, setIssueOpen] = useState(false);
  const [motdOpen, setMotdOpen] = useState(false);
  const [issuedToken, setIssuedToken] = useState<IssuedToken | null>(null);
  const [input, setInput] = useState<DownstreamServerInput | null>(null);
  const [matchDomains, setMatchDomains] = useState("");
  const [downstreamAddress, setDownstreamAddress] = useState("");
  const [iconError, setIconError] = useState("");
  const q = useQuery({ queryKey: ["admin.downstreamServer", id], queryFn: () => fetchDownstreamServer(id), enabled: !!id });
  const blueprints = useQuery({ queryKey: ["admin.limboBlueprints", "select"], queryFn: () => fetchLimboBlueprints({ page: 1, page_size: 100 }) });
  const nodesQ = useQuery({
    queryKey: ["admin.nodes", "downstream_velocity", "server-detail"],
    queryFn: () => fetchNodes("downstream_velocity", { page: 1, page_size: 100 }),
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  });
  const server = q.data;
  const node = useMemo(() => {
    if (!server) return null;
    return (nodesQ.data?.rows ?? []).map(coerceVelocityNode).find((n) => nodeBelongsToServer(n, server)) ?? null;
  }, [nodesQ.data, server]);
  const blueprintOptions = useMemo(() => [
    { value: "", label: t("admin.servers.defaultWorld") },
    ...(blueprints.data?.rows ?? []).map((bp) => ({ value: bp.id, label: bp.name })),
  ], [blueprints.data, t]);

  useEffect(() => {
    if (!server) return;
    setInput(toInput(server));
    setMatchDomains(csv(server.routing_config.portal_hosts));
    setDownstreamAddress(addressFromConfig(server.routing_config));
  }, [server]);

  const updateMut = useMutation({
    mutationFn: (nextInput?: DownstreamServerInput) => {
      const currentInput = nextInput ?? input;
      if (!currentInput) throw new Error("server input missing");
      const target = parseAddress(downstreamAddress);
      return updateDownstreamServer(id, {
        ...currentInput,
        routing_config: {
          ...currentInput.routing_config,
          host: target.host,
          port: target.port,
          transfer_host: target.host,
          transfer_port: target.port,
          portal_hosts: splitCSV(matchDomains),
          allowed_portal_sources: [],
          gate_enabled: true,
          grant_required: true,
        },
      });
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.saved") });
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServer", id] });
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServers"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  function updateStatus(patch: Partial<Pick<DownstreamServerInput, "enabled" | "visible">>) {
    if (!input) return;
    const next = { ...input, ...patch };
    setInput(next);
    updateMut.mutate(next);
  }
  const deleteMut = useMutation({
    mutationFn: () => deleteDownstreamServer(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.deleted") });
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServers"] });
      navigate("/nodes");
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const issueNodeMut = useMutation({
    mutationFn: () => createNode({ name: input?.display_name || id, kind: "downstream_velocity", server_id: id }),
    onSuccess: (res) => {
      setIssuedToken({ token_once: res.token_once, token_fingerprint: res.token_fingerprint, name: res.node.name });
      setIssueOpen(false);
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? err.message : t("common.unknown")),
  });
  const deleteNodeMut = useMutation({
    mutationFn: () => node ? deleteNode(node.id) : Promise.resolve(),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.nodes.delete.toast") });
      setDeleteNodeOpen(false);
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? err.message : t("common.unknown")),
  });
  const uploadIconMut = useMutation({
    mutationFn: (file: File) => uploadDownstreamServerIcon(id, file),
    onSuccess: (next) => {
      toast.push({ tone: "success", title: t("admin.servers.icon.saved") });
      setIconError("");
      setInput(toInput(next));
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServer", id] });
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServers"] });
    },
    onError: (err) => {
      const message = err instanceof ApiError ? err.message : t("common.unknown");
      setIconError(message);
      toast.danger(message);
    },
  });
  const deleteIconMut = useMutation({
    mutationFn: () => deleteDownstreamServerIcon(id),
    onSuccess: (next) => {
      toast.push({ tone: "success", title: t("admin.servers.icon.deleted") });
      setIconError("");
      setInput(toInput(next));
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServer", id] });
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServers"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? err.message : t("common.unknown")),
  });

  if (!server || !input) {
    return <PageShell><BackLink onClick={() => navigate("/nodes")}>{t("admin.servers.heading")}</BackLink><Card title={q.isLoading ? t("common.loading") : t("common.unknown")}><span /></Card></PageShell>;
  }
  const cfg = input.routing_config;
  const serverIcon = String(cfg.server_icon || server.target.server_icon || "").trim();
  function setConfig(next: Partial<DownstreamServerInput["routing_config"]>) {
    setInput((current) => current ? { ...current, routing_config: { ...current.routing_config, ...next } } : current);
  }

  return (
    <PageShell testId="downstream-server-detail-page">
      <div className="detail-toolbar">
        <BackLink onClick={() => navigate("/nodes")}>{t("admin.servers.heading")}</BackLink>
      </div>
      <DetailGrid>
        <DetailAside>
          <DetailSummary
            title={input.display_name}
            icon="server"
            avatarUrl={serverIcon || null}
            titleMeta={<StatusBadge status={input.enabled ? (input.visible ? "active" : "hidden") : "disabled"} />}
            meta={<span className="muted-cell">{t("admin.servers.internalId")}: <span className="mono">{server.id}</span></span>}
          >
            <div className="id-uuid">
              <span className="id-uuid-label">{t("admin.servers.connectionAddress")}</span>
              <strong className="mono">{downstreamAddress}</strong>
            </div>
          </DetailSummary>
          <DetailActions title={t("common.actions")}>
            <Button variant="primary" icon="check" block loading={updateMut.isPending} onClick={() => updateMut.mutate(undefined)}>{t("common.save")}</Button>
            <Button
              variant={input.enabled ? "secondary" : "primary"}
              icon={input.enabled ? "close" : "check"}
              block
              loading={updateMut.isPending}
              onClick={() => updateStatus({ enabled: !input.enabled })}
            >
              {input.enabled ? t("common.disable") : t("common.enable")}
            </Button>
            <Button
              variant="secondary"
              icon={input.visible ? "eyeOff" : "eye"}
              block
              loading={updateMut.isPending}
              onClick={() => updateStatus({ visible: !input.visible })}
            >
              {input.visible ? t("common.hide") : t("common.show")}
            </Button>
            {server.id !== "default" ? <Button variant="danger-soft" icon="trash" block onClick={() => setDeleteOpen(true)}>{t("common.delete")}</Button> : null}
          </DetailActions>
        </DetailAside>
        <DetailBody>
          <Card title={t("admin.servers.identity")}>
            <div className="form-grid">
              <Field label={t("admin.servers.col.name")} style={{ gridColumn: "1 / -1" }}>
                <Input value={input.display_name} onChange={(e) => setInput({ ...input, display_name: e.target.value })} />
              </Field>
            </div>
          </Card>

          <Card title={t("admin.servers.routing")}>
            <div className="form-grid two">
              <Field label={t("admin.servers.matchDomains")} hint={t("admin.servers.matchDomains.hint")} style={{ gridColumn: "1 / -1" }}>
                <Input value={matchDomains} onChange={(e) => setMatchDomains(e.target.value)} placeholder="play.example.com, survival.example.com" />
              </Field>
              <Field label={t("admin.servers.connectionAddress")} hint={t("admin.servers.connectionAddress.hint")} style={{ gridColumn: "1 / -1" }}>
                <Input value={downstreamAddress} onChange={(e) => setDownstreamAddress(e.target.value)} placeholder="127.0.0.1:25565" />
              </Field>
            </div>
          </Card>

          <Card title={t("admin.servers.loginPresentation")}>
            <div className="form-grid two">
              <Field label={t("admin.servers.field.motd")} hint={t("admin.servers.field.motd.hint")} style={{ gridColumn: "1 / -1" }}>
                <MinecraftMotdPreview
                  value={String(cfg.motd ?? "")}
                  iconUrl={serverIcon}
                  serverName={input.display_name}
                  address={downstreamAddress}
                  placeholder={t("minecraftText.empty")}
                  onClick={() => setMotdOpen(true)}
                  testId="server-motd-preview"
                />
              </Field>
              <Field label={t("admin.servers.field.icon")} hint={t("admin.servers.field.icon.hint")}>
                <div className="server-icon-control">
                  <div className="server-icon-preview">
                    {cfg.server_icon ? <img src={String(cfg.server_icon)} alt="" /> : <Icon name="server" size={24} />}
                  </div>
                  <div className="server-icon-actions">
                    <label className="btn btn--secondary btn--sm">
                      <input
                        type="file"
                        accept="image/png"
                        hidden
                        onChange={(event) => {
                          const file = event.currentTarget.files?.[0];
                          event.currentTarget.value = "";
                          if (file) uploadIconMut.mutate(file);
                        }}
                      />
                      <span>{uploadIconMut.isPending ? t("common.loading") : t("admin.servers.icon.upload")}</span>
                    </label>
                    <Button
                      size="sm"
                      variant="secondary"
                      icon="refresh"
                      disabled={!cfg.server_icon}
                      loading={deleteIconMut.isPending}
                      onClick={() => deleteIconMut.mutate()}
                    >
                      {t("admin.servers.icon.reset")}
                    </Button>
                    {iconError ? <p className="field-error">{iconError}</p> : null}
                  </div>
                </div>
              </Field>
              <Field label={t("admin.servers.col.blueprint")}>
                <Select value={String(cfg.limbo_blueprint_id ?? "")} onChange={(value) => setConfig({ limbo_blueprint_id: value })} options={blueprintOptions} />
              </Field>
            </div>
          </Card>

          <Card title={t("admin.servers.instance")}>
            {node ? (
              <div className="server-instance-card">
                <div>
                  <div className="node-name">
                    <span className="node-ico"><Icon name="server" size={15} /></span>
                    <strong>{node.name}</strong>
                  </div>
                  <p className="muted-cell">
                    {node.plugin_version || "—"}{node.velocity_version ? ` / ${node.velocity_version}` : ""} · {formatRelativeTime(node.last_seen_at)}
                  </p>
                  <p className="muted-cell mono">{node.instance_fingerprint || node.token_fingerprint}</p>
                </div>
                <div className="row-actions">
                  <NodeStatusBadge node={node} />
                  <Button size="sm" variant="danger-soft" icon="close" onClick={() => setDeleteNodeOpen(true)}>{t("admin.nodes.delete")}</Button>
                </div>
              </div>
            ) : (
              <div className="empty-inline">
                <p className="muted-cell">{t("admin.servers.instance.empty")}</p>
                <Button variant="primary" icon="plus" onClick={() => setIssueOpen(true)} data-testid="server-node-issue-open">{t("admin.servers.instance.issue")}</Button>
              </div>
            )}
          </Card>
        </DetailBody>
      </DetailGrid>

      <ConfirmDialog
        open={issueOpen}
        onCancel={() => setIssueOpen(false)}
        onConfirm={() => issueNodeMut.mutate()}
        title={t("admin.servers.instance.issue")}
        body={t("admin.servers.instance.issue.desc")}
        confirmLabel={t("admin.nodes.issueToken.submit")}
        loading={issueNodeMut.isPending}
        testId="dialog-server-node-issue"
      />
      <MiniMessageEditorDialog
        open={motdOpen}
        title={t("admin.servers.motd.editor")}
        desc={t("admin.servers.motd.editor.desc")}
        value={String(cfg.motd ?? "")}
        serverName={input.display_name}
        address={downstreamAddress}
        iconUrl={serverIcon}
        onClose={() => setMotdOpen(false)}
        onSave={(value) => {
          setConfig({ motd: value });
          setMotdOpen(false);
        }}
        testId="dialog-server-motd"
      />
      <Dialog
        open={!!issuedToken}
        onClose={() => setIssuedToken(null)}
        icon="alert"
        iconTone="warning"
        title={t("admin.nodes.secret.heading")}
        desc={t("admin.nodes.secret.body")}
        testId="dialog-server-node-secret"
        footer={<Button variant="primary" onClick={() => setIssuedToken(null)}>{t("admin.nodes.copiedDone")}</Button>}
      >
        {issuedToken ? (
          <>
            <SecretReveal value={issuedToken.token_once} valueTestId="server-node-secret" />
            <p className="dialog-note" style={{ marginTop: 12 }}>
              {t("admin.nodes.nodeLabel")}: <code className="mono">{issuedToken.name}</code> · {t("admin.nodes.col.fingerprint")}: <code className="mono">{issuedToken.token_fingerprint}</code>
            </p>
          </>
        ) : null}
      </Dialog>
      <ConfirmDialog
        open={deleteNodeOpen}
        onCancel={() => setDeleteNodeOpen(false)}
        onConfirm={() => deleteNodeMut.mutate()}
        title={t("admin.nodes.delete")}
        body={t("admin.nodes.delete.desc")}
        confirmLabel={t("admin.nodes.delete")}
        destructive
        loading={deleteNodeMut.isPending}
        testId="dialog-delete-server-node"
      />
      <ConfirmDialog
        open={deleteOpen}
        onCancel={() => setDeleteOpen(false)}
        onConfirm={() => deleteMut.mutate()}
        title={t("admin.servers.delete")}
        body={t("admin.servers.deleteDesc")}
        confirmLabel={t("common.delete")}
        destructive
        loading={deleteMut.isPending}
      />
    </PageShell>
  );
}
