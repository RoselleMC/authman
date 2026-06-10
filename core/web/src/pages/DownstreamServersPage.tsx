import { useMemo, useState } from "react";
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
import { createDownstreamServer, deleteDownstreamServer, fetchDownstreamServers, updateDownstreamServer, type DownstreamServer, type DownstreamServerInput, type ListFilters } from "../api/admin";
import { useSession } from "../auth/SessionContext";

function splitCSV(value: string): string[] {
  return value.split(",").map((item) => item.trim()).filter(Boolean);
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

function serverInput(displayName: string, matchDomains: string, connectionAddress: string): DownstreamServerInput {
  const target = parseAddress(connectionAddress);
  return {
    display_name: displayName,
    enabled: true,
    visible: true,
    registration_open: true,
    routing_config: {
      show_in_global: true,
      host: target.host,
      port: target.port,
      transfer_host: target.host,
      transfer_port: target.port,
      motd: displayName,
      server_icon: "",
      gate_enabled: true,
      grant_required: true,
      grant_ttl_seconds: 45,
      allowed_portal_sources: [],
      portal_hosts: splitCSV(matchDomains),
      limbo_blueprint_id: "",
    },
    extension_providers: ["authman.identity"],
  };
}

function inputFromServer(server: DownstreamServer, patch: Partial<Pick<DownstreamServerInput, "enabled" | "visible">>): DownstreamServerInput {
  return {
    display_name: server.display_name,
    enabled: patch.enabled ?? server.enabled,
    visible: patch.visible ?? server.visible,
    registration_open: true,
    routing_config: { ...server.routing_config },
    extension_providers: [...server.extension_providers],
  };
}

function serverIconURL(server: DownstreamServer): string {
  return String(server.target.server_icon || server.routing_config.server_icon || "").trim();
}

function ServerIconCell({ server }: { server: DownstreamServer }) {
  const icon = serverIconURL(server);
  return (
    <span className={`server-list-icon${icon ? " has-image" : ""}`} aria-hidden="true">
      {icon ? <img src={icon} alt="" /> : <Icon name="server" size={17} />}
    </span>
  );
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
  const [displayName, setDisplayName] = useState("");
  const [matchDomains, setMatchDomains] = useState("");
  const [connectionAddress, setConnectionAddress] = useState("127.0.0.1:25565");
  const filters = useMemo<ListFilters>(() => {
    const next: ListFilters = { page: list.state.page, page_size: list.state.pageSize };
    const q = (list.state.filters.name ?? "").trim();
    if (q) next.q = q;
    if (list.state.sortKey) {
      next.sort = list.state.sortKey;
      next.dir = list.state.sortDir;
    }
    return next;
  }, [list.state]);
  const q = useQuery({ queryKey: ["admin.downstreamServers", filters], queryFn: () => fetchDownstreamServers(filters) });
  const createMut = useMutation({
    mutationFn: () => createDownstreamServer(serverInput(displayName, matchDomains, connectionAddress)),
    onSuccess: (server) => {
      toast.push({ tone: "success", title: t("common.saved") });
      setOpen(false);
      setDisplayName("");
      setMatchDomains("");
      setConnectionAddress("127.0.0.1:25565");
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
  const statusMut = useMutation({
    mutationFn: async ({ rows, patch }: { rows: DownstreamServer[]; patch: Partial<Pick<DownstreamServerInput, "enabled" | "visible">> }) => {
      await Promise.all(rows.map((row) => updateDownstreamServer(row.id, inputFromServer(row, patch))));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.servers.saved.toast") });
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServers"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const columns: ListColumn<DownstreamServer>[] = [
    { key: "icon", header: t("admin.servers.col.icon"), mandatory: true, width: "52px", minWidth: "52px", align: "center", render: (r) => <ServerIconCell server={r} /> },
    { key: "name", header: t("admin.servers.col.name"), mandatory: true, sortable: true, sortValue: (r) => r.display_name, filter: { type: "text" }, render: (r) => <strong>{r.display_name}</strong> },
    { key: "status", header: t("admin.servers.col.status"), sortable: true, sortValue: (r) => r.status, render: (r) => <StatusBadge status={r.enabled ? (r.visible ? "active" : "hidden") : "disabled"} /> },
    { key: "host", header: t("admin.servers.col.host"), minWidth: "190px", render: (r) => <span className="mono">{r.target.transfer_host}:{r.target.transfer_port}</span> },
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
          mode="server"
          rows={q.data?.rows ?? []}
          total={q.data?.meta.total ?? 0}
          state={list.state}
          onStateChange={list.setState}
          loading={q.isLoading}
          onRowClick={(r) => navigate(`/nodes/${encodeURIComponent(r.id)}`)}
          selectable
          selectionActions={(rows) => (
            <>
              <Button size="sm" variant="secondary" icon={rows.every((row) => row.enabled) ? "close" : "check"} loading={statusMut.isPending} onClick={() => statusMut.mutate({ rows, patch: { enabled: !rows.every((row) => row.enabled) } })}>
                {rows.every((row) => row.enabled) ? t("common.disable") : t("common.enable")}
              </Button>
              <Button size="sm" variant="secondary" icon={rows.every((row) => row.visible) ? "eyeOff" : "eye"} loading={statusMut.isPending} onClick={() => statusMut.mutate({ rows, patch: { visible: !rows.every((row) => row.visible) } })}>
                {rows.every((row) => row.visible) ? t("common.hide") : t("common.show")}
              </Button>
              <Button size="sm" variant="danger-soft" icon="trash" onClick={() => setBulkDeleteRows(rows)}>
                {t("common.delete")}
              </Button>
            </>
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
        footer={<><Button variant="ghost" onClick={() => setOpen(false)}>{t("common.cancel")}</Button><Button variant="primary" loading={createMut.isPending} disabled={!displayName.trim()} onClick={() => createMut.mutate()}>{t("common.save")}</Button></>}
      >
        <div className="form-grid two">
          <Field label={t("admin.servers.col.name")}><Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="Survival" /></Field>
          <Field label={t("admin.servers.connectionAddress")}><Input value={connectionAddress} onChange={(e) => setConnectionAddress(e.target.value)} placeholder="127.0.0.1:25565" /></Field>
          <Field label={t("admin.servers.matchDomains")} hint={t("admin.servers.matchDomains.hint")} style={{ gridColumn: "1 / -1" }}>
            <Input value={matchDomains} onChange={(e) => setMatchDomains(e.target.value)} placeholder="play.example.com, survival.example.com" />
          </Field>
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
