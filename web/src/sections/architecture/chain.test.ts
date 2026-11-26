import { describe, expect, it } from "vitest";

import { parseArchitecture, parseChain } from "@/api/architecture";
import { buildChainMap, cardOf, chainLinkIds, chainNumbersOnCards, chainOrder, chainView, componentChainFocus, flowHash, flowTarget, memberNodeId, memberTitle } from "@/sections/architecture/chain";
import { innerLayouts } from "@/sections/architecture/layout";
import { DEFAULT_SHOW, buildMap } from "@/sections/architecture/model";
import { archGolden, CHAIN_GOLDENS, chainGolden } from "@/test/architectureFixtures";

const ROUTE = "route:api/items.py#GET%20%2Fitems";
const TABLE = "table:api/models.py#item";

/** chainsynth's overview: the two components and the database card the
 * chain goldens cross (the crafted store has no /architecture golden). */
function chainsynth(show: { ui: string } = { ui: "shown" }) {
  const comp = (id: string, visibility: string, hidden_by: string | null = null) => ({ id, directory: id, name: id, state: "read", visibility, hidden_by, kinds: ["server"], manifests: [], markers: [], rows: [] });
  return parseArchitecture({
    api_version: 0,
    repository: "chainsynth",
    index: null,
    components: [comp("api", "shown"), comp("ui", show.ui, show.ui === "hidden" ? "docs" : null)],
    links: [],
    databases: [{ id: "database:models", set: "models", tool: "sqlmodel", tables: 1, engine: null, evidence: [], accessors: [] }],
    stubs: [],
    ghosts: [],
    documents: [],
    not_proven: [],
  });
}

describe("chain answer guard", () => {
  it("reads every chain golden", () => {
    for (const id of Object.keys(CHAIN_GOLDENS)) expect(() => chainGolden(id), id).not.toThrow();
    expect(() => parseChain({ api_version: 0, repository: "x", index: null, start: { id: "a", kind: "function" } })).not.toThrow();
    // A missing depth or caps.depth is unknown, never a guessed 6 (T042 review).
    const bare = parseChain({ api_version: 0, repository: "x", index: null, start: { id: "a", kind: "function" } });
    expect(bare.depth).toBeNull();
    expect(bare.caps.depth).toBeNull();
    const sent = parseChain({ api_version: 0, repository: "x", index: null, start: { id: "a", kind: "function" }, depth: 6, caps: { depth: 10, members: 2000, fan_out: 200 } });
    expect([sent.depth, sent.caps.depth]).toEqual([6, 10]);
  });
});

describe("chainView", () => {
  it("numbers the steps top to bottom over both directions (AG16)", () => {
    const v = chainView(chainGolden(ROUTE));
    // Up 4 steps (furthest first), the start, down 2 steps.
    expect(v.positions.map((p) => [p.number, p.side, p.step])).toEqual([
      [1, "up", 4],
      [2, "up", 3],
      [3, "up", 2],
      [4, "up", 1],
      [5, "start", 0],
      [6, "down", 1],
      [7, "down", 2],
    ]);
    expect(v.numberOf.get(ROUTE)).toBe(5);
    expect(v.numberOf.get(TABLE)).toBe(7);
    // The upstream end line (CH11) and the downstream DI line are kept.
    expect(v.ends.map((e) => e.row)).toEqual(["CH11", "CH11", "CH11"]);
    // Up step 4 (Page) is past the third step but alone and joined to a
    // drawn parent: the server draws it rather than "+ 1 more".
    expect(v.folded).toBe(0);
    expect(v.positions[0]?.shown.has("function:ui/src/app.ts#Page")).toBe(true);
    expect(chainOrder(v)[0]).toBe("function:ui/src/app.ts#Page");
  });

  it("lists the edges into a step with their wording and tier (link quality per step)", () => {
    const v = chainView(chainGolden(ROUTE));
    const last = v.positions.at(-1);
    expect(last?.edges.filter((e) => e.to === TABLE).map((e) => `${e.wording} ${e.tier}`).sort()).toEqual(["builds a query on inferred 0.9", "reads fact", "writes fact"]);
    expect(last?.edges).toHaveLength(7);
  });
});

describe("buildChainMap", () => {
  it("opens the crossed cards to the drawn members, folds the rest per step and bundles lines between cards", () => {
    const r = chainsynth();
    const base = buildMap(r, DEFAULT_SHOW, null);
    const v = chainView(chainGolden(ROUTE));
    const { model, focus } = buildChainMap(base, v);
    const containers = model.nodes.filter((n) => n.type === "container").map((n) => n.id);
    expect(containers.sort()).toEqual(["c:api", "c:ui", "database:models"]);
    // The table sits in its database card (its `database`), not in api.
    const table = model.nodes.find((n) => n.id === memberNodeId(TABLE));
    expect(table?.type === "member" ? table.parent : null).toBe("database:models");
    // Nothing folds: the one-member step 1 (Page) is drawn, numbered 1.
    expect(model.nodes.filter((n) => n.type === "fold")).toEqual([]);
    const page = model.nodes.find((n) => n.id === memberNodeId("function:ui/src/app.ts#Page"));
    expect(page?.type === "member" ? [page.number, page.parent] : null).toEqual([1, "c:ui"]);
    // Every member and every edge is in the focus; nothing outside is.
    expect(focus.nodes.has(memberNodeId(ROUTE))).toBe(true);
    // Between ui and api: one bundled line, drawn caller (ui) above.
    const between = model.edges.filter((e) => e.type === "chain" && e.inner !== true);
    expect(between.map((e) => `${e.source}>${e.target}`).sort()).toEqual(["c:api>database:models", "c:ui>c:api"]);
    // Inside api: calls and contains between read_items and _fmt are one
    // line with both stored edges.
    const inner = model.edges.find((e) => e.id === `ch:${memberNodeId("function:api/items.py#read_items")}>${memberNodeId("function:api/items.py#read_items._fmt")}`);
    expect(inner?.chain?.map((k) => k.kind).sort()).toEqual(["calls", "contains"]);
    // Every opened card gets an inner layout holding its members.
    const inners = innerLayouts(model);
    expect(inners.get("c:api")?.at.has(memberNodeId(ROUTE))).toBe(true);
  });

  it("folds members of a card the Show toggles hide at the root, counted as hidden", () => {
    const base = buildMap(chainsynth({ ui: "hidden" }), DEFAULT_SHOW, null);
    const v = chainView(chainGolden(ROUTE));
    expect(cardOf(base, v.member.get("method:ui/src/app.ts#ItemsService.readItems") ?? (null as never))).toBeNull();
    const { model } = buildChainMap(base, v);
    const roots = model.nodes.filter((n) => n.type === "fold" && n.parent === null);
    expect(roots.reduce((a, n) => a + (n.type === "fold" ? n.hidden : 0), 0)).toBe(4);
    expect(model.nodes.some((n) => n.id === "c:ui")).toBe(false);
  });
});

describe("component chains (CH9)", () => {
  it("numbers the cards and keeps only the chain's cards and lines undimmed", () => {
    const r = archGolden("components");
    const base = buildMap(r, DEFAULT_SHOW, null);
    const v = chainView(chainGolden("web"));
    expect(v.componentLevel).toBe(true);
    expect(chainNumbersOnCards(base, v).get("c:web")).toEqual([1]);
    const focus = componentChainFocus(base, v);
    expect(focus.nodes.has("c:web")).toBe(true);
    expect(focus.nodes.has("c:api")).toBe(true);
    expect(focus.nodes.has("c:py")).toBe(false);
    // shared is a folded library: not on the map, so not focused.
    expect([...focus.edges].every((id) => id.includes("c:web"))).toBe(true);
    const ids = chainLinkIds(r, v);
    expect([...ids].every((id) => id.includes("web"))).toBe(true);
    expect(ids.size).toBeGreaterThan(0);
  });
});

describe("Flow tab target", () => {
  it("opens the start when it is a route, else the nearest route", () => {
    const v = chainView(chainGolden(ROUTE));
    expect(flowTarget(v, null)).toBe(ROUTE);
    expect(flowHash(ROUTE, "function:api/items.py#read_items")).toBe(`#/api?sel=${encodeURIComponent(ROUTE)}&tab=flow&node=${encodeURIComponent("function:api/items.py#read_items")}`);
    // T026 AG-C3: the chain hash rides along so the Flow tab links back.
    expect(flowHash(ROUTE, null, "#/architecture?chain=x")).toBe(`#/api?sel=${encodeURIComponent(ROUTE)}&tab=flow&back=${encodeURIComponent("#/architecture?chain=x")}`);
    const t = chainView(chainGolden(TABLE));
    expect(flowTarget(t, null)).toMatch(/^route:/);
    expect(flowTarget(chainView(chainGolden("web")), null)).toBeNull();
  });

  it("names a route member by its served path", () => {
    expect(memberTitle({ kind: "route", name: "GET /", method: "GET", path: "/", full_path: "/api/v1/items/", command: null })).toBe("GET /api/v1/items/");
    expect(memberTitle({ kind: "route", name: "GET /", method: "GET", path: "/", full_path: null, command: null })).toBe("GET /");
  });
});
