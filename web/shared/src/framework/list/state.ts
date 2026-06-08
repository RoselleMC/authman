/*
 * Pure helpers for AdvancedList state. Keeping these synchronous and
 * dependency-free lets us unit-test the URL <-> state codec and the
 * client-side filter/pagination logic without spinning up React.
 *
 * Every helper here MUST be pure. The component layer (`AdvancedList.tsx`)
 * and the React hook (`useListState.ts`) wrap them.
 */

import type { ListColumn, ListState, ListStateDefaults } from "./types";

const DEFAULT_PAGE_SIZE = 25;
const HIDDEN_SEP = ",";

export function makeDefaultState(defaults?: ListStateDefaults): ListState {
  return {
    page: 1,
    pageSize: defaults?.pageSize ?? DEFAULT_PAGE_SIZE,
    filters: { ...(defaults?.filters ?? {}) },
    hidden: [...(defaults?.hidden ?? [])],
    sortKey: defaults?.sortKey,
    sortDir: defaults?.sortDir,
  };
}

/**
 * Returns the visible-column ordering for a given state. Mandatory columns
 * can never be hidden, even if their key sneaks into state.hidden (e.g. via
 * a hand-edited URL).
 */
export function visibleColumns<T>(columns: ReadonlyArray<ListColumn<T>>, state: ListState): ListColumn<T>[] {
  const hidden = new Set(state.hidden);
  return columns.filter((c) => c.mandatory || !hidden.has(c.key));
}

/**
 * Apply client-side text/select filters. Empty filter values are no-ops.
 * Filtering is case-insensitive substring for `text`, exact match for
 * `select` (the option's `value` must equal the column's filter value).
 *
 * Columns without `getFilterValue` are skipped — those filters are meant
 * to be mapped to backend params instead.
 */
export function applyClientFilters<T>(
  rows: ReadonlyArray<T>,
  columns: ReadonlyArray<ListColumn<T>>,
  state: ListState,
): T[] {
  let out = rows.slice();
  for (const col of columns) {
    const raw = state.filters[col.key];
    if (!raw) continue;
    if (!col.filter || !col.getFilterValue) continue;
    const value = raw.trim();
    if (!value) continue;
    if (col.filter.type === "text") {
      const needle = value.toLowerCase();
      out = out.filter((r) => col.getFilterValue!(r).toLowerCase().includes(needle));
    } else if (col.filter.type === "select") {
      out = out.filter((r) => col.getFilterValue!(r) === value);
    }
  }
  return out;
}

export function totalPagesFor(total: number, pageSize: number): number {
  if (pageSize <= 0 || total <= 0) return 1;
  return Math.max(1, Math.ceil(total / pageSize));
}

export function clampPage(page: number, totalPages: number): number {
  if (!Number.isFinite(page) || page < 1) return 1;
  if (page > totalPages) return totalPages;
  return Math.floor(page);
}

export function paginateClient<T>(rows: ReadonlyArray<T>, state: ListState): T[] {
  if (state.pageSize <= 0) return rows.slice();
  const start = (state.page - 1) * state.pageSize;
  return rows.slice(start, start + state.pageSize);
}

export function sortClientRows<T>(
  rows: ReadonlyArray<T>,
  columns: ReadonlyArray<ListColumn<T>>,
  state: ListState,
): T[] {
  if (!state.sortKey || !state.sortDir) return rows.slice();
  const col = columns.find((c) => c.key === state.sortKey && c.sortable);
  if (!col) return rows.slice();
  const dir = state.sortDir === "asc" ? 1 : -1;
  const valueOf = col.sortValue ?? ((row: T) => String(col.render(row) ?? ""));
  return rows.slice().sort((a, b) => compareSortValues(valueOf(a), valueOf(b)) * dir);
}

/* ------------------------------------------------------------------ */
/* URL codec                                                            */
/* ------------------------------------------------------------------ */

function prefixed(prefix: string, name: string): string {
  return prefix ? `${prefix}.${name}` : name;
}

/**
 * Mutates `into` in place with the state's params, namespaced under
 * `prefix`. Removes empty/default values so URLs stay clean.
 *
 * - page=1 is omitted
 * - pageSize=defaults.pageSize is omitted
 * - default hidden list is omitted; an explicit empty list is kept when the default hides columns
 * - empty filter values are omitted
 */
export function writeStateToParams(
  state: ListState,
  into: URLSearchParams,
  prefix = "",
  defaults?: ListStateDefaults,
): void {
  const defSize = defaults?.pageSize ?? DEFAULT_PAGE_SIZE;
  if (state.page > 1) into.set(prefixed(prefix, "page"), String(state.page));
  else into.delete(prefixed(prefix, "page"));

  if (state.pageSize !== defSize) into.set(prefixed(prefix, "size"), String(state.pageSize));
  else into.delete(prefixed(prefix, "size"));

  const hiddenKey = prefixed(prefix, "hidden");
  const defaultHidden = defaults?.hidden ?? [];
  if (sameStringList(state.hidden, defaultHidden)) into.delete(hiddenKey);
  else into.set(hiddenKey, state.hidden.join(HIDDEN_SEP));

  if (state.sortKey) into.set(prefixed(prefix, "sort"), state.sortKey);
  else into.delete(prefixed(prefix, "sort"));
  if (state.sortKey && state.sortDir) into.set(prefixed(prefix, "dir"), state.sortDir);
  else into.delete(prefixed(prefix, "dir"));

  // Remove every existing filter param under this prefix, then add the
  // current set so cleared filters disappear from the URL.
  const filterPrefix = `${prefixed(prefix, "f")}.`;
  for (const key of Array.from(into.keys())) {
    if (key.startsWith(filterPrefix)) into.delete(key);
  }
  for (const [key, value] of Object.entries(state.filters)) {
    if (!value) continue;
    into.set(`${filterPrefix}${key}`, value);
  }
}

export function readStateFromParams(
  params: URLSearchParams,
  prefix = "",
  defaults?: ListStateDefaults,
): ListState {
  const base = makeDefaultState(defaults);
  const rawPage = Number(params.get(prefixed(prefix, "page")) ?? "");
  const rawSize = Number(params.get(prefixed(prefix, "size")) ?? "");
  const page = Number.isFinite(rawPage) && rawPage > 0 ? Math.floor(rawPage) : 1;
  const pageSize = Number.isFinite(rawSize) && rawSize > 0 ? Math.floor(rawSize) : base.pageSize;
  const hiddenKey = prefixed(prefix, "hidden");
  const hiddenRaw = params.get(hiddenKey) ?? "";
  const hidden = params.has(hiddenKey) ? hiddenRaw.split(HIDDEN_SEP).filter(Boolean) : base.hidden;

  const filterPrefix = `${prefixed(prefix, "f")}.`;
  const filters: Record<string, string> = { ...base.filters };
  for (const [key, value] of params.entries()) {
    if (!key.startsWith(filterPrefix)) continue;
    const columnKey = key.slice(filterPrefix.length);
    if (columnKey) filters[columnKey] = value;
  }
  const sortKey = params.get(prefixed(prefix, "sort")) ?? base.sortKey;
  const rawDir = params.get(prefixed(prefix, "dir")) ?? base.sortDir;
  const sortDir = rawDir === "asc" || rawDir === "desc" ? rawDir : undefined;
  return { page, pageSize, hidden, filters, sortKey: sortKey || undefined, sortDir };
}

/**
 * Convenience: when a filter, page-size, or visibility change happens we
 * want to reset the page back to 1 so the user isn't stuck on an empty
 * page-N after narrowing results.
 */
export function withPageReset(state: ListState, page = 1): ListState {
  return { ...state, page };
}

function sameStringList(a: ReadonlyArray<string>, b: ReadonlyArray<string>): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

function compareSortValues(a: unknown, b: unknown): number {
  if (a == null && b == null) return 0;
  if (a == null) return 1;
  if (b == null) return -1;
  const av = a instanceof Date ? a.getTime() : a;
  const bv = b instanceof Date ? b.getTime() : b;
  if (typeof av === "number" && typeof bv === "number") return av === bv ? 0 : av < bv ? -1 : 1;
  if (typeof av === "boolean" && typeof bv === "boolean") return av === bv ? 0 : av ? 1 : -1;
  return String(av).localeCompare(String(bv), undefined, { numeric: true, sensitivity: "base" });
}
