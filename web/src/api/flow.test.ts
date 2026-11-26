import { describe, expect, it } from "vitest";

import { ApiFormatError } from "@/api/endpoints";
import { flowPath, parseFlow } from "@/api/flow";
import { FLOW_GOLDENS, type FlowGoldenName } from "@/test/flowFixtures";

type Raw = Record<string, unknown>;

function copy(name: FlowGoldenName): Raw {
  return structuredClone(FLOW_GOLDENS[name]);
}

/** first returns the first element of a raw list, for mutation. */
function first(r: Raw, key: string): Raw {
  const list = r[key];
  const v: unknown = Array.isArray(list) ? list[0] : undefined;
  if (typeof v !== "object" || v === null) throw new Error(`no ${key}[0]`);
  return v as Raw;
}

describe("flow guard", () => {
  it("accepts every flow golden as sent", () => {
    for (const name of Object.keys(FLOW_GOLDENS) as FlowGoldenName[]) {
      const f = parseFlow(copy(name));
      const raw = FLOW_GOLDENS[name];
      expect(f.nodes).toHaveLength(raw.nodes.length);
      expect(f.edges).toHaveLength(raw.edges.length);
      expect(f.candidates).toHaveLength(raw.candidates.length);
      expect(f.counts).toEqual(raw.counts);
    }
  });

  it("keeps edges verbatim, with match null where the server sends none", () => {
    const f = parseFlow(copy("flow-graph"));
    const calls = f.edges.filter((e) => e.kind === "calls");
    expect(calls.every((e) => e.match === null)).toBe(true);
    const suffix = f.edges.find((e) => e.match === "suffix");
    expect(suffix).toMatchObject({ kind: "calls_route", provenance: "inferred", confidence: 0.6 });
    expect(suffix?.evidence[0]?.file).toBe("web/pages.ts");
  });

  it("keeps a role it does not know, and the node", () => {
    const raw = copy("flow-route");
    first(raw, "nodes").roles = ["future_role"];
    const f = parseFlow(raw);
    expect(f.nodes[0]?.roles).toEqual(["future_role"]);
    expect(f.nodes).toHaveLength(FLOW_GOLDENS["flow-route"].nodes.length);
  });

  it("rejects malformed shapes", () => {
    const cases: [string, (r: Raw) => void][] = [
      ["roles not a list", (r) => { first(r, "nodes").roles = "endpoint"; }],
      ["empty roles", (r) => { first(r, "nodes").roles = []; }],
      ["negative hop", (r) => { first(r, "nodes").hop = -1; }],
      ["omitted missing", (r) => { first(r, "nodes").omitted = undefined; }],
      ["confidence a string", (r) => { first(r, "edges").confidence = "0.9"; }],
      ["edge to a node not returned", (r) => { first(r, "edges").target = "route:nowhere"; }],
      ["candidate to a node not returned", (r) => { first(r, "candidates").endpoint = "route:nowhere"; }],
      ["counts missing", (r) => { r.counts = undefined; }],
      ["truncated not an object", (r) => { r.truncated = "yes"; }],
      ["unknown transport", (r) => { r.transport = "grpc"; }],
      ["repeated node", (r) => { r.nodes = [first(r, "nodes"), ...(r.nodes as unknown[])]; }],
    ];
    for (const [what, mutate] of cases) {
      const raw = copy("flow-graph");
      mutate(raw);
      expect(() => parseFlow(raw), what).toThrow(ApiFormatError);
    }
  });

  it("builds the request path", () => {
    expect(flowPath({ id: "route:a#GET /x", scope: "all", root: null })).toBe("/api/v0/api/flow?id=route%3Aa%23GET%20%2Fx&scope=all");
    expect(flowPath({ id: "x", scope: "production", root: "web" })).toBe("/api/v0/api/flow?id=x&scope=production&root=web");
  });
});
