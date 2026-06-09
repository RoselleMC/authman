import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
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
  const [menuStyle, setMenuStyle] = useState<CSSProperties | undefined>(undefined);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const active = options.find((option) => option.value === value);

  const positionMenu = useCallback(() => {
    const trigger = rootRef.current?.getBoundingClientRect();
    if (!trigger) return;
    const gutter = 8;
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const width = Math.min(Math.max(trigger.width, 120), viewportWidth - gutter * 2);
    const left = Math.max(gutter, Math.min(trigger.left, viewportWidth - width - gutter));
    const below = viewportHeight - trigger.bottom - gutter;
    const above = trigger.top - gutter;
    const openUp = below < 180 && above > below;
    const maxHeight = Math.max(120, Math.min(280, openUp ? above - gutter : below - gutter));
    setMenuStyle({
      position: "fixed",
      top: openUp ? undefined : trigger.bottom + 6,
      bottom: openUp ? viewportHeight - trigger.top + 6 : undefined,
      left,
      width,
      maxHeight,
    });
  }, []);

  useEffect(() => {
    if (!open) return undefined;
    positionMenu();
    function onDocClick(e: MouseEvent) {
      const target = e.target as Node;
      if (rootRef.current?.contains(target) || menuRef.current?.contains(target)) return;
      setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKey);
    window.addEventListener("resize", positionMenu);
    window.addEventListener("scroll", positionMenu, true);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("resize", positionMenu);
      window.removeEventListener("scroll", positionMenu, true);
    };
  }, [open, positionMenu]);

  const menu = open ? (
    <div
      ref={menuRef}
      className="select-ui__menu"
      role="listbox"
      aria-label={ariaLabel}
      style={menuStyle}
      data-testid={testId ? `${testId}-menu` : undefined}
    >
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
  ) : null;

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
      {menu && typeof document !== "undefined" ? createPortal(menu, document.body) : null}
    </div>
  );
}
