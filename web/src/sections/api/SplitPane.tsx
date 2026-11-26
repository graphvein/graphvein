// The resizable list/detail split (M5, T016d D8). Hand-written instead of
// a resizable-panels dependency: one handle, pixel widths, and the width
// kept per browser in localStorage. The handle is always visible (M5 fix),
// is a focusable separator, and moves with Left/Right by 16 px.

import { useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";

import { cn } from "@/lib/utils";

/** The localStorage key of the list width. */
export const WIDTH_KEY = "graphvein.api.listWidth";
export const MIN_LIST = 320;
export const MIN_DETAIL = 440;
export const KEY_STEP = 16;
/** HANDLE_WIDTH is the handle's width (w-3.5). */
const HANDLE_WIDTH = 14;
/** MIN_SPLIT_WIDTH is the least width that holds both minimums side by
 * side; below it a caller should not use the split (the API section shows
 * the list as a rail instead), or the detail is squeezed under MIN_DETAIL. */
export const MIN_SPLIT_WIDTH = MIN_LIST + HANDLE_WIDTH + MIN_DETAIL;

/** defaultWidth is M5's default: 42 % of the container, clamped to
 * 360-560 px. */
export function defaultWidth(container: number): number {
  return Math.round(Math.max(360, Math.min(560, container * 0.42)));
}

/** clampWidth keeps at least MIN_LIST for the list and MIN_DETAIL for the
 * detail; a container narrower than both gives the list its minimum. */
export function clampWidth(px: number, container: number): number {
  const max = Math.max(MIN_LIST, container - MIN_DETAIL);
  return Math.round(Math.max(MIN_LIST, Math.min(max, px)));
}

function readStored(): number | null {
  try {
    const raw = window.localStorage.getItem(WIDTH_KEY);
    const n = raw === null ? NaN : Number(raw);
    return Number.isFinite(n) && n > 0 ? n : null;
  } catch {
    // Storage can be blocked (privacy settings); the default applies.
    return null;
  }
}

function writeStored(px: number | null): void {
  try {
    if (px === null) window.localStorage.removeItem(WIDTH_KEY);
    else window.localStorage.setItem(WIDTH_KEY, String(px));
  } catch {
    // Not persisting is fine; the width still applies to this page.
  }
}

export interface SplitPaneProps {
  list: ReactNode;
  detail: ReactNode;
  /** Used when the container reports no width (jsdom). */
  fallbackWidth?: number;
  /** The handle's accessible name (the schema section names its own list). */
  label?: string;
  /** A container narrower than this stacks the list above the detail, with
   * no handle (the schema section's narrow layout, T018f 2.4); 0: never. */
  stackBelow?: number;
}

/** SplitPane lays out list and detail side by side with a drag handle. */
export function SplitPane({ list, detail, fallbackWidth = 1100, label = "Resize the endpoint list", stackBelow = 0 }: SplitPaneProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [container, setContainer] = useState(0);
  const [chosen, setChosen] = useState<number | null>(readStored);
  const [dragging, setDragging] = useState(false);
  const drag = useRef<{ x: number; w: number } | null>(null);
  const size = container > 0 ? container : fallbackWidth;
  const width = clampWidth(chosen ?? defaultWidth(size), size);

  useLayoutEffect(() => {
    const el = ref.current;
    if (el === null) return;
    setContainer(el.clientWidth);
    const observer = new ResizeObserver(() => {
      setContainer(el.clientWidth);
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
    };
  }, []);

  const set = (px: number) => {
    const w = clampWidth(px, size);
    setChosen(w);
    writeStored(w);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "ArrowLeft") set(width - KEY_STEP);
    else if (event.key === "ArrowRight") set(width + KEY_STEP);
    else if (event.key === "Home") set(MIN_LIST);
    else if (event.key === "End") set(size - MIN_DETAIL);
    else return;
    event.preventDefault();
  };

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    drag.current = { x: event.clientX, w: width };
    setDragging(true);
    event.currentTarget.setPointerCapture(event.pointerId);
  };
  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (d === null) return;
    setChosen(clampWidth(d.w + event.clientX - d.x, size));
  };
  const onPointerUp = () => {
    if (drag.current === null) return;
    drag.current = null;
    setDragging(false);
    writeStored(width);
  };

  if (container > 0 && container < stackBelow)
    return (
      <div ref={ref} className="flex min-h-0 min-w-0 flex-1 flex-col gap-3" data-stacked="true">
        <div className="flex h-[360px] min-h-0 flex-none flex-col">{list}</div>
        <div className="flex min-h-[420px] min-w-0 flex-1 flex-col">{detail}</div>
      </div>
    );

  return (
    <div ref={ref} className="flex min-h-0 min-w-0 flex-1">
      <div className="flex min-h-0 flex-none flex-col" style={{ width }}>
        {list}
      </div>
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label={label}
        aria-valuenow={width}
        aria-valuemin={MIN_LIST}
        aria-valuemax={Math.max(MIN_LIST, size - MIN_DETAIL)}
        tabIndex={0}
        title="Drag or use Left/Right to resize; double-click to reset"
        data-dragging={dragging ? "true" : undefined}
        className={cn(
          "group relative grid w-3.5 flex-none cursor-col-resize touch-none place-items-center rounded-md outline-none",
          "focus-visible:ring-2 focus-visible:ring-ring",
        )}
        onKeyDown={onKeyDown}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onDoubleClick={() => {
          setChosen(null);
          writeStored(null);
        }}
      >
        <span
          aria-hidden="true"
          className="absolute inset-y-2 left-1/2 w-px -translate-x-1/2 bg-border group-hover:bg-brand group-focus-visible:bg-brand group-data-[dragging=true]:bg-brand"
        />
        <span
          aria-hidden="true"
          className="relative z-[1] grid h-7 w-2 place-items-center rounded-[3px] border border-muted-foreground/45 bg-card group-hover:border-brand group-focus-visible:border-brand group-data-[dragging=true]:border-brand"
        >
          <span className="h-3 w-0 border-l-2 border-dotted border-muted-foreground" />
        </span>
      </div>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">{detail}</div>
    </div>
  );
}
