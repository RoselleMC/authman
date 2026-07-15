import { useI18n } from "../i18n/I18nProvider";
import { countryFlagUrl } from "../utils/flags";
import { Icon } from "./Icon";

export interface IPGeoLocale {
  country?: string;
  region?: string;
  city?: string;
}

export interface IPGeo {
  ip?: string;
  country_code?: string;
  isp?: string;
  asn?: string;
  locales?: Record<string, IPGeoLocale>;
}

interface Props {
  ip?: string | null;
  geo?: IPGeo | null;
  compact?: boolean;
  onRefresh?: () => void;
  refreshing?: boolean;
  refreshLabel?: string;
}

export function IPLocation({ ip, geo, compact = false, onRefresh, refreshing = false, refreshLabel }: Props) {
  const { locale, t } = useI18n();
  const displayIP = ip || geo?.ip || "";
  if (!displayIP) return <span className="muted-cell">{t("common.none")}</span>;
  const loc = geo?.locales?.[locale] ?? geo?.locales?.en ?? geo?.locales?.zh;
  const parts = [loc?.country, loc?.region, loc?.city].filter(Boolean);
  const location = parts.length ? parts.join(" / ") : t("geo.unknown");
  const countryCode = geo?.country_code?.toLowerCase();
  const title = [displayIP, parts.join(", "), geo?.isp, geo?.asn].filter(Boolean).join(" · ");
  const localNetwork = countryCode === "un" || countryCode === "local";
  const flag = localNetwork ? (
    <span className="ip-location__flag ip-location__flag--local" aria-hidden="true">
      <Icon name="globe" size={14} />
    </span>
  ) : countryCode ? (
    <img className="ip-location__flag" src={countryFlagUrl(countryCode)} alt="" aria-hidden="true" />
  ) : (
    <span className="ip-location__flag ip-location__flag--unknown" aria-hidden="true">
      <Icon name="globe" size={14} />
    </span>
  );
  const label = refreshLabel ?? t("geo.refresh.action");
  return (
    <span className="ip-location" title={title}>
      {onRefresh ? (
        <button
          type="button"
          className="ip-location__refresh"
          onClick={(event) => {
            event.stopPropagation();
            onRefresh();
          }}
          disabled={refreshing}
          aria-label={label}
          title={label}
          aria-busy={refreshing}
          data-testid="ip-location-refresh"
        >
          {flag}
        </button>
      ) : countryCode ? flag : null}
      <span className="ip-location__main">
        <code className="mono">{displayIP}</code>
        {!compact ? <span className="ip-location__place">{location}</span> : null}
      </span>
    </span>
  );
}
