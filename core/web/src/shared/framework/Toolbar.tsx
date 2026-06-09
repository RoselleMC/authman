import type { ReactNode } from "react";
import { Input } from "../components/Input";
import { useI18n } from "../i18n/I18nProvider";

/**
 * Standard search + filters row that lives under the PageHeader on list pages.
 * Compose with FilterChipRow for chip filters and ResultMeta for the count line.
 */
export function Toolbar({ children }: { children: ReactNode }) {
  return <div className="toolbar">{children}</div>;
}

interface ToolbarSearchProps {
  value: string;
  placeholder?: string;
  onChange: (next: string) => void;
  ariaLabel?: string;
  testId?: string;
}

/**
 * The search input that lives inside Toolbar. Pre-styled with the magnifier
 * icon and the .toolbar-search width clamp.
 */
export function ToolbarSearch({ value, placeholder, onChange, ariaLabel, testId }: ToolbarSearchProps) {
  return (
    <div className="toolbar-search">
      <Input
        icon="search"
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-label={ariaLabel}
        data-testid={testId}
      />
    </div>
  );
}

export function ToolbarFilters({ children }: { children: ReactNode }) {
  return <div className="toolbar-filters">{children}</div>;
}

interface ResultMetaProps {
  count: number | null;
  unit?: string;
  loading?: boolean;
  action?: ReactNode;
}

/**
 * Standard "N results" / "0 events" line that sits between the toolbar and
 * the data section. Pass null while loading.
 */
export function ResultMeta({ count, unit = "result", loading, action }: ResultMetaProps) {
  const { t } = useI18n();
  let label: string;
  if (loading) label = t("common.loading");
  else if (count === null) label = "—";
  else if (unit === "result") label = `${count} ${count === 1 ? t("common.result") : t("common.results")}`;
  else label = `${count} ${unit}`;
  return (
    <div className="result-meta">
      <span>{label}</span>
      {action ?? null}
    </div>
  );
}
