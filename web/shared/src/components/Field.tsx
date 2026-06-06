import type { ReactNode, CSSProperties } from "react";
import { Icon } from "./Icon";

interface Props {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  htmlFor?: string;
  children: ReactNode;
  style?: CSSProperties;
}

export function Field({ label, hint, error, htmlFor, children, style }: Props) {
  return (
    <div className="field" style={style}>
      {label ? (
        <label className="field__label" htmlFor={htmlFor}>
          {label}
        </label>
      ) : null}
      {children}
      {error ? (
        <span className="field__error">
          <Icon name="alert" size={13} />
          {error}
        </span>
      ) : hint ? (
        <span className="field__hint">{hint}</span>
      ) : null}
    </div>
  );
}
