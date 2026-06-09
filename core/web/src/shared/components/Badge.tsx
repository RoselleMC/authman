import type { ReactNode } from "react";
import { cx } from "../utils/cx";

export type BadgeTone = "neutral" | "success" | "warning" | "danger" | "info" | "premium" | "offline";

interface Props {
  tone?: BadgeTone;
  dot?: boolean;
  square?: boolean;
  children: ReactNode;
}

export function Badge({ tone = "neutral", dot, square, children }: Props) {
  return (
    <span className={cx("badge", `badge--${tone}`, square && "badge--square")}>
      {dot ? <span className="badge-dot" /> : null}
      {children}
    </span>
  );
}
