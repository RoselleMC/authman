import { Badge } from "./Badge";
import { Icon } from "./Icon";
import { useI18n } from "../i18n/I18nProvider";

export type PlayerKind = "premium" | "offline";

export function TypeBadge({ kind }: { kind: PlayerKind }) {
  const { t } = useI18n();
  if (kind === "premium") {
    return (
      <Badge tone="premium">
        <Icon name="shield" size={12} />
        {t("account.type.premium")}
      </Badge>
    );
  }
  return (
    <Badge tone="offline">
      <Icon name="shield" size={12} />
      <span className="non-premium-label">{t("account.type.premium")}</span>
    </Badge>
  );
}
