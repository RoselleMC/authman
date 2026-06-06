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
import {
  createNode,
  disableNode,
  fetchNodes,
  rotateNodeToken,
} from "../api/admin";
import { useSession } from "../auth/SessionContext";

type DialogState = { kind: "create" | "rotate" | "disable"; node?: SafeVelocityNode } | null;

function NodeStatusBadge({ status }: { status: SafeVelocityNode["status"] }) {
  const { t } = useI18n();
  const tone: "success" | "warning" | "neutral" = status === "active" ? "success" : status === "stale" ? "warning" : "neutral";
  return <Badge tone={tone} dot>{t(`admin.nodes.status.${status}`, status)}</Badge>;
}

export function NodesPage() {
  const { t, tError } = useI18n();
  const { hasPermission } = useSession();
  const toast = useToast();
  const qc = useQueryClient();
  const [dialog, setDialog] = useState<DialogState>(null);
  const [secret, setSecret] = useState<string | null>(null);
  const [newName, setNewName] = useState("");
  const [newServer, setNewServer] = useState("srv-lobby");

  const q = useQuery({
    queryKey: ["admin.nodes"],
    queryFn: fetchNodes,
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
  });

  const rows: SafeVelocityNode[] = (q.data ?? []).map(coerceVelocityNode);

  const createMut = useMutation({
    mutationFn: () => createNode({ name: newName, server_id: newServer }),
    onSuccess: (res) => {
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
      setSecret(res.token_once);
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });
  const rotateMut = useMutation({
    mutationFn: (n: SafeVelocityNode) => rotateNodeToken(n.id),
    onSuccess: (res) => {
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
      setSecret(res.token_once);
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });
  const disableMut = useMutation({
    mutationFn: (n: SafeVelocityNode) => disableNode(n.id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["admin.nodes"] });
      setDialog(null);
      toast.push({ tone: "success", title: t("admin.nodes.disabled.toast") });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  function closeDialog() {
    if (createMut.isPending || rotateMut.isPending || disableMut.isPending) return;
    setDialog(null);
    setSecret(null);
    setNewName("");
  }

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
      render: (n) => <code className="mono fingerprint" title={t("admin.nodes.fingerprint.title")}>{n.token_fingerprint}</code>,
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
          {hasPermission("nodes.rotate") ? (
            <IconButton
              name="rotate"
              size={16}
              label={t("admin.nodes.rotate")}
              onClick={() => setDialog({ kind: "rotate", node: n })}
              data-testid={`rotate-${n.id}`}
            />
          ) : null}
          {hasPermission("nodes.disable") && n.status !== "disabled" ? (
            <IconButton
              name="close"
              size={16}
              label={t("admin.nodes.disable")}
              onClick={() => setDialog({ kind: "disable", node: n })}
              data-testid={`disable-${n.id}`}
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
        action={
          hasPermission("nodes.create") ? (
            <Button variant="primary" icon="plus" onClick={() => setDialog({ kind: "create" })} data-testid="create-node">
              {t("admin.nodes.create")}
            </Button>
          ) : null
        }
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
        open={dialog?.kind === "create"}
        onClose={closeDialog}
        icon="server"
        iconTone="primary"
        title={secret ? t("admin.nodes.created") : t("admin.nodes.create")}
        desc={secret ? t("admin.nodes.secret.body") : t("admin.nodes.generate.desc")}
        testId="dialog-create-node"
        footer={
          secret ? (
            <Button variant="primary" icon="check" onClick={closeDialog} data-testid="secret-close">
              {t("admin.nodes.copiedDone")}
            </Button>
          ) : (
            <>
              <Button variant="ghost" onClick={closeDialog} disabled={createMut.isPending} data-testid="confirm-cancel">
                {t("common.cancel")}
              </Button>
              <Button
                variant="primary"
                icon="plus"
                loading={createMut.isPending}
                onClick={() => createMut.mutate()}
                data-testid="confirm-confirm"
              >
                {t("admin.nodes.create.submit")}
              </Button>
            </>
          )
        }
      >
        {secret ? (
          <SecretReveal
            value={secret}
            valueTestId="node-secret"
            warning={<p>{t("admin.nodes.secret.warning")}</p>}
          />
        ) : (
          <>
            <Field label={t("admin.nodes.field.name")} hint={t("admin.nodes.field.name.hint")}>
              <Input
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="edge-eu-2"
                mono
                data-testid="create-node-name"
              />
            </Field>
            <Field label={t("admin.nodes.field.serverId")} hint={t("admin.nodes.field.serverId.hint")}>
              <Input
                value={newServer}
                onChange={(e) => setNewServer(e.target.value)}
                placeholder="srv-lobby"
                mono
                data-testid="create-node-server"
              />
            </Field>
          </>
        )}
      </Dialog>

      <Dialog
        open={dialog?.kind === "rotate"}
        onClose={closeDialog}
        icon="rotate"
        iconTone="warning"
        title={secret ? t("admin.nodes.newTokenIssued") : t("admin.nodes.rotate")}
        desc={secret ? t("admin.nodes.secret.body") : t("admin.nodes.rotate.desc")}
        testId="dialog-rotate"
        footer={
          secret ? (
            <Button variant="primary" icon="check" onClick={closeDialog} data-testid="secret-close">
              {t("admin.nodes.copiedDone")}
            </Button>
          ) : (
            <>
              <Button variant="ghost" onClick={closeDialog} disabled={rotateMut.isPending} data-testid="confirm-cancel">
                {t("common.cancel")}
              </Button>
              <Button
                variant="primary"
                icon="rotate"
                loading={rotateMut.isPending}
                onClick={() => dialog?.node && rotateMut.mutate(dialog.node)}
                data-testid="confirm-confirm"
              >
                {t("admin.nodes.rotate")}
              </Button>
            </>
          )
        }
      >
        {secret ? (
          <SecretReveal
            value={secret}
            valueTestId="node-secret"
            warning={<p>{t("admin.nodes.secret.warning")}</p>}
          />
        ) : (
          <p className="dialog-note" style={{ marginTop: 0 }}>
            {t("admin.nodes.nodeLabel")}: {dialog?.node?.name}
          </p>
        )}
      </Dialog>

      <Dialog
        open={dialog?.kind === "disable"}
        onClose={closeDialog}
        icon="close"
        iconTone="danger"
        title={t("admin.nodes.disable")}
        desc={t("admin.nodes.confirmDisable.body")}
        testId="dialog-disable"
        footer={
          <>
            <Button variant="ghost" onClick={closeDialog} disabled={disableMut.isPending} data-testid="confirm-cancel">
              {t("common.cancel")}
            </Button>
            <Button
              variant="danger"
              icon="close"
              loading={disableMut.isPending}
              onClick={() => dialog?.node && disableMut.mutate(dialog.node)}
              data-testid="confirm-confirm"
            >
              {t("admin.nodes.disable")}
            </Button>
          </>
        }
      >
        <p className="dialog-note" style={{ marginTop: 0 }}>
          {t("admin.nodes.nodeLabel")}: {dialog?.node?.name} · {dialog?.node?.server_label}
        </p>
      </Dialog>
    </PageShell>
  );
}
