import {
  Badge,
  Card,
  DefList,
  DefRow,
  PCardGrid,
  PContent,
  PContentHead,
  ProtocolName,
  TypeBadge,
  UuidValue,
  useI18n,
} from "@authman/shared";
import { useSession } from "../auth/SessionContext";

export function AccountPage() {
  const { t } = useI18n();
  const { me } = useSession();
  if (!me) return null;
  const p = me.player;

  return (
    <PContent testId="account-page">
      <PContentHead title={t("account.heading")} desc={t("account.desc")} />

      <div className="p-id-banner">
        <span className={`pa-avatar pa-lg ${p.kind === "premium" ? "pa-premium" : "pa-offline"}`}>{p.raw_name[0]}</span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="p-id-name" data-testid="account-raw">{p.raw_name}</div>
          <ProtocolName rawName={p.raw_name} kind={p.kind} style={{ fontSize: 14 }} />
        </div>
        <TypeBadge kind={p.kind} />
      </div>

      <PCardGrid>
        <Card title={t("account.card.account")}>
          <DefList>
            <DefRow k={t("account.type")}><TypeBadge kind={p.kind} /></DefRow>
            <DefRow k={t("account.rawName")}>{p.raw_name}</DefRow>
            <DefRow k={t("account.inGameName")}><ProtocolName rawName={p.raw_name} kind={p.kind} /></DefRow>
            <DefRow k={t("account.uuid")}><UuidValue uuid={p.uuid} truncate /></DefRow>
          </DefList>
        </Card>
        <Card title={t("account.card.activity")}>
          <DefList>
            <DefRow k={t("account.registrationServer")}>{p.registration_server_label ?? "—"}</DefRow>
            <DefRow k={t("account.lastSeenServer")}>{p.last_seen_server_label ?? "—"}</DefRow>
            <DefRow k={t("account.security")}>
              <Badge tone="success" dot>
                {p.kind === "offline" ? t("account.security.password.managed") : t("account.mojangVerified")}
              </Badge>
            </DefRow>
          </DefList>
        </Card>
      </PCardGrid>

      <Card title={t("account.connectedServers")}>
        {p.connected_servers.length === 0 ? (
          <div style={{ color: "var(--color-text-muted)" }}>—</div>
        ) : (
          <div className="conn-servers">
            {p.connected_servers.map((s) => (
              <div className="conn-server" key={s.slug}>
                <span className="ctx-badge" style={{ background: "var(--color-primary)" }}>
                  {s.display_name[0]}
                </span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="conn-name">{s.display_name}</div>
                  <div className="muted-cell" style={{ fontSize: 12 }}>{s.slug}</div>
                </div>
                <Badge tone="success" dot>{t("account.connected")}</Badge>
              </div>
            ))}
          </div>
        )}
      </Card>
    </PContent>
  );
}
