// The "changed since <base>" markers of the existing views (T019 Part b
// item 6, Revision 2): which items a view marks and how a marker reads.
// Pure, 1:1 with markers.test.ts. A marker is drawn only where the API
// proves a change of a fact the view shows: the item's identity is a row,
// card, table, column or migration of that view. Knowledge changes keep
// their own words ("unknown now", "known now"), never "changed", "added"
// or "removed" (Governing principle).

import type { ChangeItem } from "@/api/changes";
import { changeLabel, itemChangeLabel, itemLabel } from "@/sections/changes/words";

/** MarkerTone picks the marker's style: solid chips for proven changes,
 * a dashed outline for knowledge changes (the V3 mockup's styles). */
export type MarkerTone = "changed" | "added" | "removed" | "edited" | "knowledge";

export interface MarkerWord {
  tone: MarkerTone;
  text: string;
}

/** The change groups each view's markers read (the summary's group ids). */
export const VIEW_GROUPS = {
  /** API section endpoint rows: routes and Tauri commands. */
  api: ["routes", "ipc"],
  /** Flow graph cards: endpoints, calls, their links and code symbols. */
  flow: ["routes", "client_calls", "ipc", "code"],
  /** Schema tables and columns. */
  tables: ["migration_tables", "model_tables"],
  /** Schema migration rows. */
  migrations: ["migrations"],
} as const satisfies Record<string, readonly string[]>;

/** changedText words a changed item: every label, then every changed
 * field (T019 Revision 5, R5-6: a route whose handler and paths changed
 * names both). The chip may cut the text; its tooltip has all of it. A
 * field that became or stopped being unknown makes the marker a
 * knowledge change. */
function changedText(item: Pick<ChangeItem, "fields" | "labels">): MarkerWord {
  const parts = [...item.labels.map(itemLabel), ...item.fields.map((f) => `${f.name} ${changeLabel(f.change)}`)];
  const knowledge = item.fields.some((f) => f.change !== "changed");
  return { tone: knowledge ? "knowledge" : "changed", text: parts.length === 0 ? "changed" : parts.join(", ") };
}

/** markerWord is the marker of one item. */
export function markerWord(item: Pick<ChangeItem, "change" | "wording" | "labels" | "fields">): MarkerWord {
  if (item.labels.includes("edited")) return { tone: "edited", text: itemLabel("edited") };
  switch (item.change) {
    case "added":
    case "link_gained":
      return { tone: "added", text: changeLabel(item.change) };
    case "removed":
    case "link_lost":
      return { tone: "removed", text: itemChangeLabel(item) };
    case "unknown_now":
    case "known_now":
    case "link_unknown_now":
      return { tone: "knowledge", text: changeLabel(item.change) };
    case "changed":
      return changedText(item);
    case "body_changed":
      return { tone: "changed", text: changeLabel(item.change) };
    default:
      // A change kind this UI does not know is shown raw.
      return { tone: "changed", text: changeLabel(item.change) };
  }
}

/** edgeEnds reads an edge item's [kind, source, target]; null for others. */
export function edgeEnds(item: Pick<ChangeItem, "record" | "identity">): [string, string, string] | null {
  if (item.record !== "edge") return null;
  try {
    const v: unknown = JSON.parse(item.identity);
    if (Array.isArray(v) && v.length === 3 && v.every((x) => typeof x === "string")) return [v[0] as string, v[1] as string, v[2] as string];
  } catch {
    // not an edge key
  }
  return null;
}

/** MarkerIndex finds the items a view marks. */
export interface MarkerIndex {
  /** Node items by identity. */
  nodes: Map<string, ChangeItem[]>;
  /** Edge items by their source node (the call side of a link). */
  edgesFrom: Map<string, ChangeItem[]>;
}

function push(m: Map<string, ChangeItem[]>, key: string, item: ChangeItem): void {
  const list = m.get(key);
  if (list === undefined) m.set(key, [item]);
  else list.push(item);
}

/** indexItems builds the lookup of loaded items. */
export function indexItems(items: readonly ChangeItem[]): MarkerIndex {
  const nodes = new Map<string, ChangeItem[]>();
  const edgesFrom = new Map<string, ChangeItem[]>();
  for (const it of items) {
    if (it.record === "node") push(nodes, it.identity, it);
    else {
      const e = edgeEnds(it);
      if (e !== null) push(edgesFrom, e[1], it);
    }
  }
  return { nodes, edgesFrom };
}

/** itemsFor lists what a view marks on one of its nodes: its node items,
 * and with edges also the edge items that leave it (a link gained, lost or
 * unknown now is shown on its call). */
export function itemsFor(index: MarkerIndex, id: string, edges = false): ChangeItem[] {
  const own = index.nodes.get(id) ?? [];
  if (!edges) return own;
  return [...own, ...(index.edgesFrom.get(id) ?? [])];
}

/** markerTitle is a marker's tooltip and accessible name. */
export function markerTitle(items: readonly Pick<ChangeItem, "change" | "wording" | "labels" | "fields">[], baseText: string): string {
  const words = items.map((i) => markerWord(i).text);
  return `${words.join("; ")} since ${baseText}: open in Changes`;
}
