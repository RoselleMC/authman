import { useEffect, useRef, useState } from "react";
import { Icon } from "./Icon";
import { useI18n } from "../i18n/I18nProvider";
import { cx } from "../utils/cx";

export interface AccountMenuRow {
  label: string;
  value: string;
  mono?: boolean;
}

interface Props {
  name: string;
  secondary?: string;
  badge?: string;
  rows?: AccountMenuRow[];
  avatarUrl?: string;
  avatarClassName?: string;
  primaryActionLabel?: string;
  primaryActionIcon?: string;
  onPrimaryAction?: () => void | Promise<void>;
  onSignOut: () => void | Promise<void>;
  testId?: string;
}

export function AccountMenu({
  name,
  secondary,
  badge,
  rows = [],
  avatarUrl,
  avatarClassName,
  primaryActionLabel,
  primaryActionIcon = "settings",
  onPrimaryAction,
  onSignOut,
  testId = "account-menu",
}: Props) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const initial = name.trim()[0]?.toUpperCase() ?? "?";
  const avatar = avatarUrl ? <img src={avatarUrl} alt="" aria-hidden="true" /> : initial;

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

  async function signOut() {
    setOpen(false);
    await onSignOut();
  }

  async function primaryAction() {
    setOpen(false);
    await onPrimaryAction?.();
  }

  return (
    <div className="account-menu" ref={rootRef} data-testid={testId}>
      <button
        type="button"
        className={cx("account-menu__trigger", open && "is-open")}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("account.menu")}
        onClick={() => setOpen((next) => !next)}
        data-testid={`${testId}-trigger`}
      >
        <span className={cx("account-menu__avatar", avatarUrl && "has-image", avatarClassName)}>{avatar}</span>
        <span className="account-menu__summary">
          <span className="account-menu__name">{name || "—"}</span>
          {secondary ? <span className="account-menu__secondary">{secondary}</span> : null}
        </span>
        <Icon name="chevronDown" size={13} />
      </button>

      {open ? (
        <div className="account-menu__panel" role="menu" aria-label={t("account.menu")} data-testid={`${testId}-panel`}>
          {primaryActionLabel ? (
            <button type="button" className="account-menu__action account-menu__action--neutral" role="menuitem" onClick={() => void primaryAction()} data-testid={`${testId}-primary-action`}>
              <Icon name={primaryActionIcon} size={15} />
              <span>{primaryActionLabel}</span>
              {badge ? <span className="account-menu__badge">{badge}</span> : null}
            </button>
          ) : null}

          {rows.length > 0 ? (
            <div className="account-menu__rows">
              {rows.map((row) => (
                <div className="account-menu__row" key={row.label}>
                  <span>{row.label}</span>
                  <strong className={row.mono ? "mono" : undefined}>{row.value || "—"}</strong>
                </div>
              ))}
            </div>
          ) : null}

          <button type="button" className="account-menu__action" role="menuitem" onClick={() => void signOut()} data-testid="logout-button">
            <Icon name="logout" size={15} />
            <span>{t("common.signOut")}</span>
          </button>
        </div>
      ) : null}
    </div>
  );
}
