import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AdvancedList,
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
  EmptyState,
  Field,
  Icon,
  IconButton,
  Input,
  PageShell,
  SecretReveal,
  Select,
  StatusBadge,
  Tabs,
  coerceVelocityNode,
  formatRelativeTime,
  useI18n,
  useListState,
  useToast,
  type ListColumn,
  type SafeVelocityNode,
} from "@authman/shared";
import {
  createNode,
  deleteDownstreamServer,
  deleteNode,
  fetchDownstreamServer,
  fetchLimboBlueprints,
  fetchNodes,
  updateDownstreamServer,
  type DownstreamServer,
  type DownstreamServerInput,
} from "../api/admin";

type Tab = "routing" | "portal" | "instances";

interface IssuedToken {
  token_once: string;
  token_fingerprint: string;
  name: string;
}

function toInput(server: DownstreamServer): DownstreamServerInput {
  return {
    slug: server.slug,
    display_name: server.display_name,
    status: server.status,
    registration_open: server.registration_open,
    portal_theme: { ...server.portal_theme },
    portal_config: { ...server.portal_config },
    extension_providers: [...server.extension_providers],
  };
}

function csv(value: string[] | undefined): string {
  return (value ?? []).join(", ");
}

function splitCSV(value: string): string[] {
  return value.split(",").map((item) => item.trim()).filter(Boolean);
}

function NodeStatusBadge({ status }: { status: SafeVelocityNode["status"] }) {
  const { t } = useI18n();
  const tone: "success" | "warning" | "neutral" = status === "active" ? "success" : status === "stale" ? "warning" : "neutral";
  return <Badge tone={tone} dot>{t(`admin.nodes.status.${status}`, status)}</Badge>;
}

function nodeRuntimeSummary(n: SafeVelocityNode, t: (key: string, fallback?: string) => string) {
  const cfg = n.runtime_config ?? {};
  const initial = typeof cfg.downstream_initial_server === "string" && cfg.downstream_initial_server ? cfg.downstream_initial_server : "—";
  const holding = typeof cfg.downstream_holding_server === "string" && cfg.downstream_holding_server ? cfg.downstream_holding_server : "—";
  return `${t("admin.nodes.runtime.initial")}: ${initial} · ${t("admin.nodes.runtime.holding")}: ${holding}`;
}

function nodeBelongsToServer(n: SafeVelocityNode, server: DownstreamServer): boolean {
  const cfg = n.runtime_config ?? {};
  const candidates = [
    n.server_id,
    typeof cfg.server_id === "string" ? cfg.server_id : "",
    typeof cfg.portal_requested_server_id === "string" ? cfg.portal_requested_server_id : "",
  ].filter(Boolean);
  return candidates.includes(server.id) || candidates.includes(server.slug);
}

export function DownstreamServerDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t } = useI18n();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [tab, setTab] = useState<Tab>("routing");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteNodeTarget, setDeleteNodeTarget] = useState<SafeVelocityNode | null>(null);
  const [bulkDeleteNodes, setBulkDeleteNodes] = useState<SafeVelocityNode[]>([]);
  const [issueOpen, setIssueOpen] = useState(false);
  const [issueName, setIssueName] = useState("");
  const [issuedToken, setIssuedToken] = useState<IssuedToken | null>(null);
  const q = useQuery({ queryKey: ["admin.downstreamServer", id], queryFn: () => fetchDownstreamServer(id), enabled: !!id });
  const blueprints = useQuery({ queryKey: ["admin.limboBlueprints"], queryFn: fetchLimboBlueprints });
  const nodesQ = useQuery({
    queryKey: ["admin.nodes", "downstream_velocity"],
    queryFn: () => fetchNodes("downstream_velocity"),
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  });
  const server = q.data;
  const nodeList = useListState({ urlPrefix: "serverNodes", urlSync: false, defaults: { pageSize: 10, hidden: ["fingerprint"] } });
  const [input, setInput] = useState<DownstreamServerInput | null>(null);
  const [portalHosts, setPortalHosts] = useState("");
  const [allowedSources, setAllowedSources] = useState("");

  useEffect(() => {
    if (!server) return;
    setInput(toInput(server));
    setPortalHosts(csv(server.portal_config.portal_hosts));
    setAllowedSources(csv(server.portal_config.allowed_portal_sources));
  }, [server]);

  const blueprintOptions = useMemo(() => [
    { value: "", label: t("admin.servers.defaultWorld") },
    ...(blueprints.data ?? []).map((bp) => ({ value: bp.id, label: bp.name })),
  ], [blueprints.data, t]);
  const nodes = useMemo(() => {
    if (!server) return [];
    return (nodesQ.data ?? []).map(coerceVelocityNode).filter((n) => nodeBelongsToServer(n, server));
  }, [nodesQ.data, server]);

  const updateMut = useMutation({
    mutationFn: () => {
      if (!input) throw new Error("server input missing");
      return updateDownstreamServer(id, {
        ...input,
        portal_config: {
          ...input.portal_config,
          portal_hosts: splitCSV(portalHosts),
          allowed_portal_sources: splitCSV(allowedSources),
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
  const deleteMut = useMutation({
    mutationFn: () => deleteDownstreamServer(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.deleted") });
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServers"] });
      navigate("/nodes");
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const deleteNodeMut = useMutation({
    mutationFn: (n: SafeVelocityNode) => deleteNode(n.id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.nodes.delete.toast") });
      setDeleteNodeTarget(null);
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? err.message : t("common.unknown")),
  });
  const bulkDeleteNodeMut = useMutation({
    mutationFn: async (rows: SafeVelocityNode[]) => {
      await Promise.all(rows.map((row) => deleteNode(row.id)));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.nodes.delete.toast") });
      setBulkDeleteNodes([]);
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? err.message : t("common.unknown")),
  });
  const issueNodeMut = useMutation({
    mutationFn: (name: string) => createNode({ name, kind: "downstream_velocity" }),
    onSuccess: (res, name) => {
      setIssuedToken({ token_once: res.token_once, token_fingerprint: res.token_fingerprint, name });
      setIssueOpen(false);
      setIssueName("");
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? err.message : t("common.unknown")),
  });

  if (!server || !input) return <PageShell><BackLink onClick={() => navigate("/nodes")}>{t("admin.servers.heading")}</BackLink><Card title={q.isLoading ? t("common.loading") : t("common.unknown")}><span /></Card></PageShell>;

  const cfg = input.portal_config;
  function setConfig(next: Partial<DownstreamServerInput["portal_config"]>) {
    setInput((current) => current ? { ...current, portal_config: { ...current.portal_config, ...next } } : current);
  }
  const nodeColumns: ListColumn<SafeVelocityNode>[] = [
    {
      key: "name",
      header: t("admin.nodes.col.name"),
      mandatory: true,
      sortable: true,
      sortValue: (n) => n.name,
      filter: { type: "text" },
      render: (n) => (
        <div className="node-name">
          <span className="node-ico"><Icon name="server" size={15} /></span>
          {n.name}
        </div>
      ),
    },
    {
      key: "status",
      header: t("admin.nodes.col.status"),
      sortable: true,
      sortValue: (n) => n.status,
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "active", label: t("admin.nodes.status.active") },
          { value: "stale", label: t("admin.nodes.status.stale") },
          { value: "offline", label: t("status.offline") },
        ],
      },
      render: (n) => <NodeStatusBadge status={n.status} />,
    },
    { key: "runtime", header: t("admin.nodes.col.runtime"), minWidth: "220px", render: (n) => <span className="muted-cell">{nodeRuntimeSummary(n, t)}</span> },
    {
      key: "fingerprint",
      header: t("admin.nodes.col.fingerprint"),
      minWidth: "170px",
      render: (n) => <code className="mono fingerprint" title={t("admin.nodes.fingerprint.title")}>{n.instance_fingerprint || n.token_fingerprint}</code>,
    },
    {
      key: "version",
      header: t("admin.nodes.col.version"),
      minWidth: "150px",
      sortable: true,
      sortValue: (n) => n.plugin_version ?? "",
      render: (n) => <span className="muted-cell">{n.plugin_version || "—"}{n.velocity_version ? ` / ${n.velocity_version}` : ""}</span>,
    },
    {
      key: "heartbeat",
      header: t("admin.nodes.col.heartbeat"),
      sortable: true,
      sortValue: (n) => n.last_seen_at ?? "",
      render: (n) => <span className="muted-cell">{formatRelativeTime(n.last_seen_at)}</span>,
    },
    {
      key: "actions",
      header: "",
      align: "right",
      width: "52px",
      minWidth: "52px",
      sticky: "right",
      render: (n) => (
        <div className="row-actions">
          <IconButton
            name="close"
            size={16}
            label={t("admin.nodes.delete")}
            onClick={(event) => {
              event.stopPropagation();
              setDeleteNodeTarget(n);
            }}
            data-testid={`delete-node-${n.id}`}
          />
        </div>
      ),
    },
  ];

  return (
    <PageShell testId="downstream-server-detail-page">
      <div className="detail-toolbar">
        <BackLink onClick={() => navigate("/nodes")}>{t("admin.servers.heading")}</BackLink>
        <Tabs<Tab> value={tab} onChange={setTab} tabs={[
          { value: "routing", label: t("admin.servers.routing"), icon: "server" },
          { value: "portal", label: t("admin.servers.portal"), icon: "box" },
          { value: "instances", label: t("admin.servers.instances"), icon: "layers" },
        ]} />
      </div>
      <DetailGrid>
        <DetailAside>
          <DetailSummary
            title={input.display_name}
            icon="server"
            titleMeta={<StatusBadge status={input.status} />}
            meta={<><span className="muted-cell">{t("admin.servers.col.slug")}</span><strong className="mono">{input.slug}</strong></>}
          >
            <div className="id-uuid">
              <span className="id-uuid-label">{t("admin.servers.col.host")}</span>
              <strong className="mono">{cfg.transfer_host}:{cfg.transfer_port}</strong>
            </div>
          </DetailSummary>
          <DetailActions title={t("common.actions")}>
            <Button variant="primary" icon="check" block loading={updateMut.isPending} onClick={() => updateMut.mutate()}>{t("common.save")}</Button>
            {server.id !== "default" ? <Button variant="danger-soft" icon="trash" block onClick={() => setDeleteOpen(true)}>{t("common.delete")}</Button> : null}
          </DetailActions>
        </DetailAside>
        <DetailBody>
          {tab === "routing" ? (
            <>
              <Card title={t("admin.servers.identity")}>
                <div className="form-grid two">
                  <Field label={t("admin.servers.col.slug")}><Input value={input.slug} onChange={(e) => setInput({ ...input, slug: e.target.value })} disabled={server.id === "default"} /></Field>
                  <Field label={t("admin.servers.col.name")}><Input value={input.display_name} onChange={(e) => setInput({ ...input, display_name: e.target.value })} /></Field>
                  <Field label={t("admin.servers.col.status")}>
                    <Select value={input.status} onChange={(status) => setInput({ ...input, status })} options={[{ value: "active", label: t("status.active") }, { value: "hidden", label: t("status.hidden") }, { value: "disabled", label: t("status.disabled") }]} />
                  </Field>
                </div>
              </Card>
              <Card title={t("admin.servers.routing")}>
                <div className="form-grid two">
                  <Field label={t("admin.servers.field.host")}><Input value={String(cfg.host ?? "")} onChange={(e) => setConfig({ host: e.target.value })} /></Field>
                  <Field label={t("admin.servers.field.port")}><Input type="number" value={cfg.port ?? 25565} onChange={(e) => setConfig({ port: Number(e.target.value) })} /></Field>
                  <Field label={t("admin.servers.field.transferHost")}><Input value={String(cfg.transfer_host ?? "")} onChange={(e) => setConfig({ transfer_host: e.target.value })} /></Field>
                  <Field label={t("admin.servers.field.transferPort")}><Input type="number" value={cfg.transfer_port ?? 25565} onChange={(e) => setConfig({ transfer_port: Number(e.target.value) })} /></Field>
                  <Field label={t("admin.servers.field.portalHosts")} hint={t("admin.servers.csvHint")} style={{ gridColumn: "1 / -1" }}><Input value={portalHosts} onChange={(e) => setPortalHosts(e.target.value)} /></Field>
                </div>
              </Card>
            </>
          ) : tab === "portal" ? (
            <>
              <Card title={t("admin.servers.portal")}>
                <div className="form-grid two">
                  <Field label={t("admin.servers.field.motd")}><Input value={String(cfg.motd ?? "")} onChange={(e) => setConfig({ motd: e.target.value })} /></Field>
                  <Field label={t("admin.servers.field.grantTtl")}><Input type="number" value={cfg.grant_ttl_seconds ?? 45} onChange={(e) => setConfig({ grant_ttl_seconds: Number(e.target.value) })} /></Field>
                  <Field label={t("admin.servers.col.blueprint")}>
                    <Select value={String(cfg.limbo_blueprint_id ?? "")} onChange={(value) => setConfig({ limbo_blueprint_id: value })} options={blueprintOptions} />
                  </Field>
                  <Field label={t("admin.servers.field.allowedSources")} hint={t("admin.servers.csvHint")} style={{ gridColumn: "1 / -1" }}><Input value={allowedSources} onChange={(e) => setAllowedSources(e.target.value)} /></Field>
                </div>
              </Card>
              <Card title={t("admin.servers.portalTheme")}>
                <div className="form-grid two">
                  <Field label={t("admin.servers.field.portalDisplayName")}><Input value={String(input.portal_theme.display_name ?? "")} onChange={(e) => setInput({ ...input, portal_theme: { ...input.portal_theme, display_name: e.target.value } })} /></Field>
                  <Field label={t("admin.servers.field.portalMessage")}><Input value={String(input.portal_theme.portal_message ?? "")} onChange={(e) => setInput({ ...input, portal_theme: { ...input.portal_theme, portal_message: e.target.value } })} /></Field>
                  <Field label={t("admin.servers.field.primaryColor")}><Input type="color" value={String(input.portal_theme.primary_color ?? "#16a34a")} onChange={(e) => setInput({ ...input, portal_theme: { ...input.portal_theme, primary_color: e.target.value } })} /></Field>
                  <Field label={t("admin.servers.field.accentColor")}><Input type="color" value={String(input.portal_theme.accent_color ?? "#2563eb")} onChange={(e) => setInput({ ...input, portal_theme: { ...input.portal_theme, accent_color: e.target.value } })} /></Field>
                </div>
              </Card>
            </>
          ) : (
            <>
              <Card
                title={t("admin.servers.instances")}
                noBody
                className="table-card"
              >
                <AdvancedList
                  loading={nodesQ.isLoading}
                  rows={nodes}
                  columns={nodeColumns}
                  rowKey={(r) => r.id}
                  state={nodeList.state}
                  onStateChange={nodeList.setState}
                  onRowClick={(r) => navigate(`/nodes/${encodeURIComponent(r.id)}`)}
                  primaryActions={<Button variant="primary" icon="plus" onClick={() => setIssueOpen(true)} data-testid="server-node-issue-open">{t("admin.nodes.issueToken")}</Button>}
                  selectable
                  selectionActions={(selectedRows) => (
                    <Button size="sm" variant="danger-soft" icon="close" onClick={() => setBulkDeleteNodes(selectedRows)}>
                      {t("admin.nodes.delete")}
                    </Button>
                  )}
                  empty={(
                    <EmptyState
                      icon="server"
                      title={t("admin.servers.instances.empty")}
                      description={t("admin.servers.instances.empty.desc")}
                    />
                  )}
                  testId="server-node-instances"
                />
                <div className="card-foot-note">
                  <Icon name="info" size={13} /> {t("admin.servers.instances.footnote").replace("{server}", server.id)}
                </div>
              </Card>
            </>
          )}
        </DetailBody>
      </DetailGrid>
      <Dialog
        open={issueOpen}
        onClose={() => !issueNodeMut.isPending && setIssueOpen(false)}
        icon="plus"
        iconTone="primary"
        title={t("admin.servers.instances.issueToken")}
        desc={t("admin.servers.instances.issueToken.desc").replace("{server}", server.id)}
        testId="dialog-server-node-issue"
        footer={(
          <>
            <Button variant="ghost" onClick={() => setIssueOpen(false)} disabled={issueNodeMut.isPending}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              icon="check"
              loading={issueNodeMut.isPending}
              disabled={!issueName.trim() || issueNodeMut.isPending}
              onClick={() => issueNodeMut.mutate(issueName.trim())}
              data-testid="server-node-issue-submit"
            >
              {t("admin.nodes.issueToken.submit")}
            </Button>
          </>
        )}
      >
        <Field label={t("admin.nodes.field.name")} hint={t("admin.nodes.field.name.hint")}>
          <Input
            value={issueName}
            onChange={(e) => setIssueName(e.target.value)}
            placeholder={`${server.slug}-velocity-1`}
            mono
            data-testid="server-node-issue-name"
          />
        </Field>
        <p className="dialog-note">
          {t("admin.nodes.field.serverId")}: <code className="mono">{server.id}</code>
        </p>
      </Dialog>

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

      <Dialog
        open={!!deleteNodeTarget}
        onClose={() => !deleteNodeMut.isPending && setDeleteNodeTarget(null)}
        icon="close"
        iconTone="danger"
        title={t("admin.nodes.delete")}
        desc={t("admin.nodes.delete.desc")}
        testId="dialog-delete-server-node"
        footer={(
          <>
            <Button variant="ghost" onClick={() => setDeleteNodeTarget(null)} disabled={deleteNodeMut.isPending}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              icon="close"
              loading={deleteNodeMut.isPending}
              onClick={() => deleteNodeTarget && deleteNodeMut.mutate(deleteNodeTarget)}
            >
              {t("admin.nodes.delete")}
            </Button>
          </>
        )}
      >
        <p className="dialog-note" style={{ marginTop: 0 }}>
          {deleteNodeTarget?.name} · {deleteNodeTarget?.instance_fingerprint || deleteNodeTarget?.token_fingerprint}
        </p>
      </Dialog>
      <ConfirmDialog
        open={bulkDeleteNodes.length > 0}
        onCancel={() => setBulkDeleteNodes([])}
        onConfirm={() => bulkDeleteNodeMut.mutate(bulkDeleteNodes)}
        title={t("admin.nodes.delete")}
        body={t("admin.nodes.delete.desc")}
        confirmLabel={t("admin.nodes.delete")}
        destructive
        loading={bulkDeleteNodeMut.isPending}
        testId="dialog-bulk-delete-server-node"
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
