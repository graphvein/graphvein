// The Explorer's view model on the generated document's golden: absence
// is shown only where the document proves it.
import { describe, expect, it } from "vitest";

import synthetic from "../../../../internal/openapi/testdata/synthetic.golden.json";
import specShape from "../../../../testdata/fixtures/route-shape-spec.openapi.golden.json";

import { parseOpenApi, type DocOperation } from "@/api/openapi";
import {
  authLines,
  emptyDocumentText,
  groupByFile,
  groupByTag,
  NO_TAG,
  operationId,
  openapiTypeText,
  operationMarkers,
  requestBodyPresence,
  routeAbsentText,
  routeOperations,
  schemeReads,
  SECTION_KEYS,
  sectionView,
  sectionItems,
  sectionStatus,
  unknownPathText,
} from "@/sections/explorer/model";
import { explorerEditHash, explorerRouteHash, parseExplorerParams } from "@/sections/explorer/params";

const doc = parseOpenApi(structuredClone(synthetic));

function op(method: string, key: string): DocOperation {
  const o = doc.operations.find((x) => x.method === method && x.key === key);
  if (o === undefined) throw new Error(`no ${method} ${key}`);
  return o;
}

describe("committed spec items (T029 SH-O)", () => {
  const spec = parseOpenApi(structuredClone(specShape));
  const find = (method: string, key: string): DocOperation => {
    const o = spec.operations.find((x) => x.method === method && x.key === key);
    if (o === undefined) throw new Error(`no ${method} ${key}`);
    return o;
  };

  it("shows mapped items beside the code's, never in the document, and a spec that differs as a note", () => {
    const item = find("get", "/v30/items/{item_id}");
    const path = sectionStatus(item, "path");
    expect(path.kind).toBe("complete");
    expect(path.kind === "complete" ? path.note : undefined).toMatch(/^spec differs: path parameter item_id is integer in the code, string in the spec/);
    const query = sectionItems(item, "query");
    expect(query.filter((i) => i.provenance.kind === "mapped").map((i) => [i.name, i.presence, i.inDocument])).toEqual([
      ["q", "required", false],
      ["extra", "optional", false],
      ["filter", "optional", false],
      ["limit", "optional", false],
    ]);
    expect(query.find((i) => i.provenance.kind === "fact")?.inDocument).toBe(true);
  });

  it("reads a section only the spec fills as not read in the code, with the spec's items", () => {
    const item = find("get", "/v30/items/{item_id}");
    const security = sectionStatus(item, "security");
    expect(security.kind).toBe("not_read");
    expect(security.kind === "not_read" ? security.note : undefined).toMatch(/^not read in the code: every item is mapped from the committed spec/);
    expect(sectionItems(item, "security").every((i) => i.provenance.kind === "mapped" && !i.inDocument)).toBe(true);
  });
});

describe("section states (T029: absence only where proven)", () => {
  it("reads every section of a route without a shape as not read, never none", () => {
    const users = op("get", "/users");
    for (const key of ["query", "header", "cookie", "body", "responses", "security"] as const) {
      expect(sectionStatus(users, key), key).toEqual({ kind: "not_read" });
      expect(sectionItems(users, key), key).toEqual([]);
    }
    // A path key without parameters is a fact of the key itself.
    expect(sectionStatus(users, "path")).toEqual({ kind: "no_template_params" });
  });

  it("names path parameters from the template with their type not read", () => {
    const orders = op("get", "/orders/{id}");
    expect(sectionStatus(orders, "path")).toEqual({ kind: "template" });
    const items = sectionItems(orders, "path");
    expect(items.map((i) => [i.name, i.type, i.presence, i.provenance.kind, i.provenance.file])).toEqual([["id", "type not read", "required", "fact", "src/orders.js"]]);
  });

  it("says none only for a stored provably_none state, with its evidence", () => {
    const states = new Set<string>();
    for (const o of doc.operations) {
      for (const key of SECTION_KEYS) {
        const s = sectionStatus(o, key);
        states.add(s.kind);
        const stored = o.shape?.sections[key]?.state;
        if (s.kind === "provably_none") {
          expect(stored).toBe("provably_none");
          expect(sectionItems(o, key)).toEqual([]);
        }
        // A section that is not read never lists items as if complete.
        if (s.kind === "not_read" && stored === undefined) expect(o.notRead.length).toBeGreaterThan(0);
      }
    }
    expect([...states].sort()).toEqual(["complete", "declared", "no_template_params", "not_read", "partial", "provably_none", "template"]);
  });

  it("keeps a partial section's reason", () => {
    const partial = doc.operations.flatMap((o) => SECTION_KEYS.map((k) => sectionStatus(o, k))).find((s) => s.kind === "partial");
    expect(partial).toMatchObject({ kind: "partial" });
    if (partial?.kind === "partial") expect(partial.reason).not.toBeNull();
  });

  it("reads a route whose shape could not be read as not read", () => {
    const broken = op("get", "/broken");
    expect(broken.shapeError).not.toBeNull();
    for (const key of ["query", "body", "responses"] as const) expect(sectionStatus(broken, key)).toEqual({ kind: "not_read" });
    expect(operationMarkers(broken, doc).map((m) => m.kind)).toContain("shape_error");
  });

  it("lists items kept out of the standard fields with their row and why", () => {
    const kept = doc.operations.flatMap((o) => SECTION_KEYS.flatMap((k) => sectionItems(o, k))).filter((i) => !i.inDocument);
    expect(kept.length).toBeGreaterThan(0);
    for (const i of kept) {
      expect(i.row).toMatch(/^OG-\d+$/);
      expect(i.why).not.toBe("");
    }
    expect(new Set(kept.map((i) => i.provenance.kind)).size).toBeGreaterThan(1);
  });

  it("writes security declared from FastAPI dependencies as declared", () => {
    const digest = op("get", "/digest");
    expect(sectionStatus(digest, "security").kind).toMatch(/declared|complete|partial/);
    expect(sectionItems(digest, "security").length).toBeGreaterThan(0);
  });

  it("says an empty auth list is none read", () => {
    const post = op("post", "/api/items/");
    expect(authLines(post)).toEqual([{ attribute: "middleware", title: "Route middleware", entries: [], unknown: false }]);
  });
});

describe("grouping and markers", () => {
  it("names operations uniquely by method and path key", () => {
    const ids = doc.operations.map(operationId);
    expect(new Set(ids).size).toBe(ids.length);
    // The route identity alone is not unique: a basePath value gives one
    // route two keys.
    expect(new Set(doc.operations.map((o) => o.route)).size).toBeLessThan(ids.length);
  });

  it("groups by router file, every operation once", () => {
    const groups = groupByFile(doc.operations);
    expect(groups.flatMap((g) => g.operations).length).toBe(doc.operations.length);
    expect(groups.map((g) => g.subtitle)).toEqual([...groups.map((g) => g.subtitle)].sort());
  });

  it("gives shared file names their directory", () => {
    const fake = (file: string) => ({ ...op("get", "/users"), file });
    expect(groupByFile([fake("apps/article/route.ts"), fake("apps/user/route.ts"), fake("api/items.py")]).map((g) => g.title)).toEqual(["items", "article/route", "user/route"]);
  });

  it("groups by the documenting spec's first tag, others under No tag", () => {
    const documented = doc.operations.filter((o) => o.documentedBy.length > 0);
    expect(documented.length).toBeGreaterThan(0);
    const tags = new Map(documented.flatMap((o) => o.documentedBy.map((d) => [d.operation, ["items", "more"]] as [string, string[]])));
    const groups = groupByTag(doc.operations, tags);
    expect(groups[0]?.title).toBe("items");
    expect(groups[0]?.operations.length).toBe(documented.length);
    expect(groups.at(-1)?.id).toBe(NO_TAG);
    expect(groups.at(-1)?.operations.length).toBe(doc.operations.length - documented.length);
    // A tag that could not be read is no tag, never a guess.
    expect(groupByTag(doc.operations, new Map()).map((g) => g.id)).toEqual([NO_TAG]);
  });

  it("marks shadowing, equivalent registrations and conditional routes", () => {
    expect(operationMarkers(op("get", "/users"), doc).map((m) => m.kind)).toContain("shadowed");
    expect(operationMarkers(op("get", "/same"), doc).map((m) => m.kind)).toContain("equivalent");
    expect(operationMarkers(op("post", "/api/items/"), doc).map((m) => m.kind)).toContain("conditional");
    expect(operationMarkers(op("post", "/capped"), doc).map((m) => m.kind)).toContain("depth_cap");
  });

  it("shows the unknown part of an unresolved path", () => {
    const u = doc.unresolved[0];
    if (u === undefined) throw new Error("no unresolved route");
    expect(unknownPathText(u)).toEqual({ unknown: "mount or prefix", rest: "/lorn" });
    const base = doc.unresolved.find((x) => x.unknown.part === "base_path");
    if (base !== undefined) expect(unknownPathText(base).unknown).toContain("basePath");
  });
});

describe("names a security scheme reads (T029 part B display fix)", () => {
  /** withProvenNone is op with a stored provably_none section, as the
   * fastapi template's PUT /api/v1/items/{id} stores its headers. */
  function withProvenNone(o: DocOperation, key: "header" | "query" | "cookie"): DocOperation {
    const c = structuredClone(o);
    if (c.shape === null) throw new Error("no shape");
    c.shape.sections[key] = { state: "provably_none", reason: null, evidence: { kind: "fact", rule: "SH-F23", file: c.file, line: c.line, chain: [], confidence: 1 }, standard: 0 };
    return c;
  }

  it("reads where each applying scheme sends its credential", () => {
    expect(schemeReads(op("get", "/api/items/{id}"), doc).map((r) => [r.scheme, r.location, r.name])).toEqual([["HTTPBearer", "header", "Authorization"]]);
    expect(schemeReads(op("get", "/key"), doc).map((r) => [r.scheme, r.location, r.name])).toEqual([["ApiKey", "header", "X-API-Key"]]);
    // An OAuth2 security dependency (no component written, OG-31) reads Authorization.
    expect(schemeReads(op("get", "/oauth"), doc).map((r) => [r.scheme, r.location, r.name])).toEqual([["OAuth2PasswordBearer", "header", "Authorization"]]);
    // A scheme kept in x-graphvein whose type the document does not give: location not known.
    expect(schemeReads(op("get", "/flows"), doc).map((r) => [r.scheme, r.location])).toEqual([["OAuth2PasswordBearer", null]]);
    expect(schemeReads(op("get", "/users"), doc)).toEqual([]);
  });

  it("shows the header a scheme reads and never says none, proven beside it", () => {
    const o = withProvenNone(op("get", "/api/items/{id}"), "header");
    const v = sectionView(o, "header", doc);
    expect(v.status.kind).toBe("none_besides_scheme");
    expect(v.items.filter((i) => i.readBy !== undefined).map((i) => [i.name, i.readBy])).toEqual([["Authorization", "HTTPBearer"]]);
    // The query and cookie sections of the same route: Authorization is not there.
    const q = sectionView(withProvenNone(o, "query"), "query", doc);
    expect(q.status.kind).toBe("provably_none");
    expect(q.items.some((i) => i.readBy !== undefined)).toBe(false);
    // Without the document the section reads as before (sectionItems is pure).
    expect(sectionView(o, "header").status.kind).toBe("provably_none");
  });

  it("says none besides the scheme in every section when its location is not known", () => {
    for (const key of ["header", "query", "cookie"] as const) {
      const v = sectionView(withProvenNone(op("get", "/flows"), key), key, doc);
      expect(v.status.kind, key).toBe("none_besides_scheme");
      expect(v.items.some((i) => i.readBy !== undefined), key).toBe(false);
    }
  });

  it("lists the scheme's name in a section that was not read too", () => {
    const v = sectionView(op("get", "/oauth"), "header", doc);
    expect(v.status.kind).toBe("not_read");
    expect(v.items.map((i) => i.readBy)).toEqual(["OAuth2PasswordBearer"]);
    // An API key in a header: its stored name.
    expect(sectionView(op("get", "/key"), "header", doc).items.map((i) => i.name)).toEqual(["X-API-Key"]);
  });

  it("says a request body's presence not read as unknown, never optional (OX-13)", () => {
    const post = op("post", "/api/items/");
    expect(post.requestBodyRequired).toBe("not_read");
    expect(requestBodyPresence(post)).toBe("unknown");
    expect(requestBodyPresence({ ...post, requestBodyRequired: true })).toBe("required");
    expect(requestBodyPresence({ ...post, requestBodyRequired: false })).toBe("optional");
    // A body the document writes without either is not called optional.
    expect(requestBodyPresence({ ...post, requestBodyRequired: null })).toBe("unknown");
    expect(requestBodyPresence(op("get", "/Users"))).toBeNull();
  });

  it("says a type's reason once", () => {
    expect(openapiTypeText({ "x-graphvein": { unknown: "Joi converts strings to numbers unless strict(): type not claimed" } })).toBe("type not claimed: Joi converts strings to numbers unless strict()");
    expect(openapiTypeText({ "x-graphvein": { unknown: "a custom validator" } })).toBe("type not claimed: a custom validator");
    expect(openapiTypeText({ "x-graphvein": { unknown: "type not claimed" } })).toBe("type not claimed");
  });

  it("builds the empty-document sentence from the document's counts and reasons", () => {
    const zero = { ...doc.counts, routes: 0, production_routes: 0, operations: 0, any_method: 0, unresolved: 0, unrepresentable: 0, conflicts: 0 };
    const text = (counts: Partial<typeof zero>, test = 0) => emptyDocumentText({ counts: { ...zero, ...counts }, excludedTest: { routes: test, pathEntries: 0 } });
    expect(text({})).toBe("No route is stored for this repository (0 routes indexed), so the document has no operation.");
    expect(text({ routes: 4 }, 4)).toBe("No operation is in the document: 4 routes indexed, all test-scope, and the document covers production routes only.");
    expect(text({ routes: 1 }, 1)).toBe("No operation is in the document: 1 route indexed, it is test-scope, and the document covers production routes only.");
    expect(text({ routes: 85, production_routes: 85, unresolved: 85 })).toBe("No operation is in the document: 85 production routes indexed; listed below: 85 routes with an unknown path part.");
    expect(text({ routes: 9, production_routes: 7, any_method: 2, conflicts: 1, unrepresentable: 1, unresolved: 3 }, 2)).toBe(
      "No operation is in the document: 7 production routes indexed (2 test-scope left out); listed below: 2 any-method entries (never expanded to methods), 3 routes with an unknown path part, 1 conflict, 1 path OpenAPI cannot express.",
    );
    expect(text({ routes: 1, production_routes: 1, any_method: 1 })).toBe("No operation is in the document: 1 production route indexed; listed below: 1 any-method entry (never expanded to methods).");
    expect(text({ routes: 2, production_routes: 2 })).toBe("No operation is in the document: 2 production routes indexed, and the document gives no reason for any of them.");
  });
});

describe("links from the API section (T029, OX-42)", () => {
  it("round-trips a route link through the hash", () => {
    const h = explorerRouteHash("route:app/items.py#GET%20%2F%7Bid%7D");
    expect(h).toBe("#/explorer?route=route%3Aapp%2Fitems.py%23GET%2520%252F%257Bid%257D");
    expect(parseExplorerParams(h).route).toBe("route:app/items.py#GET%20%2F%7Bid%7D");
  });

  it("round-trips the editor links from Settings and drops an unknown editor (T039 ST-F)", () => {
    expect(explorerEditHash("path-values")).toBe("#/explorer?edit=path-values");
    expect(parseExplorerParams(explorerEditHash("environments")).edit).toBe("environments");
    expect(parseExplorerParams("#/explorer?edit=bogus").edit).toBeNull();
  });

  it("finds a route's operations, and says why there is none from the document's own lists", () => {
    expect(routeOperations(doc, "route:app/items.py#GET%20%2F%7Bid%7D")).toEqual({ ops: [op("get", "/api/items/{id}")], equivalent: false });
    // A registration collapsed into another's operation (OG-19) finds it.
    const collapsed = doc.operations.find((o) => o.equivalentRegistrations.length > 1);
    const second = collapsed?.equivalentRegistrations.find((r) => r !== collapsed.route);
    if (collapsed === undefined || second === undefined) throw new Error("golden has no equivalent registrations");
    expect(routeOperations(doc, second)).toEqual({ ops: [collapsed], equivalent: true });
    const base = { repository: "r", unresolved: [], anyMethod: [], unrepresentable: [], conflicts: [] };
    expect(routeAbsentText({ ...base, anyMethod: [{ route: "x", file: "a", line: 1, framework: "express", key: "/a" }] }, "x")).toContain("accepts any method");
    expect(routeAbsentText({ ...base, unrepresentable: [{ route: "x", method: "get", key: "/a", reason: "r" }] }, "x")).toContain("OpenAPI cannot express its path");
    expect(routeAbsentText({ ...base, conflicts: [{ reason: "same_key", keys: ["/a"], method: "get", routes: ["x"] }] }, "x")).toContain("conflicts with another route");
    // No list names it: no guessed reason.
    expect(routeAbsentText(base, "x")).toBe("The document of r has no operation for route x, and the document lists no reason (test-scope routes are left out of the document).");
  });
});
