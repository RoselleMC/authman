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
  Field,
  Icon,
  IconButton,
  Input,
  useI18n,
  useToast,
  type DataColumn,
} from "@authman/shared";
import { createDownstreamServer, deleteDownstreamServer, fetchDownstreamServers, type DownstreamServer, type DownstreamServerInput } from "../api/admin";
import { PageHeader } from "../layout/AdminShell";
import { ErrorBlock } from "../components/ErrorBlock";

function RegBadge({ open }: { open: boolean }) {
  const { t } = useI18n();
  return open ? (
    <Badge tone="success" dot>{t("common.open")}</Badge>
  ) : (
    <Badge tone="neutral" dot>{t("common.closed")}</Badge>
  );
}

export function ServersPage() {
  const { t, tError } = useI18n();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<DownstreamServer | null>(null);
  const q = useQuery({ queryKey: ["admin.servers"], queryFn: fetchDownstreamServers });

  const createMut = useMutation({
    mutationFn: createDownstreamServer,
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.servers.created.toast") });
      setCreateOpen(false);
      void qc.invalidateQueries({ queryKey: ["admin.servers"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });
  const deleteMut = useMutation({
    mutationFn: (server: DownstreamServer) => deleteDownstreamServer(server.id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.servers.deleted.toast") });
      setDeleteTarget(null);
      void qc.invalidateQueries({ queryKey: ["admin.servers"] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  const columns: DataColumn<DownstreamServer>[] = [
    {
      key: "name",
      header: t("admin.servers.col.name"),
      render: (s) => (
        <div className="node-name">
          <span
            className="ctx-badge"
            style={{ background: s.portal_theme.primary_color ?? "var(--color-primary)", width: 26, height: 26, borderRadius: 6, fontSize: 12 }}
          >
            {s.display_name[0]}
          </span>
          {s.display_name}
        </div>
      ),
    },
    {
      key: "slug",
      header: t("admin.servers.col.slug"),
      render: (s) => <code className="mono" style={{ color: "var(--color-text-muted)", fontSize: 12.5 }}>{s.slug}</code>,
    },
    {
      key: "registration",
      header: t("admin.servers.col.registration"),
      render: (s) => <RegBadge open={s.registration_open} />,
    },
    {
      key: "extension",
      header: t("admin.servers.col.extension"),
      render: (s) => (s.extension_providers.length ? <Badge tone="info">{t("common.available")}</Badge> : <span className="muted-cell">—</span>),
    },
    {
      key: "actions",
      header: "",
      align: "right",
      render: (s) => (
        <div className="row-actions" onClick={(e) => e.stopPropagation()}>
          {s.id !== "default" ? (
            <IconButton name="close" size={16} label={t("admin.servers.delete")} onClick={() => setDeleteTarget(s)} />
          ) : null}
          <Icon name="chevronRight" size={16} style={{ color: "var(--color-text-subtle)" }} />
        </div>
      ),
    },
  ];

  return (
    <div className="page">
      <PageHeader
        title={t("admin.servers.heading")}
        desc={t("admin.servers.desc")}
        action={(
          <Button variant="primary" icon="plus" onClick={() => setCreateOpen(true)} data-testid="server-add">
            {t("admin.servers.create")}
          </Button>
        )}
      />
      {q.error ? <ErrorBlock error={q.error} onRetry={() => q.refetch()} /> : null}
      <Card noBody className="table-card">
        <DataTable
          loading={q.isLoading}
          rows={q.data ?? []}
          columns={columns}
          rowKey={(r) => r.id}
          onRowClick={(r) => navigate(`/servers/${r.id}`)}
          empty={<EmptyState icon="layers" title={t("admin.servers.empty")} />}
          testId="servers-table"
        />
      </Card>
      <ServerFormDialog
        open={createOpen}
        title={t("admin.servers.create")}
        loading={createMut.isPending}
        onClose={() => !createMut.isPending && setCreateOpen(false)}
        onSubmit={(input) => createMut.mutate(input)}
      />
      <Dialog
        open={!!deleteTarget}
        onClose={() => !deleteMut.isPending && setDeleteTarget(null)}
        icon="close"
        iconTone="danger"
        title={t("admin.servers.delete")}
        desc={t("admin.servers.delete.desc")}
        testId="dialog-delete-server"
        footer={(
          <>
            <Button variant="ghost" onClick={() => setDeleteTarget(null)} disabled={deleteMut.isPending}>
              {t("common.cancel")}
            </Button>
            <Button variant="danger" icon="close" loading={deleteMut.isPending} onClick={() => deleteTarget && deleteMut.mutate(deleteTarget)}>
              {t("admin.servers.delete")}
            </Button>
          </>
        )}
      >
        <p className="dialog-note" style={{ marginTop: 0 }}>{deleteTarget?.display_name} · {deleteTarget?.slug}</p>
      </Dialog>
    </div>
  );
}

function ServerFormDialog({
  open,
  title,
  loading,
  onClose,
  onSubmit,
}: {
  open: boolean;
  title: string;
  loading: boolean;
  onClose: () => void;
  onSubmit: (input: DownstreamServerInput) => void;
}) {
  const { t } = useI18n();
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [registrationOpen, setRegistrationOpen] = useState(true);
  const [primary, setPrimary] = useState("#16a34a");
  const [accent, setAccent] = useState("#2563eb");
  const [message, setMessage] = useState("");

  function submit() {
    onSubmit({
      slug,
      display_name: displayName,
      status: "active",
      registration_open: registrationOpen,
      portal_theme: {
        primary_color: primary,
        accent_color: accent,
        portal_message: message,
        display_name: displayName,
      },
      portal_config: {
        registration_strategy: registrationOpen ? "open" : "closed",
        show_in_global: true,
      },
      extension_providers: ["authman.identity"],
    });
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      icon="layers"
      iconTone="primary"
      title={title}
      desc={t("admin.servers.create.desc")}
      testId="dialog-server-form"
      footer={(
        <>
          <Button variant="ghost" onClick={onClose} disabled={loading}>{t("common.cancel")}</Button>
          <Button variant="primary" icon="check" loading={loading} onClick={submit}>{t("common.save")}</Button>
        </>
      )}
    >
      <div className="form-grid">
        <Field label={t("admin.servers.field.slug")}>
          <Input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder="survival" mono data-testid="server-slug" />
        </Field>
        <Field label={t("admin.servers.field.displayName")}>
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="Survival" data-testid="server-name" />
        </Field>
        <Field label={t("admin.servers.field.primary")}>
          <Input value={primary} onChange={(e) => setPrimary(e.target.value)} mono />
        </Field>
        <Field label={t("admin.servers.field.accent")}>
          <Input value={accent} onChange={(e) => setAccent(e.target.value)} mono />
        </Field>
        <Field label={t("admin.servers.field.message")}>
          <Input value={message} onChange={(e) => setMessage(e.target.value)} placeholder={t("admin.servers.field.message.placeholder")} />
        </Field>
        <label className="check-row">
          <input type="checkbox" checked={registrationOpen} onChange={(e) => setRegistrationOpen(e.target.checked)} />
          <span>{t("admin.servers.field.registrationOpen")}</span>
        </label>
      </div>
    </Dialog>
  );
}
