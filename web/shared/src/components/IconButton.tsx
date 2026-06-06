import type { ButtonHTMLAttributes } from "react";
import { Icon, type IconName } from "./Icon";
import { cx } from "../utils/cx";

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  name: IconName | string;
  size?: number;
  bordered?: boolean;
  label: string;
}

export function IconButton({ name, size = 16, bordered, label, className, ...rest }: Props) {
  return (
    <button
      {...rest}
      type={rest.type ?? "button"}
      className={cx("iconbtn", bordered && "iconbtn--bordered", className)}
      aria-label={label}
      title={label}
    >
      <Icon name={name} size={size} />
    </button>
  );
}
