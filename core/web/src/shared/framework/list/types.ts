import type { ReactNode } from "react";

/**
 * Per-column filter declaration. `text` filters render as an input,
 * `select` filters render as the shared custom Select. The framework keeps the filter
 * VALUE as a string regardless of the filter type so URL sync stays trivial.
 */
export type ListColumnFilter =
  | { type: "text"; placeholder?: string }
  | { type: "select"; options: ReadonlyArray<{ value: string; label: string }> };

/**
 * Column definition for AdvancedList.
 *
 * A column may declare:
 * - a filter (rendered as a header sub-row in the table)
 * - whether it is visible by default (default true)
 * - whether the user is allowed to hide it (mandatory=true disables hide)
 * - for client mode: how to read a filterable value from the row
 */
export interface ListColumn<T> {
  key: string;
  header: ReactNode;
  render: (row: T) => ReactNode;
  sortable?: boolean;
  sortValue?: (row: T) => string | number | boolean | Date | null | undefined;
  width?: string;
  minWidth?: string;
  align?: "left" | "right" | "center";
  /** Keep the column visible while the table scrolls horizontally. */
  sticky?: "right";
  /** Default visibility. Defaults to true. */
  defaultVisible?: boolean;
  /** When true, the column cannot be hidden by the user. */
  mandatory?: boolean;
  /** Per-column filter rendered under the header. */
  filter?: ListColumnFilter;
  /**
   * Reads a filterable string from a row (client mode only).
   * Server-mode pages map filter values to API params themselves.
   */
  getFilterValue?: (row: T) => string;
  /** When true, this column is collapsed first on narrow viewports. */
  hideOnNarrow?: boolean;
}

/**
 * AdvancedList state. Pages typically own this via `useListState` and hand
 * `{ state, onStateChange }` to AdvancedList. The shape is stable so it
 * round-trips through URL search params.
 */
export interface ListState {
  /** 1-based page number. Clamped to [1, totalPages] by the framework. */
  page: number;
  pageSize: number;
  /** column key → filter value. Empty string clears the filter. */
  filters: Record<string, string>;
  /** Set of column keys the user has hidden. Mandatory columns are ignored. */
  hidden: string[];
  /** Whether the filter header row is visible. Defaults to false. */
  filtersVisible: boolean;
  sortKey?: string;
  sortDir?: "asc" | "desc";
}

export interface ListStateDefaults {
  pageSize?: number;
  hidden?: string[];
  filtersVisible?: boolean;
  filters?: Record<string, string>;
  sortKey?: string;
  sortDir?: "asc" | "desc";
}

export type ListMode = "client" | "server";
