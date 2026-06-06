import type { ReactNode } from "react";

/**
 * Authenticated-player page wrapper. Sets the .pcontent max-width and rhythm.
 * Use instead of hand-rolled <div className="pcontent">.
 */
export function PContent({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <div className="pcontent" data-testid={testId}>
      {children}
    </div>
  );
}

interface PContentHeadProps {
  title: ReactNode;
  desc?: ReactNode;
}

export function PContentHead({ title, desc }: PContentHeadProps) {
  return (
    <div className="pcontent-head">
      <h1>{title}</h1>
      {desc ? <p>{desc}</p> : null}
    </div>
  );
}

/**
 * Two-column responsive grid for paired info cards (Account ↔ Activity).
 */
export function PCardGrid({ children }: { children: ReactNode }) {
  return <div className="pcard-grid">{children}</div>;
}
