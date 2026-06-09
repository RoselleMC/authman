import type { ReactNode, CSSProperties } from "react";
import { useI18n } from "../i18n/I18nProvider";

export interface DataColumn<T> {
  key: string;
  header: ReactNode;
  render: (row: T) => ReactNode;
  width?: string;
  minWidth?: string;
  align?: "left" | "right" | "center";
  sticky?: "right";
  /** Hide on narrow viewports. */
  hideOnNarrow?: boolean;
}

interface Props<T> {
  rows: T[];
  columns: DataColumn<T>[];
  rowKey: (row: T) => string;
  onRowClick?: (row: T) => void;
  empty?: ReactNode;
  loading?: boolean;
  testId?: string;
}

const ALIGN: Record<NonNullable<DataColumn<unknown>["align"]>, CSSProperties> = {
  left: { textAlign: "left" },
  right: { textAlign: "right" },
  center: { textAlign: "center" },
};

function cellStyle<T>(column: DataColumn<T>, head = false): CSSProperties {
  return {
    width: column.width,
    minWidth: column.minWidth ?? column.width,
    ...(column.align ? ALIGN[column.align] : null),
    ...(column.sticky === "right" ? { position: "sticky", right: 0, zIndex: head ? 4 : 2 } : null),
  };
}

export function DataTable<T>({
  rows,
  columns,
  rowKey,
  onRowClick,
  empty,
  loading,
  testId,
}: Props<T>) {
  const { t } = useI18n();
  if (loading) {
    return (
      <div data-testid={`${testId ?? "data-table"}-loading`} style={{ padding: 20 }}>
        {t("common.loading")}
      </div>
    );
  }
  if (rows.length === 0) {
    return (
      <div data-testid={`${testId ?? "data-table"}-empty`}>
        {empty}
      </div>
    );
  }
  return (
    <div className="table-scroll" data-testid={testId}>
      <table className="tbl">
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.key} className={c.sticky === "right" ? "is-sticky-right" : undefined} style={cellStyle(c, true)}>
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr
              key={rowKey(row)}
              data-testid="data-row"
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              style={{ cursor: onRowClick ? "pointer" : "default" }}
            >
              {columns.map((c) => (
                <td key={c.key} className={c.sticky === "right" ? "is-sticky-right" : undefined} style={cellStyle(c)}>
                  {c.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
