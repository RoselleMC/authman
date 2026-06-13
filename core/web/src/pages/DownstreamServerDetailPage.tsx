import { useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ApiError,
  AdvancedList,
  BackLink,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  Copyable,
  DetailActions,
  DetailAside,
  DetailBody,
  DetailGrid,
  DetailIdentifier,
  DetailSummary,
  Dialog,
  EmptyState,
  Field,
  Icon,
  Input,
  MinecraftMotdPreview,
  MiniMessageEditorDialog,
  PageShell,
  SecretReveal,
  Select,
  SimpleTextList,
  StatusBadge,
  SettingsStack,
  Tabs,
  TypeBadge,
  coerceVelocityNode,
  formatRelativeTime,
  navigateWithBack,
  useBackTarget,
  useI18n,
  useListState,
  useToast,
  type ListColumn,
  type SafeVelocityNode,
} from "@authman/shared";
import {
  addDownstreamServerPrivilegedPassport,
  createNode,
  deleteDownstreamServerIcon,
  deleteDownstreamServer,
  deleteNode,
  fetchDownstreamServer,
  fetchDownstreamServerPrivilegedPassports,
  fetchLimboBlueprints,
  fetchNodes,
  fetchPassports,
  removeDownstreamServerPrivilegedPassport,
  updateDownstreamServer,
  uploadDownstreamServerIcon,
  type DownstreamServer,
  type DownstreamServerInput,
  type DownstreamServerPrivilegedPassport,
  type DownstreamResourcePack,
  type IdentityListFilters,
} from "../api/admin";
import { PlayerMessagesPage } from "./PlayerMessagesPage";

interface IssuedToken {
  token_once: string;
  token_fingerprint: string;
  name: string;
}

type DetailTab = "overview" | "privileged" | "resources" | "messages";

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

function normalizeDomains(values: string[]): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const raw of values) {
    const trimmed = raw.trim();
    if (!trimmed || seen.has(trimmed)) continue;
    seen.add(trimmed);
    result.push(trimmed);
  }
  return result;
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

function localID(prefix = "item"): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function NodeStatusBadge({ node }: { node: SafeVelocityNode }) {
  const { t } = useI18n();
  const tone: "success" | "warning" | "neutral" = node.status === "active" ? "success" : node.status === "stale" ? "warning" : "neutral";
  return <Badge tone={tone} dot>{t(`admin.nodes.status.${node.status}`, node.status)}</Badge>;
}

function normalizeResourcePacks(value: DownstreamResourcePack[] | undefined): DownstreamResourcePack[] {
  return (value ?? [])
    .map((pack, index) => cleanResourcePack({ ...pack, id: pack.id || `pack-${index}` }))
    .filter((pack) => pack.url);
}

function cleanResourcePack(pack: DownstreamResourcePack): DownstreamResourcePack {
  return {
    id: String(pack.id || pack.url || localID("pack")).trim(),
    name: String(pack.name || "").trim(),
    url: String(pack.url || "").trim(),
    hash: String(pack.hash || "").trim(),
    prompt: String(pack.prompt || "").trim(),
  };
}

function emptyResourcePack(): DownstreamResourcePack {
  return { id: localID("pack"), name: "", url: "", hash: "", prompt: "" };
}

export function DownstreamServerDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const backTarget = useBackTarget("/nodes");
  const [searchParams, setSearchParams] = useSearchParams();
  const toast = useToast();
  const qc = useQueryClient();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteNodeOpen, setDeleteNodeOpen] = useState(false);
  const [issueOpen, setIssueOpen] = useState(false);
  const [motdOpen, setMotdOpen] = useState(false);
  const [tab, setTab] = useState<DetailTab>(() => {
    const raw = searchParams.get("tab");
    return raw === "privileged" || raw === "resources" || raw === "messages" ? raw : "overview";
  });
  const [allowOpen, setAllowOpen] = useState(false);
  const [passportSearch, setPassportSearch] = useState("");
  const [selectedPassportID, setSelectedPassportID] = useState("");
  const [resourceDialog, setResourceDialog] = useState<{ index: number | null; pack: DownstreamResourcePack } | null>(null);
  const [issuedToken, setIssuedToken] = useState<IssuedToken | null>(null);
  const [input, setInput] = useState<DownstreamServerInput | null>(null);
  const [matchDomains, setMatchDomains] = useState<string[]>([]);
  const [downstreamAddress, setDownstreamAddress] = useState("");
  const [iconError, setIconError] = useState("");
  const allowList = useListState({ urlPrefix: "serverPrivileged", urlSync: false, defaults: { pageSize: 10, hidden: ["uuid"] } });
  const resourceList = useListState({ urlPrefix: "serverPacks", urlSync: false, defaults: { pageSize: 10, hidden: ["hash", "prompt"] } });
  const allowFilters = useMemo<IdentityListFilters>(() => {
    const next: IdentityListFilters = { page: allowList.state.page, page_size: allowList.state.pageSize };
    const q = (allowList.state.filters.username ?? "").trim();
    if (q) next.q = q;
    const kind = allowList.state.filters.kind;
    if (kind === "premium" || kind === "offline") next.kind = kind;
    const status = allowList.state.filters.status;
    if (status) next.status = status;
    if (allowList.state.sortKey) {
      next.sort = allowList.state.sortKey;
      next.dir = allowList.state.sortDir;
    }
    return next;
  }, [allowList.state]);
  const q = useQuery({ queryKey: ["admin.downstreamServer", id], queryFn: () => fetchDownstreamServer(id), enabled: !!id });
  const allowedQ = useQuery({
    queryKey: ["admin.downstreamServer.privilegedPassports", id, allowFilters],
    queryFn: () => fetchDownstreamServerPrivilegedPassports(id, allowFilters),
    enabled: !!id && tab === "privileged",
  });
  const passportOptionsQ = useQuery({
    queryKey: ["admin.passports", "privileged-picker", passportSearch],
    queryFn: ({ signal }) => fetchPassports({ q: passportSearch.trim() || undefined, page: 1, page_size: 25 }, signal),
    enabled: allowOpen,
  });
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
    setMatchDomains([...(server.routing_config.portal_hosts ?? [])]);
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
          portal_hosts: normalizeDomains(matchDomains),
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
  const addAllowMut = useMutation({
    mutationFn: () => addDownstreamServerPrivilegedPassport(id, selectedPassportID),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.servers.privileged.added") });
      setAllowOpen(false);
      setSelectedPassportID("");
      setPassportSearch("");
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServer.privilegedPassports", id] });
    },
    onError: (err) => toast.danger(err instanceof ApiError ? err.message : t("common.unknown")),
  });
  const removeAllowMut = useMutation({
    mutationFn: async (rows: DownstreamServerPrivilegedPassport[]) => {
      await Promise.all(rows.map((row) => removeDownstreamServerPrivilegedPassport(id, row.passport_id || row.id)));
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.servers.privileged.removed") });
      void qc.invalidateQueries({ queryKey: ["admin.downstreamServer.privilegedPassports", id] });
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
    return <PageShell><BackLink onClick={() => navigate(backTarget)}>{t("admin.servers.heading")}</BackLink><Card title={q.isLoading ? t("common.loading") : t("common.unknown")}><span /></Card></PageShell>;
  }
  const cfg = input.routing_config;
  const serverIcon = String(cfg.server_icon || server.target.server_icon || "").trim();
  const resourcePacks = normalizeResourcePacks(cfg.resource_packs);
  const allowedIDs = new Set((allowedQ.data?.rows ?? []).map((row) => row.passport_id || row.id));
  const passportOptions = [
    { value: "", label: t("admin.servers.privileged.selectPlaceholder") },
    ...(passportOptionsQ.data?.rows ?? [])
      .filter((passport) => passport.status !== "deleted" && !allowedIDs.has(passport.id))
      .map((passport) => ({ value: passport.id, label: `${passport.username} · ${t(`admin.players.filter.${passport.kind}`, passport.kind)}` })),
  ];
  const privilegedColumns: ListColumn<DownstreamServerPrivilegedPassport>[] = [
    {
      key: "username",
      header: t("admin.passports.col.username"),
      mandatory: true,
      sortable: true,
      filter: { type: "text", placeholder: t("admin.passports.searchPlaceholder") },
      render: (row) => (
        <div className="player-cell">
          <span className={row.avatar_url ? "pa-avatar has-image" : "pa-avatar"}>
            {row.avatar_url ? <img src={row.avatar_url} alt="" aria-hidden="true" /> : (row.username || "?")[0]}
          </span>
          <span className="player-name">{row.username}</span>
        </div>
      ),
    },
    {
      key: "kind",
      header: t("admin.players.col.type"),
      sortable: true,
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "premium", label: t("admin.players.filter.premium") },
          { value: "offline", label: t("admin.players.filter.offline") },
        ],
      },
      render: (row) => <TypeBadge kind={row.kind} />,
    },
    {
      key: "status",
      header: t("admin.players.col.status"),
      sortable: true,
      filter: {
        type: "select",
        options: [
          { value: "", label: t("common.all") },
          { value: "active", label: t("status.active") },
          { value: "locked", label: t("status.locked") },
          { value: "pending_verification", label: t("status.pending") },
          { value: "deleted", label: t("status.deleted") },
        ],
      },
      render: (row) => <StatusBadge status={row.status} />,
    },
    { key: "online", header: t("admin.presences.onlineState"), minWidth: "120px", sortable: true, render: (row) => <StatusBadge status={row.online ? "online" : "offline_status"} /> },
    { key: "profiles", header: t("admin.passports.col.profiles"), minWidth: "110px", sortable: true, render: (row) => <span>{row.profile_count}</span> },
    { key: "uuid", header: "UUID", minWidth: "300px", defaultVisible: false, sortable: true, render: (row) => <Copyable value={row.uuid} /> },
    { key: "allowedAt", header: t("admin.servers.privileged.allowedAt"), minWidth: "150px", sortable: true, sortValue: (row) => row.allowed_at, render: (row) => <span className="muted-cell">{formatRelativeTime(row.allowed_at)}</span> },
    {
      key: "actions",
      header: "",
      mandatory: true,
      width: "44px",
      minWidth: "44px",
      align: "right",
      sticky: "right",
      render: () => <Icon name="chevronRight" size={16} />,
    },
  ];
  const resourcePackColumns: ListColumn<DownstreamResourcePack>[] = [
    { key: "name", header: t("admin.servers.resources.col.name"), mandatory: true, sortable: true, sortValue: (pack) => pack.name || pack.url, render: (pack) => <strong>{pack.name || pack.url}</strong> },
    { key: "url", header: "URL", mandatory: true, minWidth: "300px", render: (pack) => <span className="mono muted-cell">{pack.url}</span> },
    { key: "hash", header: "SHA-1", minWidth: "260px", defaultVisible: false, render: (pack) => <span className="mono">{pack.hash || "—"}</span> },
    { key: "prompt", header: t("admin.servers.resources.col.prompt"), minWidth: "220px", defaultVisible: false, render: (pack) => <span>{pack.prompt || "—"}</span> },
    { key: "actions", header: "", mandatory: true, width: "44px", minWidth: "44px", align: "right", sticky: "right", render: () => <Icon name="chevronRight" size={16} /> },
  ];
  function setConfig(next: Partial<DownstreamServerInput["routing_config"]>) {
    setInput((current) => current ? { ...current, routing_config: { ...current.routing_config, ...next } } : current);
  }
  function upsertResourcePack(index: number | null, pack: DownstreamResourcePack) {
    const cleaned = cleanResourcePack(pack);
    if (!cleaned.url) return;
    const next = [...resourcePacks];
    if (index === null) next.push(cleaned);
    else next[index] = cleaned;
    setConfig({ resource_packs: next });
    setResourceDialog(null);
  }
  function removeResourcePacks(rows: DownstreamResourcePack[]) {
    const removeIDs = new Set(rows.map((row) => row.id));
    setConfig({ resource_packs: resourcePacks.filter((pack) => !removeIDs.has(pack.id)) });
  }

  return (
    <PageShell testId="downstream-server-detail-page">
      <div className="detail-toolbar">
        <BackLink onClick={() => navigate(backTarget)}>{t("admin.servers.heading")}</BackLink>
        <Tabs
          value={tab}
          onChange={(next) => {
            setTab(next);
            setSearchParams(next === "overview" ? {} : { tab: next }, { replace: true });
          }}
          tabs={[
            { value: "overview", label: t("common.overview"), icon: "gauge" },
            { value: "privileged", label: t("admin.servers.privileged.heading"), icon: "key" },
            { value: "resources", label: t("admin.servers.resources.heading"), icon: "download" },
            { value: "messages", label: t("admin.playerMessages.heading"), icon: "mail" },
          ]}
        />
      </div>
      <DetailGrid>
        <DetailAside>
          <DetailSummary
            title={input.display_name}
            icon="server"
            avatarUrl={serverIcon || null}
            titleMeta={<StatusBadge status={input.enabled ? (input.visible ? "active" : "hidden") : "disabled"} />}
          >
            <DetailIdentifier label={t("admin.servers.internalId")} value={server.id} />
            <DetailIdentifier label={t("admin.servers.connectionAddress")} value={downstreamAddress} />
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
            <Button variant="danger-soft" icon="trash" block onClick={() => setDeleteOpen(true)}>{t("common.delete")}</Button>
          </DetailActions>
        </DetailAside>
        <DetailBody>
          {tab === "overview" ? (
            <>
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
                    <SimpleTextList
                      values={matchDomains}
                      onChange={setMatchDomains}
                      placeholder="play.example.com"
                      addLabel={t("admin.servers.matchDomains.add")}
                      testId="server-match-domains"
                    />
                  </Field>
                  <Field label={t("admin.servers.connectionAddress")} hint={t("admin.servers.connectionAddress.hint")} style={{ gridColumn: "1 / -1" }}>
                    <Input value={downstreamAddress} onChange={(e) => setDownstreamAddress(e.target.value)} placeholder="127.0.0.1:25565" />
                  </Field>
                  <Field label={t("admin.servers.protocolMin")} hint={t("admin.servers.protocolMin.hint")}>
                    <Input type="number" min={0} value={Number(cfg.min_protocol_version ?? 771)} onChange={(e) => setConfig({ min_protocol_version: Number(e.target.value) || 0 })} />
                  </Field>
                  <Field label={t("admin.servers.protocolMax")} hint={t("admin.servers.protocolMax.hint")}>
                    <Input type="number" min={0} value={Number(cfg.max_protocol_version ?? 0)} onChange={(e) => setConfig({ max_protocol_version: Number(e.target.value) || 0 })} />
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
            </>
          ) : tab === "privileged" ? (
            <Card noBody className="table-card">
              <AdvancedList
                title={t("admin.servers.privileged.heading")}
                description={t("admin.servers.privileged.desc")}
                columns={privilegedColumns}
                rowKey={(row) => row.passport_id || row.id}
                mode="server"
                rows={allowedQ.data?.rows ?? []}
                total={allowedQ.data?.meta.total ?? 0}
                loading={allowedQ.isLoading}
                state={allowList.state}
                onStateChange={allowList.setState}
                pageSizeOptions={[5, 10, 25]}
                onRowClick={(row) => navigateWithBack(navigate, `/passports/${row.passport_id || row.id}`, location)}
                selectable
                primaryActions={<Button size="sm" variant="primary" icon="plus" onClick={() => setAllowOpen(true)}>{t("admin.servers.privileged.add")}</Button>}
                selectionActions={(rows) => (
                  <Button size="sm" variant="danger-soft" icon="close" loading={removeAllowMut.isPending} onClick={() => removeAllowMut.mutate(rows)}>
                    {t("admin.servers.privileged.remove")}
                  </Button>
                )}
                empty={<EmptyState icon="key" title={t("admin.servers.privileged.empty")} />}
                testId="server-privileged-passports"
              />
            </Card>
          ) : tab === "messages" ? (
            <PlayerMessagesPage
              embedded
              serverId={id}
              basePath={`/nodes/${encodeURIComponent(id)}/messages`}
            />
          ) : (
            <SettingsStack>
              <Card title={t("admin.servers.resources.policy")}>
                <div className="route-selection-list">
                  <label className="toggle-row">
                    <input
                      type="checkbox"
                      checked={Boolean(cfg.resource_pack_enabled)}
                      onChange={(event) => setConfig({ resource_pack_enabled: event.currentTarget.checked })}
                    />
                    <span>
                      <strong>{t("admin.servers.resources.enabled")}</strong>
                      <small>{t("admin.servers.resources.enabled.hint")}</small>
                    </span>
                  </label>
                  <label className="toggle-row">
                    <input
                      type="checkbox"
                      checked={Boolean(cfg.resource_pack_required)}
                      onChange={(event) => setConfig({ resource_pack_required: event.currentTarget.checked })}
                    />
                    <span>
                      <strong>{t("admin.servers.resources.required")}</strong>
                      <small>{t("admin.servers.resources.required.hint")}</small>
                    </span>
                  </label>
                </div>
                <p className="card-foot-note" style={{ marginTop: 12 }}>{t("admin.servers.resources.transferNote")}</p>
              </Card>
              <Card noBody className="table-card">
                <AdvancedList
                  title={t("admin.servers.resources.heading")}
                  description={t("admin.servers.resources.desc")}
                  columns={resourcePackColumns}
                  rowKey={(pack) => pack.id || pack.url}
                  mode="client"
                  rows={resourcePacks}
                  state={resourceList.state}
                  onStateChange={resourceList.setState}
                  pageSizeOptions={[5, 10, 25]}
                  onRowClick={(pack) => setResourceDialog({ index: resourcePacks.findIndex((item) => item.id === pack.id), pack })}
                  selectable
                  primaryActions={<Button size="sm" variant="primary" icon="plus" onClick={() => setResourceDialog({ index: null, pack: emptyResourcePack() })}>{t("admin.servers.resources.add")}</Button>}
                  selectionActions={(rows) => (
                    <Button size="sm" variant="danger-soft" icon="trash" onClick={() => removeResourcePacks(rows)}>
                      {t("common.delete")}
                    </Button>
                  )}
                  empty={<EmptyState icon="download" title={t("admin.servers.resources.empty")} />}
                  testId="server-resource-packs"
                />
              </Card>
            </SettingsStack>
          )}
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
      <Dialog
        open={allowOpen}
        onClose={() => !addAllowMut.isPending && setAllowOpen(false)}
        icon="key"
        iconTone="primary"
        title={t("admin.servers.privileged.add")}
        desc={t("admin.servers.privileged.add.desc")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setAllowOpen(false)} disabled={addAllowMut.isPending}>{t("common.cancel")}</Button>
            <Button variant="primary" loading={addAllowMut.isPending} disabled={!selectedPassportID} onClick={() => addAllowMut.mutate()}>{t("common.add")}</Button>
          </>
        }
        testId="dialog-add-privileged-passport"
      >
        <div className="form-grid">
          <Field label={t("common.search")}>
            <Input value={passportSearch} onChange={(event) => setPassportSearch(event.target.value)} placeholder={t("admin.passports.searchPlaceholder")} />
          </Field>
          <Field label={t("admin.servers.privileged.passport")}>
            <Select
              value={selectedPassportID}
              onChange={setSelectedPassportID}
              options={passportOptions}
              disabled={passportOptionsQ.isLoading}
              placeholder={passportOptionsQ.isLoading ? t("common.loading") : t("admin.servers.privileged.selectPlaceholder")}
              testId="server-privileged-passport-select"
            />
          </Field>
        </div>
      </Dialog>
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
        open={!!resourceDialog}
        onClose={() => setResourceDialog(null)}
        icon="download"
        iconTone="primary"
        title={resourceDialog?.index === null ? t("admin.servers.resources.add") : t("admin.servers.resources.edit")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setResourceDialog(null)}>{t("common.cancel")}</Button>
            <Button
              variant="primary"
              disabled={!resourceDialog?.pack.url.trim()}
              onClick={() => resourceDialog && upsertResourcePack(resourceDialog.index, resourceDialog.pack)}
            >
              {t("common.save")}
            </Button>
          </>
        }
        testId="dialog-server-resource-pack"
      >
        {resourceDialog ? (
          <div className="form-grid">
            <Field label={t("admin.servers.resources.col.name")}>
              <Input value={resourceDialog.pack.name || ""} onChange={(e) => setResourceDialog({ ...resourceDialog, pack: { ...resourceDialog.pack, name: e.target.value } })} />
            </Field>
            <Field label="URL">
              <Input value={resourceDialog.pack.url} onChange={(e) => setResourceDialog({ ...resourceDialog, pack: { ...resourceDialog.pack, url: e.target.value } })} placeholder="https://example.com/pack.zip" />
            </Field>
            <Field label="SHA-1">
              <Input value={resourceDialog.pack.hash || ""} onChange={(e) => setResourceDialog({ ...resourceDialog, pack: { ...resourceDialog.pack, hash: e.target.value } })} mono />
            </Field>
            <Field label={t("admin.servers.resources.col.prompt")}>
              <Input value={resourceDialog.pack.prompt || ""} onChange={(e) => setResourceDialog({ ...resourceDialog, pack: { ...resourceDialog.pack, prompt: e.target.value } })} />
            </Field>
          </div>
        ) : null}
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
