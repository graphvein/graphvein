import { describe, expect, it } from "vitest";

import type { FlowResponse } from "@/api/flow";
import { buildGraph, flowPeek, isDrawnSubset, truncatedText, visibleEntries, type GraphOptions } from "@/sections/api/flow/graph";
import { cardSize, chips } from "@/sections/api/flow/measure";
import { FILTER_TIERS } from "@/sections/api/flow/tiers";
import { flowGolden, FLOW_GOLDENS, type FlowGoldenName } from "@/test/flowFixtures";

const ALL: GraphOptions = { hops: 2, nearMisses: true, tiers: new Set(FILTER_TIERS) };
const id = (f: FlowResponse, tail: string) => {
  const n = f.nodes.find((x) => x.node.identity.endsWith(tail));
  if (n === undefined) throw new Error(`no node ending ${tail}`);
  return n.node.identity;
};

describe("graph building (D4)", () => {
  it("draws only stored edges and candidates, each with its origin", () => {
    for (const name of Object.keys(FLOW_GOLDENS) as FlowGoldenName[]) {
      const f = flowGolden(name);
      for (const opts of [ALL, { ...ALL, hops: 1 as const }, { ...ALL, nearMisses: false }]) {
        const g = buildGraph(f, opts);
        for (const e of g.edges) {
          if (e.origin.type === "edge") {
            const s = f.edges[e.origin.index];
            expect(s, `${name} ${e.id}`).toBeDefined();
            // Drawn between the cards of its stored ends.
            const src = g.nodes.find((n) => n.id === e.source);
            const tgt = g.nodes.find((n) => n.id === e.target);
            expect(src?.members).toContain(s?.source);
            expect(tgt?.members).toContain(s?.target);
            expect(e.tier).not.toBe("candidate");
          } else {
            const c = f.candidates[e.origin.index];
            expect(c, `${name} ${e.id}`).toBeDefined();
            expect(e.tier).toBe("candidate");
            expect(e.target).toBe(f.endpoint.identity);
          }
        }
        // No two lines for one stored edge.
        expect(new Set(g.edges.map((e) => e.id)).size).toBe(g.edges.length);
      }
    }
  });

  it("draws candidates dashed only, and no solid line from a near miss to the endpoint", () => {
    const f = flowGolden("flow-graph");
    const g = buildGraph(f, ALL);
    const near = id(f, "#Other.GET%20%27%2Ftie%27");
    const fromNear = g.edges.filter((e) => e.source === near);
    expect(fromNear.map((e) => e.tier)).toEqual(["candidate"]);
    expect(fromNear[0]?.label).toBe("ambiguous · 1 of 2");
    expect(g.tierCounts.get("candidate")).toBe(1);
  });

  it("1 hop hides exactly the hop-2 nodes and their edges", () => {
    const f = flowGolden("flow-graph");
    const two = buildGraph(f, ALL);
    const one = buildGraph(f, { ...ALL, hops: 1 });
    const hop2 = new Set(f.nodes.filter((n) => n.hop === 2).map((n) => n.node.identity));
    expect(hop2.size).toBe(4);
    expect(two.nodes.map((n) => n.id).filter((x) => !one.nodes.some((n) => n.id === x))).toEqual(
      two.nodes.map((n) => n.id).filter((x) => hop2.has(x)),
    );
    const gone = two.edges.filter((e) => !one.edges.some((x) => x.id === e.id));
    expect(gone.length).toBeGreaterThan(0);
    for (const e of gone) expect(hop2.has(e.source) || hop2.has(e.target)).toBe(true);
    expect(one.hiddenHop2).toBe(4);
  });

  it("the near-miss toggle hides near misses and their owners, not an owner of a call", () => {
    const f = flowGolden("flow-graph");
    const off = buildGraph(f, { ...ALL, nearMisses: false });
    const roles = (g: ReturnType<typeof buildGraph>) => g.nodes.flatMap((n) => n.roles);
    expect(roles(buildGraph(f, ALL))).toContain("near_miss_owner");
    expect(roles(off)).not.toContain("near_miss");
    expect(roles(off)).not.toContain("near_miss_owner");
    expect(off.edges.some((e) => e.tier === "candidate")).toBe(false);
    // A node that owns a call and a near miss stays.
    const raw = structuredClone(f);
    const page = raw.nodes.find((n) => n.node.identity.endsWith("#Page"));
    page?.roles.push("near_miss_owner");
    const kept = buildGraph(raw, { ...ALL, nearMisses: false });
    expect(kept.nodes.some((n) => n.id.endsWith("#Page"))).toBe(true);
    expect(visibleEntries(raw, { hops: 2, nearMisses: false }).some((n) => n.node.identity.endsWith("#Page"))).toBe(true);
  });

  it("folds an owner with one call into the call card, not one with two", () => {
    const f = flowGolden("flow-graph");
    const g = buildGraph(f, ALL);
    const getFlow = g.nodes.find((n) => n.id === id(f, "#makeHooks.useGetFlow.getFlowFn.GET%20%27%2Fflows%2F%3Aid%27"));
    expect(getFlow?.title).toBe("makeHooks.useGetFlow.getFlowFn");
    expect(getFlow?.members).toContain(id(f, "#makeHooks.useGetFlow.getFlowFn"));
    expect(g.nodes.some((n) => n.id === id(f, "#makeHooks.useGetFlow.getFlowFn"))).toBe(false);
    // Its contains edge is not drawn; the enclosing step attaches to the card.
    const contains = g.edges.filter((e) => e.tier === "contains");
    expect(contains.some((e) => e.target === getFlow?.id && e.source === id(f, "#makeHooks.useGetFlow"))).toBe(true);
    // Page owns two calls: its own card with two "defined in" lines.
    const page = id(f, "#Page");
    expect(g.nodes.some((n) => n.id === page)).toBe(true);
    expect(contains.filter((e) => e.source === page)).toHaveLength(2);
    expect(contains.every((e) => e.label === "defined in")).toBe(true);
    // A file owner folds as "<file> (top level)".
    expect(g.nodes.some((n) => n.title === "hooks.test.ts (top level)")).toBe(true);
  });

  it("labels callers through the enclosing step and never counts them as callers", () => {
    const f = flowGolden("flow-graph");
    const g = buildGraph(f, ALL);
    const flowPage = g.nodes.find((n) => n.id.endsWith("#FlowPage"));
    expect(flowPage?.notes).toEqual(["calls makeHooks.useGetFlow (contains this call)"]);
    const playground = g.nodes.find((n) => n.id.endsWith("#Playground"));
    expect(playground?.notes).toEqual(["calls makeHooks.useGetFlow (contains this call)"]);
    // App calls the owner Page, not an enclosing function: no note.
    expect(g.nodes.find((n) => n.id.endsWith("#App"))?.notes).toEqual([]);
    // The calls into the enclosing function are plain stored call lines.
    const toEnclosing = g.edges.filter((e) => e.target === id(f, "#makeHooks.useGetFlow"));
    expect(toEnclosing.map((e) => e.tier).sort()).toEqual(["call", "call"]);
    // Route callers are the response's counts and the call cards only.
    let calls = 0;
    for (const n of f.nodes) if (n.roles.includes("call")) calls++;
    expect(f.counts.callers).toBe(calls);
    expect([...g.callerTiers.values()].reduce((a, b) => a + b, 0)).toBe(f.counts.callers);
    expect(g.upstreamHop2).toBe(3);
    // The enclosing function's own card says what it is.
    expect(g.nodes.find((n) => n.id === id(f, "#makeHooks.useGetFlow"))?.step).toBe("enclosing function (defined in)");
  });

  it("draws a node in two roles once, placed by its first role", () => {
    const f = flowGolden("flow-graph");
    const g = buildGraph(f, ALL);
    const audit = g.nodes.filter((n) => n.id.endsWith("#audit"));
    expect(audit).toHaveLength(1);
    expect(audit[0]?.roles).toEqual(["callee", "caller"]);
    expect(audit[0]?.column).toBe(6);
    expect(new Set(g.nodes.map((n) => n.id)).size).toBe(g.nodes.length);
  });

  it("labels a conditional call's stored targets 1 of K and mutes the other target", () => {
    const f = flowGolden("flow-route");
    const g = buildGraph(f, ALL);
    const cond = id(f, "#conditional.GET%20flag%3F%27%2Ft016d-items%27%3A%27%2Ft016d-api%2Ft016d-users%27");
    const lines = g.edges.filter((e) => e.source === cond);
    expect(lines.map((e) => e.label)).toEqual(["Exact path match · 0.9 · 1 of 2", "Exact path match · 0.9 · 1 of 2"]);
    const other = g.nodes.find((n) => n.roles[0] === "other_target");
    expect(other?.muted).toBe(true);
    expect(other?.badges.map((b) => b.text)).toContain("other target");
    // A call with one stored edge has no "1 of".
    expect(g.edges.filter((e) => e.label?.startsWith("Exact path match") === true && !e.label.includes(" of "))).toHaveLength(4);
    // A linked edge of a call whose status is not linked shows the status.
    const partly = g.nodes.find((n) => n.id.includes("#partlyDynamic."));
    expect(partly?.badges.map((b) => b.text)).toContain("dynamic");
  });

  it("puts the wrapper in a pill on the call card", () => {
    const cmd = buildGraph(flowGolden("flow-command"), ALL);
    expect(cmd.nodes.find((n) => n.id.includes("#viaWrapper."))?.wrapper).toBe("via call()");
    expect(cmd.edges.filter((e) => e.tier === "fact").length).toBeGreaterThan(0);
    const http = buildGraph(flowGolden("flow-route-suffix"), ALL);
    expect(http.nodes.find((n) => n.id.includes("#viaWrapper."))?.wrapper).toBe("via apiGet()");
    expect(http.tierCounts.get("suffix")).toBe(1);
  });

  it("dims filtered tiers and the cards left without lines, never removing or moving anything", () => {
    const f = flowGolden("flow-graph");
    const all = buildGraph(f, ALL);
    const noCalls = buildGraph(f, { ...ALL, tiers: new Set(FILTER_TIERS.filter((t) => t !== "call")) });
    expect(noCalls.nodes.map((n) => [n.id, n.column])).toEqual(all.nodes.map((n) => [n.id, n.column]));
    expect(noCalls.edges.map((e) => e.id)).toEqual(all.edges.map((e) => e.id));
    expect(noCalls.edges.filter((e) => e.tier === "call").every((e) => e.dim)).toBe(true);
    expect(noCalls.edges.filter((e) => e.tier === "exact").every((e) => !e.dim)).toBe(true);
    // strip has only call lines: dimmed; the endpoint never is.
    expect(noCalls.nodes.find((n) => n.id.endsWith("#strip"))?.dim).toBe(true);
    expect(noCalls.nodes.find((n) => n.focus)?.dim).toBe(false);
    const none = buildGraph(f, { ...ALL, tiers: new Set() });
    expect(none.edges.filter((e) => e.tier !== "contains").every((e) => e.dim)).toBe(true);
  });

  it("shows +N more chips from omitted, and the truncated text", () => {
    const f = flowGolden("flow-route");
    const handler = f.nodes.find((n) => n.roles.includes("handler"));
    if (handler === undefined) throw new Error("golden changed");
    handler.omitted = { callers: 0, callees: 5 };
    const g = buildGraph(f, ALL);
    const card = g.nodes.find((n) => n.roles.includes("handler"));
    if (card === undefined) throw new Error("no handler card");
    expect(chips(card)).toEqual(["+5 more callees"]);
    expect(truncatedText(f)).toBeNull();
    f.truncated = { nodes_omitted: 12, edges_omitted: 30, reasons: ["hop2_dropped"] };
    expect(truncatedText(f)).toBe(
      "The neighbourhood is capped: hop 2 was left out because the graph would exceed 300 nodes or 600 edges (12 nodes and 30 edges not drawn).",
    );
    f.truncated = { nodes_omitted: 0, edges_omitted: 0, reasons: ["hop1_over_totals", "something_new"] };
    expect(truncatedText(f)).toBe("The neighbourhood is capped: hop 1 alone exceeds 300 nodes or 600 edges and is drawn whole; reason something_new.");
  });

  it("draws an unknown role with its raw role, and the handler state when no handler is stored", () => {
    const f = flowGolden("flow-route");
    const n = f.nodes.find((x) => x.roles.includes("other_target"));
    if (n === undefined) throw new Error("golden changed");
    n.roles = ["future_role"];
    f.handler = { state: "unresolved", expression: "makeHandler()", node: null, edge: null };
    f.nodes = f.nodes.filter((x) => !x.roles.includes("handler"));
    f.edges = f.edges.filter((e) => e.kind !== "exposes_route");
    const g = buildGraph(f, ALL);
    expect(g.nodes.find((x) => x.id === n.node.identity)?.badges.map((b) => b.text)).toContain("role future_role");
    const state = g.nodes.find((x) => x.type === "state");
    expect(state?.title).toBe("Handler not resolved");
    expect(state?.sub).toBe("makeHandler()");
    expect(g.edges.some((e) => e.source === state?.id || e.target === state?.id)).toBe(false);
  });

  it("finds the peek of a card and of a line, with the line's stored facts", () => {
    const f = flowGolden("flow-graph");
    const g = buildGraph(f, ALL);
    const card = g.nodes.find((n) => n.id.includes("#Page.GET%20%27%2Fflows%2F2%27"));
    expect(flowPeek(f, g, card?.id ?? null, null)).toMatchObject({ id: card?.id, facts: null });
    const suffix = g.edges.find((e) => e.tier === "suffix");
    expect(flowPeek(f, g, null, suffix?.id ?? null)).toEqual({
      id: card?.id,
      title: card?.title,
      step: "source of the edge",
      facts: "calls_route · inferred · 0.6 · match suffix · evidence web/pages.ts:3",
      differences: null,
    });
    // A compared line carries its request differences (T029 W4); a card
    // and a candidate never do.
    const stored = suffix?.origin.type === "edge" ? f.edges[suffix.origin.index] : undefined;
    if (stored === undefined) throw new Error("golden changed");
    const compared = { crossRepository: false, compared: ["body"], differences: [], callLinks: {}, routeLinks: {} };
    stored.shape_differences = compared;
    expect(flowPeek(f, g, null, suffix?.id ?? null)?.differences).toBe(compared);
    expect(flowPeek(f, g, card?.id ?? null, null)?.differences).toBeNull();
    const cand = g.edges.find((e) => e.tier === "candidate");
    expect(flowPeek(f, g, null, cand?.id ?? null)?.facts).toBe("near miss · ambiguous_candidate · 2 candidates · a recorded candidate, not a stored edge");
    expect(flowPeek(f, g, null, cand?.id ?? null)?.differences).toBeNull();
    // A line that is not drawn (1 hop) has no peek.
    const hop2 = g.edges.find((e) => e.source.endsWith("#FlowPage"));
    expect(flowPeek(f, buildGraph(f, { ...ALL, hops: 1 }), null, hop2?.id ?? null)).toBeNull();
    expect(flowPeek(f, g, "nope", null)).toBeNull();
  });
});

describe("T016i: card texts and the hop subset", () => {
  it("an unfolded call card shows its name as title and only file:line as sub (D2.4)", () => {
    let unfolded = 0;
    for (const name of Object.keys(FLOW_GOLDENS) as FlowGoldenName[]) {
      const f = flowGolden(name);
      const g = buildGraph(f, ALL);
      for (const n of g.nodes) {
        const entry = f.nodes.find((e) => e.node.identity === n.id);
        if (entry === undefined || !(entry.node.kind === "client_call" || entry.node.kind === "ipc_call")) continue;
        const line = entry.node.span === null ? entry.node.file : `${entry.node.file}:${String(entry.node.span.start_line)}`;
        // Full texts: the sub is the whole place, never cut here.
        expect(n.sub, `${name} ${n.id}`).toBe(line);
        if (n.members.length === 1) {
          unfolded++;
          expect(n.title).toBe(entry.node.name);
          expect(n.sub.includes(entry.node.name)).toBe(false);
        }
      }
    }
    expect(unfolded).toBeGreaterThan(0);
  });

  it("every 1-hop card and line is the 2-hop one, same id, texts, size and ends (D3.4)", () => {
    const names = Object.keys(FLOW_GOLDENS) as FlowGoldenName[];
    expect(names).toContain("flow-graph");
    for (const name of names) {
      const f = flowGolden(name);
      for (const nearMisses of [true, false]) {
        const two = buildGraph(f, { ...ALL, nearMisses });
        const one = buildGraph(f, { ...ALL, nearMisses, hops: 1 });
        const byId = new Map(two.nodes.map((n) => [n.id, n]));
        for (const n of one.nodes) {
          const w = byId.get(n.id);
          expect(w, `${name} nm=${String(nearMisses)} ${n.id}`).toBeDefined();
          if (w === undefined) continue;
          expect([n.title, n.sub, n.method, n.notes, n.badges.map((b) => b.text), n.wrapper, n.omitted, n.column]).toEqual([
            w.title,
            w.sub,
            w.method,
            w.notes,
            w.badges.map((b) => b.text),
            w.wrapper,
            w.omitted,
            w.column,
          ]);
          expect(cardSize(n)).toEqual(cardSize(w));
        }
        const edges = new Map(two.edges.map((e) => [e.id, e]));
        for (const e of one.edges) {
          const w = edges.get(e.id);
          expect(w, `${name} ${e.id}`).toBeDefined();
          expect([e.source, e.target, e.label]).toEqual([w?.source, w?.target, w?.label]);
        }
        expect(isDrawnSubset(one, two, cardSize)).toBe(true);
      }
    }
  });

  it("isDrawnSubset refuses a card whose size or text differs", () => {
    const f = flowGolden("flow-graph");
    const two = buildGraph(f, ALL);
    const one = buildGraph(f, { ...ALL, hops: 1 });
    const first = one.nodes[0];
    if (first === undefined) throw new Error("empty graph");
    first.notes = [...first.notes, "a note only 1 hop has"];
    expect(isDrawnSubset(one, two, cardSize)).toBe(false);
    const again = buildGraph(f, { ...ALL, hops: 1 });
    expect(isDrawnSubset(again, two, (n) => (n === again.nodes[0] ? { width: 1, height: 1 } : cardSize(n)))).toBe(false);
  });
});

describe("label style (T025b)", () => {
  it("draws the worded tiers with their stored confidence in the technical style, plain words in the tooltip", () => {
    const f = flowGolden("flow-graph");
    const base = { hops: 2 as const, nearMisses: true, tiers: new Set(FILTER_TIERS) };
    const plain = buildGraph(f, base);
    const technical = buildGraph(f, { ...base, labels: "technical" });
    expect(technical.edges.map((e) => e.id)).toEqual(plain.edges.map((e) => e.id));
    const fact = technical.edges.find((e) => e.tier === "fact");
    expect(fact?.label).toBe("fact 1.0");
    expect(fact?.labelTitle).toBe("Fact");
    expect(plain.edges.find((e) => e.tier === "fact")?.labelTitle).toBe("fact 1.0. The number is a fixed rank per match rule, not a probability.");
    for (const [p, t] of plain.edges.map((e, i) => [e, technical.edges[i]] as const)) {
      if (p.tier === "fact" || p.tier === "exact" || p.tier === "suffix") expect(t?.label).not.toBe(p.label);
      else expect(t?.label).toBe(p.label);
    }
  });
});
