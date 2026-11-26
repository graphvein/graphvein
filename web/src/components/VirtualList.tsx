// A small fixed-height windowed list (T016d D8). Items are group headers
// and rows with fixed heights, so the offset of every item is a prefix sum
// and the visible window is found by binary search. Only the window plus
// an overscan is in the DOM. Rows are positioned with transform through
// React style props (CSSOM), which the page's CSP allows; no <style>
// element is created. The header of the group at the top is drawn again
// in a sticky overlay, so the transport stays visible while scrolling.
//
// Accessibility: the scroll container is the focusable listbox; rows are
// options referenced by aria-activedescendant, with aria-setsize and
// aria-posinset because most options are not in the DOM. Headers are
// decoration for sighted users (each option's label names its group).

import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import { cn } from "@/lib/utils";

export interface VirtualItem {
  key: string;
  kind: "header" | "row";
}

export interface VirtualListProps<T extends VirtualItem> {
  items: readonly T[];
  rowHeight: number;
  headerHeight: number;
  /** Items rendered above and below the visible window. */
  overscan?: number;
  selectedKey: string | null;
  onSelect: (key: string) => void;
  renderRow: (item: T, selected: boolean) => ReactNode;
  renderHeader: (item: T) => ReactNode;
  /** The DOM id of a row's option element. */
  optionId: (key: string) => string;
  /** The accessible name of a row. */
  optionLabel: (item: T) => string;
  ariaLabel: string;
  className?: string;
  /** Used when the container reports no height (jsdom, first paint). */
  fallbackHeight?: number;
  /** Shown instead of rows when items is empty. */
  empty?: ReactNode;
}

/** findItem returns the largest index i with offsets[i] <= y (0 for y
 * above the first item); offsets is ascending. */
export function findItem(offsets: readonly number[], y: number): number {
  let lo = 0;
  let hi = offsets.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if ((offsets[mid] ?? 0) <= y) lo = mid;
    else hi = mid - 1;
  }
  return lo;
}

/** VirtualList renders items in a scrolling, windowed listbox. */
export function VirtualList<T extends VirtualItem>(props: VirtualListProps<T>) {
  const { items, rowHeight, headerHeight, selectedKey, onSelect, optionId } = props;
  const overscan = props.overscan ?? 6;
  const ref = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [measured, setMeasured] = useState(0);
  const height = measured > 0 ? measured : (props.fallbackHeight ?? 600);

  const layout = useMemo(() => {
    const offsets: number[] = [];
    const headers: number[] = [];
    const rows: number[] = [];
    let y = 0;
    items.forEach((item, i) => {
      offsets.push(y);
      if (item.kind === "header") {
        headers.push(i);
        y += headerHeight;
      } else {
        rows.push(i);
        y += rowHeight;
      }
    });
    return { offsets, headers, rows, total: y };
  }, [items, rowHeight, headerHeight]);

  const rowPos = useMemo(() => {
    const m = new Map<string, number>();
    layout.rows.forEach((itemIndex, pos) => {
      const item = items[itemIndex];
      if (item !== undefined) m.set(item.key, pos);
    });
    return m;
  }, [items, layout]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (el === null) return;
    setMeasured(el.clientHeight);
    const observer = new ResizeObserver(() => {
      setMeasured(el.clientHeight);
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
    };
  }, []);

  // Keep the selected row visible (keyboard moves, deep links, filters),
  // below the sticky header overlay.
  useEffect(() => {
    const el = ref.current;
    if (el === null || selectedKey === null) return;
    const pos = rowPos.get(selectedKey);
    if (pos === undefined) return;
    const top = layout.offsets[layout.rows[pos] ?? 0] ?? 0;
    const viewport = el.clientHeight > 0 ? el.clientHeight : height;
    const overlay = layout.headers.length > 0 ? headerHeight : 0;
    if (top - overlay < el.scrollTop) el.scrollTop = Math.max(0, top - overlay);
    else if (top + rowHeight > el.scrollTop + viewport) el.scrollTop = top + rowHeight - viewport;
    setScrollTop(el.scrollTop);
    // Only a new selection or new items move the scroll position; the
    // layout and height follow from items.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedKey, items]);

  const first = Math.max(0, findItem(layout.offsets, scrollTop) - overscan);
  const last = Math.min(items.length - 1, findItem(layout.offsets, scrollTop + height) + overscan);

  // The sticky overlay shows the header of the group at the top; the next
  // header pushes it up as it arrives.
  let overlay: ReactNode = null;
  if (layout.headers.length > 0) {
    const hOffsets = layout.headers.map((i) => layout.offsets[i] ?? 0);
    const h = findItem(hOffsets, scrollTop);
    const headerItem = items[layout.headers[h] ?? 0];
    const next = hOffsets[h + 1];
    const push = next !== undefined && next - scrollTop < headerHeight ? next - scrollTop - headerHeight : 0;
    if (headerItem !== undefined && (hOffsets[h] ?? 0) <= scrollTop) {
      overlay = (
        <div className="pointer-events-none sticky top-0 z-10 h-0" aria-hidden="true">
          <div data-testid="sticky-header" style={{ height: headerHeight, transform: `translateY(${String(push)}px)` }}>
            {props.renderHeader(headerItem)}
          </div>
        </div>
      );
    }
  }

  const select = (pos: number) => {
    const clamped = Math.max(0, Math.min(layout.rows.length - 1, pos));
    const item = items[layout.rows[clamped] ?? -1];
    if (item !== undefined) onSelect(item.key);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (layout.rows.length === 0) return;
    const cur = selectedKey === null ? -1 : (rowPos.get(selectedKey) ?? -1);
    const page = Math.max(1, Math.floor(height / rowHeight) - 1);
    let next: number;
    switch (event.key) {
      case "ArrowDown":
        next = cur + 1;
        break;
      case "ArrowUp":
        next = cur === -1 ? 0 : cur - 1;
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = layout.rows.length - 1;
        break;
      case "PageDown":
        next = cur + page;
        break;
      case "PageUp":
        next = cur === -1 ? 0 : cur - page;
        break;
      default:
        return;
    }
    event.preventDefault();
    select(next);
  };

  const rendered: ReactNode[] = [];
  for (let i = first; i <= last; i++) {
    const item = items[i];
    if (item === undefined) continue;
    const style = {
      height: item.kind === "header" ? headerHeight : rowHeight,
      transform: `translateY(${String(layout.offsets[i] ?? 0)}px)`,
    };
    if (item.kind === "header") {
      rendered.push(
        <div key={item.key} className="absolute inset-x-0 top-0" style={style} aria-hidden="true">
          {props.renderHeader(item)}
        </div>,
      );
    } else {
      const selected = item.key === selectedKey;
      rendered.push(
        <div
          key={item.key}
          id={optionId(item.key)}
          role="option"
          aria-selected={selected}
          aria-label={props.optionLabel(item)}
          aria-setsize={layout.rows.length}
          aria-posinset={(rowPos.get(item.key) ?? 0) + 1}
          className="absolute inset-x-0 top-0"
          style={style}
          onClick={() => {
            onSelect(item.key);
          }}
        >
          {props.renderRow(item, selected)}
        </div>,
      );
    }
  }

  const activeId = selectedKey !== null && rowPos.has(selectedKey) ? optionId(selectedKey) : undefined;
  return (
    <div
      ref={ref}
      role="listbox"
      tabIndex={0}
      aria-label={props.ariaLabel}
      aria-activedescendant={activeId}
      className={cn("relative min-h-0 flex-1 overflow-auto overscroll-contain", props.className)}
      onScroll={(event) => {
        setScrollTop(event.currentTarget.scrollTop);
      }}
      onKeyDown={onKeyDown}
    >
      {overlay}
      {items.length === 0 ? props.empty : <div className="relative" style={{ height: layout.total }}>{rendered}</div>}
    </div>
  );
}
