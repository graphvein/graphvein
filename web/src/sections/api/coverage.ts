// Shared coverage helpers for the cards and the summary strip.

import { CATEGORY_ORDER, CATEGORY_TONE, TRANSPORT_LABEL, categoryCounts, type Category } from "@/api/categories";
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

/** noneFound: no endpoints and no calls in any source root, so the root
 * filter cannot be what hides them. Such a transport gets no strip and no
 * card; the toolbar shows a dashed "none found" tab (T061 D3). */
export function noneFound(t: TransportSummary): boolean {
  return isEmptyTransport(t) && t.roots.every((r) => r.total === 0);
}

/** LOOKS_FOR names what each transport's reader looks for, for the
 * "none found" tab's tooltip. */
const LOOKS_FOR: Record<Transport, string> = {
  http: "routes of the supported web frameworks and client calls such as fetch or axios",
  tauri_ipc: "#[tauri::command] functions and invoke calls",
  server_action: '"use server" functions and the places that use them',
};

/** WHAT names a transport's endpoints and calls in one phrase, without
 * repeating the transport's own name ("server actions or action uses",
 * never "Server actions actions", label defect 10). */
const WHAT: Record<Transport, string> = {
  http: "HTTP routes or client calls",
  tauri_ipc: "Tauri IPC commands or invoke sites",
  server_action: "server actions or action uses",
};

/** filesWhere is where "none found" holds: the indexed files, or in
 * production scope the indexed production files. */
export function filesWhere(scope: string): string {
  return scope === "production" ? "in the indexed production files" : "in the indexed files";
}

/** noneFoundSentence says once that none of the given transports has
 * endpoints or calls ("No HTTP routes or client calls, Tauri IPC commands
 * or invoke sites or server actions or action uses found in the indexed
 * files."). testsHint adds, in production scope, that the API page's
 * Include tests also counts test files; a page without that control (the
 * Overview) leaves it out. */
export function noneFoundSentence(transports: readonly Transport[], scope: string, testsHint = true): string {
  const parts = transports.map((t) => WHAT[t]);
  const list = parts.length <= 1 ? (parts[0] ?? "") : `${parts.slice(0, -1).join(", ")} or ${parts.at(-1) ?? ""}`;
  const tests = testsHint && scope === "production" ? "; Include tests also counts test files" : "";
  return `No ${list} found ${filesWhere(scope)}${tests}.`;
}

/** noneFoundTitle is the tooltip of a "none found" tab. The answer counts
 * what the indexed files hold, not whether a reader ran (T061 open
 * question 8), so it says "found in the indexed files"; in production
 * scope it also says that test files were not counted. */
export function noneFoundTitle(transport: Transport, scope: string): string {
  const where = scope === "production" ? `${filesWhere(scope)}; Include tests also counts test files` : filesWhere(scope);
  return `${TRANSPORT_LABEL[transport]}: none found ${where}. Graphvein looks for ${LOOKS_FOR[transport]}.`;
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
