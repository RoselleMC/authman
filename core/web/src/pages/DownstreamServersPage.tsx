import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AdvancedList,
  Button,
  Card,
  ConfirmDialog,
  Dialog,
  EmptyState,
  Field,
  Icon,
  Input,
  PageHeader,
  PageShell,
  StatusBadge,
  formatRelativeTime,
  useI18n,
  useListState,
  useToast,
  type ListColumn,
} from "@authman/shared";
import { createDownstreamServer, deleteDownstreamServer, fetchDownstreamServers, type DownstreamServer, type DownstreamServerInput } from "../api/admin";
import { useSession } from "../auth/SessionContext";

function serverInput(slug: string, displayName: string): DownstreamServerInput {
  return {
    slug,
    display_name: displayName,
    status: "active",
    registration_open: true,
    portal_theme: { display_name: displayName, portal_message: displayName },
    portal_config: {
      registration_strategy: "open",
      show_in_global: true,
      host: "127.0.0.1",
      port: 25565,
      transfer_host: "127.0.0.1",
      transfer_port: 25565,
      motd: displayName,
      gate_enabled: true,
      grant_required: true,
      grant_ttl_seconds: 45,
      allowed_portal_sources: [],
      portal_hosts: [],
      limbo_blueprint_id: "",
    },
    extension_providers: ["authman.identity"],
  };
}

export function DownstreamServersPage() {
  const { t } = useI18n();
  const { user } = useSession();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const list = useListState({ urlPrefix: "ds", defaults: { pageSize: 25, hidden: ["host"] }, storageScope: user?.id });
  const [open, setOpen] = useState(false);
  const [bulkDeleteRows, setBulkDeleteRows] = useState<DownstreamServer[]>([]);
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const q = useQuery({ queryKey: ["admin.downstreamServers"], queryFn: fetchDownstreamServers });
  const createMut = useMutation({
    mutationFn: () => createDownstreamServer(serverInput(slug, displayName)),
    onSuccess: (server) => {
      toast.push({ tone: "success", title: t("common.saved") });
      setOpen(false);
      setSlug("");
      setDisplayName("");
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServers"] });
      navigate(`/nodes/${encodeURIComponent(server.id)}`);
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const deleteMut = useMutation({
    mutationFn: async (rows: DownstreamServer[]) => {
      await Promise.all(rows.map((row) => deleteDownstreamServer(row.id)));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.servers.deleted.toast") });
      setBulkDeleteRows([]);
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServers"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const columns: ListColumn<DownstreamServer>[] = [
    { key: "name", header: t("admin.servers.col.name"), mandatory: true, sortable: true, sortValue: (r) => r.display_name, filter: { type: "text" }, render: (r) => <strong>{r.display_name}</strong> },
    { key: "slug", header: t("admin.servers.col.slug"), sortable: true, sortValue: (r) => r.slug, render: (r) => <span className="mono">{r.slug}</span> },
    { key: "status", header: t("admin.servers.col.status"), sortable: true, sortValue: (r) => r.status, render: (r) => <StatusBadge status={r.status} /> },
    { key: "host", header: t("admin.servers.col.host"), minWidth: "190px", render: (r) => <span className="mono">{r.target.transfer_host}:{r.target.transfer_port}</span> },
    { key: "blueprint", header: t("admin.servers.col.blueprint"), minWidth: "180px", render: (r) => <span>{r.portal_config?.limbo_blueprint_id || t("admin.servers.defaultWorld")}</span> },
    { key: "updated", header: t("common.updated"), sortable: true, sortValue: (r) => r.updated_at ?? "", render: (r) => <span className="muted-cell">{formatRelativeTime(r.updated_at)}</span> },
    { key: "open", header: "", mandatory: true, width: "44px", minWidth: "44px", align: "right", sticky: "right", render: () => <Icon name="chevronRight" size={16} /> },
  ];
  return (
    <PageShell testId="downstream-servers-page">
      <PageHeader
        title={t("admin.servers.heading")}
        desc={t("admin.servers.desc")}
        action={<Button variant="primary" icon="plus" onClick={() => setOpen(true)}>{t("admin.servers.add")}</Button>}
      />
      <Card noBody className="table-card">
        <AdvancedList
          columns={columns}
          rowKey={(r) => r.id}
          rows={q.data ?? []}
          state={list.state}
          onStateChange={list.setState}
          loading={q.isLoading}
          onRowClick={(r) => navigate(`/nodes/${encodeURIComponent(r.id)}`)}
          selectable={(row) => row.id !== "default"}
          selectionActions={(rows) => (
            <Button size="sm" variant="danger-soft" icon="trash" onClick={() => setBulkDeleteRows(rows)}>
              {t("common.delete")}
            </Button>
          )}
          empty={<EmptyState icon="server" title={t("admin.servers.empty")} />}
          testId="downstream-servers"
        />
      </Card>
      <Dialog
        open={open}
        onClose={() => !createMut.isPending && setOpen(false)}
        icon="server"
        iconTone="primary"
        title={t("admin.servers.add")}
        footer={<><Button variant="ghost" onClick={() => setOpen(false)}>{t("common.cancel")}</Button><Button variant="primary" loading={createMut.isPending} disabled={!slug.trim() || !displayName.trim()} onClick={() => createMut.mutate()}>{t("common.save")}</Button></>}
      >
        <div className="form-grid two">
          <Field label={t("admin.servers.col.slug")}><Input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder="survival" /></Field>
          <Field label={t("admin.servers.col.name")}><Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="Survival" /></Field>
        </div>
      </Dialog>
      <ConfirmDialog
        open={bulkDeleteRows.length > 0}
        onCancel={() => setBulkDeleteRows([])}
        onConfirm={() => deleteMut.mutate(bulkDeleteRows)}
        title={t("admin.servers.delete")}
        body={t("admin.servers.deleteDesc")}
        confirmLabel={t("common.delete")}
        destructive
        loading={deleteMut.isPending}
        testId="dialog-bulk-delete-server"
      />
    </PageShell>
  );
}
