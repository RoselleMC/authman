import type { ReactNode, CSSProperties } from "react";
import { useI18n } from "../i18n/I18nProvider";

export interface DataColumn<T> {
  key: string;
  header: ReactNode;
  render: (row: T) => ReactNode;
  width?: string;
  align?: "left" | "right" | "center";
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
              <th key={c.key} style={{ width: c.width, ...(c.align ? ALIGN[c.align] : null) }}>
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
                <td key={c.key} style={c.align ? ALIGN[c.align] : undefined}>
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
