import { describe, expect, it } from "vitest";

import golden from "../../../internal/server/testdata/route-shape.golden.json";

import { ApiFormatError, parseEndpointDetail } from "@/api/endpoints";
import { depthCapText, parseRouteShape, parseRouteShapeSummary, SHAPE_SECTIONS } from "@/api/routeShape";
import { DETAIL_GOLDENS, raw } from "@/test/apiFixtures";

type Obj = Record<string, unknown>;

/** The shape TestRouteShapeServed serves (Go writes it), cloned. */
function shape(): Obj {
  return structuredClone(golden);
}

describe("parseRouteShape", () => {
  it("reads the Go server's served shape", () => {
    const s = parseRouteShape(shape(), "shape");
    expect(s).not.toBeNull();
    if (s === null) return;
    expect(s.path.state).toBe("complete");
    expect(s.path.items[0]).toMatchObject({ name: "id", access: "declared", presence: "required", schema: { type: "integer" } });
    expect(s.header.items[0]).toMatchObject({ access: "read_by_dependency", presence: "unknown", schema: null });
    expect(s.header.items[0]?.provenance.chain).toHaveLength(1);
    expect(s.body.state).toBe("not_read");
    expect(s.responses.items.map((r) => [r.status, r.provenance.kind, r.provenance.confidence])).toEqual([
      ["200", "fact", 1],
      ["404", "mapped", 0.9],
    ]);
    expect(s.models[0]?.schema.fields?.map((f) => f.schema.type)).toEqual(["integer", "enum", "array"]);
    expect(s.models[0]?.schema.fields?.[1]?.schema.enum).toEqual(["a", null]);
    expect(s.security_schemes[0]).toMatchObject({ name: "HTTPBearer", type: "http", scheme: "bearer", token_url: null });
    expect(s.follow.depth_limit).toBe(4);
    expect(s.follow.chains[0]).toMatchObject({ stop: "depth_cap", depth: 4, section: "query" });
    expect(s.follow.chains[0]?.steps).toHaveLength(4);
  });

  it("reads absence and another model version as no shape", () => {
    expect(parseRouteShape(undefined, "shape")).toBeNull();
    expect(parseRouteShape(null, "shape")).toBeNull();
    expect(parseRouteShape({ ...shape(), version: 2 }, "shape")).toBeNull();
  });

  it.each<[string, (s: Obj) => void]>([
    ["unknown state", (s) => { (s.query as Obj).state = "maybe"; }],
    ["unknown kind", (s) => { ((((s.path as Obj).items as Obj[])[0] as Obj).provenance as Obj).kind = "guess"; }],
    ["unknown stop", (s) => { (((s.follow as Obj).chains as Obj[])[0] as Obj).stop = "gave_up"; }],
    ["items not a list", (s) => { (s.body as Obj).items = {}; }],
    ["enum value not a literal", (s) => { ((((((s.models as Obj[])[0] as Obj).schema as Obj).fields as Obj[])[1] as Obj).schema as Obj).enum = [{}]; }],
  ])("refuses %s", (_, mutate) => {
    const s = shape();
    mutate(s);
    expect(() => parseRouteShape(s, "shape")).toThrow(ApiFormatError);
  });
});

describe("parseRouteShapeSummary", () => {
  it("reads every section", () => {
    const sections = Object.fromEntries(SHAPE_SECTIONS.map((n) => [n, { state: "not_read", items: 0 }]));
    const s = parseRouteShapeSummary({ sections, depth_cap: 1, depth_limit: 6 }, "row.shape");
    expect(s?.sections.query.state).toBe("not_read");
    expect(s?.depth_cap).toBe(1);
    expect(parseRouteShapeSummary(undefined, "row.shape")).toBeNull();
    expect(() => parseRouteShapeSummary({ sections: {}, depth_cap: 0, depth_limit: 6 }, "row.shape")).toThrow(ApiFormatError);
  });
});

describe("endpoint detail", () => {
  it("has no shape when the served detail has none", () => {
    const g = { ...(raw(DETAIL_GOLDENS["api-endpoint-route"]) as Obj) };
    delete g.shape;
    const d = parseEndpointDetail(g);
    expect(d.shape).toBeNull();
    expect(d.shape_error).toBeNull();
  });

  it("reads the shape the Go server serves for an Express route", () => {
    const d = parseEndpointDetail(raw(DETAIL_GOLDENS["api-endpoint-route"]));
    expect(d.shape).not.toBeNull();
    expect(d.shape_error).toBeNull();
  });

  it("carries the served shape and its error", () => {
    const g = raw(DETAIL_GOLDENS["api-endpoint-route"]) as Obj;
    expect(parseEndpointDetail({ ...g, shape: shape() }).shape?.follow.depth_limit).toBe(4);
    expect(parseEndpointDetail({ ...g, shape_error: "invalid route shape" }).shape_error).toBe("invalid route shape");
  });

  it("words the depth cap with the limit and where to raise it", () => {
    expect(depthCapText(6)).toBe("depth cap reached (6); raise index.shape_depth in graphvein.yaml (at most 10)");
  });
});
