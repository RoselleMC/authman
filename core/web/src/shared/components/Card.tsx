import type { ReactNode, CSSProperties } from "react";
import { cx } from "../utils/cx";

interface CardProps {
  title?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  /** When true, render children directly without the .card__body wrapper. Useful for tables/lists. */
  noBody?: boolean;
  bodyStyle?: CSSProperties;
  className?: string;
  testId?: string;
}

export function Card({ title, actions, children, noBody, bodyStyle, className, testId }: CardProps) {
  return (
    <section className={cx("card", className)} data-testid={testId}>
      {title || actions ? (
        <div className="card__head">
          {title ? <h3 className="card__title">{title}</h3> : <span />}
          {actions ? <div className="card__actions">{actions}</div> : null}
        </div>
      ) : null}
      {noBody ? children : <div className="card__body" style={bodyStyle}>{children}</div>}
    </section>
  );
}

export function CardHeader({ title, action }: { title: ReactNode; action?: ReactNode }) {
  return (
    <div className="card__head" style={{ marginBottom: 12 }}>
      <h3 className="card__title">{title}</h3>
      {action}
    </div>
  );
}
