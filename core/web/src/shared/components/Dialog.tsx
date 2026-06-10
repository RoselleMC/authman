import { useEffect, type ReactNode, type CSSProperties } from "react";
import { Icon, type IconName } from "./Icon";
import { cx } from "../utils/cx";

export type DialogIconTone = "neutral" | "danger" | "primary" | "warning";
export type DialogSize = "md" | "lg" | "xl";

interface Props {
  open: boolean;
  onClose: () => void;
  icon?: IconName | string;
  iconTone?: DialogIconTone;
  title: ReactNode;
  desc?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  testId?: string;
  size?: DialogSize;
}

const TONE_STYLE: Record<DialogIconTone, CSSProperties> = {
  neutral: { background: "var(--color-surface-muted)", color: "var(--color-text-muted)" },
  danger: { background: "var(--color-danger-soft)", color: "var(--color-danger)" },
  primary: { background: "var(--color-primary-soft)", color: "var(--color-primary)" },
  warning: { background: "var(--color-warning-soft)", color: "var(--color-warning)" },
};

export function Dialog({ open, onClose, icon, iconTone = "neutral", title, desc, children, footer, testId, size = "md" }: Props) {
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div
      className="overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      data-testid={testId}
    >
      <div className={cx("dialog", size !== "md" ? `dialog--${size}` : null)} role="dialog" aria-modal="true" aria-label={typeof title === "string" ? title : undefined}>
        <div className="dialog__head">
          {icon ? (
            <div className="dialog__icon" style={TONE_STYLE[iconTone]}>
              <Icon name={icon} size={19} />
            </div>
          ) : null}
          <div style={{ flex: 1 }}>
            <h2 className="dialog__title">{title}</h2>
            {desc ? <p className="dialog__desc">{desc}</p> : null}
          </div>
        </div>
        {children ? <div className="dialog__body">{children}</div> : null}
        {footer ? <div className="dialog__foot">{footer}</div> : null}
      </div>
    </div>
  );
}
