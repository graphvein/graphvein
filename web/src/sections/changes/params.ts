// Deep-link parameters of the Changes section: #/changes?group=&change=&q=
// &tests=1&sel=&tab=&depth=&pane=. The base is not part of the hash: the
// compare chip owns it (remembered per browser, T019 Revision 2).

import { parseDepth } from "@/lib/depthChoice";
import { parseHashQuery } from "@/router";

export type ItemTab = "change" | "impact" | "raw";

export interface ChangesParams {
  group: string | null;
  change: string | null;
  q: string;
  tests: boolean;
  sel: string | null;
  tab: ItemTab;
  /** The depth this link names, or null: then this browser's choice, the
   * repository default or 3 applies (T042, hooks/useDepth.ts). */
  depth: number | null;
  /** Below 600 px one pane at a time: the list or the item. */
  pane: "list" | "item";
}

export function parseChangesParams(hash: string): ChangesParams {
  const q = parseHashQuery(hash);
  const tab = q.get("tab");
  return {
    group: q.get("group"),
    change: q.get("change"),
    q: q.get("q") ?? "",
    tests: q.get("tests") === "1",
    sel: q.get("sel"),
    tab: tab === "impact" || tab === "raw" ? tab : "change",
    depth: parseDepth(q.get("depth")),
    pane: q.get("pane") === "item" ? "item" : "list",
  };
}

export function changesHash(p: ChangesParams): string {
  const q = new URLSearchParams();
  if (p.group !== null) q.set("group", p.group);
  if (p.change !== null) q.set("change", p.change);
  if (p.q !== "") q.set("q", p.q);
  if (p.tests) q.set("tests", "1");
  if (p.sel !== null) q.set("sel", p.sel);
  if (p.tab !== "change") q.set("tab", p.tab);
  if (p.depth !== null) q.set("depth", String(p.depth));
  if (p.pane === "item") q.set("pane", "item");
  const s = q.toString();
  return s === "" ? "#/changes" : `#/changes?${s}`;
}

/** groupHash is the sidebar link of a group: the group alone. */
export function groupHash(group: string): string {
  return changesHash({ group, change: null, q: "", tests: false, sel: null, tab: "change", depth: null, pane: "list" });
}
