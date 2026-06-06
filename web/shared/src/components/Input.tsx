import { forwardRef, type InputHTMLAttributes, type ReactNode } from "react";
import { Icon, type IconName } from "./Icon";
import { cx } from "../utils/cx";

interface Props extends Omit<InputHTMLAttributes<HTMLInputElement>, "size"> {
  icon?: IconName | string;
  affix?: ReactNode;
  trail?: ReactNode;
  mono?: boolean;
  invalid?: boolean;
  valid?: boolean;
}

export const Input = forwardRef<HTMLInputElement, Props>(function Input(
  { icon, affix, trail, mono, invalid, valid, className, ...rest },
  ref,
) {
  return (
    <div className="input-wrap">
      {icon ? (
        <span className="input-icon">
          <Icon name={icon} size={16} />
        </span>
      ) : null}
      {affix ? <span className="input-affix">{affix}</span> : null}
      <input
        ref={ref}
        {...rest}
        aria-invalid={invalid ? "true" : undefined}
        data-valid={valid ? "true" : undefined}
        className={cx(
          "input",
          icon ? "input--has-icon" : null,
          affix ? "input--has-affix" : null,
          trail ? "input--has-trail" : null,
          mono ? "input--mono" : null,
          className,
        )}
      />
      {trail ? <span className="input-trail">{trail}</span> : null}
    </div>
  );
});
