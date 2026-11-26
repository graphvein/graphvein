// The API section's deep-link state (T016d D8, T016f D6):
//   #/api?transport=&tests=1&root=<root>&sel=<id>&gap=<transport>:<category>
//        &tab=overview|callers|flow&hops=1&nm=0&tiers=<tier,...>&node=<id>&edge=<id>
//        &peek=<identity>|0&back=<architecture hash>
// hops, nm, tiers, node and edge belong to the Flow tab (defaults: 2 hops,
// near misses on, every tier shown, nothing selected). peek (T016i D4) is
// the open code peek: on the Overview and Callers tabs the peeked node's
// identity; on the Flow tab only "0", a selection whose peek was closed
// (a selection's peek is open by default). Default: absent.
// Unknown parameters and values are ignored; the section writes the hash
// back with replaceHash, so reloading or sharing the URL restores the view.

import { CATEGORY_ORDER, type Category } from "@/api/categories";
import { isTransport as isKnownTransport, type Transport } from "@/api/endpoints";
import { parseHashQuery } from "@/router";
import { FILTER_TIERS, type FilterTier } from "@/sections/api/flow/tiers";

/** A gap sheet: the calls of one non-linked category, or the endpoints
 * never called. */
export type GapCategory = Exclude<Category, "linked"> | "never";

export interface Gap {
  transport: Transport;
  category: GapCategory;
}

export type TransportFilter = "all" | Transport;

/** The tabs of the detail pane (UQ3 B). Callers is the default: selecting
 * an endpoint answers "who calls this" first, as before the tabs. */
export type DetailTab = "overview" | "callers" | "flow";

export const DETAIL_TABS: readonly DetailTab[] = ["overview", "callers", "flow"];

/** DEFAULT_TAB is the tab an endpoint opens on, and the one the hash
 * leaves out. The default is set here only (whether it stays Callers is
 * open with the developer). */
export const DEFAULT_TAB: DetailTab = "callers";

export interface ApiParams {
  transport: TransportFilter;
  /** "Include tests": scope all instead of production. */
  tests: boolean;
  root: string | null;
  sel: string | null;
  gap: Gap | null;
  tab: DetailTab;
  /** Flow tab: 1 or 2 hops each way (UQ8 A: 2). */
  hops: 1 | 2;
  /** Flow tab: near misses drawn (UQ8 A: on). */
  nm: boolean;
  /** Flow tab: the tiers the filter shows undimmed, in FILTER_TIERS order. */
  tiers: FilterTier[];
  /** Flow tab: the selected card (its drawn id) or line, if any. */
  node: string | null;
  edge: string | null;
  /** The code peek: a node identity (Overview, Callers) or "0" (Flow tab,
   * closed); see FLOW_PEEK_CLOSED. */
  peek: string | null;
  /** T026 AG-C3: the Architecture chain this Flow tab was opened from
   * ("Open in Flow tab"), a hash starting with #/architecture; the Flow tab
   * then starts at the downstream side with the Tables column in view and
   * links back. Cleared when the endpoint or the tab changes. */
  back: string | null;
}

/** isArchitectureHash accepts only the Architecture section's own hashes
 * as a back link (never another place or an absolute URL). */
export function isArchitectureHash(v: string | null): v is string {
  return v !== null && (v === "#/architecture" || v.startsWith("#/architecture?"));
}

/** FLOW_PEEK_CLOSED is the peek value of a Flow selection whose peek was
 * closed. Any other value on the Flow tab is ignored (open). */
export const FLOW_PEEK_CLOSED = "0";

export const DEFAULT_PARAMS: ApiParams = {
  transport: "all",
  tests: false,
  root: null,
  sel: null,
  gap: null,
  tab: DEFAULT_TAB,
  hops: 2,
  nm: true,
  tiers: [...FILTER_TIERS],
  node: null,
  edge: null,
  peek: null,
  back: null,
};

function isTransport(v: string): v is Transport {
  return isKnownTransport(v);
}

function isGapCategory(v: string): v is GapCategory {
  return v === "never" || (v !== "linked" && (CATEGORY_ORDER as readonly string[]).includes(v));
}

/** parseGap reads "<transport>:<category>"; null for anything else. */
export function parseGap(value: string | null): Gap | null {
  if (value === null) return null;
  const i = value.indexOf(":");
  if (i === -1) return null;
  const t = value.slice(0, i);
  const c = value.slice(i + 1);
  return isTransport(t) && isGapCategory(c) ? { transport: t, category: c } : null;
}

function isTab(v: string | null): v is DetailTab {
  return v !== null && (DETAIL_TABS as readonly string[]).includes(v);
}

/** parseTiers reads "fact,exact"; unknown names are ignored. Three cases
 * differ on purpose:
 *   - no `tiers` parameter: every tier (the default, left out of the hash);
 *   - `tiers=` (empty): no tier, every tiered line dimmed (apiHash writes
 *     this when every chip is off, so that view round-trips);
 *   - a value naming no known tier (`tiers=bogus`, or a tier name a later
 *     version dropped): every tier, as if absent, so a stale link never
 *     opens with every line dimmed. */
export function parseTiers(value: string | null): FilterTier[] {
  if (value === null) return [...FILTER_TIERS];
  const names = new Set(value.split(","));
  if (value === "") return [];
  const out = FILTER_TIERS.filter((t) => names.has(t));
  return out.length === 0 ? [...FILTER_TIERS] : out;
}

/** parseApiParams reads the section state from a location hash. */
export function parseApiParams(hash: string): ApiParams {
  const q = parseHashQuery(hash);
  const t = q.get("transport");
  const tab = q.get("tab");
  return {
    transport: t !== null && isTransport(t) ? t : "all",
    tests: q.get("tests") === "1",
    root: q.get("root"),
    sel: q.get("sel") || null,
    gap: parseGap(q.get("gap")),
    tab: isTab(tab) ? tab : DEFAULT_TAB,
    hops: q.get("hops") === "1" ? 1 : 2,
    nm: q.get("nm") !== "0",
    tiers: parseTiers(q.get("tiers")),
    node: q.get("node") || null,
    edge: q.get("edge") || null,
    peek: q.get("peek") || null,
    back: isArchitectureHash(q.get("back")) ? q.get("back") : null,
  };
}

/** isGraphView says whether a hash opens a graph view (the Flow tab), in
 * which the shell's sidebar collapses to its icon rail. */
export function isGraphView(hash: string): boolean {
  const p = parseApiParams(hash);
  return p.tab === "flow" && p.sel !== null;
}

/** apiHash writes the section state as a hash; defaults are left out. */
export function apiHash(p: ApiParams): string {
  const q = new URLSearchParams();
  if (p.transport !== "all") q.set("transport", p.transport);
  if (p.tests) q.set("tests", "1");
  if (p.root !== null) q.set("root", p.root);
  if (p.sel !== null) q.set("sel", p.sel);
  if (p.gap !== null) q.set("gap", `${p.gap.transport}:${p.gap.category}`);
  if (p.tab !== DEFAULT_TAB) q.set("tab", p.tab);
  if (p.hops !== 2) q.set("hops", String(p.hops));
  if (!p.nm) q.set("nm", "0");
  const tiers = FILTER_TIERS.filter((t) => p.tiers.includes(t));
  if (tiers.length !== FILTER_TIERS.length) q.set("tiers", tiers.join(","));
  if (p.node !== null) q.set("node", p.node);
  if (p.edge !== null) q.set("edge", p.edge);
  if (p.peek !== null) q.set("peek", p.peek);
  if (p.back !== null) q.set("back", p.back);
  const s = q.toString();
  return s === "" ? "#/api" : `#/api?${s}`;
}
