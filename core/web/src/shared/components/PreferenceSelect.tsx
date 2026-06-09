import { useEffect, useRef, useState, type ReactNode } from "react";
import { Icon } from "./Icon";
import { useI18n, type LocalePreference } from "../i18n/I18nProvider";
import { useTheme, type ThemePreference } from "../theme/ThemeProvider";
import { cx } from "../utils/cx";
import flagCn from "../assets/flags/cn.svg";
import flagGb from "../assets/flags/gb.svg";

export interface PreferenceOption<T extends string> {
  value: T;
  label: string;
  icon?: ReactNode;
}

interface PreferenceSelectProps<T extends string> {
  value: T;
  options: ReadonlyArray<PreferenceOption<T>>;
  onChange: (value: T) => void;
  ariaLabel: string;
  displayLabel?: string;
  displayIcon?: ReactNode;
  testId?: string;
}

export function PreferenceSelect<T extends string>({
  value,
  options,
  onChange,
  ariaLabel,
  displayLabel,
  displayIcon,
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
        {displayIcon ?? active?.icon ?? null}
        <span>{displayLabel ?? active?.label ?? value}</span>
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
              <span className="pref-select__item-label">
                {opt.icon ?? null}
                <span>{opt.label}</span>
              </span>
              {opt.value === value ? <Icon name="check" size={13} /> : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function LocaleFlag({ locale }: { locale: "en" | "zh" }) {
  const src = locale === "zh" ? flagCn : flagGb;
  return <img className="pref-select__flag" src={src} alt="" aria-hidden="true" />;
}

function ThemeGlyph({ mode }: { mode: "light" | "dark" }) {
  return <Icon className="pref-select__glyph" name={mode === "dark" ? "moon" : "sun"} size={15} />;
}

export function LocaleSelect() {
  const { t, locale, localePreference, setLocalePreference } = useI18n();
  const options: ReadonlyArray<PreferenceOption<LocalePreference>> = [
    { value: "system", label: t("common.locale.system") },
    { value: "en", label: "EN", icon: <LocaleFlag locale="en" /> },
    { value: "zh", label: "ZH", icon: <LocaleFlag locale="zh" /> },
  ];
  const displayLabel = locale.toUpperCase();
  const displayIcon = <LocaleFlag locale={locale} />;
  return (
    <PreferenceSelect
      value={localePreference}
      options={options}
      onChange={setLocalePreference}
      ariaLabel={t("common.locale.toggle")}
      displayLabel={displayLabel}
      displayIcon={displayIcon}
      testId="locale-toggle"
    />
  );
}

export function ThemeSelect() {
  const { t } = useI18n();
  const { effectiveMode, preference, setPreference } = useTheme();
  const options: ReadonlyArray<PreferenceOption<ThemePreference>> = [
    { value: "system", label: t("common.theme.system") },
    { value: "light", label: t("common.theme.light"), icon: <ThemeGlyph mode="light" /> },
    { value: "dark", label: t("common.theme.dark"), icon: <ThemeGlyph mode="dark" /> },
  ];
  const displayLabel = t(`common.theme.${effectiveMode}`);
  const displayIcon = <ThemeGlyph mode={effectiveMode} />;
  return (
    <PreferenceSelect
      value={preference}
      options={options}
      onChange={setPreference}
      ariaLabel={t("common.theme.toggle")}
      displayLabel={displayLabel}
      displayIcon={displayIcon}
      testId="theme-toggle"
    />
  );
}
