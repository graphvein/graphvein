// The route detail's Request and response views (T029, rows OX-40..OX-43)
// on the shape the Go server serves (route-shape.golden.json).
import { describe, expect, it } from "vitest";

import golden from "../../../../internal/server/testdata/route-shape.golden.json";

import { parseRouteShape, type RouteShape, type ShapeProvenance } from "@/api/routeShape";
import { SECTION_KEYS } from "@/sections/explorer/model";
import { depthCapChains, routeShapeViews, shapeSchemeReads, type RouteFacts } from "@/sections/api/shapeView";

function shape(): RouteShape {
  const s = parseRouteShape(structuredClone(golden), "shape");
  if (s === null) throw new Error("golden shape not read");
  return s;
}

const facts: RouteFacts = { file: "app.py", line: 3, framework: "fastapi", attributes: {} };
const fact: ShapeProvenance = { kind: "fact", rule: "SH-T1", file: "app.py", line: 3, chain: [], confidence: 1 };

describe("routeShapeViews (T029, OX-40)", () => {
  it("keeps every stored state and lists items with their provenance", () => {
    const v = routeShapeViews(shape(), facts);
    expect(SECTION_KEYS.map((k) => v[k].status.kind)).toEqual(["complete", "partial", "partial", "not_read", "not_read", "partial", "complete"]);
    expect(v.path.items.map((i) => [i.name, i.type, i.presence])).toEqual([["id", "integer", "required"]]);
    const token = v.header.items.find((i) => i.name === "X-Token");
    expect(token?.access).toBe("read_by_dependency");
    expect(token?.presence).toBe("unknown");
    expect(token?.type).toBe("type not read");
    expect(token?.provenance.kind).toBe("read_in_handler");
    expect(v.responses.items.map((i) => [i.name, i.type])).toEqual([
      ["200", "Item"],
      ["404", "body not read"],
    ]);
    expect(v.responses.status).toEqual({ kind: "partial", reason: "error statuses not read" });
    expect(v.security.items.map((i) => i.name)).toEqual(["HTTPBearer"]);
  });

  it("says not read in every section for a route without a stored shape, never none", () => {
    const v = routeShapeViews(null, facts);
    for (const k of SECTION_KEYS) {
      expect(v[k].status.kind, k).toBe("not_read");
      expect(v[k].items, k).toEqual([]);
    }
  });

  it("says none only for a stored provably_none state, with its evidence", () => {
    const s = shape();
    s.body = { state: "provably_none", reason: null, evidence: fact, items: [] };
    s.cookie = { state: "not_read", reason: null, evidence: null, items: [] };
    const v = routeShapeViews(s, facts);
    expect(v.body.status).toEqual({ kind: "provably_none", evidence: fact });
    expect(v.cookie.status.kind).toBe("not_read");
    // A not read section with no item is never turned into "none".
    expect(Object.values(v).filter((x) => x.status.kind === "provably_none")).toHaveLength(1);
  });

  it("says a proven empty response body as proven empty, a missing schema as not read", () => {
    const s = shape();
    s.responses.items = [{ status: "204", content_type: null, schema: { type: "null" }, provenance: fact }];
    expect(routeShapeViews(s, facts).responses.items[0]?.type).toBe("no body (proven empty)");
  });
});

describe("the security-scheme rule (T029, OX-41)", () => {
  it("shows the header an http scheme reads and never says none, proven beside it", () => {
    const s = shape();
    s.header = { state: "provably_none", reason: null, evidence: fact, items: [] };
    const v = routeShapeViews(s, facts);
    expect(v.header.status).toEqual({ kind: "none_besides_scheme", evidence: fact, schemes: ["HTTPBearer"] });
    expect(v.header.items.map((i) => [i.name, i.readBy])).toEqual([["Authorization", "HTTPBearer"]]);
    // Query and cookies stay "None, proven": the scheme reads a header.
    s.query = { state: "provably_none", reason: null, evidence: fact, items: [] };
    expect(routeShapeViews(s, facts).query.status.kind).toBe("provably_none");
  });

  it("names an API key's location and name from the stored scheme", () => {
    const s = shape();
    s.security_schemes = [{ name: "HTTPBearer", type: "apiKey", scheme: null, bearer_format: null, in: "query", param_name: "api_key", token_url: null, openid_connect_url: null, provenance: fact }];
    s.query = { state: "provably_none", reason: null, evidence: fact, items: [] };
    s.header = { state: "provably_none", reason: null, evidence: fact, items: [] };
    const v = routeShapeViews(s, facts);
    expect(v.query.status.kind).toBe("none_besides_scheme");
    expect(v.query.items.map((i) => i.name)).toEqual(["api_key"]);
    expect(v.header.status.kind).toBe("provably_none");
  });

  it("says none besides the scheme in every location when where it reads is not known", () => {
    const s = shape();
    s.security_schemes = [{ ...(s.security_schemes[0] as RouteShape["security_schemes"][number]), type: "unknown", scheme: null }];
    for (const k of ["header", "query", "cookie"] as const) s[k] = { state: "provably_none", reason: null, evidence: fact, items: [] };
    const v = routeShapeViews(s, facts);
    for (const k of ["header", "query", "cookie"] as const) {
      expect(v[k].status.kind, k).toBe("none_besides_scheme");
      expect(v[k].items, k).toEqual([]);
    }
  });

  it("reads a FastAPI security dependency's class when no stored scheme describes it", () => {
    const attributes = { security_dependencies: [{ class: "APIKeyHeader", scheme_name: "key", chain: [{ file: "app/deps.py", span: { start_line: 12 } }] }] };
    const reads = shapeSchemeReads(null, { ...facts, attributes });
    expect(reads).toEqual([{ scheme: "key", location: "header", name: null, provenance: { ...fact, rule: "fastapi registration", file: "app/deps.py", line: 12 } }]);
    // Without a shape the section stays not read; the name the scheme
    // reads is listed, as "name not stored".
    const v = routeShapeViews(null, { ...facts, attributes });
    expect(v.header.status.kind).toBe("not_read");
    expect(v.header.items.map((i) => [i.name, i.readBy])).toEqual([["name not stored", "key"]]);
  });
});

describe("depthCapChains (T029, OX-40)", () => {
  it("lists the chains cut at the depth limit", () => {
    expect(depthCapChains(shape()).map((c) => c.at?.function)).toEqual(["e"]);
    expect(depthCapChains(null)).toEqual([]);
  });
});
