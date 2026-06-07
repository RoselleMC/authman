import { useEffect, useRef, useState } from "react";
import { Icon } from "./Icon";
import { useI18n, type LocalePreference } from "../i18n/I18nProvider";
import { useTheme, type ThemePreference } from "../theme/ThemeProvider";
import { cx } from "../utils/cx";

export interface PreferenceOption<T extends string> {
  value: T;
  label: string;
}

interface PreferenceSelectProps<T extends string> {
  value: T;
  options: ReadonlyArray<PreferenceOption<T>>;
  onChange: (value: T) => void;
  ariaLabel: string;
  testId?: string;
}

export function PreferenceSelect<T extends string>({
  value,
  options,
  onChange,
  ariaLabel,
  testId,
}: PreferenceSelectProps<T>) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const active = options.find((opt) => opt.value === value) ?? options[0];

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
    <div className="pref-select" ref={rootRef}>
      <button
        type="button"
        className={cx("pref-select__trigger", open && "is-open")}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={ariaLabel}
        title={ariaLabel}
        data-testid={testId}
        onClick={() => setOpen((next) => !next)}
      >
        <span>{active?.label ?? value}</span>
        <Icon name="chevronDown" size={13} />
      </button>
      {open ? (
        <div className="pref-select__menu" role="menu" aria-label={ariaLabel}>
          {options.map((opt) => (
            <button
              key={opt.value}
              type="button"
              role="menuitemradio"
              aria-checked={opt.value === value}
              className={cx("pref-select__item", opt.value === value && "is-selected")}
              onClick={() => {
                onChange(opt.value);
                setOpen(false);
              }}
            >
              <span>{opt.label}</span>
              {opt.value === value ? <Icon name="check" size={13} /> : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

export function LocaleSelect() {
  const { t, localePreference, setLocalePreference } = useI18n();
  const options: ReadonlyArray<PreferenceOption<LocalePreference>> = [
    { value: "system", label: t("common.locale.system") },
    { value: "en", label: "EN" },
    { value: "zh", label: "ZH" },
  ];
  return (
    <PreferenceSelect
      value={localePreference}
      options={options}
      onChange={setLocalePreference}
      ariaLabel={t("common.locale.toggle")}
      testId="locale-toggle"
    />
  );
}

export function ThemeSelect() {
  const { t } = useI18n();
  const { preference, setPreference } = useTheme();
  const options: ReadonlyArray<PreferenceOption<ThemePreference>> = [
    { value: "system", label: t("common.theme.system") },
    { value: "light", label: t("common.theme.light") },
    { value: "dark", label: t("common.theme.dark") },
  ];
  return (
    <PreferenceSelect
      value={preference}
      options={options}
      onChange={setPreference}
      ariaLabel={t("common.theme.toggle")}
      testId="theme-toggle"
    />
  );
}
