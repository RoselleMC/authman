import { useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { Icon } from "../../components/Icon";
import { Select } from "../../components/Select";
import { useI18n } from "../../i18n/I18nProvider";
import { cx } from "../../utils/cx";
import { ColumnVisibilityMenu } from "./ColumnVisibilityMenu";
import { Pagination } from "./Pagination";
import {
  applyClientFilters,
  paginateClient,
  sortClientRows,
  totalPagesFor,
  visibleColumns,
  withPageReset,
} from "./state";
import type { ListColumn, ListMode, ListState } from "./types";

const ALIGN: Record<NonNullable<ListColumn<unknown>["align"]>, CSSProperties> = {
  left: { textAlign: "left" },
  right: { textAlign: "right" },
  center: { textAlign: "center" },
};

function cellStyle<T>(column: ListColumn<T>, head = false): CSSProperties {
  return {
    width: column.width,
    minWidth: column.minWidth ?? column.width,
    ...(column.align ? ALIGN[column.align] : null),
    ...(column.sticky === "right" ? { position: "sticky", right: 0, zIndex: head ? 4 : 2 } : null),
  };
}

interface BaseProps<T> {
  columns: ReadonlyArray<ListColumn<T>>;
  rowKey: (row: T) => string;
  state: ListState;
  onStateChange: (next: ListState) => void;
  pageSizeOptions?: ReadonlyArray<number>;
  onRowClick?: (row: T) => void;
  empty?: ReactNode;
  loading?: boolean;
  /** Permanent list-level actions, rendered on the toolbar left. */
  primaryActions?: ReactNode;
  /** Extra controls rendered next to the column-visibility menu on the toolbar right. */
  toolbarActions?: ReactNode;
  selectable?: boolean | ((row: T) => boolean);
  selectionActions?: (selectedRows: T[]) => ReactNode;
  /** Stable test ID prefix. Children controls derive their own IDs from it. */
  testId?: string;
}

interface ClientProps<T> extends BaseProps<T> {
  mode?: "client";
  /** Full dataset. Filters + pagination applied client-side. */
  rows: ReadonlyArray<T>;
  total?: undefined;
}

interface ServerProps<T> extends BaseProps<T> {
  mode: "server";
  /** Pre-paginated rows for the current page. */
  rows: ReadonlyArray<T>;
  /** Total row count across all pages. */
  total: number;
}

export type AdvancedListProps<T> = ClientProps<T> | ServerProps<T>;

const DEFAULT_PAGE_SIZES = [10, 25, 50, 100] as const;

/**
 * AdvancedList — a single primitive for every operational table in Authman.
 *
 *   <AdvancedList
 *     columns={columns}
 *     rowKey={r => r.id}
 *     state={list.state}
 *     onStateChange={list.setState}
 *     mode="server"
 *     rows={query.data?.rows ?? []}
 *     total={query.data?.meta?.total ?? 0}
 *     loading={query.isLoading}
 *     testId="players"
 *   />
 *
 * The component is dumb about what filters mean. A `client` page lets the
 * primitive filter and paginate the rows it received. A `server` page maps
 * `state.filters / state.page / state.pageSize` into API params, fetches
 * the matching slice, and hands back exactly `{ rows, total }`.
 */
export function AdvancedList<T>(props: AdvancedListProps<T>) {
  const { t } = useI18n();
  const {
    columns,
    rowKey,
    state,
    onStateChange,
    pageSizeOptions = DEFAULT_PAGE_SIZES,
    onRowClick,
    empty,
    loading,
    primaryActions,
    toolbarActions,
    selectable,
    selectionActions,
    testId,
  } = props;
  const [selectedKeys, setSelectedKeys] = useState<string[]>([]);

  const visible = useMemo(() => visibleColumns(columns, state), [columns, state]);
  const selectionEnabled = !!selectable || !!selectionActions;

  // Client mode applies filters + pagination locally; server mode trusts the
  // caller to have done that already.
  const { rowsToRender, total } = useMemo(() => {
    if (props.mode === "server") {
      return { rowsToRender: props.rows.slice(), total: props.total };
    }
    const filtered = applyClientFilters(props.rows, columns, state);
    const sorted = sortClientRows(filtered, columns, state);
    return { rowsToRender: paginateClient(sorted, state), total: filtered.length };
  }, [props, columns, state]);

  const totalPages = totalPagesFor(total, state.pageSize);
  // If the user navigates to /list?page=999 and only 3 pages exist, snap to
  // the last available page lazily on the next state change.
  const effectivePage = Math.min(Math.max(1, state.page), totalPages);

  function setFilter(columnKey: string, value: string) {
    onStateChange(withPageReset({ ...state, filters: { ...state.filters, [columnKey]: value } }));
  }
  function setPage(page: number) {
    onStateChange({ ...state, page });
  }
  function setPageSize(pageSize: number) {
    onStateChange(withPageReset({ ...state, pageSize }));
  }
  function toggleHidden(columnKey: string) {
    const set = new Set(state.hidden);
    if (set.has(columnKey)) set.delete(columnKey);
    else set.add(columnKey);
    onStateChange({ ...state, hidden: Array.from(set) });
  }
  function toggleSort(column: ListColumn<T>) {
    if (!column.sortable) return;
    const sortDir = state.sortKey === column.key && state.sortDir === "asc" ? "desc" : "asc";
    onStateChange(withPageReset({ ...state, sortKey: column.key, sortDir }));
  }
  function rowSelectable(row: T) {
    if (!selectionEnabled) return false;
    if (typeof selectable === "function") return selectable(row);
    return selectable !== false;
  }
  function toggleRow(row: T) {
    const key = rowKey(row);
    const set = new Set(selectedKeys);
    if (set.has(key)) set.delete(key);
    else set.add(key);
    setSelectedKeys(Array.from(set));
  }
  function toggleAllVisible() {
    const visibleKeys = rowsToRender.filter(rowSelectable).map(rowKey);
    const selected = new Set(selectedKeys);
    const allSelected = visibleKeys.length > 0 && visibleKeys.every((key) => selected.has(key));
    for (const key of visibleKeys) {
      if (allSelected) selected.delete(key);
      else selected.add(key);
    }
    setSelectedKeys(Array.from(selected));
  }

  const hasFilterRow = visible.some((c) => c.filter);
  const activeFilters = Object.entries(state.filters).filter(([k, v]) => v && visible.some((c) => c.key === k));
  const selectedRows = rowsToRender.filter((row) => selectedKeys.includes(rowKey(row)));
  const selectableVisibleRows = rowsToRender.filter(rowSelectable);
  const allVisibleSelected = selectableVisibleRows.length > 0 && selectableVisibleRows.every((row) => selectedKeys.includes(rowKey(row)));
  const someVisibleSelected = selectableVisibleRows.some((row) => selectedKeys.includes(rowKey(row)));

  return (
    <div className="adv-list" data-testid={testId}>
      <div className="adv-list-toolbar" data-testid={testId ? `${testId}-toolbar` : undefined}>
        <div className="adv-list-toolbar__left">
          {activeFilters.length > 0 ? (
            <button
              type="button"
              className="link-btn"
              onClick={() => onStateChange(withPageReset({ ...state, filters: {} }))}
              data-testid={testId ? `${testId}-clear-filters` : "list-clear-filters"}
            >
              {t("common.clearFilters")} ({activeFilters.length})
            </button>
          ) : null}
        </div>
        <div className="adv-list-toolbar__right">
          {primaryActions ? <div className="adv-list-primary-actions">{primaryActions}</div> : null}
          {selectedRows.length > 0 && selectionActions ? (
            <div className="adv-list-selection-actions" data-testid={testId ? `${testId}-selection-actions` : undefined}>
              <span className="adv-list-selection-count">{selectedRows.length}</span>
              {selectionActions(selectedRows)}
            </div>
          ) : null}
          {toolbarActions}
          <ColumnVisibilityMenu
            columns={columns}
            hidden={state.hidden}
            onToggle={toggleHidden}
            testId={testId ? `${testId}-vis` : undefined}
          />
        </div>
      </div>
      <div className="table-scroll" data-testid={testId ? `${testId}-table` : "advanced-list-table"}>
        <table className="tbl">
          <thead>
            <tr>
              {selectionEnabled ? (
                <th className="adv-list-select-cell" style={{ width: 42, minWidth: 42 }} data-testid={testId ? `${testId}-th-select` : undefined}>
                  <label className="adv-list-select-hit" onClick={(e) => e.stopPropagation()}>
                    <input
                      type="checkbox"
                      checked={allVisibleSelected}
                      ref={(el) => {
                        if (el) el.indeterminate = !allVisibleSelected && someVisibleSelected;
                      }}
                      disabled={selectableVisibleRows.length === 0}
                      onChange={toggleAllVisible}
                      aria-label={t("list.selectAll")}
                      data-testid={testId ? `${testId}-select-all` : undefined}
                    />
                  </label>
                </th>
              ) : null}
              {visible.map((c) => (
                <th
                  key={c.key}
                  className={cx(c.sticky === "right" && "is-sticky-right", c.sortable && "sortable")}
                  style={cellStyle(c, true)}
                  data-testid={testId ? `${testId}-th-${c.key}` : undefined}
                >
                  {c.sortable ? (
                    <button
                      type="button"
                      className="tbl-sort"
                      onClick={() => toggleSort(c)}
                      data-testid={testId ? `${testId}-sort-${c.key}` : undefined}
                    >
                      <span>{c.header}</span>
                      {state.sortKey === c.key ? <Icon name={state.sortDir === "desc" ? "chevronDown" : "chevronUp"} size={13} /> : null}
                    </button>
                  ) : c.header}
                </th>
              ))}
            </tr>
            {hasFilterRow ? (
              <tr className="adv-list-filter-row" data-testid={testId ? `${testId}-filter-row` : undefined}>
                {selectionEnabled ? <th className="adv-list-select-cell" style={{ width: 42, minWidth: 42 }} /> : null}
                {visible.map((c) => (
                  <th
                    key={c.key}
                    className={cx("adv-list-filter-cell", c.sticky === "right" && "is-sticky-right")}
                    style={cellStyle(c, true)}
                  >
                    {c.filter ? (
                      <ColumnFilterControl
                        column={c}
                        value={state.filters[c.key] ?? ""}
                        onChange={(next) => setFilter(c.key, next)}
                        testId={testId ? `${testId}-filter-${c.key}` : `list-filter-${c.key}`}
                      />
                    ) : null}
                  </th>
                ))}
              </tr>
            ) : null}
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={visible.length + (selectionEnabled ? 1 : 0)} style={{ padding: 20 }} data-testid={testId ? `${testId}-loading` : "list-loading"}>
                  {t("common.loading")}
                </td>
              </tr>
            ) : rowsToRender.length === 0 ? (
              <tr>
                <td colSpan={visible.length + (selectionEnabled ? 1 : 0)} data-testid={testId ? `${testId}-empty` : "list-empty"}>
                  {empty ?? <DefaultEmpty />}
                </td>
              </tr>
            ) : (
              rowsToRender.map((row) => (
                <tr
                  key={rowKey(row)}
                  data-testid="data-row"
                  onClick={onRowClick ? () => onRowClick(row) : undefined}
                  style={{ cursor: onRowClick ? "pointer" : "default" }}
                >
                  {selectionEnabled ? (
                    <td className="adv-list-select-cell" onClick={(e) => e.stopPropagation()}>
                      <label className="adv-list-select-hit">
                        <input
                          type="checkbox"
                          checked={selectedKeys.includes(rowKey(row))}
                          disabled={!rowSelectable(row)}
                          onChange={() => toggleRow(row)}
                          aria-label={t("list.selectRow")}
                          data-testid={testId ? `${testId}-select-${rowKey(row)}` : undefined}
                        />
                      </label>
                    </td>
                  ) : null}
                  {visible.map((c) => (
                    <td key={c.key} className={c.sticky === "right" ? "is-sticky-right" : undefined} style={cellStyle(c)}>
                      {c.render(row)}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <Pagination
        page={effectivePage}
        pageSize={state.pageSize}
        total={total}
        pageSizeOptions={pageSizeOptions}
        onPage={setPage}
        onPageSize={setPageSize}
        testId={testId ? `${testId}-pagination` : "list-pagination"}
      />
    </div>
  );
}

function ColumnFilterControl<T>({
  column,
  value,
  onChange,
  testId,
}: {
  column: ListColumn<T>;
  value: string;
  onChange: (next: string) => void;
  testId: string;
}) {
  const { t } = useI18n();
  if (!column.filter) return null;
  if (column.filter.type === "text") {
    const columnLabel = typeof column.header === "string" ? column.header : column.key;
    return (
      <input
        type="search"
        className="adv-list-filter__input"
        placeholder={column.filter.placeholder ?? t("list.filterPlaceholder")}
        value={value}
        aria-label={t("list.filter").replace("{column}", columnLabel)}
        onChange={(e) => onChange(e.target.value)}
        data-testid={testId}
      />
    );
  }
  return (
    <Select
      className="select-ui--compact adv-list-filter__select"
      value={value}
      ariaLabel={t("list.filter").replace("{column}", typeof column.header === "string" ? column.header : column.key)}
      onChange={onChange}
      options={column.filter.options.map((opt) => ({ value: opt.value, label: opt.label }))}
      testId={testId}
    />
  );
}

function DefaultEmpty() {
  const { t } = useI18n();
  return <div style={{ padding: 24, color: "var(--color-text-muted)", textAlign: "center" }}>{t("list.noMatchingRows")}</div>;
}
