import { useState } from "react";
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
  SecretReveal,
  coerceVelocityNode,
  formatRelativeTime,
  useI18n,
  useListState,
  useToast,
  type ListColumn,
  type SafeVelocityNode,
} from "@authman/shared";
import { createNode, deleteNode, fetchNodes } from "../api/admin";
import { useSession } from "../auth/SessionContext";

function NodeStatusBadge({ status }: { status: SafeVelocityNode["status"] }) {
  const { t } = useI18n();
  const tone: "success" | "warning" | "neutral" = status === "active" ? "success" : status === "stale" ? "warning" : "neutral";
  return <Badge tone={tone} dot>{t(`admin.nodes.status.${status}`, status)}</Badge>;
}

function NodeModeBadge({ mode }: { mode: SafeVelocityNode["mode"] }) {
  const { t } = useI18n();
  return <Badge tone={mode === "downstream_velocity" ? "warning" : "info"} dot>{t(`admin.nodes.mode.${mode}`)}</Badge>;
}

function nodeRuntimeSummary(n: SafeVelocityNode, t: (key: string, fallback?: string) => string) {
  const cfg = n.runtime_config ?? {};
  if (n.kind === "downstream_velocity") {
    const initial = typeof cfg.downstream_initial_server === "string" && cfg.downstream_initial_server ? cfg.downstream_initial_server : "—";
    const holding = typeof cfg.downstream_holding_server === "string" && cfg.downstream_holding_server ? cfg.downstream_holding_server : "—";
    return `${t("admin.nodes.runtime.initial")}: ${initial} · ${t("admin.nodes.runtime.holding")}: ${holding}`;
  }
  const target = typeof cfg.portal_requested_server_id === "string" && cfg.portal_requested_server_id ? cfg.portal_requested_server_id : "default";
  const source = typeof cfg.portal_source_id === "string" && cfg.portal_source_id ? cfg.portal_source_id : n.name;
  return `${t("admin.nodes.runtime.target")}: ${target} · ${t("admin.nodes.runtime.source")}: ${source}`;
}

function configString(cfg: Record<string, unknown>, key: string): string {
  const value = cfg[key];
  return typeof value === "string" ? value.trim() : "";
}

function configBool(cfg: Record<string, unknown>, key: string, fallback: boolean): boolean {
  const value = cfg[key];
  return typeof value === "boolean" ? value : fallback;
}

function LimboAuthBadges({ node }: { node: SafeVelocityNode }) {
  const { t } = useI18n();
  const cfg = node.runtime_config ?? {};
  return (
    <div className="row-actions" style={{ justifyContent: "flex-start" }}>
      <Badge tone={configBool(cfg, "dialog_enabled", true) ? "success" : "neutral"} dot>
        {t("admin.loginPortals.auth.dialog")}
      </Badge>
      <Badge tone={configBool(cfg, "dialog_fallback_chat_enabled", true) ? "info" : "neutral"} dot>
        {t("admin.loginPortals.auth.chat")}
      </Badge>
    </div>
  );
}

interface IssuedToken {
  token_once: string;
  token_fingerprint: string;
  name: string;
}

export function NodesPage({ kind, embedded = false }: { kind: "limbo_portal" | "downstream_velocity"; embedded?: boolean }) {
  const { t, tError } = useI18n();
  const { user } = useSession();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<SafeVelocityNode | null>(null);
  const [bulkDeleteRows, setBulkDeleteRows] = useState<SafeVelocityNode[]>([]);
  const [issueOpen, setIssueOpen] = useState(false);
  const [issueName, setIssueName] = useState("");
  const [issuedToken, setIssuedToken] = useState<IssuedToken | null>(null);
  const list = useListState({
    urlPrefix: kind === "limbo_portal" ? "lpn" : "dsn",
    defaults: { pageSize: 25, hidden: kind === "limbo_portal" ? ["source"] : [] },
    storageScope: user?.id,
  });

  const q = useQuery({
    queryKey: ["admin.nodes", kind],
    queryFn: () => fetchNodes(kind),
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  });

  const rows: SafeVelocityNode[] = (q.data ?? []).map(coerceVelocityNode);

  const deleteMut = useMutation({
    mutationFn: (n: SafeVelocityNode) => deleteNode(n.id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.nodes.delete.toast") });
      setDeleteTarget(null);
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const bulkDeleteMut = useMutation({
    mutationFn: async (rows: SafeVelocityNode[]) => {
      await Promise.all(rows.map((row) => deleteNode(row.id)));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.nodes.delete.toast") });
      setBulkDeleteRows([]);
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const issueMut = useMutation({
    mutationFn: (name: string) => createNode({ name, kind }),
    onSuccess: (res, name) => {
      setIssuedToken({
        token_once: res.token_once,
        token_fingerprint: res.token_fingerprint,
        name,
      });
      setIssueOpen(false);
      setIssueName("");
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const downstreamColumns: ListColumn<SafeVelocityNode>[] = [
    {
      key: "name",
      header: t("admin.nodes.col.name"),
      mandatory: true,
      sortable: true,
      sortValue: (n) => n.name,
      filter: { type: "text" },
      render: (n) => (
        <div className="node-name">
          <span className="node-ico">
            <Icon name="server" size={15} />
          </span>
          {n.name}
        </div>
      ),
    },
    { key: "mode", header: t("admin.nodes.col.mode"), sortable: true, sortValue: (n) => n.mode, render: (n) => <NodeModeBadge mode={n.mode} /> },
    {
      key: "runtime",
      header: t("admin.nodes.col.runtime"),
      minWidth: "220px",
      render: (n) => <span className="muted-cell">{nodeRuntimeSummary(n, t)}</span>,
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
    {
      key: "fingerprint",
      header: t("admin.nodes.col.fingerprint"),
      minWidth: "180px",
      render: (n) => <code className="mono fingerprint" title={t("admin.nodes.fingerprint.title")}>{n.instance_fingerprint || n.token_fingerprint}</code>,
    },
    {
      key: "version",
      header: t("admin.nodes.col.version"),
      minWidth: "150px",
      render: (n) => (
        <span className="muted-cell">
          {n.plugin_version || "—"}{n.velocity_version ? ` / ${n.velocity_version}` : ""}
        </span>
      ),
    },
    {
      key: "heartbeat",
      header: t("admin.nodes.col.heartbeat"),
      minWidth: "140px",
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
              setDeleteTarget(n);
            }}
            data-testid={`delete-${n.id}`}
          />
        </div>
      ),
    },
  ];
  const limboColumns: ListColumn<SafeVelocityNode>[] = [
    {
      key: "name",
      header: t("admin.nodes.col.name"),
      mandatory: true,
      minWidth: "180px",
      sortable: true,
      sortValue: (n) => n.name,
      filter: { type: "text" },
      render: (n) => (
        <div className="node-name">
          <span className="node-ico">
            <Icon name="layers" size={15} />
          </span>
          {n.name}
        </div>
      ),
    },
    {
      key: "status",
      header: t("admin.nodes.col.status"),
      minWidth: "110px",
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
    {
      key: "target",
      header: t("admin.loginPortals.col.target"),
      minWidth: "180px",
      sortable: true,
      sortValue: (n) => n.server_label || configString(n.runtime_config, "portal_requested_server_id") || n.server_id || "",
      render: (n) => (
        <span>
          {n.server_label && n.server_label !== "—" ? n.server_label : configString(n.runtime_config, "portal_requested_server_id") || n.server_id || "—"}
          <br />
          <code className="mono muted-cell">{configString(n.runtime_config, "portal_requested_server_id") || n.server_id || "—"}</code>
        </span>
      ),
    },
    {
      key: "host",
      header: t("admin.loginPortals.col.host"),
      minWidth: "180px",
      sortable: true,
      sortValue: (n) => configString(n.runtime_config, "portal_requested_host"),
      render: (n) => <code className="mono">{configString(n.runtime_config, "portal_requested_host") || t("common.all")}</code>,
    },
    {
      key: "source",
      header: t("admin.loginPortals.col.source"),
      minWidth: "150px",
      defaultVisible: false,
      render: (n) => <code className="mono">{configString(n.runtime_config, "portal_source_id") || n.name}</code>,
    },
    {
      key: "auth",
      header: t("admin.loginPortals.col.auth"),
      minWidth: "150px",
      render: (n) => <LimboAuthBadges node={n} />,
    },
    {
      key: "version",
      header: t("admin.loginPortals.col.version"),
      minWidth: "130px",
      sortable: true,
      sortValue: (n) => n.plugin_version ?? "",
      render: (n) => <span className="muted-cell">{n.plugin_version || "—"}</span>,
    },
    {
      key: "heartbeat",
      header: t("admin.nodes.col.heartbeat"),
      minWidth: "130px",
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
              setDeleteTarget(n);
            }}
            data-testid={`delete-${n.id}`}
          />
        </div>
      ),
    },
  ];
  const columns = kind === "limbo_portal" ? limboColumns : downstreamColumns;
  const issueButton = (
    <Button
      variant="primary"
      icon="plus"
      onClick={() => setIssueOpen(true)}
      data-testid="node-issue-open"
    >
      {t("admin.nodes.issueToken")}
    </Button>
  );

  const content = (
    <>
      {embedded ? (
        <div className="section-toolbar">
          <span />
          {issueButton}
        </div>
      ) : (
        <PageHeader
          title={kind === "limbo_portal" ? t("admin.loginPortals.heading") : t("admin.nodes.heading")}
          desc={kind === "limbo_portal" ? t("admin.loginPortals.desc") : t("admin.nodes.desc")}
          action={issueButton}
        />
      )}
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      <Card noBody className="table-card">
        <AdvancedList
          loading={q.isLoading}
          rows={rows}
          columns={columns}
          rowKey={(r) => r.id}
          state={list.state}
          onStateChange={list.setState}
          onRowClick={(r) => navigate(`${kind === "limbo_portal" ? "/login-portals" : "/nodes"}/${encodeURIComponent(r.id)}`)}
          selectable
          selectionActions={(selectedRows) => (
            <Button size="sm" variant="danger-soft" icon="close" onClick={() => setBulkDeleteRows(selectedRows)}>
              {t("admin.nodes.delete")}
            </Button>
          )}
          empty={
            <EmptyState
              icon="server"
              title={kind === "limbo_portal" ? t("admin.loginPortals.empty") : t("admin.nodes.empty")}
              description={kind === "limbo_portal" ? t("admin.loginPortals.empty.desc") : t("admin.nodes.empty.desc")}
              testId="nodes-empty"
            />
          }
          testId="nodes-table"
        />
        <div className="card-foot-note">
          <Icon name="info" size={13} /> {kind === "limbo_portal" ? t("admin.loginPortals.footnote") : t("admin.nodes.footnote")}
        </div>
      </Card>
      <Dialog
        open={issueOpen}
        onClose={() => !issueMut.isPending && setIssueOpen(false)}
        icon="plus"
        iconTone="primary"
        title={t("admin.nodes.issueToken")}
        desc={t("admin.nodes.issueToken.desc")}
        testId="dialog-node-issue"
        footer={(
          <>
            <Button variant="ghost" onClick={() => setIssueOpen(false)} disabled={issueMut.isPending}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              icon="check"
              loading={issueMut.isPending}
              disabled={!issueName.trim() || issueMut.isPending}
              onClick={() => issueMut.mutate(issueName.trim())}
              data-testid="node-issue-submit"
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
            placeholder="edge-eu-1"
            mono
            data-testid="node-issue-name"
          />
        </Field>
      </Dialog>

      <Dialog
        open={!!issuedToken}
        onClose={() => setIssuedToken(null)}
        icon="alert"
        iconTone="warning"
        title={t("admin.nodes.secret.heading")}
        desc={t("admin.nodes.secret.body")}
        testId="dialog-node-secret"
        footer={(
          <Button variant="primary" onClick={() => setIssuedToken(null)} data-testid="secret-close">
            {t("admin.nodes.copiedDone")}
          </Button>
        )}
      >
        {issuedToken ? (
          <>
            <SecretReveal value={issuedToken.token_once} valueTestId="node-secret" />
            <p className="dialog-note" style={{ marginTop: 12 }}>
              {t("admin.nodes.nodeLabel")}: <code className="mono">{issuedToken.name}</code> · {t("admin.nodes.col.fingerprint")}: <code className="mono">{issuedToken.token_fingerprint}</code>
            </p>
          </>
        ) : null}
      </Dialog>

      <Dialog
        open={!!deleteTarget}
        onClose={() => !deleteMut.isPending && setDeleteTarget(null)}
        icon="close"
        iconTone="danger"
        title={t("admin.nodes.delete")}
        desc={t("admin.nodes.delete.desc")}
        testId="dialog-delete-node"
        footer={
          <>
            <Button variant="ghost" onClick={() => setDeleteTarget(null)} disabled={deleteMut.isPending} data-testid="confirm-cancel">
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              icon="close"
              loading={deleteMut.isPending}
              onClick={() => deleteTarget && deleteMut.mutate(deleteTarget)}
              data-testid="confirm-confirm"
            >
              {t("admin.nodes.delete")}
            </Button>
          </>
        }
      >
        <p className="dialog-note" style={{ marginTop: 0 }}>
          {deleteTarget?.name} · {deleteTarget?.instance_fingerprint || deleteTarget?.token_fingerprint}
        </p>
      </Dialog>

      <ConfirmDialog
        open={bulkDeleteRows.length > 0}
        onCancel={() => setBulkDeleteRows([])}
        onConfirm={() => bulkDeleteMut.mutate(bulkDeleteRows)}
        title={t("admin.nodes.delete")}
        body={t("admin.nodes.delete.desc")}
        confirmLabel={t("admin.nodes.delete")}
        destructive
        loading={bulkDeleteMut.isPending}
        testId="dialog-bulk-delete-node"
      />
    </>
  );

  return embedded ? content : <PageShell>{content}</PageShell>;
}
