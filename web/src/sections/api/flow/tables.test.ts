// T026 AG-C3: the Flow tab's Tables column, tested against the server's
// flow-tables golden (internal/server/testdata/flow-tables.golden.json).
import { describe, expect, it } from "vitest";

import { parseFlow } from "@/api/flow";
import { downstreamFocus } from "@/sections/api/flow/FlowTab";
import { buildGraph, COL, isDrawnSubset, tableAccess, type GraphOptions } from "@/sections/api/flow/graph";
import { cardSize } from "@/sections/api/flow/measure";
import { edgeTier, FILTER_TIERS, tierLabel } from "@/sections/api/flow/tiers";
import { flowGolden, FLOW_GOLDENS } from "@/test/flowFixtures";
import { parseApiParams, apiHash, DEFAULT_PARAMS } from "@/sections/api/params";

const ALL: GraphOptions = { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) };
const T = (name: string) => `table:api/models.py#${name}`;
const F = (file: string, name: string) => `function:${file}#${name}`;

describe("Flow Tables column", () => {
  const f = flowGolden("flow-tables");

  it("places tables in the Tables column and nested functions with their container's hop", () => {
    const g = buildGraph(f, ALL);
    const col = (id: string) => g.nodes.find((n) => n.id === id)?.column;
    expect(col(T("item"))).toBe(COL.tables);
    expect(col(T("audit"))).toBe(COL.tables);
    expect(col(F("api/items.py", "read_items.operation"))).toBe(COL.handler);
    expect(col(F("api/helpers.py", "_patch"))).toBe(COL.callees);
    expect(col(F("api/helpers.py", "_helper.inner"))).toBe(COL.callees2);
    expect(g.nodes.find((n) => n.id === F("api/items.py", "read_items.operation"))?.badges.map((b) => b.text)).toContain("nested");
    expect(g.tables).toEqual({ drawn: 3, written: 2, hiddenHop2: 0 });
  });

  it("gives each table its access, tier and the server's wording", () => {
    const g = buildGraph(f, ALL);
    const badge = (id: string) => g.nodes.find((n) => n.id === id)?.badges[0]?.text;
    expect(badge(T("item"))).toBe("Reads · 1.0, builds a query on · 0.9");
    expect(badge(T("audit"))).toBe("Reads · 1.0");
    expect(badge(T("log"))).toBe("Writes · 1.0");
    const tech = buildGraph(f, { ...ALL, labels: "technical" });
    const techBadge = (id: string) => tech.nodes.find((n) => n.id === id)?.badges[0]?.text;
    expect(techBadge(T("item"))).toBe("B 0.9 · R fact");
    expect(techBadge(T("audit"))).toBe("R fact");
    expect(techBadge(T("log"))).toBe("W fact");
    const labels = g.edges.filter((e) => e.tier === "access").map((e) => e.label);
    expect(labels.sort()).toEqual(["builds a query on · 0.9", "reads · 1.0", "reads · 1.0", "writes · 1.0"]);
    const nested = g.edges.find((e) => e.target === F("api/items.py", "read_items.operation"));
    expect(nested?.label).toBe("nested, not a call");
    expect(tierLabel({ kind: "writes_table", provenance: "inferred", confidence: 0.9, match: null }, "access", "technical")).toBe("writes_table inferred 0.9");
    expect(edgeTier({ kind: "reads_table", provenance: "fact", confidence: 1, match: null })).toBe("access");
    expect(tableAccess(T("item"), [])).toBeNull();
  });

  it("states unknowns and the hop-3 fold as boxes no line touches, the same in both hop settings", () => {
    const two = buildGraph(f, ALL);
    const one = buildGraph(f, { ...ALL, hops: 1 });
    const states = two.nodes.filter((n) => n.type === "state" && n.column === COL.tables);
    expect(states.map((n) => n.title)).toEqual(["may also use", "+ 2 at hop 3"]);
    const may = states[0];
    expect(may?.notes[0]).toBe("_patch:12 (hop 1): user, ghost · object_class_unproven");
    expect(may?.notes[1]).toBe("_patch:14 (hop 1): any sql table · sql_not_literal · partial");
    expect(states[1]?.notes).toContain("+ 1 unbound site at hop 3");
    expect(states[1]?.notes).toContain("deeper calls not read");
    for (const e of two.edges) for (const s of states) expect([e.source, e.target]).not.toContain(s.id);
    // 1 hop hides the hop-2 tables, counts them and draws at the 2-hop layout.
    expect(one.nodes.some((n) => n.id === T("audit"))).toBe(false);
    expect(one.tables.hiddenHop2).toBe(2);
    expect(isDrawnSubset(one, two, cardSize)).toBe(true);
  });

  it("never claims an absence: no fold box says a table is unused", () => {
    for (const name of Object.keys(FLOW_GOLDENS) as (keyof typeof FLOW_GOLDENS)[]) {
      const g = buildGraph(flowGolden(name), ALL);
      for (const n of g.nodes.filter((x) => x.column === COL.tables && x.type === "state")) {
        expect(`${n.title} ${n.sub} ${n.notes.join(" ")}`).not.toMatch(/no tables|unused|does not (use|read|write)/i);
      }
    }
    // No tables, no unknowns, no beyond: no Tables column at all.
    const g = buildGraph(flowGolden("flow-graph"), ALL);
    expect(g.nodes.some((n) => n.column === COL.tables)).toBe(false);
  });

  it("guards the new fields", () => {
    const raw = structuredClone(FLOW_GOLDENS["flow-tables"]) as Record<string, unknown>;
    const bad = { ...raw, may_also_use: [{ accessor: "function:x#nope", hop: 1 }] };
    expect(() => parseFlow(bad)).toThrow(/not a returned node/);
    const old = { ...raw };
    delete old.may_also_use;
    delete old.tables_beyond;
    const parsed = parseFlow(old);
    expect(parsed.may_also_use).toEqual([]);
    expect(parsed.tables_beyond).toBeNull();
  });

  it("starts at the Tables column when opened from the Architecture chain", () => {
    const g = buildGraph(f, ALL);
    // A layout with one column every 300 px.
    const boxes = new Map(g.nodes.map((n, i) => [n.id, { x: n.column * 300, y: i * 70, width: 240, height: 60 }]));
    const layout = { boxes, routes: new Map(), width: 2700, height: 1000 };
    const wide = downstreamFocus(g, layout, [], 1400);
    expect(wide?.box.x).toBe(COL.handler * 300);
    const narrow = downstreamFocus(g, layout, [], 700);
    expect(narrow?.box.x).toBe(COL.callees2 * 300);
    expect(narrow?.anchor?.x).toBe(COL.tables * 300);
    const tableYs = g.nodes.filter((n) => n.column === COL.tables).map((n) => boxes.get(n.id)?.y ?? 0);
    expect(narrow?.box.y).toBe(Math.min(...tableYs));
    expect(downstreamFocus(buildGraph(flowGolden("flow-graph"), ALL), layout, [], 1400)).toBeNull();
  });

  it("keeps only an Architecture hash as the back link and drops it with the endpoint", () => {
    expect(parseApiParams("#/api?sel=r&tab=flow&back=%23%2Farchitecture%3Fat%3Dx").back).toBe("#/architecture?at=x");
    expect(parseApiParams("#/api?sel=r&tab=flow&back=https%3A%2F%2Fexample.com").back).toBeNull();
    expect(parseApiParams("#/api?sel=r&tab=flow&back=%23%2Fapi").back).toBeNull();
    expect(apiHash({ ...DEFAULT_PARAMS, sel: "r", tab: "flow", back: "#/architecture" })).toBe("#/api?sel=r&tab=flow&back=%23%2Farchitecture");
  });
});
