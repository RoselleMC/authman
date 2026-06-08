import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import {
  AdvancedList,
  ApiError,
  Badge,
  Button,
  Card,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  Icon,
  IconButton,
  Input,
  PageHeader,
  PageShell,
  useI18n,
  useListState,
  useToast,
  type ListColumn,
} from "@authman/shared";
import {
  createDownstreamServer,
  deleteDownstreamServer,
  fetchDownstreamServers,
  type DownstreamServer,
  type DownstreamServerInput,
} from "../api/admin";

const PAGE_SIZE_OPTIONS = [10, 25, 50, 100] as const;

function statusTone(status: DownstreamServer["status"]): "success" | "warning" | "neutral" {
  if (status === "active") return "success";
  if (status === "hidden") return "warning";
  return "neutral";
}

function PortalAliasesCell({ slug, aliases }: { slug: string; aliases: string[] }) {
  const { t } = useI18n();
  if (aliases.length === 0) {
    return <span className="muted-cell" data-testid={`portal-aliases-${slug}`}>{t("admin.servers.col.portalAliases.none")}</span>;
  }
  return (
    <div className="portal-alias-list" data-testid={`portal-aliases-${slug}`}>
      {aliases.slice(0, 3).map((alias) => (
        <code key={alias} className="mono portal-alias-chip" style={{ fontSize: 12 }}>
          {alias}
        </code>
      ))}
      {aliases.length > 3 ? (
        <span className="muted-cell" style={{ fontSize: 12 }}>
          {t("admin.servers.col.portalAliases.more").replace("{count}", String(aliases.length - 3))}
        </span>
      ) : null}
    </div>
  );
}

export function ServersPage() {
  const { t, tError } = useI18n();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<DownstreamServer | null>(null);

  const list = useListState({
    urlPrefix: "s",
    defaults: { pageSize: 25 },
  });

  const q = useQuery({ queryKey: ["admin.servers"], queryFn: fetchDownstreamServers });

  const filteredRows = useMemo(() => {
    const rows = q.data ?? [];
    const nameFilter = (list.state.filters.name ?? "").trim().toLowerCase();
    const statusFilter = list.state.filters.status ?? "";
    const registrationFilter = list.state.filters.registration ?? "";
    return rows.filter((row) => {
      if (statusFilter && row.status !== statusFilter) return false;
      if (registrationFilter === "open" && !row.registration_open) return false;
      if (registrationFilter === "closed" && row.registration_open) return false;
      if (!nameFilter) return true;
      const haystack = [
        row.display_name,
        row.slug,
        row.target.motd,
        row.target.transfer_host,
        row.target.host,
        ...(row.portal_config.portal_hosts ?? []),
      ]
        .join(" ")
        .toLowerCase();
      return haystack.includes(nameFilter);
    });
  }, [q.data, list.state.filters.name, list.state.filters.status, list.state.filters.registration]);

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

  const columns: ListColumn<DownstreamServer>[] = [
    {
      key: "name",
      header: t("admin.servers.col.name"),
      mandatory: true,
      filter: { type: "text", placeholder: t("admin.servers.searchPlaceholder") },
      render: (s) => (
        <div className="node-name" data-testid={`server-row-${s.slug}`}>
          <span className="node-ico">
            <Icon name="layers" size={15} />
          </span>
          <div style={{ display: "flex", flexDirection: "column" }}>
            <span style={{ fontWeight: 600 }}>{s.display_name}</span>
            <code className="mono" style={{ color: "var(--color-text-muted)", fontSize: 12 }}>
              {s.slug}
            </code>
          </div>
        </div>
      ),
    },
    {
      key: "portalAliases",
      header: t("admin.servers.col.portalAliases"),
      render: (s) => <PortalAliasesCell slug={s.slug} aliases={s.portal_config.portal_hosts ?? []} />,
    },
    {
      key: "motd",
      header: t("admin.servers.col.motd"),
      hideOnNarrow: true,
      render: (s) => <span className="muted-cell" data-testid={`motd-cell-${s.slug}`}>{s.target.motd || "—"}</span>,
    },
    {
      key: "transfer",
      header: t("admin.servers.col.transfer"),
      render: (s) => (
        <code className="mono" style={{ color: "var(--color-text-muted)", fontSize: 12.5 }} data-testid={`transfer-cell-${s.slug}`}>
          {s.target.transfer_host}:{s.target.transfer_port}
        </code>
      ),
    },
    {
      key: "gate",
      header: t("admin.servers.col.gate"),
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "enabled", label: t("common.enabled") },
          { value: "disabled", label: t("common.disabled") },
        ],
      },
      getFilterValue: (s) => (s.target.gate_enabled ? "enabled" : "disabled"),
      render: (s) => (
        <Badge tone={s.target.gate_enabled ? "success" : "neutral"} dot>
          {s.target.gate_enabled ? t("common.enabled") : t("common.disabled")}
        </Badge>
      ),
    },
    {
      key: "registration",
      header: t("admin.servers.col.registration"),
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "open", label: t("common.open") },
          { value: "closed", label: t("common.closed") },
        ],
      },
      render: (s) => (
        <Badge tone={s.registration_open ? "success" : "neutral"} dot>
          {s.registration_open ? t("common.open") : t("common.closed")}
        </Badge>
      ),
    },
    {
      key: "status",
      header: t("admin.servers.col.status"),
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "active", label: t("admin.servers.status.active") },
          { value: "hidden", label: t("admin.servers.status.hidden") },
          { value: "disabled", label: t("admin.servers.status.disabled") },
        ],
      },
      render: (s) => (
        <Badge tone={statusTone(s.status)} dot>
          {t(`admin.servers.status.${s.status}`)}
        </Badge>
      ),
    },
    {
      key: "actions",
      header: "",
      mandatory: true,
      width: "72px",
      align: "right",
      render: (s) => (
        <div className="row-actions" onClick={(e) => e.stopPropagation()}>
          {s.id !== "default" ? (
            <IconButton
              name="close"
              size={16}
              label={t("admin.servers.delete")}
              onClick={() => setDeleteTarget(s)}
              data-testid={`server-delete-${s.slug}`}
            />
          ) : null}
          <Icon name="chevronRight" size={16} style={{ color: "var(--color-text-subtle)" }} />
        </div>
      ),
    },
  ];

  return (
    <PageShell>
      <PageHeader
        title={t("admin.servers.heading")}
        desc={t("admin.servers.desc")}
        action={(
          <Button variant="primary" icon="plus" onClick={() => setCreateOpen(true)} data-testid="server-add">
            {t("admin.servers.create")}
          </Button>
        )}
      />
      {q.error ? <ErrorState error={q.error} onRetry={() => q.refetch()} /> : null}
      <Card noBody className="table-card">
        <AdvancedList
          mode="client"
          columns={columns}
          rowKey={(r) => r.id}
          rows={filteredRows}
          loading={q.isLoading}
          state={list.state}
          onStateChange={list.setState}
          pageSizeOptions={PAGE_SIZE_OPTIONS}
          onRowClick={(r) => navigate(`/servers/${r.id}`)}
          empty={
            <EmptyState
              icon="layers"
              title={t("admin.servers.empty")}
              description={t("admin.servers.empty.desc")}
              testId="servers-empty"
            />
          }
          testId="servers"
        />
      </Card>
      <ServerCreateDialog
        open={createOpen}
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
              {t("admin.servers.delete")}
            </Button>
          </>
        )}
      >
        <p className="dialog-note" style={{ marginTop: 0 }}>
          {deleteTarget?.display_name} · {deleteTarget?.slug}
        </p>
      </Dialog>
    </PageShell>
  );
}

function ServerCreateDialog({
  open,
  loading,
  onClose,
  onSubmit,
}: {
  open: boolean;
  loading: boolean;
  onClose: () => void;
  onSubmit: (input: DownstreamServerInput) => void;
}) {
  const { t } = useI18n();
  const [slug, setSlug] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [registrationOpen, setRegistrationOpen] = useState(true);
  const [host, setHost] = useState("127.0.0.1");
  const [port, setPort] = useState("25565");
  const [transferHost, setTransferHost] = useState("127.0.0.1");
  const [transferPort, setTransferPort] = useState("25565");
  const [motd, setMotd] = useState("Welcome to Authman");
  const [gateEnabled, setGateEnabled] = useState(true);
  const [grantTtl, setGrantTtl] = useState("45");
  const [portalHosts, setPortalHosts] = useState("");
  const [allowedSources, setAllowedSources] = useState("");

  function submit() {
    const parseList = (value: string) =>
      value
        .split(",")
        .map((item) => item.trim())
        .filter(Boolean);
    onSubmit({
      slug,
      display_name: displayName,
      status: "active",
      registration_open: registrationOpen,
      portal_theme: {
        display_name: displayName,
      },
      portal_config: {
        registration_strategy: registrationOpen ? "open" : "closed",
        show_in_global: true,
        host,
        port: Number(port),
        transfer_host: transferHost,
        transfer_port: Number(transferPort),
        motd,
        gate_enabled: gateEnabled,
        grant_ttl_seconds: Number(grantTtl),
        portal_hosts: parseList(portalHosts),
        allowed_portal_sources: parseList(allowedSources),
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
      title={t("admin.servers.create")}
      desc={t("admin.servers.create.desc")}
      testId="dialog-server-form"
      footer={(
        <>
          <Button variant="ghost" onClick={onClose} disabled={loading}>
            {t("common.cancel")}
          </Button>
          <Button variant="primary" icon="check" loading={loading} onClick={submit} data-testid="server-create-submit">
            {t("common.save")}
          </Button>
        </>
      )}
    >
      <div className="form-grid">
        <Field label={t("admin.servers.field.slug")} hint={t("admin.servers.field.slug.hint")}>
          <Input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder="survival" mono data-testid="server-slug" />
        </Field>
        <Field label={t("admin.servers.field.displayName")}>
          <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder="Survival" data-testid="server-name" />
        </Field>
        <Field label={t("admin.servers.field.portalHosts")} hint={t("admin.servers.field.portalHosts.hint")}>
          <Input
            value={portalHosts}
            onChange={(e) => setPortalHosts(e.target.value)}
            placeholder="play.example.com, us.example.com"
            mono
            data-testid="server-portal-hosts"
          />
        </Field>
        <Field label={t("admin.servers.field.host")} hint={t("admin.servers.field.host.hint")}>
          <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="portal.example.com" mono />
        </Field>
        <Field label={t("admin.servers.field.port")}>
          <Input value={port} onChange={(e) => setPort(e.target.value)} placeholder="25565" mono />
        </Field>
        <Field label={t("admin.servers.field.transferHost")} hint={t("admin.servers.field.transferHost.hint")}>
          <Input
            value={transferHost}
            onChange={(e) => setTransferHost(e.target.value)}
            placeholder="gate.example.com"
            mono
            data-testid="server-transfer-host"
          />
        </Field>
        <Field label={t("admin.servers.field.transferPort")}>
          <Input
            value={transferPort}
            onChange={(e) => setTransferPort(e.target.value)}
            placeholder="25565"
            mono
            data-testid="server-transfer-port"
          />
        </Field>
        <Field label={t("admin.servers.field.motd")}>
          <Input value={motd} onChange={(e) => setMotd(e.target.value)} placeholder="Welcome to Authman" data-testid="server-motd" />
        </Field>
        <Field label={t("admin.servers.field.grantTtl")} hint={t("admin.servers.field.grantTtl.hint")}>
          <Input value={grantTtl} onChange={(e) => setGrantTtl(e.target.value)} placeholder="45" mono />
        </Field>
        <Field label={t("admin.servers.field.allowedSources")} hint={t("admin.servers.field.allowedSources.hint")}>
          <Input
            value={allowedSources}
            onChange={(e) => setAllowedSources(e.target.value)}
            placeholder="portal-us, portal-eu"
            mono
          />
        </Field>
        <label className="check-row">
          <input
            type="checkbox"
            checked={gateEnabled}
            onChange={(e) => setGateEnabled(e.target.checked)}
            data-testid="server-gate-enabled"
          />
          <span>{t("admin.servers.field.gateEnabled")}</span>
        </label>
        <label className="check-row">
          <input
            type="checkbox"
            checked={registrationOpen}
            onChange={(e) => setRegistrationOpen(e.target.checked)}
          />
          <span>{t("admin.servers.field.registrationOpen")}</span>
        </label>
      </div>
    </Dialog>
  );
}
