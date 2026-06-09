import type { ButtonHTMLAttributes, ReactNode } from "react";
import { Icon, type IconName } from "./Icon";
import { cx } from "../utils/cx";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "danger-soft";
export type ButtonSize = "sm" | "md" | "lg";

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  block?: boolean;
  icon?: IconName | string;
  iconRight?: IconName | string;
  loading?: boolean;
  children?: ReactNode;
}

const VARIANT_CLASS: Record<ButtonVariant, string> = {
  primary: "btn--primary",
  secondary: "btn--secondary",
  ghost: "btn--ghost",
  danger: "btn--danger",
  "danger-soft": "btn--danger-soft",
};

export function Button({
  variant = "secondary",
  size,
  block,
  icon,
  iconRight,
  loading,
  disabled,
  children,
  className,
  ...rest
}: Props) {
  const sized = size && size !== "md" ? `btn--${size}` : undefined;
  const iconOnly = !children;
  const iconSize = size === "sm" ? 15 : 16;
  return (
    <button
      {...rest}
      className={cx("btn", VARIANT_CLASS[variant], sized, block && "btn--block", iconOnly && "btn--icon", className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
    >
      {loading ? <span className="spinner" style={{ width: 14, height: 14 }} /> : icon ? <Icon name={icon} size={iconSize} /> : null}
      {children ? <span>{children}</span> : null}
      {!loading && iconRight ? <Icon name={iconRight} size={iconSize} /> : null}
    </button>
  );
}
