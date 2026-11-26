// usePaged loads the first page of a cursor-paged change endpoint (items,
// impact paths) with useApiRequest and appends later pages on request. A
// new key drops every page at once; a cursor answered with
// index_changed (another head run) reloads from the first page.

import { useCallback, useState } from "react";

import { ApiError } from "@/api/client";
import { INDEX_CHANGED } from "@/api/endpoints";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";

export interface Paged<P> {
  first: RequestResult<P>;
  /** The pages after the first, in order. */
  more: P[];
  loadMore: () => void;
  loadingMore: boolean;
  moreError: Error | null;
}

interface Extra<P> {
  key: string | null;
  pages: P[];
  loading: boolean;
  error: Error | null;
}

export function usePaged<P extends { next_cursor: string | null }>(key: string | null, load: (cursor: string | null, signal: AbortSignal) => Promise<P>): Paged<P> {
  const first = useApiRequest(key, (signal) => load(null, signal));
  const [extra, setExtra] = useState<Extra<P>>({ key, pages: [], loading: false, error: null });
  let current = extra;
  if (extra.key !== key) {
    current = { key, pages: [], loading: false, error: null };
    setExtra(current);
  }
  const last = current.pages.at(-1) ?? (first.state.kind === "ok" ? first.state.data : null);
  const cursor = last?.next_cursor ?? null;
  const { retry } = first;
  const loadMore = useCallback(() => {
    if (cursor === null) return;
    setExtra((e) => ({ ...e, loading: true, error: null }));
    const controller = new AbortController();
    load(cursor, controller.signal).then(
      (page) => {
        setExtra((e) => (e.key === key ? { ...e, pages: [...e.pages, page], loading: false } : e));
      },
      (err: unknown) => {
        if (err instanceof ApiError && err.code === INDEX_CHANGED) {
          setExtra({ key, pages: [], loading: false, error: null });
          retry();
          return;
        }
        setExtra((e) => (e.key === key ? { ...e, loading: false, error: err instanceof Error ? err : new Error(String(err)) } : e));
      },
    );
    // load is an inline function of the caller; key and cursor name what it reads.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, cursor, retry]);
  return { first, more: current.pages, loadMore, loadingMore: current.loading, moreError: current.error };
}
