import type { ReactNode } from "react";
import { Icon, type IconName } from "../components/Icon";
import { Skeleton } from "../components/Skeleton";
import { cx } from "../utils/cx";

export type StatTone = "neutral" | "success" | "warning" | "danger" | "info";

interface StatCardProps {
  icon: IconName | string;
  label: string;
  value?: ReactNode;
  sub?: string;
  tone?: StatTone;
  loading?: boolean;
  testId?: string;
}

/**
 * One tile inside StatGrid. Always shows the label even while loading so the
 * grid does not reflow when data lands.
 */
export function StatCard({ icon, label, value, sub, tone = "neutral", loading, testId }: StatCardProps) {
  const cleanId = label.replace(/\s+/g, "-").toLowerCase();
  return (
    <div className="stat" data-testid={testId ?? `stat-${cleanId}`}>
      <div className={cx("stat-icon", tone !== "neutral" && `stat-icon--${tone}`)}>
        <Icon name={icon} size={18} />
      </div>
      <div className="stat-body">
        <div className="stat-value">{loading ? <Skeleton width={64} height={22} radius={6} /> : value ?? "—"}</div>
        <div className="stat-label">{label}</div>
        {sub ? <div className="stat-sub">{sub}</div> : null}
      </div>
    </div>
  );
}

export function StatGrid({ children }: { children: ReactNode }) {
  return <div className="stat-grid">{children}</div>;
}
