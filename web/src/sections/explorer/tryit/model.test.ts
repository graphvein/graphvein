// The try-it builder's pure helpers (T029 part B) on the generated
// document's golden.
import { describe, expect, it } from "vitest";

import synthetic from "../../../../../internal/openapi/testdata/synthetic.golden.json";

import { parseOpenApi, type DocOperation } from "@/api/openapi";
import { env, prepared } from "@/test/tryitFixtures";
import { bodyTemplate, curlCommand, pathParams, prefillRows, prettyBody, suggestedName, unavailableReason } from "@/sections/explorer/tryit/model";

const doc = parseOpenApi(structuredClone(synthetic));

function op(method: string, key: string): DocOperation {
  const o = doc.operations.find((x) => x.method === method && x.key === key);
  if (o === undefined) throw new Error(`no ${method} ${key}`);
  return o;
}

describe("try-it builder (T029 part B)", () => {
  it("lists path parameters as required with their type or type not read", () => {
    expect(pathParams(op("get", "/orders/{id}")).map((p) => [p.name, p.type])).toEqual([["id", "type not read"]]);
    expect(pathParams(op("get", "/api/items/{id}")).map((p) => p.name)).toEqual(["id"]);
    expect(pathParams(op("get", "/users"))).toEqual([]);
  });

  it("pre-fills query and header names with their provenance, leaving scheme names to the auth", () => {
    const rows = prefillRows(op("get", "/api/items/{id}"), doc, "header");
    expect(rows.every((r) => r.provenance !== null)).toBe(true);
    // Authorization is read by the HTTPBearer scheme: the environment's auth sets it.
    expect(rows.map((r) => r.name)).not.toContain("Authorization");
    // Values start empty; only required rows start enabled.
    expect(rows.every((r) => r.value === "")).toBe(true);
    for (const r of rows) expect(r.enabled).toBe(r.required === true);
    expect(prefillRows(op("get", "/users"), doc, "query")).toEqual([]);
  });

  it("builds a JSON template from the body's schema and says where it comes from", () => {
    const withBody = doc.operations.find((o) => (o.requestBody ?? []).some((m) => m.schema !== null && /json/.test(m.contentType)));
    if (withBody === undefined) throw new Error("no operation with a JSON body");
    const t = bodyTemplate(withBody, doc);
    expect(t).not.toBeNull();
    expect(t?.kind).toBe("fact");
    expect(() => JSON.parse(t?.text ?? "") as unknown).not.toThrow();
    expect(bodyTemplate(op("get", "/users"), doc)).toBeNull();
  });

  it("copies as curl with the auth variable or a placeholder, never a value", () => {
    const req = { headers: [{ name: "X-Trace", value: "1" }], body: '{"a":1}', contentType: "application/json", multipart: null };
    const text = curlCommand(prepared(), env(), req);
    expect(text).toContain("curl -X POST 'http://127.0.0.1:8000/api/items/'");
    expect(text).toContain('-H "Authorization: Bearer $API_TOKEN"');
    expect(text).toContain("--data-raw '{\"a\":1}'");
    const session = curlCommand(prepared(), env({ auth: { header: "Authorization", env: "API_TOKEN", envSet: true, prefix: "Bearer ", sessionValue: true } }), req);
    expect(session).toContain("<value typed for this session>");
    expect(session).not.toContain("$API_TOKEN");
    // Missing auth: no auth header at all.
    expect(curlCommand(prepared({ authHeader: null, authMissing: { header: "Authorization", env: "API_TOKEN" } }), env(), req)).not.toContain("Authorization");
    // Quotes in values are shell-quoted.
    expect(curlCommand(prepared(), env(), { ...req, body: "it's" })).toContain(`'it'"'"'s'`);
  });

  it("re-indents JSON token for token and leaves other text as received", () => {
    expect(prettyBody('{"a":[1]}', "application/json")).toEqual({ text: '{\n  "a": [\n    1\n  ]\n}', json: true, indented: true, note: null, duplicateKeys: [] });
    expect(prettyBody("<b>hi</b>", "text/html")).toEqual({ text: "<b>hi</b>", json: false, indented: false, note: null, duplicateKeys: [] });
    expect(prettyBody('{"cut', "application/json")).toEqual({ text: '{"cut', json: false, indented: false, note: "shown as received: not valid JSON", duplicateKeys: [] });
    expect(prettyBody('{"a":', "application/json").note).toBe("shown as received: incomplete JSON");
    // Already indented the same way: shown as received, nothing to toggle.
    expect(prettyBody('{\n  "a": 1\n}', "application/json").indented).toBe(false);
  });

  it("never re-serialises: large integers, number spellings, repeated keys and escapes stay as received", () => {
    const body = '{"count": 12345678901234567891, "id": 9007199254740993, "dup": 1, "dup": 2, "price": 1.10, "e": 1E+02, "s": "\\u00e9\\n", "\\u0064up": 3}';
    const v = prettyBody(body, "application/json");
    expect(v.json).toBe(true);
    for (const tok of ["12345678901234567891", "9007199254740993", '"dup": 1', '"dup": 2', "1.10", "1E+02", '"\\u00e9\\n"', '"\\u0064up": 3']) expect(v.text).toContain(tok);
    expect(v.duplicateKeys).toEqual(["dup"]);
    // Only whitespace changed.
    const strip = (t: string) => t.replace(/("(?:[^"\\]|\\.)*")|\s+/g, (_m, str: string | undefined) => str ?? "");
    expect(strip(v.text)).toBe(strip(body));
  });


  it("says why an operation cannot be tried", () => {
    expect(unavailableReason([], null)).toContain("No environment is configured");
    expect(unavailableReason([env({ selected: false })], null)).toContain("No environment is selected");
    expect(unavailableReason([env()], env())).toBeNull();
  });

  it("names a typed URL by host and port", () => {
    expect(suggestedName("https://api.staging.example.com")).toBe("api.staging.example.com-443");
    expect(suggestedName("http://127.0.0.1:8000/api")).toBe("127.0.0.1-8000");
    expect(suggestedName("not a url")).toBe("");
  });
});
