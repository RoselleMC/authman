import type { ReactNode } from "react";
import { Icon } from "../components/Icon";

/**
 * Outer wrapper for every admin and player page. Sets max-width and
 * standardizes vertical rhythm via the .page class. Page bodies should
 * never set their own max-width / margin / padding.
 */
export function PageShell({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <div className="page" data-testid={testId}>
      {children}
    </div>
  );
}

interface PageHeaderProps {
  title: ReactNode;
  desc?: ReactNode;
  eyebrow?: ReactNode;
  action?: ReactNode;
}

/**
 * Standard page heading: eyebrow + title + optional description on the left,
 * an action slot on the right. Use this instead of hand-rolled .page-head
 * markup so spacing stays consistent.
 */
export function PageHeader({ title, desc, eyebrow, action }: PageHeaderProps) {
  return (
    <div className="page-head">
      <div>
        {eyebrow ? <p className="page-eyebrow">{eyebrow}</p> : null}
        <h2 className="page-title">{title}</h2>
        {desc ? <p className="page-desc">{desc}</p> : null}
      </div>
      {action}
    </div>
  );
}

interface BackLinkProps {
  onClick: () => void;
  children: ReactNode;
  testId?: string;
}

export function BackLink({ onClick, children, testId }: BackLinkProps) {
  return (
    <button type="button" className="back-link" onClick={onClick} data-testid={testId}>
      <Icon name="arrowLeft" size={15} />
      {children}
    </button>
  );
}
