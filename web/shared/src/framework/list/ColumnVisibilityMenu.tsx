import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { IconButton } from "../../components/IconButton";
import { useI18n } from "../../i18n/I18nProvider";
import type { ListColumn } from "./types";

interface Props<T> {
  columns: ReadonlyArray<ListColumn<T>>;
  hidden: ReadonlyArray<string>;
  onToggle: (columnKey: string) => void;
  testId?: string;
}

/**
 * Popover trigger that lets the user hide/show non-mandatory columns.
 * Mandatory columns appear in the list as a disabled row so the user knows
 * they exist and why they can't be hidden.
 */
export function ColumnVisibilityMenu<T>({ columns, hidden, onToggle, testId }: Props<T>) {
  const [open, setOpen] = useState(false);
  const [menuStyle, setMenuStyle] = useState<CSSProperties | undefined>(undefined);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const { t } = useI18n();

  const positionMenu = useCallback(() => {
    const trigger = rootRef.current?.getBoundingClientRect();
    if (!trigger) return;
    const width = 260;
    const gutter = 8;
    const left = Math.max(gutter, Math.min(trigger.right - width, window.innerWidth - width - gutter));
    const below = window.innerHeight - trigger.bottom - gutter;
    const above = trigger.top - gutter;
    const openUp = below < 180 && above > below;
    const maxHeight = Math.max(160, Math.min(360, openUp ? above - gutter : below - gutter));
    setMenuStyle({
      position: "fixed",
      top: openUp ? undefined : trigger.bottom + 6,
      bottom: openUp ? window.innerHeight - trigger.top + 6 : undefined,
      left,
      width,
      maxHeight,
    });
  }, []);

  useEffect(() => {
    if (!open) return undefined;
    positionMenu();
    function onDocClick(e: MouseEvent) {
      if (!rootRef.current) return;
      if (!rootRef.current.contains(e.target as Node)) setOpen(false);
    }
    function onEsc(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onEsc);
    window.addEventListener("resize", positionMenu);
    window.addEventListener("scroll", positionMenu, true);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onEsc);
      window.removeEventListener("resize", positionMenu);
      window.removeEventListener("scroll", positionMenu, true);
    };
  }, [open, positionMenu]);

  const hiddenSet = new Set(hidden);

  return (
    <div ref={rootRef} className="adv-list-vis" data-testid={testId}>
      <IconButton
        bordered
        name="grid"
        size={16}
        label={t("list.columnVisibility")}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        data-testid={testId ? `${testId}-trigger` : undefined}
      />
      {open ? (
        <div
          className="adv-list-vis__menu"
          style={menuStyle}
          role="menu"
          aria-label={t("list.columnVisibility")}
          data-testid={testId ? `${testId}-menu` : undefined}
        >
          <div className="adv-list-vis__head">{t("list.columns")}</div>
          {columns.map((c) => {
            const isHidden = hiddenSet.has(c.key);
            const isMandatory = !!c.mandatory;
            return (
              <button
                type="button"
                key={c.key}
                className="adv-list-vis__opt"
                role="menuitemcheckbox"
                aria-checked={!isHidden || isMandatory}
                disabled={isMandatory}
                data-testid={testId ? `${testId}-opt-${c.key}` : undefined}
                onClick={() => !isMandatory && onToggle(c.key)}
              >
                <input
                  type="checkbox"
                  checked={!isHidden || isMandatory}
                  disabled={isMandatory}
                  readOnly
                  tabIndex={-1}
                />
                <span>{typeof c.header === "string" ? c.header : c.key}</span>
                {isMandatory ? <span className="adv-list-vis__lock">{t("list.alwaysShown")}</span> : null}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
