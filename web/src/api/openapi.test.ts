// The generated document's golden (internal/openapi/testdata, the same
// bytes the server and CLI write) read by the Explorer's guards.
import { describe, expect, it } from "vitest";

import synthetic from "../../../internal/openapi/testdata/synthetic.golden.json";

import { parseOpenApi, parseOpenApiAnswer, parsePathValueEdit, parsePathValues } from "@/api/openapi";

function raw<T>(v: T): T {
  return structuredClone(v);
}

describe("parseOpenApi (T029)", () => {
  const doc = parseOpenApi(raw(synthetic));

  it("lists every operation the document writes, with its counts", () => {
    expect(doc.operations.length).toBe(doc.counts.operations);
    expect(doc.counts.operations).toBe(33);
    expect(doc.unresolved.length).toBe(doc.counts.unresolved);
    expect(doc.unrepresentable.length).toBe(doc.counts.unrepresentable);
    expect(doc.conflicts.length).toBe(doc.counts.conflicts);
    expect(doc.anyMethod.length).toBe(doc.counts.any_method);
    expect(doc.operations.filter((o) => o.resolution === "set_by_you").length).toBe(doc.counts.set_by_you);
    expect(doc.operations.filter((o) => o.mayBeShadowed.length > 0).length).toBe(doc.counts.may_be_shadowed);
    expect(doc.operations.filter((o) => o.shape !== null).length).toBe(doc.counts.with_shape);
    expect(doc.operations.filter((o) => o.shapeError !== null).length).toBe(doc.shapeErrors);
  });

  it("keeps provenance, sections and the items kept out of the standard fields", () => {
    const post = doc.operations.find((o) => o.method === "post" && o.key === "/api/items/");
    expect(post?.parameters.map((p) => [p.name, p.in, p.required, p.typeNotRead])).toEqual([
      ["q", "query", true, false],
      ["limit", "query", false, false],
      ["X-Trace", "header", false, true],
    ]);
    expect(post?.parameters[0]?.provenance).toMatchObject({ kind: "fact", rule: "SH-T1", file: "app/items.py", line: 10 });
    expect(post?.requestBody?.map((m) => [m.contentType, m.schema === null])).toEqual([
      ["application/json", false],
      ["text/plain", true],
    ]);
    expect(post?.responses?.find((r) => r.status === "204")?.emptyBody).toBe(true);
    expect(post?.shape?.notStandard.length).toBeGreaterThan(0);
    // A route without a stored shape names what was not read.
    const users = doc.operations.find((o) => o.key === "/users");
    expect(users?.shape).toBeNull();
    expect(users?.notRead).toEqual(["query", "header", "cookie", "requestBody", "responses", "operationId", "summary", "tags", "security", "path_parameter_types"]);
  });

  it("reads the root lists", () => {
    expect(doc.userValues.find((v) => v.key === "nextjs_base_path:docs")).toMatchObject({ status: "used", operations: 2 });
    expect(doc.unresolved[0]?.unknown.part).toBe("mount_or_prefix");
    expect(doc.importedSpecs[0]?.file).toBe("openapi.yaml");
    expect(doc.index?.extractor).toBe("graphvein extractors-50");
  });

  it("refuses a document without its x-graphvein", () => {
    const broken = raw(synthetic) as Record<string, unknown>;
    delete broken["x-graphvein"];
    expect(() => parseOpenApi(broken)).toThrow();
  });

  it("reads a project's member list", () => {
    const a = parseOpenApiAnswer({ api_version: 0, project: "users", members: [{ repository: "backend", document: "/api/v0/openapi?repository=backend", checkout: true }] });
    expect(a.kind).toBe("members");
  });

  it("reads the path values and an edit preview", () => {
    const v = parsePathValues({
      api_version: 0,
      file: "graphvein.yaml",
      exists: false,
      hash: "sha256:0",
      editable: true,
      reason: null,
      token: "t",
      values: [{ key: "route:a#GET%20%2F", value: "/a", status: "unused", operations: 0, why: "registration changed: ..." }],
      unresolved: [{ route: "route:a#GET%20%2F", method: "GET", pattern: "/", file: "a", line: 1, reason: "no_path", keys: ["route:a#GET%20%2F"], unknown: { part: "mount_or_prefix", text: "x" } }],
    });
    expect(v.values[0]?.why).toContain("registration changed");
    const e = parsePathValueEdit({ api_version: 0, applied: false, base_hash: "a", result_hash: "b", diff: "+x", valid: true, problems: [], changes: ["sets"] });
    expect(e.resultHash).toBe("b");
  });
});
