// The Architecture section's state in the hash (#/architecture?...), so a
// reload or a shared link shows the same view (ADR 0041 routing):
//   tab=links            the Links tab (default: the map)
//   show=libraries,tests,docs,configured,nosdk   the Show toggles that
//                        differ from the defaults
//   exp=<component id>   the component expanded in place (AG11, AG14)
//   node=<map node id>   the selected card
//   edge=<map edge id>   the selected line
//   item=<identity>      the selected row of the expanded component
//   chain=<identity or component directory>   the chain drawn in place
//                        (AG-C2, AG16); with the Links tab, its filter
//   dir=up|down          the chain's direction (default both)
//   depth=1..10          the chain's depth when this link names one
//                        (T042; absent: this browser's choice, the
//                        repository's ui.chain_depth, else 6)
//   at=<identity>        the chain member selected (synced with the
//                        inspector's step list and code peek)

import type { ChainDirection } from "@/api/architecture";
import { parseDepth } from "@/lib/depthChoice";
import { parseHashQuery } from "@/router";
import { DEFAULT_SHOW, type ShowOptions } from "@/sections/architecture/model";

export type ArchTab = "map" | "links";

export interface ArchParams {
  tab: ArchTab;
  show: ShowOptions;
  expanded: string | null;
  node: string | null;
  edge: string | null;
  item: string | null;
  chain: string | null;
  dir: ChainDirection;
  /** The chain depth this link names, or null (T042). */
  depth: number | null;
  at: string | null;
}

export const ARCH_HASH = "#/architecture";

/** parseArchParams reads the section state from a hash; unknown values
 * fall back to the defaults. */
export function parseArchParams(hash: string): ArchParams {
  const q = parseHashQuery(hash);
  const show = new Set((q.get("show") ?? "").split(",").filter((s) => s !== ""));
  const opt = (key: string) => {
    const v = q.get(key);
    return v === null || v === "" ? null : v;
  };
  return {
    tab: q.get("tab") === "links" ? "links" : "map",
    show: {
      libraries: show.has("libraries"),
      tests: show.has("tests"),
      docs: show.has("docs"),
      configured: show.has("configured"),
      sdk: !show.has("nosdk"),
    },
    expanded: opt("exp"),
    node: opt("node"),
    edge: opt("edge"),
    item: opt("item"),
    chain: opt("chain"),
    dir: q.get("dir") === "up" ? "up" : q.get("dir") === "down" ? "down" : "both",
    depth: parseDepth(q.get("depth")),
    at: opt("at"),
  };
}

/** archHash writes the state back; defaults are left out. */
export function archHash(p: ArchParams): string {
  const q = new URLSearchParams();
  if (p.tab !== "map") q.set("tab", p.tab);
  const show: string[] = [];
  for (const k of ["libraries", "tests", "docs", "configured"] as const) if (p.show[k] !== DEFAULT_SHOW[k]) show.push(k);
  if (!p.show.sdk) show.push("nosdk");
  if (show.length > 0) q.set("show", show.join(","));
  if (p.expanded !== null) q.set("exp", p.expanded);
  if (p.node !== null) q.set("node", p.node);
  if (p.edge !== null) q.set("edge", p.edge);
  if (p.item !== null) q.set("item", p.item);
  if (p.chain !== null) q.set("chain", p.chain);
  if (p.chain !== null && p.dir !== "both") q.set("dir", p.dir);
  if (p.chain !== null && p.depth !== null) q.set("depth", String(p.depth));
  if (p.chain !== null && p.at !== null) q.set("at", p.at);
  const s = q.toString();
  return s === "" ? ARCH_HASH : `${ARCH_HASH}?${s}`;
}
