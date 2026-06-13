import type { ReactNode } from "react";
import { Card } from "../components/Card";
import { Copyable } from "../components/Copyable";
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
        <div className="id-summary-main">
          <span className={cx("pa-avatar pa-lg", avatarUrl && "has-image")}>
            {avatarUrl ? <img src={avatarUrl} alt="" aria-hidden="true" /> : icon ? <Icon name={icon} size={24} /> : avatarText}
          </span>
          <div className="id-name-row">
            <h2 className="id-raw">{title}</h2>
            {titleMeta}
          </div>
          {meta ? <div className="identity-meta-row">{meta}</div> : null}
        </div>
        {children ? <div className="id-summary-details">{children}</div> : null}
      </div>
    </Card>
  );
}

interface DetailIdentifierProps {
  label: ReactNode;
  value?: string | null;
  display?: string;
  copy?: boolean;
  mono?: boolean;
  children?: ReactNode;
}

export function DetailIdentifier({ label, value, display, copy = true, mono = true, children }: DetailIdentifierProps) {
  const text = value?.trim() ?? "";
  return (
    <div className="id-uuid">
      <span className="id-uuid-label">{label}</span>
      <div className="id-uuid-value">
        {children ? children : text ? (
          copy ? <Copyable value={text} display={display} mono={mono} /> : <strong className={cx(mono && "mono")}>{display ?? text}</strong>
        ) : (
          <strong>—</strong>
        )}
      </div>
    </div>
  );
}

export function DetailActions({ title, children }: { title: ReactNode; children: ReactNode }) {
  return (
    <Card title={title}>
      <div className="action-stack">{children}</div>
    </Card>
  );
}
