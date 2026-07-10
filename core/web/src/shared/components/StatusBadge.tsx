import { Badge, type BadgeTone } from "./Badge";
import { useI18n } from "../i18n/I18nProvider";

export type PlayerStatus = "active" | "locked" | "pending_verification" | "deleted" | "archived" | "online" | "offline_status" | "expired" | "used" | "revoked";

interface Map {
  tone: BadgeTone;
  key: string;
}

const MAP: Record<PlayerStatus, Map> = {
  active: { tone: "success", key: "status.active" },
  locked: { tone: "danger", key: "status.locked" },
  pending_verification: { tone: "warning", key: "status.pending" },
  deleted: { tone: "neutral", key: "status.deleted" },
  archived: { tone: "neutral", key: "status.archived" },
  online: { tone: "success", key: "status.online" },
  offline_status: { tone: "neutral", key: "status.offline" },
  expired: { tone: "neutral", key: "status.expired" },
  used: { tone: "neutral", key: "status.used" },
  revoked: { tone: "danger", key: "status.revoked" },
};

export function StatusBadge({ status }: { status: PlayerStatus | string }) {
  const { t } = useI18n();
  const m = (MAP as Record<string, Map | undefined>)[status] ?? { tone: "neutral" as BadgeTone, key: status };
  return (
    <Badge tone={m.tone} dot>
      {t(m.key, status)}
    </Badge>
  );
}
