// Project mode (serve --project; TOP-2 of 2026-11-18, rows CE8 and CH13): a
// member's calls the stored link pass links into another member are a
// line to that member's muted card, never a "not linked" chip; a chain
// crosses into that card as far as the stored link.
import { describe, expect, it } from "vitest";

import { label } from "@/lib/labels";
import { buildChainMap, cardOf, chainView } from "@/sections/architecture/chain";
import { DEFAULT_SHOW, buildMap, endpointName } from "@/sections/architecture/model";
import { archGolden, projectArchGolden, projectChainGolden } from "@/test/architectureFixtures";

describe("architecture in project mode", () => {
  it("draws the other member as a muted card with the linked calls, not as a ghost", () => {
    const r = projectArchGolden("frontend-overview");
    expect(r.project).not.toBeNull();
    const m = buildMap(r, DEFAULT_SHOW, null);
    const card = m.nodes.find((n) => n.type === "repository");
    expect(card).toMatchObject({ id: "repository:backend", member: { name: "backend (other repository)", calls_out: 2, operations_out: 1, operations_in: 0 } });
    const comp = m.nodes.find((n) => n.type === "component");
    expect(comp?.type === "component" ? comp.across : null).toMatchObject({ linked_across: 2, repository_not_linked: 2, operations_across: 1, repository_no_route: 1, repositories: ["backend"] });
    // R1: the operation the pass links to a backend route is a documents
    // row with the backend's card, not "documented, no route".
    expect(r.documents).toContainEqual({ source: ".", target: "repository:backend", row: "CE8", by_kind: { documents_route: 1 } });
    expect(r.not_proven.some((n) => n.kind === "documented_no_route")).toBe(false);
    expect(comp?.type === "component" ? comp.ghosts : []).toEqual([]);
    const line = m.edges.find((e) => e.type === "link" && e.target === "repository:backend");
    expect(line?.count).toBe(2);
    expect(line?.links[0]).toMatchObject({ row: "CE8", repository: "backend", assumptions: ["matched across project repositories; base URL not proven"] });
    expect(endpointName(r, "repository:backend")).toBe("backend (other repository)");
    expect(r.not_proven.some((n) => n.kind === "calls_not_linked")).toBe(false);
  });

  it("draws incoming project links into the provider's component", () => {
    const r = projectArchGolden("backend-overview");
    const m = buildMap(r, DEFAULT_SHOW, null);
    const line = m.edges.find((e) => e.type === "link" && e.source === "repository:frontend");
    expect(line?.links[0]?.members.every((x) => x.source_repository === "frontend")).toBe(true);
    // T044: the frontend card counts its operation documenting a backend
    // route (CE8 R1) beside its calls.
    expect(m.nodes.find((n) => n.type === "repository")).toMatchObject({ member: { calls_in: 2, operations_in: 1, operations_out: 0 } });
  });

  it("words the links line with singular and plural nouns in both styles (row 57)", () => {
    expect(label("plain", "archLinksSummary", { links: 1, components: 1 }).text).toBe("1 link between 1 component");
    expect(label("plain", "archLinksSummary", { links: 2, components: 3 }).text).toBe("2 links between 3 components");
    expect(label("technical", "archLinksSummary", { links: 1, components: 1 }).title).toBe("1 link between 1 component");
  });

  it("keeps a single-repository answer without a project", () => {
    const r = archGolden("synthetic");
    expect(r.project).toBeNull();
    expect(buildMap(r, DEFAULT_SHOW, null).nodes.some((n) => n.type === "repository")).toBe(false);
  });

  it("places a chain's member of the other repository in its card, with the end line", () => {
    const base = buildMap(projectArchGolden("frontend-overview"), DEFAULT_SHOW, null);
    const v = chainView(projectChainGolden("frontend-chain-call"));
    const remote = [...v.member.values()].find((x) => x.repository === "backend");
    expect(remote).toBeDefined();
    if (remote === undefined) return;
    expect(cardOf(base, remote)).toBe("repository:backend");
    expect(v.ends.find((e) => e.member === remote.id)?.text).toMatch(/^continues in backend/);
    const { model } = buildChainMap(base, v);
    expect(model.nodes.some((n) => n.type === "container" && n.id === "repository:backend")).toBe(true);
    const cv = chainView(projectChainGolden("frontend-chain-component"));
    const card = [...cv.member.values()].find((x) => x.kind === "repository");
    expect(card !== undefined && cardOf(base, card)).toBe("repository:backend");
  });
});
