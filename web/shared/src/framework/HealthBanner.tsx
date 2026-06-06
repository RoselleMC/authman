import type { ReactNode } from "react";
import { Icon, type IconName } from "../components/Icon";
import { cx } from "../utils/cx";

export type HealthTone = "success" | "warning" | "danger" | "neutral";

interface Props {
  tone: HealthTone;
  icon?: IconName | string;
  title: ReactNode;
  desc?: ReactNode;
  testId?: string;
}

const DEFAULT_ICON: Record<HealthTone, IconName> = {
  success: "shield",
  warning: "alert",
  danger: "alert",
  neutral: "info",
};

/**
 * Full-bleed banner used at the top of operational pages (Overview, Mojang)
 * to surface the overall health of a subsystem.
 *
 * The tone never throws on unknown values — use coerceMojangOverall or
 * similar to derive `tone` from a backend status code before passing it in.
 */
export function HealthBanner({ tone, icon, title, desc, testId }: Props) {
  return (
    <div className={cx("health-banner", `health-banner--${tone === "neutral" ? "success" : tone}`)} data-testid={testId}>
      <div className="health-ico">
        <Icon name={icon ?? DEFAULT_ICON[tone]} size={20} />
      </div>
      <div>
        <p className="health-title">{title}</p>
        {desc ? <p className="health-desc">{desc}</p> : null}
      </div>
    </div>
  );
}
