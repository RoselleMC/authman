import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ApiError,
  Badge,
  Button,
  Card,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  Icon,
  IconButton,
  PageHeader,
  PageShell,
  coerceVelocityNode,
  formatRelativeTime,
  useI18n,
  useToast,
  type DataColumn,
  type SafeVelocityNode,
} from "@authman/shared";
import { deleteNode, fetchNodes } from "../api/admin";

function NodeStatusBadge({ status }: { status: SafeVelocityNode["status"] }) {
  const { t } = useI18n();
  const tone: "success" | "warning" | "neutral" = status === "active" ? "success" : status === "stale" ? "warning" : "neutral";
  return <Badge tone={tone} dot>{t(`admin.nodes.status.${status}`, status)}</Badge>;
}

export function NodesPage() {
  const { t, tError } = useI18n();
  const toast = useToast();
  const qc = useQueryClient();
  const [deleteTarget, setDeleteTarget] = useState<SafeVelocityNode | null>(null);

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
    { key: "server", header: t("admin.nodes.col.server"), render: (n) => <span className="muted-cell">{n.server_label}</span> },
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
      render: (n) => (
        <div className="row-actions">
          {n.status !== "active" ? (
            <IconButton
              name="close"
              size={16}
              label={t("admin.nodes.delete")}
              onClick={() => setDeleteTarget(n)}
              data-testid={`delete-${n.id}`}
            />
          ) : null}
        </div>
      ),
    },
  ];

  return (
    <PageShell>
      <PageHeader
        title={t("admin.nodes.heading")}
        desc={t("admin.nodes.desc")}
      />
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      <Card noBody className="table-card">
        <DataTable
          loading={q.isLoading}
          rows={rows}
          columns={columns}
          rowKey={(r) => r.id}
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
