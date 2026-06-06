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
  const { urlPrefix = "", urlSync = true, defaults } = opts;
  const [params, setParams] = useSearchParams();

  const initial = useMemo(
    () => (urlSync ? readStateFromParams(params, urlPrefix, defaults) : makeDefaultState(defaults)),
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
    const decoded = readStateFromParams(params, urlPrefix, defaults);
    const last = lastWriteRef.current;
    if (
      decoded.page !== last.page ||
      decoded.pageSize !== last.pageSize ||
      decoded.hidden.join(",") !== last.hidden.join(",") ||
      JSON.stringify(decoded.filters) !== JSON.stringify(last.filters)
    ) {
      lastWriteRef.current = decoded;
      setMemState(decoded);
    }
  }, [params, urlPrefix, urlSync, defaults]);

  const setState = useCallback(
    (next: ListState) => {
      lastWriteRef.current = next;
      setMemState(next);
      if (!urlSync) return;
      const nextParams = new URLSearchParams(params);
      writeStateToParams(next, nextParams, urlPrefix, defaults);
      setParams(nextParams, { replace: true });
    },
    [params, setParams, urlPrefix, urlSync, defaults],
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
  const reset = useCallback(() => setState(makeDefaultState(defaults)), [defaults, setState]);

  return { state: memState, setState, setPage, setPageSize, setFilter, setHidden, toggleHidden, reset };
}
