import { useQuery } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import {
  Alert,
  Badge,
  Card,
  DefList,
  DefRow,
  Icon,
  cx,
  useI18n,
} from "@authman/shared";
import { fetchDownstreamServer, type DownstreamServer } from "../api/admin";
import { ErrorBlock } from "../components/ErrorBlock";

function ColorToken({ value }: { value: string }) {
  return (
    <span className="color-token">
      <span className="color-swatch" style={{ background: value }} />
      <code className="mono" style={{ fontSize: 12.5 }}>{value}</code>
    </span>
  );
}

function PortalPreview({ s }: { s: DownstreamServer }) {
  const { t } = useI18n();
  const primary = s.portal_theme.primary_color ?? "var(--color-primary)";
  return (
    <div className={cx("portal-preview")} data-testid="theme-preview">
      <div className="pp-nav">
        <span className="pp-brand">
          <span className="pp-mark" style={{ background: primary }}>{s.display_name[0]}</span>
          {s.display_name}
        </span>
        <span className="pp-toggle">
          <Icon name="moon" size={13} />
        </span>
      </div>
      <div className="pp-body">
        {s.portal_theme.portal_message ? (
          <div className="pp-banner" style={{ background: primary }}>{s.portal_theme.portal_message}</div>
        ) : null}
        <h4 className="pp-title">{t("common.signIn")} · {s.display_name}</h4>
        <div className="pp-field"><span>{t("common.username")}</span></div>
        <div className="pp-field"><span>{t("common.password")}</span></div>
        <div className="pp-btn" style={{ background: primary }}>{t("common.signIn")}</div>
        {s.registration_open ? null : <p className="pp-note">{t("servers.registrationClosed")}</p>}
      </div>
    </div>
  );
}

export function ServerDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const { t } = useI18n();
  const navigate = useNavigate();
  const q = useQuery({
    queryKey: ["admin.server", id],
    queryFn: () => fetchDownstreamServer(id),
    enabled: !!id,
  });

  if (q.error) {
    return (
      <div className="page">
        <ErrorBlock error={q.error} onRetry={() => q.refetch()} />
      </div>
    );
  }
  if (q.isLoading || !q.data) {
    return (
      <div className="page">
        <Card>{t("common.loading")}</Card>
      </div>
    );
  }
  const s = q.data;
  const primary = s.portal_theme.primary_color ?? "var(--color-primary)";

  return (
    <div className="page">
      <button type="button" className="back-link" onClick={() => navigate("/servers")}>
        <Icon name="arrowLeft" size={15} />
        {t("admin.servers.heading")}
      </button>
      <div className="page-head">
        <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
          <span className="ctx-badge" style={{ background: primary, width: 44, height: 44, borderRadius: 10, fontSize: 20 }}>
            {s.display_name[0]}
          </span>
          <div>
            <h2 className="page-title">{s.display_name}</h2>
            <code className="mono" style={{ color: "var(--color-text-muted)", fontSize: 13 }}>/server/{s.slug}</code>
          </div>
        </div>
      </div>

      <div className="detail-grid">
        <div className="detail-aside">
          <Card title={t("admin.servers.detail.configuration")}>
            <DefList>
              <DefRow k={t("admin.servers.detail.displayName")}>{s.display_name}</DefRow>
              <DefRow k="Slug">
                <code className="mono" style={{ fontSize: 12.5 }}>{s.slug}</code>
              </DefRow>
              <DefRow k={t("admin.servers.detail.registration")}>
                <Badge tone={s.registration_open ? "success" : "neutral"} dot>
                  {s.registration_open ? t("common.open") : t("common.closed")}
                </Badge>
              </DefRow>
              <DefRow k={t("admin.servers.detail.extensionData")}>
                {s.extension_providers.length ? (
                  <Badge tone="info">{t("common.available")}</Badge>
                ) : (
                  <Badge tone="neutral">{t("common.none")}</Badge>
                )}
              </DefRow>
            </DefList>
          </Card>
          <Card title={t("admin.servers.detail.themeTokens")}>
            <DefList>
              <DefRow k={t("admin.servers.detail.primary")}><ColorToken value={primary} /></DefRow>
              <DefRow k={t("admin.servers.detail.accent")}><ColorToken value={s.portal_theme.accent_color ?? primary} /></DefRow>
            </DefList>
          </Card>
        </div>
        <div className="detail-body">
          <Card title={t("admin.servers.detail.theme")}>
            <PortalPreview s={s} />
            <div style={{ marginTop: 16 }}>
              <Alert tone="neutral">
                <p>{t("admin.servers.detail.clamped")}</p>
              </Alert>
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
}
