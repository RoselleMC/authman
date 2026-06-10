import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
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
  useToast,
  useBackTarget,
} from "@authman/shared";
import { bindProfile, createProfileBan, deleteProfileSkin, extendBan, fetchPassports, fetchProfile, kickPresence, revokeBan, unbindProfile, updateProfileSkinSource, updateProfileStatus, uploadProfileSkin, type PlayerBan, type ProfileRow } from "../api/admin";
import { AuditEventList } from "../components/AuditEventList";
import { ErrorBlock } from "../components/ErrorBlock";
import { MinecraftSkinPreview } from "../components/MinecraftSkinPreview";

type DialogState = null | "status" | "bind" | "unbind" | "ban" | "extendBan" | "revokeBan";
type DetailTab = "overview" | "skin" | "audit";
type DurationUnit = "s" | "min" | "h" | "d" | "w" | "m" | "y";

const DEFAULT_BAN_VALUE = "1";
const DEFAULT_BAN_UNIT: DurationUnit = "d";

export function ProfileDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const backTarget = useBackTarget("/profiles");
  const toast = useToast();
  const qc = useQueryClient();
  const [dialog, setDialog] = useState<DialogState>(null);
  const [status, setStatus] = useState<ProfileRow["status"]>("active");
  const [passportID, setPassportID] = useState("");
  const [tab, setTab] = useState<DetailTab>("overview");
  const [banReason, setBanReason] = useState("");
  const [banDurationValue, setBanDurationValue] = useState(DEFAULT_BAN_VALUE);
  const [banDurationUnit, setBanDurationUnit] = useState<DurationUnit>(DEFAULT_BAN_UNIT);
  const [revokeTarget, setRevokeTarget] = useState<PlayerBan | null>(null);
  const [skinFile, setSkinFile] = useState<File | null>(null);
  const [capeFile, setCapeFile] = useState<File | null>(null);
  const [elytraFile, setElytraFile] = useState<File | null>(null);
  const [skinModel, setSkinModel] = useState<"wide" | "slim">("wide");
  const q = useQuery({ queryKey: ["admin.profile", id], queryFn: () => fetchProfile(id), enabled: !!id });
  const previewSkinURL = useObjectURL(skinFile);
  const previewCapeURL = useObjectURL(capeFile);
  const previewElytraURL = useObjectURL(elytraFile);
  const passportsQ = useQuery({
    queryKey: ["admin.passports.bind-options"],
    queryFn: ({ signal }) => fetchPassports({ page: 1, page_size: 200 }, signal),
    enabled: dialog === "bind",
  });
  const statusMut = useMutation({
    mutationFn: () => updateProfileStatus(id, status),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("common.saved") });
      setDialog(null);
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const bindMut = useMutation({
    mutationFn: () => bindProfile(id, passportID, true),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.profiles.bound") });
      setDialog(null);
      setPassportID("");
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const unbindMut = useMutation({
    mutationFn: () => unbindProfile(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.profiles.unbound") });
      setDialog(null);
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const banMut = useMutation({
    mutationFn: () => createProfileBan(id, { reason: banReason, expires_in_seconds: durationSeconds(banDurationValue, banDurationUnit) }),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.bans.created") });
      setDialog(null);
      setBanReason("");
      resetDuration(setBanDurationValue, setBanDurationUnit);
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
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
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const kickPresenceMut = useMutation({
    mutationFn: (presenceID: string) => kickPresence(presenceID, t("admin.presences.kickReasonDefault")),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.presences.kicked") });
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const revokeMut = useMutation({
    mutationFn: () => revokeBan(revokeTarget?.id ?? "", ""),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.bans.revoked") });
      setDialog(null);
      setRevokeTarget(null);
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const skinMut = useMutation({
    mutationFn: () => {
      if (!skinFile && !q.data?.skin.has_custom_skin) throw new Error("skin file missing");
      return uploadProfileSkin(id, { skin: skinFile, cape: capeFile, elytra: elytraFile, model: skinModel });
    },
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.skins.saved") });
      setSkinFile(null);
      setCapeFile(null);
      setElytraFile(null);
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const skinDeleteMut = useMutation({
    mutationFn: () => deleteProfileSkin(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.skins.resetDone") });
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
    },
    onError: () => toast.danger(t("common.unknown")),
  });
  const skinSourceMut = useMutation({
    mutationFn: (usePassportSkin: boolean) => updateProfileSkinSource(id, { use_passport_skin: usePassportSkin }),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.skins.sourceSaved") });
      void qc.invalidateQueries({ queryKey: ["admin.profile", id] });
      void qc.invalidateQueries({ queryKey: ["admin.profiles"] });
      void qc.invalidateQueries({ queryKey: ["admin.passports"] });
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
  const usingPassportSkin = Boolean(p.skin.use_passport_skin);
  const persistedSkinModel = normalizeSkinModel(p.skin.model);
  const hasSkinChanges = !usingPassportSkin && Boolean(skinFile || capeFile || elytraFile || (p.skin.has_custom_skin && skinModel !== persistedSkinModel));
  const activeBan = firstActiveBan(p.bans);
  const banDurationValid = isValidDuration(banDurationValue);
  const relatedAuditIDs = [p.id, p.uuid, p.passport?.id].filter(Boolean).join(",");
  const passportOptions = (passportsQ.data?.rows ?? [])
    .filter((passport) => passport.status !== "deleted")
    .map((passport) => ({ value: passport.id, label: `${passport.username} · ${passport.uuid}` }));

  return (
    <div className="page">
      <div className="detail-toolbar">
        <button type="button" className="back-link" onClick={() => navigate(backTarget)}>
          <Icon name="arrowLeft" size={15} />
          {t("common.back")}
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
            title={p.protocol_name}
            avatarUrl={p.skin?.avatar_url ?? p.avatar_url}
            icon="user"
            titleMeta={<StatusBadge status={p.online ? "online" : "offline_status"} />}
            meta={<><span className="muted-cell">{t("admin.profiles.profileIdentity")}</span><StatusBadge status={p.status} /></>}
          />
          <DetailActions title={t("admin.player.actions")}>
            {activeBan ? (
              <>
                <Button variant="secondary" icon="unlock" block onClick={() => { setRevokeTarget(activeBan); setDialog("revokeBan"); }}>{t("admin.bans.unban")}</Button>
                <Button variant="danger-soft" icon="alert" block onClick={() => { setBanReason(""); resetDuration(setBanDurationValue, setBanDurationUnit); setDialog("extendBan"); }}>{t("admin.bans.append")}</Button>
              </>
            ) : (
              <Button variant="danger-soft" icon="alert" block onClick={() => { setBanReason(""); resetDuration(setBanDurationValue, setBanDurationUnit); setDialog("ban"); }}>{t("admin.bans.banProfile")}</Button>
            )}
            {p.status === "archived" ? (
              <Button variant="secondary" icon="refresh" block onClick={() => { setStatus("active"); setDialog("status"); }}>{t("admin.profiles.restore")}</Button>
            ) : (
              <>
                <Button variant="secondary" icon={p.status === "locked" ? "unlock" : "lock"} block onClick={() => { setStatus(p.status === "locked" ? "active" : "locked"); setDialog("status"); }}>
                  {p.status === "locked" ? t("admin.player.unlock") : t("admin.player.lock")}
                </Button>
                <Button variant="secondary" icon="box" block onClick={() => { setStatus("archived"); setDialog("status"); }}>{t("admin.profiles.archive")}</Button>
              </>
            )}
            <Button variant="secondary" icon="link" block onClick={() => { setPassportID(""); setDialog("bind"); }}>{t("admin.profiles.bind")}</Button>
            {p.passport ? <Button variant="danger" icon="link" block onClick={() => setDialog("unbind")}>{t("admin.profiles.unbind")}</Button> : null}
          </DetailActions>
        </div>
        <div className="detail-body">
          {tab === "overview" ? (
            <>
              <Card title={t("admin.profiles.identity")}>
                <DefList>
                  <DefRow k="UUID"><Copyable value={p.uuid} /></DefRow>
                  <DefRow k={t("admin.profiles.col.protocol")}>{p.protocol_name}</DefRow>
                  <DefRow k={t("admin.presences.onlineState")}><StatusBadge status={p.online ? "online" : "offline_status"} /></DefRow>
                  <DefRow k={t("admin.profiles.skinSource")}>{p.skin_source}</DefRow>
                  <DefRow k={t("admin.players.col.lastSeen")}>{formatAbsTime(p.last_seen_at)}</DefRow>
                  <DefRow k={t("admin.players.col.lastSeenIp")}><IPLocation ip={p.last_seen_ip} geo={p.last_seen_geo} /></DefRow>
                </DefList>
              </Card>
              <Card title={t("admin.profiles.passport")}>
                {p.passport ? (
                  <DefList>
                    <DefRow k="UUID"><Link to={`/passports/${p.passport.id}`} state={{ backTo: `${location.pathname}${location.search}${location.hash}` }}>{p.passport.id}</Link></DefRow>
                    <DefRow k={t("common.username")}>{p.passport.username}</DefRow>
                    <DefRow k={t("admin.players.col.type")}><TypeBadge kind={p.passport.kind} /></DefRow>
                    <DefRow k={t("admin.players.col.status")}><StatusBadge status={p.passport.status} /></DefRow>
                  </DefList>
                ) : (
                  <p className="muted-cell">{t("admin.profiles.unboundState")}</p>
                )}
              </Card>
              <Card title={t("admin.presences.heading")}>
                {p.presences?.length ? (
                  <div className="presence-list">
                    {p.presences.map((presence) => (
                      <div key={presence.id} className="presence-row">
                        <div>
                          <strong>{presence.server_id}</strong>
                          <p className="muted-cell">
                            {presence.node_id || "—"} · {presence.remote_addr || "—"} · {formatAbsTime(presence.connected_at)}
                          </p>
                        </div>
                        <Button
                          size="sm"
                          variant="danger-soft"
                          icon="close"
                          loading={kickPresenceMut.isPending && kickPresenceMut.variables === presence.id}
                          onClick={() => kickPresenceMut.mutate(presence.id)}
                        >
                          {t("admin.presences.kick")}
                        </Button>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="muted-cell">{t("admin.presences.empty")}</p>
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
              <Card title={t("admin.player.profileProperties")}>
                {p.properties?.length ? (
                  <DefList>
                    {p.properties.map((prop) => <DefRow key={prop.name} k={prop.name}><Copyable value={prop.value} truncate={16} /></DefRow>)}
                  </DefList>
                ) : (
                  <p className="muted-cell">{t("common.none")}</p>
                )}
              </Card>
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
                  <DefRow k={t("admin.skins.effectiveSource")}>{t(`admin.skins.source.${p.skin.effective_source}`)}</DefRow>
                  <DefRow k={t("admin.skins.model")}>{t(`admin.skins.model.${p.skin.model === "slim" ? "slim" : "wide"}`)}</DefRow>
                  <DefRow k={t("admin.skins.defaultVariant")}>{p.skin.default_variant} · {p.skin.default_model}</DefRow>
                  <DefRow k={t("admin.skins.customSkin")}>{p.skin.has_custom_skin ? t("common.yes") : t("common.no")}</DefRow>
                  <DefRow k={t("admin.skins.customCape")}>{p.skin.has_custom_cape ? t("common.yes") : t("common.no")}</DefRow>
                  <DefRow k={t("admin.skins.customElytra")}>{p.skin.has_custom_elytra ? t("common.yes") : t("common.no")}</DefRow>
                  <DefRow k={t("admin.skins.inheritPassport")}>{usingPassportSkin ? t("common.yes") : t("common.no")}</DefRow>
                  <DefRow k={t("common.updated")}>{formatAbsTime(p.skin.updated_at)}</DefRow>
                </DefList>
              </Card>
              {p.passport ? (
                <Card title={t("admin.skins.sourceSettings")}>
                  <label className="toggle-row skin-inherit-toggle">
                    <input
                      type="checkbox"
                      checked={usingPassportSkin}
                      disabled={skinSourceMut.isPending}
                      onChange={(event) => skinSourceMut.mutate(event.currentTarget.checked)}
                    />
                    <span>
                      <strong>{t("admin.skins.inheritPassport")}</strong>
                      <small>{t("admin.skins.inheritPassportHint")}</small>
                    </span>
                  </label>
                </Card>
              ) : null}
              {!usingPassportSkin ? (
                <Card title={t("admin.skins.upload")}>
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
                    <SkinFilePicker id="profile-skin-file" label={t("admin.skins.file.skin")} file={skinFile} onChange={setSkinFile} required />
                    <SkinFilePicker id="profile-cape-file" label={t("admin.skins.file.cape")} file={capeFile} onChange={setCapeFile} />
                    <SkinFilePicker id="profile-elytra-file" label={t("admin.skins.file.elytra")} file={elytraFile} onChange={setElytraFile} />
                  </div>
                  <div className="skin-actions">
                    <Button icon="check" loading={skinMut.isPending} disabled={!hasSkinChanges} onClick={() => skinMut.mutate()}>{t("admin.skins.saveCustom")}</Button>
                    <Button variant="secondary" icon="refresh" loading={skinDeleteMut.isPending} disabled={!p.skin.has_custom_skin} onClick={() => skinDeleteMut.mutate()}>{t("admin.skins.reset")}</Button>
                  </div>
                  <p className="card-copy">{t("admin.skins.uploadHint")}</p>
                </Card>
              ) : null}
            </div>
          ) : (
            <Card noBody className="table-card">
              <AuditEventList title={t("admin.player.audit")} baseFilters={{ related_id: relatedAuditIDs }} filterable={false} testId="profile-audit" urlPrefix="profileAudit" />
            </Card>
          )}
        </div>
      </div>
      <Dialog
        open={dialog === "status"}
        onClose={() => !statusMut.isPending && setDialog(null)}
        title={t("admin.profiles.statusDialog")}
        icon="settings"
        footer={<><Button variant="ghost" onClick={() => setDialog(null)}>{t("common.cancel")}</Button><Button loading={statusMut.isPending} onClick={() => statusMut.mutate()}>{t("common.confirm")}</Button></>}
      >
        <p>{t("admin.profiles.statusDialogBody")}</p>
      </Dialog>
      <Dialog
        open={dialog === "bind"}
        onClose={() => !bindMut.isPending && setDialog(null)}
        title={t("admin.profiles.bind")}
        icon="link"
        footer={<><Button variant="ghost" onClick={() => setDialog(null)}>{t("common.cancel")}</Button><Button loading={bindMut.isPending} disabled={!passportID} onClick={() => bindMut.mutate()}>{t("common.confirm")}</Button></>}
      >
        <Field label={t("admin.profiles.bind.passportId")}>
          <Select
            value={passportID}
            options={passportOptions}
            onChange={setPassportID}
            placeholder={passportsQ.isLoading ? t("common.loading") : t("admin.passports.selectPassport")}
            disabled={passportsQ.isLoading || passportOptions.length === 0}
          />
        </Field>
        {!passportsQ.isLoading && passportOptions.length === 0 ? <p className="muted-cell">{t("admin.passports.noPassports")}</p> : null}
      </Dialog>
      <Dialog
        open={dialog === "unbind"}
        onClose={() => !unbindMut.isPending && setDialog(null)}
        title={t("admin.profiles.unbind")}
        icon="link"
        footer={<><Button variant="ghost" onClick={() => setDialog(null)}>{t("common.cancel")}</Button><Button variant="danger" loading={unbindMut.isPending} onClick={() => unbindMut.mutate()}>{t("common.confirm")}</Button></>}
      >
        <p>{t("admin.profiles.unbindBody")}</p>
      </Dialog>
      <Dialog
        open={dialog === "ban"}
        onClose={() => !banMut.isPending && setDialog(null)}
        icon="alert"
        iconTone="danger"
        title={t("admin.bans.banProfile")}
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
