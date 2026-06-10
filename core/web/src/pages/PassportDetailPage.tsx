import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AdvancedList,
  Button,
  Card,
  Copyable,
  DefList,
  DefRow,
  DetailActions,
  DetailSummary,
  Dialog,
  Field,
  Icon,
  Input,
  IPLocation,
  Select,
  StatusBadge,
  Tabs,
  TypeBadge,
  formatAbsTime,
  useI18n,
  useListState,
  useToast,
  type ListColumn,
} from "@authman/shared";
import { AuditEventList } from "../components/AuditEventList";
import {
  bindProfile,
  createPassportBan,
  createProfile,
  deletePassportSkin,
  extendBan,
  fetchPassport,
  fetchProfiles,
  revokeBan,
  unbindProfile,
  updatePassportSkinSource,
  updatePassportStatus,
  uploadPassportSkin,
  type PassportRow,
  type PlayerBan,
  type ProfileSummary,
} from "../api/admin";
import { ErrorBlock } from "../components/ErrorBlock";
import { BanStateCell, LockUntilCell } from "../components/PlayerStateCells";
import { MinecraftSkinPreview } from "../components/MinecraftSkinPreview";

type DialogState = null | "status" | "bind" | "create" | "ban" | "extendBan" | "revokeBan";
type DetailTab = "overview" | "skin" | "audit";
type DurationUnit = "s" | "min" | "h" | "d" | "w" | "m" | "y";

const DEFAULT_BAN_VALUE = "1";
const DEFAULT_BAN_UNIT: DurationUnit = "d";

export function PassportDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t } = useI18n();
  const navigate = useNavigate();
  const toast = useToast();
  const qc = useQueryClient();
  const [nextStatus, setNextStatus] = useState<PassportRow["status"] | null>(null);
  const [dialog, setDialog] = useState<DialogState>(null);
  const [selectedProfileID, setSelectedProfileID] = useState("");
  const [protocolName, setProtocolName] = useState("");
  const [tab, setTab] = useState<DetailTab>("overview");
  const [banReason, setBanReason] = useState("");
  const [banDurationValue, setBanDurationValue] = useState(DEFAULT_BAN_VALUE);
  const [banDurationUnit, setBanDurationUnit] = useState<DurationUnit>(DEFAULT_BAN_UNIT);
  const [revokeTarget, setRevokeTarget] = useState<PlayerBan | null>(null);
  const [skinFile, setSkinFile] = useState<File | null>(null);
  const [capeFile, setCapeFile] = useState<File | null>(null);
  const [elytraFile, setElytraFile] = useState<File | null>(null);
  const [skinModel, setSkinModel] = useState<"wide" | "slim">("wide");
  const profileList = useListState({ urlPrefix: "boundProfiles", urlSync: false, defaults: { pageSize: 10, hidden: ["uuid", "lockedUntil"] } });
  const q = useQuery({ queryKey: ["admin.passport", id], queryFn: () => fetchPassport(id), enabled: !!id });
  const previewSkinURL = useObjectURL(skinFile);
  const previewCapeURL = useObjectURL(capeFile);
  const previewElytraURL = useObjectURL(elytraFile);
  const unboundQ = useQuery({
    queryKey: ["admin.profiles.unbound"],
    queryFn: ({ signal }) => fetchProfiles({ binding: "unbound", page: 1, page_size: 200 }, signal),
    enabled: dialog === "bind",
  });
  const statusMut = useMutation({
    mutationFn: (status: PassportRow["status"]) => updatePassportStatus(id, status),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.saved") });
      setNextStatus(null);
      setDialog(null);
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const bindMut = useMutation({
    mutationFn: () => bindProfile(selectedProfileID, id, false),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.profiles.bound") });
      setDialog(null);
      setSelectedProfileID("");
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles.unbound"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const createMut = useMutation({
    mutationFn: () => createProfile({
      protocol_name: protocolName,
      passport_id: id,
    }),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.profiles.created") });
      setDialog(null);
      setProtocolName("");
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles.unbound"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const unbindMut = useMutation({
    mutationFn: (profileID: string) => unbindProfile(profileID),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.profiles.unbound") });
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles.unbound"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const banMut = useMutation({
    mutationFn: () => createPassportBan(id, { reason: banReason, expires_in_seconds: durationSeconds(banDurationValue, banDurationUnit) }),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.bans.created") });
      setDialog(null);
      setBanReason("");
      resetDuration(setBanDurationValue, setBanDurationUnit);
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const extendMut = useMutation({
    mutationFn: () => extendBan(activeBan?.id ?? "", { expires_in_seconds: durationSeconds(banDurationValue, banDurationUnit), reason: banReason }),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.bans.extended") });
      setDialog(null);
      setBanReason("");
      resetDuration(setBanDurationValue, setBanDurationUnit);
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const revokeMut = useMutation({
    mutationFn: () => revokeBan(revokeTarget?.id ?? "", ""),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.bans.revoked") });
      setDialog(null);
      setRevokeTarget(null);
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const skinMut = useMutation({
    mutationFn: () => {
      if (!skinFile && !q.data?.skin.has_custom_skin) throw new Error("skin file missing");
      return uploadPassportSkin(id, { skin: skinFile, cape: capeFile, elytra: elytraFile, model: skinModel });
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.skins.saved") });
      setSkinFile(null);
      setCapeFile(null);
      setElytraFile(null);
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const skinDeleteMut = useMutation({
    mutationFn: () => deletePassportSkin(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.skins.resetDone") });
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const skinSourceMut = useMutation({
    mutationFn: (useUpstreamSkin: boolean) => updatePassportSkinSource(id, { use_upstream_skin: useUpstreamSkin }),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.skins.sourceSaved") });
      void qc.invalidateQueries({ queryKey: ["admin.passport", id] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  useEffect(() => {
    if (q.data?.skin.model) {
      setSkinModel(normalizeSkinModel(q.data.skin.model));
    }
  }, [q.data?.id, q.data?.skin.model]);

  if (q.isLoading) return <div className="page"><Card>{t("common.loading")}</Card></div>;
  if (q.error || !q.data) return <div className="page"><ErrorBlock error={q.error} onRetry={() => q.refetch()} /></div>;
  const p = q.data;
  const usingUpstreamSkin = Boolean(p.skin.use_upstream_skin);
  const persistedSkinModel = normalizeSkinModel(p.skin.model);
  const hasSkinChanges = !usingUpstreamSkin && Boolean(skinFile || capeFile || elytraFile || (p.skin.has_custom_skin && skinModel !== persistedSkinModel));
  const activeBan = firstActiveBan(p.bans);
  const banDurationValid = isValidDuration(banDurationValue);
  const relatedAuditIDs = [p.id, p.uuid, ...p.profiles.flatMap((profile) => [profile.id, profile.uuid])]
    .filter(Boolean)
    .join(",");
  const unboundProfiles = unboundQ.data?.rows ?? [];
  const profileOptions = unboundProfiles.map((profile) => ({
    value: profile.id,
    label: `${profile.protocol_name} · ${profile.uuid}`,
  }));
  const boundProfileColumns: ListColumn<ProfileSummary>[] = [
    {
      key: "protocol",
      header: t("admin.profiles.col.protocol"),
      mandatory: true,
      sortable: true,
      sortValue: (profile) => profile.protocol_name,
      render: (profile) => <Link className="passport-profile-name" to={`/profiles/${profile.id}`}>{profile.protocol_name}</Link>,
    },
    { key: "uuid", header: "UUID", minWidth: "300px", defaultVisible: false, sortable: true, sortValue: (profile) => profile.uuid, render: (profile) => <Copyable value={profile.uuid} /> },
    { key: "online", header: t("admin.presences.onlineState"), minWidth: "120px", sortable: true, sortValue: (profile) => profile.online, render: (profile) => <StatusBadge status={profile.online ? "online" : "offline_status"} /> },
    { key: "ban", header: t("admin.bans.heading"), minWidth: "180px", sortable: true, sortValue: (profile) => profile.ban_expires_at ?? "", render: (profile) => <BanStateCell ban={profile.active_ban} /> },
    { key: "lockedUntil", header: t("admin.player.lockedUntil"), minWidth: "180px", defaultVisible: false, sortable: true, sortValue: (profile) => profile.locked_until ?? "", render: (profile) => <LockUntilCell lockedUntil={profile.locked_until} /> },
    {
      key: "actions",
      header: "",
      mandatory: true,
      width: "112px",
      minWidth: "112px",
      align: "right",
      sticky: "right",
      render: (profile) => (
        <Button
          size="sm"
          variant="danger-soft"
          icon="link"
          loading={unbindMut.isPending && unbindMut.variables === profile.id}
          onClick={(e) => {
            e.stopPropagation();
            unbindMut.mutate(profile.id);
          }}
        >
          {t("admin.profiles.unbind")}
        </Button>
      ),
    },
  ];

  return (
    <div className="page">
      <div className="detail-toolbar">
        <button type="button" className="back-link" onClick={() => navigate("/passports")}>
          <Icon name="arrowLeft" size={15} />
          {t("admin.passports.heading")}
        </button>
        <Tabs
          value={tab}
          onChange={setTab}
          tabs={[
            { value: "overview", label: t("common.overview"), icon: "gauge" },
            { value: "skin", label: t("admin.skins.heading"), icon: "user" },
            { value: "audit", label: t("admin.player.audit"), icon: "list" },
          ]}
        />
      </div>
      <div className="detail-grid">
        <div className="detail-aside">
          <DetailSummary
            title={p.username}
            avatarUrl={p.avatar_url}
            avatarText={(p.username || "?")[0]}
            titleMeta={<StatusBadge status={p.online ? "online" : "offline_status"} />}
            meta={<><TypeBadge kind={p.kind} /><StatusBadge status={p.status} /></>}
          />
          <DetailActions title={t("admin.player.actions")}>
            {activeBan ? (
              <>
                <Button variant="secondary" icon="unlock" block onClick={() => { setRevokeTarget(activeBan); setDialog("revokeBan"); }}>{t("admin.bans.unban")}</Button>
                <Button variant="danger-soft" icon="alert" block onClick={() => { setBanReason(""); resetDuration(setBanDurationValue, setBanDurationUnit); setDialog("extendBan"); }}>{t("admin.bans.append")}</Button>
              </>
            ) : (
              <Button variant="danger-soft" icon="alert" block onClick={() => { setBanReason(""); resetDuration(setBanDurationValue, setBanDurationUnit); setDialog("ban"); }}>{t("admin.bans.banPassport")}</Button>
            )}
            {p.status !== "locked" ? (
              <Button variant="secondary" icon="lock" block onClick={() => { setNextStatus("locked"); setDialog("status"); }}>{t("admin.player.lock")}</Button>
            ) : (
              <Button variant="secondary" icon="unlock" block onClick={() => { setNextStatus("active"); setDialog("status"); }}>{t("admin.player.unlock")}</Button>
            )}
            {p.status !== "deleted" ? (
              <Button variant="danger" icon="trash" block onClick={() => { setNextStatus("deleted"); setDialog("status"); }}>{t("common.delete")}</Button>
            ) : null}
          </DetailActions>
        </div>
        <div className="detail-body">
          {tab === "overview" ? (
            <>
              <Card title={t("admin.passports.identity")}>
                <DefList>
                  <DefRow k="UUID"><Copyable value={p.uuid} /></DefRow>
                  <DefRow k={t("common.username")}>{p.username}</DefRow>
                  <DefRow k={t("admin.presences.onlineState")}><StatusBadge status={p.online ? "online" : "offline_status"} /></DefRow>
                  <DefRow k={t("admin.players.col.lastSeen")}>{formatAbsTime(p.last_seen_at)}</DefRow>
                  <DefRow k={t("admin.players.col.lastSeenIp")}><IPLocation ip={p.last_seen_ip} geo={p.last_seen_geo} /></DefRow>
                </DefList>
              </Card>
              <Card noBody className="table-card">
                {p.profiles.length ? (
                  <AdvancedList
                    title={t("admin.passports.profiles")}
                    mode="client"
                    columns={boundProfileColumns}
                    rowKey={(profile) => profile.id}
                    rows={p.profiles}
                    state={profileList.state}
                    onStateChange={profileList.setState}
                    pageSizeOptions={[5, 10, 25]}
                    primaryActions={
                      <>
                        <Button size="sm" variant="secondary" icon="link" onClick={() => { setSelectedProfileID(""); setDialog("bind"); }}>{t("admin.profiles.bind")}</Button>
                        <Button size="sm" variant="primary" icon="plus" onClick={() => { setProtocolName(""); setDialog("create"); }}>{t("admin.profiles.create")}</Button>
                      </>
                    }
                    onRowClick={(profile) => navigate(`/profiles/${profile.id}`)}
                    testId="passport-bound-profiles"
                  />
                ) : (
                  <div className="adv-list">
                    <div className="adv-list-toolbar">
                      <div className="adv-list-toolbar__left">
                        <div className="adv-list-title">
                          <h3>{t("admin.passports.profiles")}</h3>
                        </div>
                      </div>
                      <div className="adv-list-toolbar__right">
                        <div className="adv-list-primary-actions">
                          <Button size="sm" variant="secondary" icon="link" onClick={() => { setSelectedProfileID(""); setDialog("bind"); }}>{t("admin.profiles.bind")}</Button>
                          <Button size="sm" variant="primary" icon="plus" onClick={() => { setProtocolName(""); setDialog("create"); }}>{t("admin.profiles.create")}</Button>
                        </div>
                      </div>
                    </div>
                    <p className="muted-cell" style={{ padding: 16 }}>{t("admin.passports.noProfiles")}</p>
                  </div>
                )}
              </Card>
              <Card title={t("admin.bans.heading")}>
                {p.bans?.length ? (
                  <div className="ban-list">
                    {p.bans.map((ban) => (
                      <div key={ban.id} className="ban-row">
                        <div>
                          <strong>{ban.reason}</strong>
                          <p className="muted-cell">{banTimeText(ban, t)}</p>
                        </div>
                        {isActiveBan(ban) ? (
                          <Button size="sm" variant="secondary" icon="unlock" onClick={() => { setRevokeTarget(ban); setDialog("revokeBan"); }}>
                            {t("admin.bans.revoke")}
                          </Button>
                        ) : null}
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="muted-cell">{t("admin.bans.empty")}</p>
                )}
              </Card>
              {p.credential ? (
                <Card title={t("account.security")}>
                  <DefList>
                    <DefRow k={t("admin.player.failedAttempts")}>{p.credential.failed_attempts}</DefRow>
                    <DefRow k={t("admin.player.passwordUpdated")}>{formatAbsTime(p.credential.password_updated_at)}</DefRow>
                    <DefRow k={t("admin.player.lockedUntil")}>{formatAbsTime(p.credential.locked_until)}</DefRow>
                  </DefList>
                </Card>
              ) : null}
            </>
          ) : tab === "skin" ? (
            <div className="skin-detail-grid">
              <Card title={t("admin.skins.preview")}>
                <MinecraftSkinPreview
                  skinUrl={previewSkinURL ?? p.skin.skin_url}
                  capeUrl={previewCapeURL ?? p.skin.cape_url}
                  elytraUrl={previewElytraURL ?? p.skin.elytra_url}
                  model={skinModel}
                />
              </Card>
              <Card title={t("admin.skins.state")}>
                <DefList>
                  <DefRow k={t("admin.skins.configuredSource")}>{t(`admin.skins.source.${p.skin.source}`)}</DefRow>
                  <DefRow k={t("admin.skins.effectiveSource")}>{t(`admin.skins.source.${p.skin.effective_source}`)}</DefRow>
                  <DefRow k={t("admin.skins.model")}>{t(`admin.skins.model.${p.skin.model === "slim" ? "slim" : "wide"}`)}</DefRow>
                  <DefRow k={t("admin.skins.defaultVariant")}>{p.skin.default_variant} · {p.skin.default_model}</DefRow>
                  <DefRow k={t("admin.skins.customSkin")}>{p.skin.has_custom_skin ? t("common.yes") : t("common.no")}</DefRow>
                  <DefRow k={t("admin.skins.customCape")}>{p.skin.has_custom_cape ? t("common.yes") : t("common.no")}</DefRow>
                  <DefRow k={t("admin.skins.customElytra")}>{p.skin.has_custom_elytra ? t("common.yes") : t("common.no")}</DefRow>
                  <DefRow k={t("common.updated")}>{formatAbsTime(p.skin.updated_at)}</DefRow>
                </DefList>
              </Card>
              <Card title={t("admin.skins.sourceSettings")}>
                <label className="toggle-row">
                  <input
                    type="checkbox"
                    checked={usingUpstreamSkin}
                    disabled={skinSourceMut.isPending}
                    onChange={(event) => skinSourceMut.mutate(event.currentTarget.checked)}
                  />
                  <span>
                    <strong>{t("admin.skins.useUpstream")}</strong>
                    <small>{t("admin.skins.useUpstreamHint")}</small>
                  </span>
                </label>
              </Card>
              {!usingUpstreamSkin ? (
                <Card title={t("admin.skins.passportUpload")}>
                  <div className="skin-upload-grid">
                    <Field label={t("admin.skins.model")}>
                      <Select<"wide" | "slim">
                        value={skinModel}
                        onChange={setSkinModel}
                        options={[
                          { value: "wide", label: t("admin.skins.model.wide") },
                          { value: "slim", label: t("admin.skins.model.slim") },
                        ]}
                      />
                    </Field>
                    <SkinFilePicker id="passport-skin-file" label={t("admin.skins.file.skin")} file={skinFile} onChange={setSkinFile} required />
                    <SkinFilePicker id="passport-cape-file" label={t("admin.skins.file.cape")} file={capeFile} onChange={setCapeFile} />
                    <SkinFilePicker id="passport-elytra-file" label={t("admin.skins.file.elytra")} file={elytraFile} onChange={setElytraFile} />
                  </div>
                  <div className="skin-actions">
                    <Button icon="check" loading={skinMut.isPending} disabled={!hasSkinChanges} onClick={() => skinMut.mutate()}>{t("admin.skins.saveCustom")}</Button>
                    <Button variant="secondary" icon="refresh" loading={skinDeleteMut.isPending} disabled={!p.skin.has_custom_skin} onClick={() => skinDeleteMut.mutate()}>{t("admin.skins.reset")}</Button>
                  </div>
                  <p className="card-copy">{t("admin.skins.passportUploadHint")}</p>
                </Card>
              ) : null}
            </div>
          ) : (
            <Card noBody className="table-card">
              <AuditEventList title={t("admin.player.audit")} baseFilters={{ related_id: relatedAuditIDs }} filterable={false} testId="passport-audit" urlPrefix="paAudit" />
            </Card>
          )}
        </div>
      </div>
      <Dialog
        open={dialog === "status"}
        onClose={() => !statusMut.isPending && setDialog(null)}
        icon="settings"
        title={t("admin.passports.statusDialog")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDialog(null)}>{t("common.cancel")}</Button>
            <Button variant={nextStatus === "deleted" ? "danger" : "primary"} loading={statusMut.isPending} onClick={() => nextStatus && statusMut.mutate(nextStatus)}>
              {t("common.confirm")}
            </Button>
          </>
        }
      >
        <p>{t("admin.passports.statusDialogBody")}</p>
      </Dialog>
      <Dialog
        open={dialog === "bind"}
        onClose={() => !bindMut.isPending && setDialog(null)}
        icon="link"
        title={t("admin.passports.bindProfile")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDialog(null)}>{t("common.cancel")}</Button>
            <Button variant="primary" loading={bindMut.isPending} disabled={!selectedProfileID} onClick={() => bindMut.mutate()}>{t("common.confirm")}</Button>
          </>
        }
      >
        <Field label={t("admin.passports.unboundProfiles")}>
          <Select
            value={selectedProfileID}
            options={profileOptions}
            onChange={setSelectedProfileID}
            placeholder={unboundQ.isLoading ? t("common.loading") : t("admin.passports.selectProfile")}
            disabled={unboundQ.isLoading || profileOptions.length === 0}
          />
        </Field>
        {!unboundQ.isLoading && profileOptions.length === 0 ? <p className="muted-cell">{t("admin.passports.noUnboundProfiles")}</p> : null}
      </Dialog>
      <Dialog
        open={dialog === "create"}
        onClose={() => !createMut.isPending && setDialog(null)}
        icon="plus"
        title={t("admin.profiles.create")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDialog(null)}>{t("common.cancel")}</Button>
            <Button variant="primary" loading={createMut.isPending} disabled={!protocolName.trim()} onClick={() => createMut.mutate()}>{t("common.confirm")}</Button>
          </>
        }
      >
        <Field label={t("admin.profiles.col.protocol")}>
          <Input value={protocolName} onChange={(e) => setProtocolName(e.target.value)} autoFocus />
        </Field>
      </Dialog>
      <Dialog
        open={dialog === "ban"}
        onClose={() => !banMut.isPending && setDialog(null)}
        icon="alert"
        iconTone="danger"
        title={t("admin.bans.banPassport")}
        footer={<><Button variant="ghost" onClick={() => setDialog(null)}>{t("common.cancel")}</Button><Button variant="danger" loading={banMut.isPending} disabled={!banReason.trim() || !banDurationValid} onClick={() => banMut.mutate()}>{t("common.confirm")}</Button></>}
      >
        <Field label={t("admin.bans.reason")}>
          <Input value={banReason} onChange={(e) => setBanReason(e.target.value)} autoFocus />
        </Field>
        <DurationField
          t={t}
          value={banDurationValue}
          unit={banDurationUnit}
          onValueChange={setBanDurationValue}
          onUnitChange={setBanDurationUnit}
        />
      </Dialog>
      <Dialog
        open={dialog === "extendBan"}
        onClose={() => !extendMut.isPending && setDialog(null)}
        icon="alert"
        iconTone="danger"
        title={t("admin.bans.append")}
        footer={<><Button variant="ghost" onClick={() => setDialog(null)}>{t("common.cancel")}</Button><Button variant="danger" loading={extendMut.isPending} disabled={!banDurationValid} onClick={() => extendMut.mutate()}>{t("common.confirm")}</Button></>}
      >
        <DurationField
          t={t}
          value={banDurationValue}
          unit={banDurationUnit}
          onValueChange={setBanDurationValue}
          onUnitChange={setBanDurationUnit}
          autoFocus
        />
        <Field label={t("admin.bans.note")}>
          <Input value={banReason} onChange={(e) => setBanReason(e.target.value)} />
        </Field>
      </Dialog>
      <Dialog
        open={dialog === "revokeBan"}
        onClose={() => !revokeMut.isPending && setDialog(null)}
        icon="unlock"
        title={t("admin.bans.revoke")}
        footer={<><Button variant="ghost" onClick={() => setDialog(null)}>{t("common.cancel")}</Button><Button variant="primary" loading={revokeMut.isPending} onClick={() => revokeMut.mutate()}>{t("common.confirm")}</Button></>}
      >
        <p className="dialog-note">{revokeTarget?.reason}</p>
      </Dialog>
    </div>
  );
}

function banTimeText(ban: PlayerBan, t: (key: string, fallback?: string) => string) {
  if (ban.revoked_at) return t("admin.bans.revokedAt").replace("{time}", formatAbsTime(ban.revoked_at));
  if (ban.expires_at) return t("admin.bans.expiresAt").replace("{time}", formatAbsTime(ban.expires_at));
  return t("admin.bans.permanent");
}

function SkinFilePicker({
  id,
  label,
  file,
  onChange,
  required,
}: {
  id: string;
  label: string;
  file: File | null;
  onChange: (file: File | null) => void;
  required?: boolean;
}) {
  return (
    <Field label={label}>
      <div className="skin-file-picker">
        <input
          id={id}
          className="skin-file-picker__input"
          type="file"
          accept="image/png"
          onChange={(event) => onChange(event.currentTarget.files?.[0] ?? null)}
          required={required}
        />
        <label className="skin-file-picker__button" htmlFor={id}>
          <Icon name="plus" size={14} />
          {file ? file.name : label}
        </label>
        {file ? (
          <button type="button" className="skin-file-picker__clear" onClick={() => onChange(null)} aria-label="Clear">
            <Icon name="close" size={13} />
          </button>
        ) : null}
      </div>
    </Field>
  );
}

function useObjectURL(file: File | null) {
  const [url, setURL] = useState<string | null>(null);
  useEffect(() => {
    if (!file) {
      setURL(null);
      return undefined;
    }
    const next = URL.createObjectURL(file);
    setURL(next);
    return () => URL.revokeObjectURL(next);
  }, [file]);
  return url;
}

function normalizeSkinModel(model: string | undefined): "wide" | "slim" {
  return model === "slim" ? "slim" : "wide";
}

function DurationField({
  t,
  value,
  unit,
  onValueChange,
  onUnitChange,
  autoFocus,
}: {
  t: (key: string, fallback?: string) => string;
  value: string;
  unit: DurationUnit;
  onValueChange: (value: string) => void;
  onUnitChange: (unit: DurationUnit) => void;
  autoFocus?: boolean;
}) {
  return (
    <Field label={t("admin.bans.duration")}>
      <div className="duration-input-row">
        <Input type="number" min={1} step={1} value={value} onChange={(e) => onValueChange(e.target.value)} autoFocus={autoFocus} />
        <Select<DurationUnit> value={unit} options={durationUnitOptions(t)} onChange={onUnitChange} ariaLabel={t("admin.bans.duration")} />
      </div>
    </Field>
  );
}

function durationUnitOptions(t: (key: string, fallback?: string) => string) {
  return (["s", "min", "h", "d", "w", "m", "y"] as DurationUnit[]).map((unit) => ({
    value: unit,
    label: t(`admin.bans.unit.${unit}`),
  }));
}

function isValidDuration(value: string) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0;
}

function durationSeconds(value: string, unit: DurationUnit) {
  const parsed = Number(value);
  const multipliers: Record<DurationUnit, number> = {
    s: 1,
    min: 60,
    h: 60 * 60,
    d: 24 * 60 * 60,
    w: 7 * 24 * 60 * 60,
    m: 30 * 24 * 60 * 60,
    y: 365 * 24 * 60 * 60,
  };
  return Math.max(1, Math.floor(parsed * multipliers[unit]));
}

function resetDuration(setValue: (value: string) => void, setUnit: (unit: DurationUnit) => void) {
  setValue(DEFAULT_BAN_VALUE);
  setUnit(DEFAULT_BAN_UNIT);
}

function isActiveBan(ban: PlayerBan) {
  if (ban.revoked_at) return false;
  if (!ban.expires_at) return true;
  return new Date(ban.expires_at).getTime() > Date.now();
}

function firstActiveBan(bans: PlayerBan[]) {
  return bans.find(isActiveBan) ?? null;
}
