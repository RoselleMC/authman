import { useEffect, useRef, useState, type ReactNode } from "react";
import { Icon } from "./Icon";
import { cx } from "../utils/cx";

export interface SelectOption<T extends string = string> {
  value: T;
  label: ReactNode;
  disabled?: boolean;
}

interface Props<T extends string = string> {
  value: T;
  options: ReadonlyArray<SelectOption<T>>;
  onChange: (value: T) => void;
  ariaLabel?: string;
  disabled?: boolean;
  placeholder?: ReactNode;
  className?: string;
  testId?: string;
}

export function Select<T extends string = string>({
  value,
  options,
  onChange,
  ariaLabel,
  disabled,
  placeholder,
  className,
  testId,
}: Props<T>) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const active = options.find((option) => option.value === value);

  useEffect(() => {
    if (!open) return undefined;
    function onDocClick(e: MouseEvent) {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div className={cx("select-ui", className)} ref={rootRef} data-testid={testId}>
      <button
        type="button"
        className={cx("select-ui__trigger", open && "is-open")}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={() => setOpen((next) => !next)}
        data-testid={testId ? `${testId}-trigger` : undefined}
      >
        <span className={cx("select-ui__value", !active && "is-placeholder")}>{active?.label ?? placeholder ?? value}</span>
        <Icon name="chevronDown" size={14} />
      </button>
      {open ? (
        <div className="select-ui__menu" role="listbox" aria-label={ariaLabel} data-testid={testId ? `${testId}-menu` : undefined}>
          {options.map((option) => (
            <button
              key={option.value}
              type="button"
              role="option"
              aria-selected={option.value === value}
              disabled={option.disabled}
              className={cx("select-ui__item", option.value === value && "is-selected")}
              onClick={() => {
                if (option.disabled) return;
                onChange(option.value);
                setOpen(false);
              }}
              data-testid={testId ? `${testId}-option-${option.value}` : undefined}
            >
              <span>{option.label}</span>
              {option.value === value ? <Icon name="check" size={13} /> : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
