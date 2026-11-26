// Shared coverage helpers for the cards and the summary strip.

import { CATEGORY_ORDER, CATEGORY_TONE, categoryCounts, type Category } from "@/api/categories";
import type { RootCounts, StatusCounts, TransportSummary, Transport } from "@/api/endpoints";
import type { GapCategory } from "@/sections/api/params";
import type { Tone } from "@/sections/api/parts";

/** barParts turns status counts into stacked-bar segments in category
 * order. */
export function barParts(transport: Transport, byStatus: StatusCounts): { tone: Tone; count: number }[] {
  const counts = categoryCounts(transport, byStatus);
  return CATEGORY_ORDER.map((c) => ({ tone: CATEGORY_TONE[c], count: counts.get(c)?.count ?? 0 }));
}

/** rootCategoryCounts lists the non-zero categories of one root, in D10
 * order. */
export function rootCategoryCounts(transport: Transport, root: RootCounts): { category: Category; count: number; raws: string[] }[] {
  const counts = categoryCounts(transport, root.by_status);
  const out: { category: Category; count: number; raws: string[] }[] = [];
  for (const c of CATEGORY_ORDER) {
    const e = counts.get(c);
    if (e !== undefined && e.count > 0) out.push({ category: c, count: e.count, raws: e.raws.map((r) => r.status) });
  }
  return out;
}

/** The props every coverage view needs to open a gap sheet or set the
 * root filter. */
export interface CoverageActions {
  openGap: (transport: Transport, category: GapCategory, root?: string | null) => void;
  setRoot: (root: string | null) => void;
  activeGap: { transport: Transport; category: GapCategory } | null;
  root: string | null;
}

/** isEmptyTransport: no endpoints and no calls (for example no Tauri app). */
export function isEmptyTransport(t: TransportSummary): boolean {
  return t.endpoint_count === 0 && t.calls.total === 0;
}

/** manifestTitle explains how a root was found. */
/** rootLabel is how a source root reads in the UI: the repository root
 * "." is "(repository root)"; any other root is its path. */
export function rootLabel(root: string): string {
  return root === "." ? "(repository root)" : root;
}

export function manifestTitle(root: RootCounts): string {
  return root.manifest !== null
    ? `Source root from ${root.manifest}`
    : root.root === "."
      ? "Repository root (no manifest)"
      : "No manifest above these files; grouped by their first path segment";
}

/** sortRootsByCalls orders roots by call count, largest first. The sort is
 * stable, so ties keep the server's order ("." first, then byte order):
 * the folded roots line (T016i D1) shows a prefix of this order, so what
 * it shows is always "the largest roots". */
export function sortRootsByCalls(roots: readonly RootCounts[]): RootCounts[] {
  return [...roots].sort((a, b) => b.total - a.total);
}

/** The input of foldRoots: rendered widths in CSS pixels (fractional
 * widths are fine; foldRoots rounds them). */
export interface FoldInput {
  /** The roots sorted by sortRootsByCalls. */
  roots: readonly RootCounts[];
  /** The rendered width of each root's chip, by root name. */
  widths: ReadonlyMap<string, number>;
  /** The width of the "+N more roots" chip for N hidden roots. */
  moreWidth: (n: number) => number;
  /** The list's CSS gap between chips. */
  gap: number;
  /** The width the line has for chips. */
  available: number;
  /** The active root filter (DD3 A: its chip is always shown). */
  active: string | null;
}

/** The outcome of foldRoots: every chip fits on one line (the list is
 * shown as before), or the roots a folded line shows, in display order,
 * and how many it leaves out. */
export type FoldResult = { kind: "fits" } | { kind: "folded"; shown: RootCounts[]; hidden: number };

/** foldRoots decides what a roots list shows on one line (T016i D1.2-3,
 * DD3 A). Widths are rounded up and the available width down, so a chip
 * that measures 80.2 px never squeezes into 80 px. Folded, the line holds
 * the longest prefix of the sorted roots that fits together with the
 * "+N more roots" chip and, if a root filter is active, that root's chip:
 * a larger root is never skipped to fit a smaller one, and the active
 * root's chip comes last (before the "+N" chip) when it is not in the
 * prefix. Without an active root and with no room even for one root, the
 * line is the "+N" chip alone. */
export function foldRoots(input: FoldInput): FoldResult {
  const { roots, gap, active } = input;
  const available = Math.floor(input.available);
  const w = (root: string) => Math.ceil(input.widths.get(root) ?? 0);
  const all = roots.reduce((sum, r) => sum + w(r.root), 0) + Math.max(0, roots.length - 1) * gap;
  if (all <= available) return { kind: "fits" };

  const pinned = active !== null && roots.some((r) => r.root === active) ? active : null;
  // The width of a line holding the first k roots, the pinned root (when
  // not among them) and the "+N" chip.
  const lineWidth = (k: number): { width: number; count: number } => {
    const prefix = roots.slice(0, k);
    let count = k;
    let width = prefix.reduce((sum, r) => sum + w(r.root), 0);
    if (pinned !== null && !prefix.some((r) => r.root === pinned)) {
      width += w(pinned);
      count++;
    }
    width += Math.ceil(input.moreWidth(roots.length - count));
    return { width: width + count * gap, count };
  };
  let k = 0;
  while (k < roots.length && lineWidth(k + 1).width <= available) k++;
  const shown = roots.slice(0, k);
  const pin = pinned === null ? undefined : roots.find((r) => r.root === pinned);
  if (pin !== undefined && !shown.includes(pin)) shown.push(pin);
  return { kind: "folded", shown, hidden: roots.length - shown.length };
}

/** moreRootsLabel is the folded line's chip text. */
export function moreRootsLabel(n: number): string {
  return `+${String(n)} more ${n === 1 ? "root" : "roots"}`;
}
