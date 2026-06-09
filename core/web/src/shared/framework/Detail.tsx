import type { ReactNode } from "react";
import { Card } from "../components/Card";
import { Icon } from "../components/Icon";
import { cx } from "../utils/cx";

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

interface DetailSummaryProps {
  title: ReactNode;
  titleMeta?: ReactNode;
  meta?: ReactNode;
  avatarUrl?: string | null;
  avatarText?: string;
  icon?: string;
  children?: ReactNode;
}

export function DetailSummary({ title, titleMeta, meta, avatarUrl, avatarText, icon, children }: DetailSummaryProps) {
  return (
    <Card>
      <div className="id-summary">
        <span className={cx("pa-avatar pa-lg", avatarUrl && "has-image")}>
          {avatarUrl ? <img src={avatarUrl} alt="" aria-hidden="true" /> : icon ? <Icon name={icon} size={24} /> : avatarText}
        </span>
        <div className="id-name-row">
          <h2 className="id-raw">{title}</h2>
          {titleMeta}
        </div>
        {meta ? <div className="identity-meta-row">{meta}</div> : null}
        {children}
      </div>
    </Card>
  );
}

export function DetailActions({ title, children }: { title: ReactNode; children: ReactNode }) {
  return (
    <Card title={title}>
      <div className="action-stack">{children}</div>
    </Card>
  );
}
