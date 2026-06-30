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
}

export function IPLocation({ ip, geo, compact = false }: Props) {
  const { locale, t } = useI18n();
  const displayIP = ip || geo?.ip || "";
  if (!displayIP) return <span className="muted-cell">{t("common.none")}</span>;
  const loc = geo?.locales?.[locale] ?? geo?.locales?.en ?? geo?.locales?.zh;
  const parts = [loc?.country, loc?.region, loc?.city].filter(Boolean);
  const location = parts.length ? parts.join(" / ") : t("geo.unknown");
  const countryCode = geo?.country_code?.toLowerCase();
  const title = [displayIP, parts.join(", "), geo?.isp, geo?.asn].filter(Boolean).join(" · ");
  const localNetwork = countryCode === "un" || countryCode === "local";
  return (
    <span className="ip-location" title={title}>
      {localNetwork ? (
        <span className="ip-location__flag ip-location__flag--local" aria-hidden="true">
          <Icon name="globe" size={14} />
        </span>
      ) : countryCode ? (
        <img className="ip-location__flag" src={countryFlagUrl(countryCode)} alt="" aria-hidden="true" />
      ) : null}
      <span className="ip-location__main">
        <code className="mono">{displayIP}</code>
        {!compact ? <span className="ip-location__place">{location}</span> : null}
      </span>
    </span>
  );
}
