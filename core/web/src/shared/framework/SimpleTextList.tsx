import { useCallback, useRef, type ChangeEvent, type KeyboardEvent } from "react";
import { Icon } from "../components/Icon";
import { IconButton } from "../components/IconButton";
import { useI18n } from "../i18n/I18nProvider";
import { cx } from "../utils/cx";

interface Props {
  values: string[];
  onChange: (next: string[]) => void;
  placeholder?: string;
  addLabel?: string;
  removeLabel?: string;
  /** Minimum rows kept on screen even when values is shorter. Defaults to 1. */
  minRows?: number;
  testId?: string;
}

/**
 * Lightweight inline list editor for small sets of plain text entries —
 * domains, aliases, tags. Each entry is one row using the standard input
 * style; a same-sized dashed row at the bottom adds another entry. Remove
 * buttons appear per row once there are more than two visible rows.
 *
 * No drag-reorder, no validation, no inline dedup. Callers normalise on
 * save (trim, drop empties, dedup) — this component just edits text.
 */
export function SimpleTextList({
  values,
  onChange,
  placeholder,
  addLabel,
  removeLabel,
  minRows = 1,
  testId,
}: Props) {
  const { t } = useI18n();
  const rows = values.length >= minRows ? values : [...values, ...Array.from({ length: minRows - values.length }, () => "")];
  const showRemove = rows.length > 2;
  const inputsRef = useRef<Array<HTMLInputElement | null>>([]);
  const focusOnMountRef = useRef<number | null>(null);

  const setInputRef = useCallback((index: number) => (el: HTMLInputElement | null) => {
    inputsRef.current[index] = el;
    if (focusOnMountRef.current === index && el) {
      el.focus();
      focusOnMountRef.current = null;
    }
  }, []);

  function handleChange(index: number, event: ChangeEvent<HTMLInputElement>) {
    const next = [...rows];
    next[index] = event.target.value;
    onChange(next);
  }
  function handleRemove(index: number) {
    const next = rows.filter((_, i) => i !== index);
    onChange(next);
  }
  function handleAdd() {
    focusOnMountRef.current = rows.length;
    onChange([...rows, ""]);
  }
  function handleKeyDown(index: number, event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter") {
      event.preventDefault();
      if (index === rows.length - 1) {
        handleAdd();
      } else {
        inputsRef.current[index + 1]?.focus();
      }
    }
  }

  return (
    <div className="simple-text-list" data-testid={testId}>
      {rows.map((value, index) => (
        <div className="simple-text-list__row" key={index}>
          <input
            ref={setInputRef(index)}
            className={cx("input", showRemove && "input--has-trail")}
            value={value}
            placeholder={placeholder}
            onChange={(event) => handleChange(index, event)}
            onKeyDown={(event) => handleKeyDown(index, event)}
          />
          {showRemove ? (
            <span className="simple-text-list__remove">
              <IconButton
                name="close"
                size={14}
                label={removeLabel ?? t("common.remove")}
                onClick={() => handleRemove(index)}
              />
            </span>
          ) : null}
        </div>
      ))}
      <button
        type="button"
        className="simple-text-list__add"
        onClick={handleAdd}
      >
        <Icon name="plus" size={14} />
        <span>{addLabel ?? t("common.add")}</span>
      </button>
    </div>
  );
}
