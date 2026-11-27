import { describe, expect, it } from "vitest";

import { parseArchitecture, unknownCell, type Cell } from "@/api/architecture";
import { BAND_ID, DEFAULT_SHOW, MINIMAP_CARDS, buildMap, focusOf, mergeCells, placement } from "@/sections/architecture/model";
import { ARCH_GOLDENS, archGolden } from "@/test/architectureFixtures";

const cell = (value: string, state: string, counts: Record<string, number> | null = null): Cell => ({ value, state, source: `${state}: x`, rows: ["R"], counts });

describe("architecture answer guard", () => {
  it("reads every server golden", () => {
    for (const name of Object.keys(ARCH_GOLDENS) as (keyof typeof ARCH_GOLDENS)[]) expect(() => archGolden(name), name).not.toThrow();
  });

  it("reads an absent cell as unknown, never as a value, and keeps unknown kinds raw", () => {
    const raw = structuredClone(ARCH_GOLDENS.synthetic) as unknown as { links: { cells: Record<string, unknown>; family: string }[]; components: { kinds: string[] }[] };
    const first = raw.links[0];
    const comp = raw.components[0];
    if (first === undefined || comp === undefined) throw new Error("fixture");
    delete first.cells.tls;
    first.family = "queue";
    comp.kinds = ["grpc_server"];
    const r = parseArchitecture(raw);
    expect(r.links[0]?.cells.tls).toEqual(unknownCell("TLS"));
    expect(r.links[0]?.family).toBe("queue");
    expect(r.components[0]?.kinds).toEqual(["grpc_server"]);
  });
});

describe("buildMap", () => {
  it("shows the shown components and the libraries band, hides tests and docs by default (AG4 as revised by T067, AG10)", () => {
    const r = archGolden("components");
    const m = buildMap(r, DEFAULT_SHOW, null);
    const comps = m.nodes.filter((n) => n.type === "component").map((n) => n.id);
    expect(comps).toEqual(r.components.filter((c) => c.visibility === "shown").map((c) => `c:${c.id}`));
    expect(m.nodes.some((n) => n.id === BAND_ID)).toBe(true);
    expect(m.nodes.filter((n) => n.type === "library").map((n) => n.id.slice(4)).sort()).toEqual([...r.libraries.components].sort());
    expect(m.hidden).toMatchObject({ tests: 1, docs: 1, configured: 1, libraries: 0 });
  });

  it("hides the libraries with the toggle off, counted in the Hidden note", () => {
    const r = archGolden("components");
    const m = buildMap(r, { ...DEFAULT_SHOW, libraries: false }, null);
    expect(m.nodes.some((n) => n.type === "band" || n.type === "library")).toBe(false);
    expect(m.hidden).toMatchObject({ libraries: 4 });
    // A link to a hidden library is not drawn.
    expect(m.edges.filter((e) => e.type === "link").flatMap((e) => e.links.map((l) => l.id))).not.toContain("in_process:web->shared");
  });

  it("draws one line per card-library pair into the band and each toggled component", () => {
    const r = archGolden("components");
    const m = buildMap(r, { ...DEFAULT_SHOW, tests: true, docs: true, configured: true }, null);
    expect(m.nodes.filter((n) => n.type === "component")).toHaveLength(r.components.filter((c) => c.visibility !== "folded").length);
    expect(m.hidden).toMatchObject({ tests: 0, docs: 0, configured: 0, libraries: 0 });
    const toLib = m.edges.find((e) => e.band === "card");
    expect(toLib).toMatchObject({ source: "c:web", target: "lib:shared" });
    expect(toLib?.links.map((l) => l.id)).toEqual(["in_process:web->shared"]);
    // The card's line chip counts it (T062 A).
    expect(m.nodes.find((n) => n.id === "c:web")).toMatchObject({ fan: { count: toLib?.count, libraries: 1 } });
  });

  it("bundles parallel links of one pair into one line with mixed cells (AG15)", () => {
    const m = buildMap(archGolden("synthetic"), DEFAULT_SHOW, null);
    const pair = m.edges.filter((e) => e.type === "link" && [e.source, e.target].sort().join() === "c:svc,c:ui");
    expect(pair).toHaveLength(1);
    const e = pair[0];
    // The server action link (CE7, TOP-2 of 2026-11-18) joins the bundle.
    expect(e?.families).toEqual(["http", "in_process", "server_action"]);
    expect(e?.both).toBe(true);
    expect(e?.cells.protocol.state).toBe("mixed");
    expect(e?.cells.protocol.counts).toEqual({ HTTP: 5, "in-process": 1, "server action": 1 });
  });

  it("draws only the answer's links and stubs: markers and ghosts never make a line (AG3, CE1)", () => {
    const r = archGolden("synthetic");
    const m = buildMap(r, DEFAULT_SHOW, null);
    const drawn = m.edges.filter((e) => e.type === "link").flatMap((e) => e.links);
    for (const l of drawn) expect(r.links).toContainEqual(l);
    expect(drawn).toHaveLength(r.links.length);
    expect(r.components.some((c) => c.markers.length > 0)).toBe(true);
    expect(m.edges.every((e) => e.links.length > 0)).toBe(true);
  });

  it("puts one SDK chip on each component card, never a line, and none when the toggle is off (AG5, O2)", () => {
    const r = archGolden("components");
    const on = buildMap(r, DEFAULT_SHOW, null);
    const owners = new Set(on.nodes.filter((n) => n.type === "component").map((n) => n.id.slice(2)));
    const chips = on.nodes.filter((n) => n.type === "component" && n.stub !== null);
    expect(chips).toHaveLength(r.stubs.filter((s) => owners.has(s.component)).length);
    expect(chips.length).toBeGreaterThan(0);
    expect(on.edges.every((e) => e.links.length > 0)).toBe(true);
    const off = buildMap(r, { ...DEFAULT_SHOW, sdk: false }, null);
    expect(off.nodes.some((n) => n.type === "component" && n.stub !== null)).toBe(false);
    expect(off.hidden.sdk).toBe(chips.length);
  });

  it("counts test-scope edges in the production scope only", () => {
    expect(buildMap(archGolden("synthetic"), DEFAULT_SHOW, null).hidden.testEdges).toBe(1);
  });

  it("marks the expanded component", () => {
    const m = buildMap(archGolden("synthetic"), DEFAULT_SHOW, "svc");
    expect(m.nodes.find((n) => n.id === "c:svc")).toMatchObject({ expanded: true });
  });

  it("draws an unknown visibility as the answer returns it", () => {
    const c = archGolden("synthetic").components[0];
    if (c === undefined) throw new Error("fixture");
    expect(placement({ ...c, visibility: "pinned" }, DEFAULT_SHOW)).toBe("card");
    expect(placement({ ...c, visibility: "hidden", hidden_by: "something" }, DEFAULT_SHOW)).toBe("hidden");
  });

  it("has a minimap threshold of 12 cards", () => {
    expect(MINIMAP_CARDS).toBe(12);
  });
});

describe("mergeCells", () => {
  it("keeps equal cells and never invents a value", () => {
    const a = cell("auth ?", "unknown");
    expect(mergeCells([{ cell: a, weight: 3 }, { cell: { ...a }, weight: 2 }])).toBe(a);
    const m = mergeCells([
      { cell: cell("HTTP", "proven"), weight: 3 },
      { cell: cell("mixed", "mixed", { "auth ?": 1, "documented: none": 2 }), weight: 3 },
    ]);
    expect(m.state).toBe("mixed");
    expect(m.counts).toEqual({ HTTP: 3, "auth ?": 1, "documented: none": 2 });
  });
});

describe("focusOf", () => {
  it("keeps the selection and its neighbours, nothing else", () => {
    const m = buildMap(archGolden("synthetic"), DEFAULT_SHOW, null);
    expect(focusOf(m, { node: null, edge: null })).toBeNull();
    const f = focusOf(m, { node: "c:app", edge: null });
    expect([...(f?.nodes ?? [])].sort()).toEqual(["c:app", "c:ui", "database:ledger"]);
    const link = m.edges.find((e) => e.type === "link");
    const fe = focusOf(m, { node: null, edge: link?.id ?? null });
    expect(fe?.edges.size).toBe(1);
  });
});
