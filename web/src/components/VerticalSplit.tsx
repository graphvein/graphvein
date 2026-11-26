// A top/bottom split with one horizontal resize handle (T016e2 Q1 B: the
// code peek below the detail pane). Hand-written like the list/detail
// SplitPane: pixel heights, the bottom height kept per browser in
// localStorage, a focusable separator that moves with Up/Down by 16 px,
// Home/End to the limits, and double-click to reset.

import { useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";

import { cn } from "@/lib/utils";

export const MIN_TOP = 120;
export const MIN_BOTTOM = 160;
export const SPLIT_STEP = 16;

/** DEFAULT_FRACTION is the bottom pane's default share, as in the T016e
 * mockup. */
export const DEFAULT_FRACTION = 0.54;

/** defaultBottom is the default bottom height: a share of the container
 * (54 % unless a caller whose top pane needs more room asks for less). */
export function defaultBottom(container: number, fraction = DEFAULT_FRACTION): number {
  return Math.round(container * fraction);
}

/** clampBottom keeps minTop above and minBottom below the handle; a
 * container smaller than both gives the bottom its minimum. */
export function clampBottom(px: number, container: number, minTop = MIN_TOP, minBottom = MIN_BOTTOM): number {
  const max = Math.max(minBottom, container - minTop);
  return Math.round(Math.max(minBottom, Math.min(max, px)));
}

function readStored(key: string): number | null {
  try {
    const raw = window.localStorage.getItem(key);
    const n = raw === null ? NaN : Number(raw);
    return Number.isFinite(n) && n > 0 ? n : null;
  } catch {
    // Storage can be blocked (privacy settings); the default applies.
    return null;
  }
}

function writeStored(key: string, px: number | null): void {
  try {
    if (px === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, String(px));
  } catch {
    // Not persisting is fine; the height still applies to this page.
  }
}

export interface VerticalSplitProps {
  top: ReactNode;
  /** The bottom pane; null shows the top pane alone (no handle). */
  bottom: ReactNode;
  /** The localStorage key of the bottom height. */
  storageKey: string;
  /** The separator's accessible name. */
  label: string;
  /** Used when the container reports no height (jsdom). */
  fallbackHeight?: number;
  /** The bottom pane's default share of the container (DEFAULT_FRACTION). */
  defaultFraction?: number;
  /** The least height left to the top pane (MIN_TOP). */
  minTop?: number;
  /** The least height of the bottom pane (MIN_BOTTOM). A caller whose top
   * pane must keep a header visible may let the bottom give way further. */
  minBottom?: number;
  className?: string;
}

/** VerticalSplit stacks top and bottom with a drag handle between them. */
export function VerticalSplit({
  top,
  bottom,
  storageKey,
  label,
  fallbackHeight = 700,
  defaultFraction = DEFAULT_FRACTION,
  minTop = MIN_TOP,
  minBottom = MIN_BOTTOM,
  className,
}: VerticalSplitProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [container, setContainer] = useState(0);
  // The chosen height belongs to its storage key: a caller that switches
  // keys (the Flow tab keeps its own height) gets that key's height.
  const [chosenFor, setChosenFor] = useState<{ key: string; px: number | null }>(() => ({ key: storageKey, px: readStored(storageKey) }));
  if (chosenFor.key !== storageKey) setChosenFor({ key: storageKey, px: readStored(storageKey) });
  const chosen = chosenFor.key === storageKey ? chosenFor.px : readStored(storageKey);
  const setChosen = (px: number | null) => {
    setChosenFor({ key: storageKey, px });
  };
  const [dragging, setDragging] = useState(false);
  const drag = useRef<{ y: number; h: number } | null>(null);
  const size = container > 0 ? container : fallbackHeight;
  const height = clampBottom(chosen ?? defaultBottom(size, defaultFraction), size, minTop, minBottom);

  useLayoutEffect(() => {
    const el = ref.current;
    if (el === null) return;
    setContainer(el.clientHeight);
    const observer = new ResizeObserver(() => {
      setContainer(el.clientHeight);
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
    };
  }, []);

  const set = (px: number) => {
    const h = clampBottom(px, size, minTop, minBottom);
    setChosen(h);
    writeStored(storageKey, h);
  };

  // Up makes the bottom pane taller (the handle moves up).
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "ArrowUp") set(height + SPLIT_STEP);
    else if (event.key === "ArrowDown") set(height - SPLIT_STEP);
    else if (event.key === "Home") set(size - minTop);
    else if (event.key === "End") set(minBottom);
    else return;
    event.preventDefault();
  };

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    drag.current = { y: event.clientY, h: height };
    setDragging(true);
    event.currentTarget.setPointerCapture(event.pointerId);
  };
  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (d === null) return;
    setChosen(clampBottom(d.h - (event.clientY - d.y), size, minTop, minBottom));
  };
  const onPointerUp = () => {
    if (drag.current === null) return;
    drag.current = null;
    setDragging(false);
    writeStored(storageKey, height);
  };

  return (
    <div ref={ref} className={cn("flex min-h-0 flex-1 flex-col", className)}>
      <div className="flex min-h-0 flex-1 flex-col">{top}</div>
      {bottom !== null && (
        <>
          <div
            role="separator"
            aria-orientation="horizontal"
            aria-label={label}
            aria-valuenow={height}
            aria-valuemin={minBottom}
            aria-valuemax={Math.max(minBottom, size - minTop)}
            tabIndex={0}
            title="Drag or use Up/Down to resize; double-click to reset"
            data-dragging={dragging ? "true" : undefined}
            className="group relative grid h-3 flex-none cursor-row-resize touch-none place-items-center outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onKeyDown={onKeyDown}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={onPointerUp}
            onDoubleClick={() => {
              setChosen(null);
              writeStored(storageKey, null);
            }}
          >
            <span
              aria-hidden="true"
              className="absolute inset-x-0 top-1/2 h-px -translate-y-1/2 bg-border group-hover:bg-brand group-focus-visible:bg-brand group-data-[dragging=true]:bg-brand"
            />
            <span
              aria-hidden="true"
              className="relative z-[1] grid h-2 w-7 place-items-center rounded-[3px] border border-muted-foreground/45 bg-card group-hover:border-brand group-focus-visible:border-brand group-data-[dragging=true]:border-brand"
            >
              <span className="h-0 w-3 border-t-2 border-dotted border-muted-foreground" />
            </span>
          </div>
          <div className="flex min-h-0 flex-none flex-col" style={{ height }} data-testid="split-bottom">
            {bottom}
          </div>
        </>
      )}
    </div>
  );
}
