import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  makeDefaultState,
  readStateFromParams,
  withPageReset,
  writeStateToParams,
} from "./state";
import type { ListState, ListStateDefaults } from "./types";

export interface UseListStateOptions {
  /**
   * Prefix for URL params: prefix="players" → ?players.page=2&players.size=50.
   * Pass an empty string for simple pages with a single list. Two lists on
   * one page must use different prefixes.
   */
  urlPrefix?: string;
  /** When false the hook keeps state in memory only. */
  urlSync?: boolean;
  /** Default pageSize, default hidden columns, default filter values. */
  defaults?: ListStateDefaults;
  /**
   * Optional persistence scope, normally the current admin user id.
   * Layout preferences (page size, hidden columns, and filter-row visibility) are saved per scope and
   * URL prefix, while filters/page remain transient.
   */
  storageScope?: string;
}

export interface UseListStateReturn {
  state: ListState;
  setState: (next: ListState) => void;
  setPage: (next: number) => void;
  setPageSize: (next: number) => void;
  setFilter: (columnKey: string, value: string) => void;
  setHidden: (next: string[]) => void;
  toggleHidden: (columnKey: string) => void;
  reset: () => void;
}

/**
 * State manager for AdvancedList. Use one of these per list. The hook
 * persists state to the URL by default (single source of truth, share-able
 * links, browser back-button support).
 *
 *   const players = useListState({ urlPrefix: "p", defaults: { pageSize: 25 } });
 *   <AdvancedList state={players.state} onStateChange={players.setState} ... />
 */
export function useListState(opts: UseListStateOptions = {}): UseListStateReturn {
  const { urlPrefix = "", urlSync = true, defaults, storageScope } = opts;
  const [params, setParams] = useSearchParams();
  const storageKey = useMemo(() => makeStorageKey(storageScope, urlPrefix), [storageScope, urlPrefix]);
  const effectiveDefaults = useMemo(
    () => mergeStoredDefaults(defaults, readStoredLayout(storageKey)),
    [defaults, storageKey],
  );

  const initial = useMemo(
    () => (urlSync ? readStateFromParams(params, urlPrefix, effectiveDefaults) : makeDefaultState(effectiveDefaults)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  const [memState, setMemState] = useState<ListState>(initial);

  // Track which side most recently changed state so we don't fight ourselves
  // when react-router fires its own params subscription.
  const lastWriteRef = useRef<ListState>(initial);

  // External URL changes (back button / shared link) update our local state.
  useEffect(() => {
    if (!urlSync) return;
    const last = lastWriteRef.current;
    const decoded = { ...readStateFromParams(params, urlPrefix, effectiveDefaults), filtersVisible: last.filtersVisible };
    if (
      decoded.page !== last.page ||
      decoded.pageSize !== last.pageSize ||
      decoded.hidden.join(",") !== last.hidden.join(",") ||
      decoded.sortKey !== last.sortKey ||
      decoded.sortDir !== last.sortDir ||
      JSON.stringify(decoded.filters) !== JSON.stringify(last.filters)
    ) {
      lastWriteRef.current = decoded;
      setMemState(decoded);
    }
  }, [params, urlPrefix, urlSync, effectiveDefaults]);

  const setState = useCallback(
    (next: ListState) => {
      lastWriteRef.current = next;
      setMemState(next);
      writeStoredLayout(storageKey, next);
      if (!urlSync) return;
      const nextParams = new URLSearchParams(params);
      writeStateToParams(next, nextParams, urlPrefix, effectiveDefaults);
      setParams(nextParams, { replace: true });
    },
    [params, setParams, storageKey, urlPrefix, urlSync, effectiveDefaults],
  );

  const setPage = useCallback((page: number) => setState({ ...memState, page }), [memState, setState]);
  const setPageSize = useCallback(
    (pageSize: number) => setState(withPageReset({ ...memState, pageSize })),
    [memState, setState],
  );
  const setFilter = useCallback(
    (columnKey: string, value: string) =>
      setState(withPageReset({ ...memState, filters: { ...memState.filters, [columnKey]: value } })),
    [memState, setState],
  );
  const setHidden = useCallback(
    (next: string[]) => setState({ ...memState, hidden: next }),
    [memState, setState],
  );
  const toggleHidden = useCallback(
    (columnKey: string) => {
      const set = new Set(memState.hidden);
      if (set.has(columnKey)) set.delete(columnKey);
      else set.add(columnKey);
      setState({ ...memState, hidden: Array.from(set) });
    },
    [memState, setState],
  );
  const reset = useCallback(() => setState(makeDefaultState(effectiveDefaults)), [effectiveDefaults, setState]);

  return { state: memState, setState, setPage, setPageSize, setFilter, setHidden, toggleHidden, reset };
}

interface StoredLayout {
  pageSize?: number;
  hidden?: string[];
  filtersVisible?: boolean;
}

function makeStorageKey(scope: string | undefined, prefix: string) {
  const cleanScope = scope?.trim();
  if (!cleanScope || typeof window === "undefined") return "";
  return `authman.list.layout.v1.${encodeURIComponent(cleanScope)}.${encodeURIComponent(prefix || "default")}`;
}

function readStoredLayout(key: string): StoredLayout | null {
  if (!key || typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as StoredLayout;
    const pageSize = typeof parsed.pageSize === "number" ? parsed.pageSize : Number.NaN;
    return {
      pageSize: Number.isFinite(pageSize) && pageSize > 0 ? Math.floor(pageSize) : undefined,
      hidden: Array.isArray(parsed.hidden) ? parsed.hidden.filter((v): v is string => typeof v === "string") : undefined,
      filtersVisible: typeof parsed.filtersVisible === "boolean" ? parsed.filtersVisible : undefined,
    };
  } catch {
    return null;
  }
}

function writeStoredLayout(key: string, state: ListState) {
  if (!key || typeof window === "undefined") return;
  try {
    window.localStorage.setItem(key, JSON.stringify({ pageSize: state.pageSize, hidden: state.hidden, filtersVisible: state.filtersVisible }));
  } catch {
    // localStorage can be disabled; URL state still works.
  }
}

function mergeStoredDefaults(defaults: ListStateDefaults | undefined, stored: StoredLayout | null): ListStateDefaults | undefined {
  if (!stored) return defaults;
  return {
    ...defaults,
    pageSize: stored.pageSize ?? defaults?.pageSize,
    hidden: stored.hidden ?? defaults?.hidden,
    filtersVisible: stored.filtersVisible ?? defaults?.filtersVisible,
  };
}
