import type { ReactNode } from "react";
import { Icon, type IconName } from "../components/Icon";
import { cx } from "../utils/cx";

/**
 * Centered icon + heading + description used by the link-login flow:
 *
 * - "verifying" → spinner
 * - "success" → green check
 * - "failure" → red icon
 *
 * Compose with LinkRecover for the recovery CTAs.
 */
interface LinkStateProps {
  variant: "verifying" | "success" | "failure";
  icon?: IconName | string;
  title: ReactNode;
  desc?: ReactNode;
  children?: ReactNode;
  testId?: string;
}

export function LinkState({ variant, icon, title, desc, children, testId }: LinkStateProps) {
  return (
    <div className="link-state" data-testid={testId}>
      {variant === "verifying" ? (
        <div className="link-spinner">
          <span className="spinner" style={{ width: 28, height: 28 }} />
        </div>
      ) : (
        <div className={cx("link-icon", variant === "success" ? "link-icon--ok" : "link-icon--bad")}>
          <Icon name={icon ?? (variant === "success" ? "check" : "alert")} size={26} />
        </div>
      )}
      <h1>{title}</h1>
      {desc ? <p>{desc}</p> : null}
      {children}
    </div>
  );
}

export function FragNote({ children }: { children: ReactNode }) {
  return <div className="frag-note">{children}</div>;
}

export function LinkRecover({ children }: { children: ReactNode }) {
  return <div className="link-recover">{children}</div>;
}
