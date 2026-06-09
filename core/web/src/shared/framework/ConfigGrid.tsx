import type { ReactNode } from "react";
import { Icon } from "../components/Icon";
import { cx } from "../utils/cx";

interface ConfigRowProps {
  k: ReactNode;
  v: ReactNode;
  mono?: boolean;
  ok?: boolean;
  testId?: string;
}

/**
 * One key/value row inside ConfigGrid. Use the `mono` flag for monospace
 * values (IDs, version strings); use `ok` to render a green check next to
 * the value (e.g. "Database: postgres ✓").
 */
export function ConfigRow({ k, v, mono, ok, testId }: ConfigRowProps) {
  return (
    <div className="config-item" data-testid={testId}>
      <span className="config-k">{k}</span>
      <span className={cx("config-v", mono && "mono")}>
        {v}
        {ok ? (
          <span className="config-ok">
            <Icon name="check" size={12} />
          </span>
        ) : null}
      </span>
    </div>
  );
}

interface ConfigGridProps {
  children: ReactNode;
  testId?: string;
}

/**
 * Two-column key/value grid used in admin Settings → System summary and
 * similar read-only configuration surfaces.
 */
export function ConfigGrid({ children, testId }: ConfigGridProps) {
  return (
    <div className="config-grid" data-testid={testId}>
      {children}
    </div>
  );
}
