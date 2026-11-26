// The roots fold (T016i D1, developer decision 2026-11-04 option A with
// DD1-DD3 A): a list of source roots that would wrap folds to one line of
// the largest roots by call count, the active root's chip and a
// "+N more roots" chip; that chip unfolds the full list (in the server's
// order, as before) and becomes "Show fewer". One remembered choice covers
// every roots list (both transports, cards and strip). A list whose chips
// fit on one line is shown exactly as before.
//
// "Would wrap" is measured, not estimated: a hidden measuring row holds
// every root chip with the real markup, and its rendered widths are
// compared with the width of the line, which comes from the card or strip
// (never from the chips shown), so folding cannot change what it is
// measured against.

import {
  createContext,
  useContext,
  useEffectEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
  type RefCallback,
  type RefObject,
} from "react";

import { CATEGORY_LABEL, CATEGORY_TONE, TRANSPORT_LABEL, linkedCount, linkedPercent } from "@/api/categories";
import type { RootCounts, TransportSummary } from "@/api/endpoints";
import { cn } from "@/lib/utils";
import {
  foldRoots,
  manifestTitle,
  rootLabel,
  moreRootsLabel,
  rootCategoryCounts,
  sortRootsByCalls,
  type CoverageActions,
  type FoldResult,
} from "@/sections/api/coverage";
import { BAR_CLASS } from "@/sections/api/parts";

/** RootChip is one root of the compact strip's Roots row: the name button
 * (filters by the root, like the Source root select), the call count and
 * linked percentage, and a button per non-linked category. With `measure`
 * it is the measuring row's copy: no data-root (tests and gates count
 * [data-root]), no tab stop and no action. */
export function RootChip({ t, r, actions, measure = false }: { t: TransportSummary; r: RootCounts; actions: CoverageActions; measure?: boolean }) {
  const on = actions.root === r.root;
  const pct = linkedPercent(linkedCount(r.by_status), r.total);
  const tab = measure ? { tabIndex: -1 } : {};
  return (
    <span
      data-root={measure ? undefined : r.root}
      data-measure-root={measure ? r.root : undefined}
      className={cn("inline-flex h-[24px] items-center gap-1.5 rounded-md border pr-1 text-xs", on && "border-brand bg-brand-soft")}
    >
      <button
        type="button"
        aria-pressed={on}
        title={`${manifestTitle(r)}. ${on ? "Show all roots" : "Filter by this root"}`}
        className="h-full rounded-l-md px-1.5 font-mono hover:bg-accent"
        {...tab}
        onClick={() => {
          if (!measure) actions.setRoot(on ? null : r.root);
        }}
      >
        {rootLabel(r.root)}
      </button>
      <span className="tabular-nums text-muted-foreground">
        {r.total} · <b className="font-semibold text-foreground">{pct === null ? "-" : `${String(pct)}%`}</b>
      </span>
      {rootCategoryCounts(t.transport, r)
        .filter((c) => c.category !== "linked")
        .map((c) => (
          <button
            key={c.category}
            type="button"
            title={`${CATEGORY_LABEL[c.category]} in ${r.root}: show these calls`}
            className="inline-flex items-center gap-1 rounded px-1 hover:bg-accent"
            {...tab}
            onClick={() => {
              if (!measure && c.category !== "linked") actions.openGap(t.transport, c.category, r.root);
            }}
          >
            <i aria-hidden="true" className={cn("size-[7px] rounded-[2px]", BAR_CLASS[CATEGORY_TONE[c.category]])} />
            <b className="font-semibold tabular-nums">{c.count}</b>
            <span className="sr-only">{CATEGORY_LABEL[c.category]}</span>
          </button>
        ))}
    </span>
  );
}

/** The class of the "+N more roots" / "Show fewer" chip: the strip's root
 * chip look, so it reads as part of the line. */
const TOGGLE = "inline-flex h-[24px] items-center rounded-md border px-1.5 text-xs whitespace-nowrap text-muted-foreground hover:bg-accent";

// ---------------------------------------------------------------------------
// The remembered choice (D1.6, DD2 A): one key for every roots list.

/** ROOTS_EXPANDED_KEY is the localStorage key of the roots choice: exactly
 * "1" means unfolded; absent or any other value means folded (the
 * default). If the meaning ever changes, a new key is used instead of
 * reinterpreting old values. */
export const ROOTS_EXPANDED_KEY = "graphvein.api.rootsExpanded";

// When storage is blocked or a write throws (privacy settings, quota), the
// choice lives here for the page; a later successful write clears it.
let memoryChoice: boolean | undefined;
const listeners = new Set<() => void>();

function readChoice(): boolean {
  if (memoryChoice !== undefined) return memoryChoice;
  try {
    return window.localStorage.getItem(ROOTS_EXPANDED_KEY) === "1";
  } catch {
    // Storage can be blocked; folded is the default.
    return false;
  }
}

function writeChoice(expanded: boolean): void {
  try {
    if (expanded) window.localStorage.setItem(ROOTS_EXPANDED_KEY, "1");
    else window.localStorage.removeItem(ROOTS_EXPANDED_KEY);
    memoryChoice = undefined;
  } catch {
    // Not persisting is fine: the choice still applies to this page.
    memoryChoice = expanded;
  }
  for (const l of listeners) l();
}

function subscribeChoice(onChange: () => void): () => void {
  listeners.add(onChange);
  return () => {
    listeners.delete(onChange);
  };
}

/** resetRootsChoice drops the in-memory choice kept when storage failed,
 * so the next read comes from storage again. Tests use it between cases;
 * the page never needs it. */
export function resetRootsChoice(): void {
  memoryChoice = undefined;
  for (const l of listeners) l();
}

/** useRootsExpanded is the shared roots choice: every list on the page
 * reads and writes the same value, so "+N more roots" on one transport
 * unfolds both. Another tab's change applies on its next load (no
 * cross-tab sync). */
export function useRootsExpanded(): [boolean, (expanded: boolean) => void] {
  const expanded = useSyncExternalStore(subscribeChoice, readChoice, () => false);
  return [expanded, writeChoice];
}

// ---------------------------------------------------------------------------
// Measuring.

/** What a roots list measured: the room its line has and the rendered
 * width of each chip. */
export interface RootsMeasurement {
  available: number;
  widths: Map<string, number>;
  /** The "+N more roots" chip at its widest text (N = every root). */
  more: number;
  gap: number;
}

/** A measurer reads the line, the measuring row and the line's own label
 * (the strip's "Roots"; null for the cards); null when nothing can be
 * measured (a hidden surface, width 0), which shows the list as before. */
export type RootsMeasurer = (line: HTMLElement, row: HTMLElement, label: HTMLElement | null) => RootsMeasurement | null;

function px(v: string): number {
  const n = Number.parseFloat(v);
  return Number.isFinite(n) ? n : 0;
}

/** measureRoots is the browser measurer: rendered widths from
 * getBoundingClientRect, the available width from the line's content box
 * minus its label, the gap from the measuring row's computed style (the
 * same classes as the line). */
export const measureRoots: RootsMeasurer = (line, row, label) => {
  const box = line.getBoundingClientRect();
  if (box.width <= 0) return null;
  const style = getComputedStyle(line);
  const gap = px(getComputedStyle(row).columnGap);
  let available = box.width - px(style.paddingLeft) - px(style.paddingRight) - px(style.borderLeftWidth) - px(style.borderRightWidth);
  if (label !== null) available -= label.getBoundingClientRect().width + gap;
  const widths = new Map<string, number>();
  let more = 0;
  for (const el of row.children) {
    const w = el.getBoundingClientRect().width;
    const root = el.getAttribute("data-measure-root");
    if (root !== null) widths.set(root, w);
    else if (el.hasAttribute("data-measure-more")) more = w;
  }
  if ([...widths.values()].some((w) => w <= 0)) return null;
  return { available, widths, more, gap };
};

/** RootsMeasureContext supplies the measurer; tests replace it (jsdom has
 * no layout). */
export const RootsMeasureContext = createContext<RootsMeasurer>(measureRoots);

function same(a: RootsMeasurement | null, b: RootsMeasurement | null): boolean {
  if (a === null || b === null) return a === b;
  if (a.available !== b.available || a.more !== b.more || a.gap !== b.gap || a.widths.size !== b.widths.size) return false;
  for (const [k, v] of a.widths) if (b.widths.get(k) !== v) return false;
  return true;
}

/** The state of one roots list. */
export type RootsView =
  | { kind: "fits" }
  | { kind: "folded"; shown: RootCounts[]; hidden: number }
  | { kind: "expanded" };

export interface RootsFold {
  view: RootsView;
  /** A callback ref, so the resize observer follows the line when it
   * mounts late (a card whose roots start empty renders no line). */
  lineRef: RefCallback<HTMLDivElement>;
  labelRef: RefObject<HTMLSpanElement | null>;
  /** The hidden measuring row, rendered inside the line. */
  measuring: ReactNode;
  expanded: boolean;
  setExpanded: (expanded: boolean) => void;
}

/** useRootsFold measures one roots list and decides what it shows. It
 * re-measures in a layout effect when the roots change (so the first
 * paint is already folded), when the line or the measuring row resizes
 * (window resize, zoom, a system font change) and when fonts finish
 * loading. The line and the measuring row are held in state through
 * callback refs: a caller may render neither at first (no roots yet) and
 * mount them later, and the observer must attach to them then. */
export function useRootsFold(t: TransportSummary, actions: CoverageActions): RootsFold {
  const measure = useContext(RootsMeasureContext);
  const [line, lineRef] = useState<HTMLDivElement | null>(null);
  const labelRef = useRef<HTMLSpanElement>(null);
  const [row, rowRef] = useState<HTMLDivElement | null>(null);
  const [measured, setMeasured] = useState<RootsMeasurement | null>(null);
  const [expanded, setExpanded] = useRootsExpanded();
  const sorted = useMemo(() => sortRootsByCalls(t.roots), [t.roots]);

  const run = useEffectEvent(() => {
    if (line === null || row === null) return;
    const next = measure(line, row, labelRef.current);
    setMeasured((prev) => (same(prev, next) ? prev : next));
  });
  useLayoutEffect(() => {
    run();
  }, [t.roots, actions.root, measure]);
  useLayoutEffect(() => {
    if (line === null || row === null) return;
    run();
    const observer = new ResizeObserver(() => {
      run();
    });
    observer.observe(line);
    observer.observe(row);
    const fonts = typeof document !== "undefined" ? document.fonts : undefined;
    const onFonts = () => {
      run();
    };
    fonts?.addEventListener("loadingdone", onFonts);
    return () => {
      observer.disconnect();
      fonts?.removeEventListener("loadingdone", onFonts);
    };
  }, [line, row]);

  let result: FoldResult = { kind: "fits" };
  if (measured !== null) {
    result = foldRoots({
      roots: sorted,
      widths: measured.widths,
      moreWidth: () => measured.more,
      gap: measured.gap,
      available: measured.available,
      active: actions.root,
    });
  }
  const view: RootsView = result.kind === "fits" ? result : expanded ? { kind: "expanded" } : result;

  // Hidden, inert and without height: it only lends its chips' widths. The
  // outer box clips it, so it never adds scroll overflow to the page.
  const measuring = (
    <div aria-hidden="true" inert className="pointer-events-none invisible absolute top-0 left-0 h-0 w-0 overflow-hidden" data-testid="roots-measure">
      <div ref={rowRef} className="flex w-max gap-1.5">
        {t.roots.map((r) => (
          <RootChip key={r.root} t={t} r={r} actions={actions} measure />
        ))}
        <span data-measure-more="" className={TOGGLE}>
          {moreRootsLabel(t.roots.length)}
        </span>
      </div>
    </div>
  );
  return { view, lineRef, labelRef, measuring, expanded, setExpanded };
}

/** groupLabel is the accessible name of a roots list. */
export function groupLabel(t: TransportSummary, view: RootsView): string {
  const base = `Source roots, ${TRANSPORT_LABEL[t.transport]}`;
  return view.kind === "folded" ? `${base}, ${String(view.shown.length)} of ${String(t.roots.length)} shown` : base;
}

/** RootsToggle is "+N more roots" (folded) or "Show fewer" (unfolded): one
 * button whose text changes, so the focus stays on it when it toggles. It
 * is reachable with Tab and acts on Enter and Space like any button; no
 * shortcut is added. */
export function RootsToggle({
  view,
  t,
  listId,
  setExpanded,
}: {
  view: RootsView;
  t: TransportSummary;
  listId: string;
  setExpanded: (expanded: boolean) => void;
}) {
  if (view.kind === "fits") return null;
  const folded = view.kind === "folded";
  return (
    <button
      type="button"
      aria-expanded={!folded}
      aria-controls={listId}
      title={folded ? `Show all ${String(t.roots.length)} roots` : "Show only the largest roots"}
      className={TOGGLE}
      data-testid="roots-toggle"
      onClick={() => {
        setExpanded(folded);
      }}
    >
      {folded ? moreRootsLabel(view.hidden) : "Show fewer"}
    </button>
  );
}
