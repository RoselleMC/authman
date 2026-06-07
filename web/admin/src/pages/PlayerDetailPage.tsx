import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  ApiError,
  Badge,
  Button,
  Card,
  Copyable,
  DefList,
  DefRow,
  Dialog,
  EmptyState,
  Icon,
  ProtocolName,
  SchemaRenderer,
  SecretReveal,
  StatusBadge,
  Tabs,
  TypeBadge,
  cx,
  formatAbsTime,
  formatRelativeTime,
  useI18n,
  useToast,
  type TabItem,
} from "@authman/shared";
import {
  fetchPlayer,
  lockPlayer,
  resetPlayerPassword,
  unlockPlayer,
  type PlayerDetail,
} from "../api/admin";
import { useSession } from "../auth/SessionContext";
import { ErrorBlock } from "../components/ErrorBlock";

type DialogKind = null | "lock" | "unlock" | "reset" | "link";
type TabKey = "identity" | "sessions" | "extension";

export function PlayerDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t, tError } = useI18n();
  const navigate = useNavigate();
  const { hasPermission } = useSession();
  const toast = useToast();
  const qc = useQueryClient();
  const [dialog, setDialog] = useState<DialogKind>(null);
  const [tab, setTab] = useState<TabKey>("identity");
  const [linkSecret, setLinkSecret] = useState<string | null>(null);

  const q = useQuery({
    queryKey: ["admin.player", id],
    queryFn: () => fetchPlayer(id),
    enabled: !!id,
  });

  const lockMut = useMutation({
    mutationFn: () => lockPlayer(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.player.lock.toast"), msg: `#${q.data?.raw_name ?? id} - ${t("admin.player.auditChanged")}` });
      void qc.invalidateQueries({ queryKey: ["admin.player", id] });
      setDialog(null);
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });
  const unlockMut = useMutation({
    mutationFn: () => unlockPlayer(id),
    onSuccess: () => {
      toast.push({ tone: "success", title: t("admin.player.unlock.toast"), msg: `#${q.data?.raw_name ?? id} - ${t("admin.player.auditChangedShort")}` });
      void qc.invalidateQueries({ queryKey: ["admin.player", id] });
      setDialog(null);
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });
  const resetMut = useMutation({
    mutationFn: () => resetPlayerPassword(id),
    onSuccess: (res) => {
      toast.push({ tone: "success", title: t("admin.player.reset.toast"), msg: t("admin.player.hint").replace("{hint}", res.reset_token_hint) });
      void qc.invalidateQueries({ queryKey: ["admin.player", id] });
      setDialog(null);
    },
    onError: (err) => toast.danger(err instanceof ApiError ? tError(err.code) : t("common.unknown")),
  });

  if (q.isLoading) {
    return (
      <div className="page">
        <Card>{t("common.loading")}</Card>
      </div>
    );
  }
  if (q.error || !q.data) {
    return (
      <div className="page">
        <ErrorBlock error={q.error} onRetry={() => q.refetch()} />
      </div>
    );
  }
  const p = q.data;
  const canLock = hasPermission("players.lock");
  const canReset = hasPermission("players.password.reset");
  const canLink = hasPermission("players.portal_link.create");

  const sessions = p.sessions ?? [];

  const tabs: TabItem<TabKey>[] = [
    { value: "identity", label: t("admin.player.identity"), icon: "user" },
    { value: "sessions", label: t("admin.player.history"), icon: "clock", count: sessions.length || undefined },
    { value: "extension", label: t("nav.player.extensions"), icon: "box" },
  ];

  return (
    <div className="page">
      <button type="button" className="back-link" onClick={() => navigate("/players")} data-testid="back-link">
        <Icon name="arrowLeft" size={15} />
        {t("admin.players.heading")}
      </button>

      <div className="detail-grid">
        <div className="detail-aside">
          <Card testId="identity-card">
            <div className="id-summary">
              <span className={cx("pa-avatar pa-lg", p.kind === "premium" ? "pa-premium" : "pa-offline")}>
                {p.raw_name[0]}
              </span>
              <div className="id-name-row">
                <h2 className="id-raw">{p.raw_name}</h2>
                <StatusBadge status={p.status} />
              </div>
              <ProtocolName rawName={p.raw_name} kind={p.kind} style={{ fontSize: 15 }} />
              <div style={{ marginTop: 6 }}>
                <TypeBadge kind={p.kind} />
              </div>
            </div>
            <div className="id-uuid">
              <span className="id-uuid-label">UUID</span>
              <Copyable value={p.uuid} />
            </div>
            {p.status === "locked" ? (
              <div style={{ marginTop: 14 }}>
                <Alert tone="danger" title={t("admin.player.locked.title")}>
                  <p>{t("admin.player.locked.body")}</p>
                </Alert>
              </div>
            ) : null}
          </Card>

          <Card title={t("admin.player.actions")}>
            <div className="action-stack">
              {canLock && p.status !== "locked" ? (
                <Button variant="secondary" icon="lock" block onClick={() => setDialog("lock")} data-testid="action-lock">
                  {t("admin.player.lock")}
                </Button>
              ) : null}
              {canLock && p.status === "locked" ? (
                <Button variant="secondary" icon="unlock" block onClick={() => setDialog("unlock")} data-testid="action-unlock">
                  {t("admin.player.unlock")}
                </Button>
              ) : null}
              {canReset && p.kind === "offline" ? (
                <Button variant="secondary" icon="key" block onClick={() => setDialog("reset")} data-testid="action-reset">
                  {t("admin.player.resetPassword")}
                </Button>
              ) : null}
              {canLink ? (
                <Button
                  variant="secondary"
                  icon="link"
                  block
                  onClick={() => {
                    setLinkSecret(null);
                    setDialog("link");
                  }}
                  data-testid="action-link"
                >
                  {t("admin.player.generateLink")}
                </Button>
              ) : null}
              {p.kind === "premium" ? (
                <p className="action-note">
                  <Icon name="info" size={13} />
                  {t("admin.player.premiumNoPassword")}
                </p>
              ) : null}
            </div>
          </Card>
        </div>

        <div className="detail-body">
          <Tabs<TabKey> value={tab} onChange={setTab} tabs={tabs} />
          <div className="tab-panel">
            {tab === "identity" ? <IdentityPanel p={p} /> : null}
            {tab === "sessions" ? <SessionsPanel sessions={sessions} /> : null}
            {tab === "extension" ? <ExtensionPanel p={p} /> : null}
          </div>
        </div>
      </div>

      <Dialog
        open={dialog === "lock"}
        onClose={() => !lockMut.isPending && setDialog(null)}
        icon="lock"
        iconTone="danger"
        title={t("admin.player.confirmLock.title")}
        desc={t("admin.player.confirmLock.body")}
        testId="dialog-lock"
        footer={
          <>
            <Button variant="ghost" onClick={() => setDialog(null)} disabled={lockMut.isPending} data-testid="confirm-cancel">
              {t("common.cancel")}
            </Button>
            <Button variant="danger" icon="lock" loading={lockMut.isPending} onClick={() => lockMut.mutate()} data-testid="confirm-confirm">
              {t("admin.player.lock")}
            </Button>
          </>
        }
      >
        <ConfirmTarget player={p} />
        <p className="dialog-note">{t("admin.player.onlineDisconnectNote")}</p>
      </Dialog>

      <Dialog
        open={dialog === "unlock"}
        onClose={() => !unlockMut.isPending && setDialog(null)}
        icon="unlock"
        iconTone="primary"
        title={t("admin.player.confirmUnlock.title")}
        desc={t("admin.player.confirmUnlock.body")}
        testId="dialog-unlock"
        footer={
          <>
            <Button variant="ghost" onClick={() => setDialog(null)} disabled={unlockMut.isPending} data-testid="confirm-cancel">
              {t("common.cancel")}
            </Button>
            <Button variant="primary" icon="unlock" loading={unlockMut.isPending} onClick={() => unlockMut.mutate()} data-testid="confirm-confirm">
              {t("admin.player.unlock")}
            </Button>
          </>
        }
      >
        <ConfirmTarget player={p} />
      </Dialog>

      <Dialog
        open={dialog === "reset"}
        onClose={() => !resetMut.isPending && setDialog(null)}
        icon="key"
        iconTone="warning"
        title={t("admin.player.confirmReset.title")}
        desc={t("admin.player.confirmReset.body")}
        testId="dialog-reset"
        footer={
          <>
            <Button variant="ghost" onClick={() => setDialog(null)} disabled={resetMut.isPending} data-testid="confirm-cancel">
              {t("common.cancel")}
            </Button>
            <Button variant="primary" icon="key" loading={resetMut.isPending} onClick={() => resetMut.mutate()} data-testid="confirm-confirm">
              {t("admin.player.resetPassword")}
            </Button>
          </>
        }
      >
        <ConfirmTarget player={p} />
      </Dialog>

      <Dialog
        open={dialog === "link"}
        onClose={() => setDialog(null)}
        icon="link"
        iconTone="primary"
        title={linkSecret ? t("admin.player.portalLink.created") : t("admin.player.generateLink")}
        desc={
          linkSecret
            ? t("admin.player.portalLink.copyNow")
            : t("admin.player.portalLink.desc")
        }
        testId="dialog-link"
        footer={
          linkSecret ? (
            <Button
              variant="primary"
              icon="check"
              onClick={() => {
                setDialog(null);
                setLinkSecret(null);
                toast.push({ tone: "info", title: t("admin.player.portalLink.closed"), msg: t("admin.player.portalLink.closedMsg") });
              }}
            >
              {t("admin.player.portalLink.close")}
            </Button>
          ) : (
            <>
              <Button variant="ghost" onClick={() => setDialog(null)} data-testid="confirm-cancel">
                {t("common.cancel")}
              </Button>
              <Button
                variant="primary"
                icon="link"
                onClick={() => setLinkSecret(`amn_lnk_${Math.random().toString(36).slice(2, 12)}${Math.random().toString(36).slice(2, 12)}`)}
                data-testid="confirm-confirm"
              >
                {t("admin.player.portalLink.generate")}
              </Button>
            </>
          )
        }
      >
        {linkSecret ? (
          <SecretReveal
            value={`https://account.example.invalid/link#token=${linkSecret}`}
            valueTestId="link-secret"
            warning={<p>{t("admin.player.portalLink.warning")}</p>}
          />
        ) : (
          <ConfirmTarget player={p} />
        )}
      </Dialog>
    </div>
  );
}

function ConfirmTarget({ player }: { player: PlayerDetail }) {
  const { t } = useI18n();
  return (
    <div className="confirm-target">
      <div className="ct-row">
        <span>{t("admin.player.confirm.protocolName")}</span>
        <ProtocolName rawName={player.raw_name} kind={player.kind} />
      </div>
      <div className="ct-row">
        <span>UUID</span>
        <code className="mono ct-uuid">{player.uuid}</code>
      </div>
    </div>
  );
}

function IdentityPanel({ p }: { p: PlayerDetail }) {
  const { t } = useI18n();
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <Card title={t("admin.player.identity")}>
        <DefList>
          <DefRow k={t("admin.player.rawName")}>{p.raw_name}</DefRow>
          <DefRow k={t("admin.player.confirm.protocolName")}><ProtocolName rawName={p.raw_name} kind={p.kind} /></DefRow>
          <DefRow k="UUID"><Copyable value={p.uuid} /></DefRow>
          <DefRow k={t("admin.player.accountType")}><TypeBadge kind={p.kind} /></DefRow>
          <DefRow k={t("account.registrationServer")}>{p.registration_server_label ?? "—"}</DefRow>
          <DefRow k={t("account.lastSeenServer")}>{p.last_seen_server_label ?? "—"}</DefRow>
          <DefRow k={t("admin.player.created")}>{formatAbsTime(p.created_at)}</DefRow>
        </DefList>
      </Card>
      {p.kind === "premium" ? (
        <Card title={t("admin.player.profileSkin")}>
          <DefList>
            <DefRow k={t("admin.player.profileSource")}>{t("admin.player.profileSource.mojang")}</DefRow>
            <DefRow k={t("admin.player.properties")}>
              {p.profile.properties.length ? (
                <Badge tone="success" dot>{t("admin.player.synced")}</Badge>
              ) : (
                <Badge tone="neutral">{t("common.none")}</Badge>
              )}
            </DefRow>
            <DefRow k={t("admin.player.skinStatus")}>
              <Badge tone="success" dot>{t("common.available")}</Badge>
            </DefRow>
          </DefList>
        </Card>
      ) : (
        <Card title={t("admin.player.offlineCredentials")}>
          <DefList>
            <DefRow k={t("admin.player.password")}>
              {p.offline_credentials?.password_updated_at ? (
                <Badge tone="neutral">{t("admin.player.password.setHashed")}</Badge>
              ) : (
                <Badge tone="warning">{t("admin.player.password.notSet")}</Badge>
              )}
            </DefRow>
            <DefRow k={t("admin.player.failedAttempts")}>
              <span className={cx((p.offline_credentials?.failed_attempts ?? 0) > 5 && "warn-text")}>
                {p.offline_credentials?.failed_attempts ?? 0}
              </span>
            </DefRow>
            <DefRow k={t("admin.player.lockedUntil")}>
              {p.offline_credentials?.locked_until ? formatAbsTime(p.offline_credentials.locked_until) : "—"}
            </DefRow>
          </DefList>
        </Card>
      )}
    </div>
  );
}

function SessionsPanel({ sessions }: { sessions: PlayerDetail["sessions"] }) {
  const { t } = useI18n();
  if (!sessions.length) {
    return (
      <Card>
        <EmptyState icon="clock" title={t("admin.player.sessions.empty")} description={t("admin.player.sessions.empty.desc")} />
      </Card>
    );
  }
  return (
    <Card title={t("admin.player.sessions.title")} noBody>
      <div className="table-scroll">
        <table className="tbl">
          <thead>
            <tr>
              <th>{t("admin.player.sessions.when")}</th>
              <th>{t("admin.player.sessions.result")}</th>
              <th>{t("admin.player.sessions.server")}</th>
              <th>{t("admin.player.sessions.reason")}</th>
            </tr>
          </thead>
          <tbody>
            {sessions.map((s) => (
              <tr key={s.id} style={{ cursor: "default" }}>
                <td>{formatAbsTime(s.created_at)}</td>
                <td>
                  {s.result === "success" ? (
                    <Badge tone="success" dot>{t("common.success")}</Badge>
                  ) : (
                    <Badge tone="danger" dot>{t("common.failed")}</Badge>
                  )}
                </td>
                <td className="muted-cell">{s.server_label ?? "—"}</td>
                <td className="muted-cell">{s.failure_reason ?? "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

function ExtensionPanel({ p }: { p: PlayerDetail }) {
  const { t } = useI18n();
  if (!p.extension_data.length) {
    return (
      <Card>
        <EmptyState icon="box" title={t("admin.player.extension.empty")} />
      </Card>
    );
  }
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }} data-testid="extension-data-card">
      {p.extension_data.map((d) => (
        <Card
          key={`${d.server_slug}-${d.provider}`}
          title={d.schema.title}
          actions={
            <span style={{ display: "flex", alignItems: "center", gap: 10 }}>
              <span className="ext-provider mono">{d.provider}</span>
              <span className="mono" style={{ fontSize: 11, color: "var(--color-text-subtle)" }}>
                {t("admin.extensions.schemaVersion").replace("{version}", String(d.schema.version))}
              </span>
            </span>
          }
          noBody
        >
          <SchemaRenderer data={{ ...d, server_slug: d.server_slug, server_display_name: d.server_display_name }} testId={`ext-${d.server_slug}-${d.provider}`} />
        </Card>
      ))}
    </div>
  );
}

export type { PlayerDetail };
