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
  Icon,
  IconButton,
  Input,
  PageHeader,
  PageShell,
  SecretReveal,
  coerceVelocityNode,
  formatRelativeTime,
  useI18n,
  useToast,
  type DataColumn,
  type SafeVelocityNode,
} from "@authman/shared";
import { createNode, deleteNode, fetchNodes } from "../api/admin";

function NodeStatusBadge({ status }: { status: SafeVelocityNode["status"] }) {
  const { t } = useI18n();
  const tone: "success" | "warning" | "neutral" = status === "active" ? "success" : status === "stale" ? "warning" : "neutral";
  return <Badge tone={tone} dot>{t(`admin.nodes.status.${status}`, status)}</Badge>;
}

function NodeModeBadge({ mode }: { mode: SafeVelocityNode["mode"] }) {
  const { t } = useI18n();
  return <Badge tone={mode === "gate" ? "warning" : "info"} dot>{t(`admin.nodes.mode.${mode}`)}</Badge>;
}

function nodeRuntimeSummary(n: SafeVelocityNode, t: (key: string, fallback?: string) => string) {
  const cfg = n.runtime_config ?? {};
  if (n.mode === "gate") {
    const initial = typeof cfg.gate_initial_server === "string" && cfg.gate_initial_server ? cfg.gate_initial_server : "—";
    const holding = typeof cfg.gate_holding_server === "string" && cfg.gate_holding_server ? cfg.gate_holding_server : "—";
    return `${t("admin.nodes.runtime.initial")}: ${initial} · ${t("admin.nodes.runtime.holding")}: ${holding}`;
  }
  const target = typeof cfg.portal_requested_server_id === "string" && cfg.portal_requested_server_id ? cfg.portal_requested_server_id : "default";
  const source = typeof cfg.portal_source_id === "string" && cfg.portal_source_id ? cfg.portal_source_id : n.name;
  return `${t("admin.nodes.runtime.target")}: ${target} · ${t("admin.nodes.runtime.source")}: ${source}`;
}

interface IssuedToken {
  token_once: string;
  token_fingerprint: string;
  name: string;
}

export function NodesPage() {
  const { t, tError } = useI18n();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<SafeVelocityNode | null>(null);
  const [issueOpen, setIssueOpen] = useState(false);
  const [issueName, setIssueName] = useState("");
  const [issuedToken, setIssuedToken] = useState<IssuedToken | null>(null);

  const q = useQuery({
    queryKey: ["admin.nodes"],
    queryFn: fetchNodes,
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

  const issueMut = useMutation({
    mutationFn: (name: string) => createNode({ name }),
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

  const columns: DataColumn<SafeVelocityNode>[] = [
    {
      key: "name",
      header: t("admin.nodes.col.name"),
      render: (n) => (
        <div className="node-name">
          <span className="node-ico">
            <Icon name="server" size={15} />
          </span>
          {n.name}
        </div>
      ),
    },
    { key: "mode", header: t("admin.nodes.col.mode"), render: (n) => <NodeModeBadge mode={n.mode} /> },
    {
      key: "runtime",
      header: t("admin.nodes.col.runtime"),
      render: (n) => <span className="muted-cell">{nodeRuntimeSummary(n, t)}</span>,
    },
    { key: "status", header: t("admin.nodes.col.status"), render: (n) => <NodeStatusBadge status={n.status} /> },
    {
      key: "fingerprint",
      header: t("admin.nodes.col.fingerprint"),
      render: (n) => <code className="mono fingerprint" title={t("admin.nodes.fingerprint.title")}>{n.instance_fingerprint || n.token_fingerprint}</code>,
    },
    {
      key: "version",
      header: t("admin.nodes.col.version"),
      render: (n) => (
        <span className="muted-cell">
          {n.plugin_version || "—"}{n.velocity_version ? ` / ${n.velocity_version}` : ""}
        </span>
      ),
    },
    {
      key: "heartbeat",
      header: t("admin.nodes.col.heartbeat"),
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

  return (
    <PageShell>
      <PageHeader
        title={t("admin.nodes.heading")}
        desc={t("admin.nodes.desc")}
        action={(
          <Button
            variant="primary"
            icon="plus"
            onClick={() => setIssueOpen(true)}
            data-testid="node-issue-open"
          >
            {t("admin.nodes.issueToken")}
          </Button>
        )}
      />
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      <Card noBody className="table-card">
        <DataTable
          loading={q.isLoading}
          rows={rows}
          columns={columns}
          rowKey={(r) => r.id}
          onRowClick={(r) => navigate(`/nodes/${encodeURIComponent(r.id)}`)}
          empty={
            <EmptyState
              icon="server"
              title={t("admin.nodes.empty")}
              description={t("admin.nodes.empty.desc")}
              testId="nodes-empty"
            />
          }
          testId="nodes-table"
        />
        <div className="card-foot-note">
          <Icon name="info" size={13} /> {t("admin.nodes.footnote")}
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
    </PageShell>
  );
}
