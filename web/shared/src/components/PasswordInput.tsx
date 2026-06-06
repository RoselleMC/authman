import { forwardRef, useState, type InputHTMLAttributes } from "react";
import { Icon, type IconName } from "./Icon";
import { IconButton } from "./IconButton";
import { cx } from "../utils/cx";
import { useI18n } from "../i18n/I18nProvider";

interface Props extends Omit<InputHTMLAttributes<HTMLInputElement>, "size" | "type"> {
  icon?: IconName | string;
  invalid?: boolean;
  valid?: boolean;
}

export const PasswordInput = forwardRef<HTMLInputElement, Props>(function PasswordInput(
  { icon, invalid, valid, className, ...rest },
  ref,
) {
  const [show, setShow] = useState(false);
  const { t } = useI18n();
  return (
    <div className="input-wrap">
      {icon ? (
        <span className="input-icon">
          <Icon name={icon} size={16} />
        </span>
      ) : null}
      <input
        ref={ref}
        type={show ? "text" : "password"}
        {...rest}
        aria-invalid={invalid ? "true" : undefined}
        data-valid={valid ? "true" : undefined}
        className={cx("input", icon && "input--has-icon", "input--has-trail", className)}
      />
      <span className="input-trail">
        <IconButton
          name={show ? "eyeOff" : "eye"}
          size={16}
          label={show ? t("common.hidePassword") : t("common.showPassword")}
          onClick={() => setShow((s) => !s)}
          style={{ width: 30, height: 30 }}
        />
      </span>
    </div>
  );
});
