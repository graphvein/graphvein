// The markers' words and lookup, 1:1 with markers.ts: every change kind a
// golden sends has its marker; knowledge changes never read as a change,
// an addition or a removal (Governing principle); edge items attach to
// their source node.
import { describe, expect, it } from "vitest";

import type { ChangeItem } from "@/api/changes";
import { edgeEnds, indexItems, itemsFor, markerTitle, markerWord, VIEW_GROUPS } from "@/sections/changes/markers";
import { CHANGE_GOLDENS, itemsOf, summaryOf, type ChangeCase } from "@/test/changesFixtures";

const CASES = Object.keys(CHANGE_GOLDENS) as ChangeCase[];
const all: ChangeItem[] = CASES.flatMap((c) => itemsOf(c, { base: "previous", tests: true, limit: 500 }).items);

function item(o: Partial<ChangeItem>): Pick<ChangeItem, "change" | "wording" | "labels" | "fields"> {
  return { change: "changed", wording: null, labels: [], fields: [], ...o };
}

describe("markerWord", () => {
  it("covers every change kind of the goldens with a known word", () => {
    for (const it of all) {
      const w = markerWord(it);
      expect(w.text, it.change).not.toBe("");
      // Known kinds are worded, never passed through raw.
      if (it.change.includes("_")) expect(w.text, it.change).not.toBe(it.change);
    }
  });

  it("knowledge changes keep their own words and the dashed tone", () => {
    for (const change of ["unknown_now", "known_now", "link_unknown_now"]) {
      const w = markerWord(item({ change }));
      expect(w.tone).toBe("knowledge");
      expect(w.text).not.toMatch(/^(changed|added|removed|no longer)/);
    }
    for (const it of all.filter((i) => i.change === "unknown_now" || i.change === "known_now")) expect(markerWord(it).tone).toBe("knowledge");
  });

  it("a field that became unknown is a knowledge marker, never 'changed'", () => {
    expect(markerWord(item({ fields: [{ name: "type", change: "unknown_now", before: "int", after: null }] }))).toEqual({ tone: "knowledge", text: "type unknown now" });
  });

  it("proven changes", () => {
    expect(markerWord(item({ change: "added" }))).toEqual({ tone: "added", text: "added" });
    expect(markerWord(item({ change: "link_gained" }))).toEqual({ tone: "added", text: "link gained" });
    expect(markerWord(item({ change: "removed" }))).toEqual({ tone: "removed", text: "no longer in the index" });
    expect(markerWord(item({ change: "removed", wording: "deleted" }))).toEqual({ tone: "removed", text: "deleted" });
    expect(markerWord(item({ change: "link_lost" }))).toEqual({ tone: "removed", text: "link lost" });
    expect(markerWord(item({ change: "body_changed" }))).toEqual({ tone: "changed", text: "body changed" });
    expect(markerWord(item({ change: "changed" }))).toEqual({ tone: "changed", text: "changed" });
    expect(markerWord(item({ labels: ["handler_changed"] }))).toEqual({ tone: "changed", text: "handler changed" });
    expect(markerWord(item({ labels: ["edited"] }))).toEqual({ tone: "edited", text: "edited (history rewritten)" });
    const fields = ["paths", "status", "nullable"].map((name) => ({ name, change: "changed", before: 1, after: 2 }));
    // R5-6: every changed field and every label, never cut to "+N".
    expect(markerWord(item({ fields }))).toEqual({ tone: "changed", text: "paths changed, status changed, nullable changed" });
    expect(markerWord(item({ labels: ["handler_changed"], fields: fields.slice(0, 1) }))).toEqual({ tone: "changed", text: "handler changed, paths changed" });
    expect(markerWord(item({ labels: ["handler_changed", "table_access_changed"] }))).toEqual({ tone: "changed", text: "handler changed, table access changed" });
  });

  it("an unknown change kind is shown raw", () => {
    expect(markerWord(item({ change: "teleported" }))).toEqual({ tone: "changed", text: "teleported" });
  });

  it("the title names the base and the action", () => {
    expect(markerTitle([item({ change: "added" })], "pin release-1")).toBe("added since pin release-1: open in Changes");
  });
});

describe("lookup", () => {
  it("node items by identity, edge items on their source", () => {
    const items = itemsOf("previous-rev1-rev2", { base: "previous", tests: true, limit: 500 }).items;
    const index = indexItems(items);
    const route = items.find((i) => i.kind === "route" && i.change === "changed");
    expect(route).toBeDefined();
    if (route === undefined) return;
    expect(itemsFor(index, route.identity)).toEqual([route]);
    const lost = items.filter((i) => i.change === "link_lost" && i.kind === "calls_route");
    expect(lost.length).toBeGreaterThan(0);
    for (const e of lost) {
      const ends = edgeEnds(e);
      expect(ends?.[0]).toBe("calls_route");
      const source = ends?.[1] ?? "";
      expect(itemsFor(index, source, true)).toContain(e);
      expect(itemsFor(index, source)).not.toContain(e);
    }
    // Every node and edge item is reachable exactly once.
    const reached = new Set<ChangeItem>();
    for (const list of [...index.nodes.values(), ...index.edgesFrom.values()]) for (const i of list) reached.add(i);
    expect(reached.size).toBe(items.filter((i) => i.record === "node" || i.record === "edge").length);
  });

  it("view groups are groups of the summary", () => {
    const groups = new Set(summaryOf("previous-rev1-rev2").summary.groups.map((g) => g.group));
    for (const list of Object.values(VIEW_GROUPS)) for (const g of list) expect(groups.has(g), g).toBe(true);
  });
});
