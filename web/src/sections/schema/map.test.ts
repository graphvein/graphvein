import { describe, expect, it } from "vitest";

import { mapView, NEIGHBOURHOOD_ABOVE, tableEdges } from "@/sections/schema/map";
import { craftedBigSet, setsGolden } from "@/test/schemaFixtures";

describe("map view", () => {
  it("draws one edge per table pair, only from stored foreign_keys", () => {
    const g = setsGolden("states").sets[0];
    if (g === undefined) throw new Error("golden changed");
    const edges = tableEdges(g);
    // t_multi → t_linked and t_multi → t_self; the self-reference of t_self
    // and the pair into a column without table are not edges.
    expect(edges.map((e) => e.id).sort()).toEqual(["table:a/migrations/2_more.sql#t_multi→table:a/migrations/1_init.sql#t_linked", "table:a/migrations/2_more.sql#t_multi→table:a/migrations/1_init.sql#t_self"].sort());
    const v = mapView(g, null);
    expect(v.mode).toBe("all");
    expect(v.self).toBe(1);
    expect(v.outside).toBe(1);
  });

  it(`switches to a 1-hop neighbourhood above ${String(NEIGHBOURHOOD_ABOVE)} tables (Q7 X)`, () => {
    const g = craftedBigSet().sets[0];
    if (g === undefined) throw new Error("crafted set lost");
    const v = mapView(g, "table:big/migrations/1.sql#t10");
    expect(v.mode).toBe("neighbourhood");
    expect(v.total).toBe(26);
    expect(v.tables.map((t) => t.node.name)).toEqual(["t09", "t10", "t11"]);
    expect(v.edges.length).toBe(2);
    // t08 → t09 and t11 → t12 leave the view.
    expect(v.outside).toBe(2);
    expect(mapView(g, null, true).tables.length).toBe(26);
  });
});
