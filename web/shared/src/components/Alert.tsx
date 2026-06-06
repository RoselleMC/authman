import type { ReactNode } from "react";
import { Icon, type IconName } from "./Icon";
import { cx } from "../utils/cx";

export type AlertTone = "neutral" | "info" | "success" | "warning" | "danger";

interface Props {
  tone?: AlertTone;
  title?: ReactNode;
  icon?: IconName | string;
  children: ReactNode;
  testId?: string;
}

const DEFAULT_ICON: Record<AlertTone, IconName> = {
  neutral: "info",
  info: "info",
  success: "check",
  warning: "alert",
  danger: "alert",
};

export function Alert({ tone = "neutral", title, icon, children, testId }: Props) {
  const ic = icon ?? DEFAULT_ICON[tone];
  return (
    <div className={cx("alert", `alert--${tone}`)} role={tone === "danger" ? "alert" : undefined} data-testid={testId}>
      <Icon name={ic} size={17} style={{ marginTop: 1 }} />
      <div className="alert__body">
        {title ? <p className="alert__title">{title}</p> : null}
        <div>{children}</div>
      </div>
    </div>
  );
}
