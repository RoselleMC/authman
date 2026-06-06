import { useEffect, useRef, useState } from "react";
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
  const rootRef = useRef<HTMLDivElement | null>(null);
  const { t } = useI18n();

  useEffect(() => {
    if (!open) return undefined;
    function onDocClick(e: MouseEvent) {
      if (!rootRef.current) return;
      if (!rootRef.current.contains(e.target as Node)) setOpen(false);
    }
    function onEsc(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onEsc);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onEsc);
    };
  }, [open]);

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
          role="menu"
          aria-label={t("list.columnVisibility")}
          data-testid={testId ? `${testId}-menu` : undefined}
        >
          <div className="adv-list-vis__head">{t("list.columns")}</div>
          {columns.map((c) => {
            const isHidden = hiddenSet.has(c.key);
            const isMandatory = !!c.mandatory;
            return (
              <label
                key={c.key}
                className="adv-list-vis__opt"
                data-testid={testId ? `${testId}-opt-${c.key}` : undefined}
              >
                <input
                  type="checkbox"
                  checked={!isHidden || isMandatory}
                  disabled={isMandatory}
                  onChange={() => !isMandatory && onToggle(c.key)}
                />
                <span>{typeof c.header === "string" ? c.header : c.key}</span>
                {isMandatory ? <span className="adv-list-vis__lock">{t("list.alwaysShown")}</span> : null}
              </label>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
