import type { ReactNode } from "react";

/**
 * Two-column layout for detail pages (core/passports/:id, core/profiles/:id):
 * a sticky aside on the left with summary + actions, and a body on the right
 * with tabs / panels.
 *
 * Use the matching DetailAside and DetailBody children so the grid columns
 * track the design's 320px + 1fr template.
 */
export function DetailGrid({ children }: { children: ReactNode }) {
  return <div className="detail-grid">{children}</div>;
}

export function DetailAside({ children }: { children: ReactNode }) {
  return <div className="detail-aside">{children}</div>;
}

export function DetailBody({ children }: { children: ReactNode }) {
  return <div className="detail-body">{children}</div>;
}
