import { useMemo, type CSSProperties, type ReactNode } from "react";
import { Select } from "../../components/Select";
import { useI18n } from "../../i18n/I18nProvider";
import { ColumnVisibilityMenu } from "./ColumnVisibilityMenu";
import { Pagination } from "./Pagination";
import {
  applyClientFilters,
  paginateClient,
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

interface BaseProps<T> {
  columns: ReadonlyArray<ListColumn<T>>;
  rowKey: (row: T) => string;
  state: ListState;
  onStateChange: (next: ListState) => void;
  pageSizeOptions?: ReadonlyArray<number>;
  onRowClick?: (row: T) => void;
  empty?: ReactNode;
  loading?: boolean;
  /** Extra controls rendered next to the column-visibility menu on the toolbar right. */
  toolbarActions?: ReactNode;
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
    toolbarActions,
    testId,
  } = props;

  const visible = useMemo(() => visibleColumns(columns, state), [columns, state]);

  // Client mode applies filters + pagination locally; server mode trusts the
  // caller to have done that already.
  const { rowsToRender, total } = useMemo(() => {
    if (props.mode === "server") {
      return { rowsToRender: props.rows.slice(), total: props.total };
    }
    const filtered = applyClientFilters(props.rows, columns, state);
    return { rowsToRender: paginateClient(filtered, state), total: filtered.length };
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

  const hasFilterRow = visible.some((c) => c.filter);
  const activeFilters = Object.entries(state.filters).filter(([k, v]) => v && visible.some((c) => c.key === k));

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
              {visible.map((c) => (
                <th
                  key={c.key}
                  style={{ width: c.width, ...(c.align ? ALIGN[c.align] : null) }}
                  data-testid={testId ? `${testId}-th-${c.key}` : undefined}
                >
                  {c.header}
                </th>
              ))}
            </tr>
            {hasFilterRow ? (
              <tr className="adv-list-filter-row" data-testid={testId ? `${testId}-filter-row` : undefined}>
                {visible.map((c) => (
                  <th key={c.key} className="adv-list-filter-cell">
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
                <td colSpan={visible.length} style={{ padding: 20 }} data-testid={testId ? `${testId}-loading` : "list-loading"}>
                  {t("common.loading")}
                </td>
              </tr>
            ) : rowsToRender.length === 0 ? (
              <tr>
                <td colSpan={visible.length} data-testid={testId ? `${testId}-empty` : "list-empty"}>
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
                  {visible.map((c) => (
                    <td key={c.key} style={c.align ? ALIGN[c.align] : undefined}>
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
