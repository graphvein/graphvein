import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import {
  ApiFormatError,
  callsPath,
  endpointPath,
  endpointsPath,
  httpClient,
  parseCalls,
  parseEndpointDetail,
  parseEndpoints,
  requestableStatuses,
} from "@/api/endpoints";
import { CALLS_GOLDENS, DETAIL_GOLDENS, ENDPOINTS_GOLDENS, raw } from "@/test/apiFixtures";

type Obj = Record<string, unknown>;

describe("guards accept every T016d1 golden", () => {
  it.each(Object.keys(ENDPOINTS_GOLDENS) as (keyof typeof ENDPOINTS_GOLDENS)[])("%s", (name) => {
    const g = ENDPOINTS_GOLDENS[name];
    const r = parseEndpoints(raw(g));
    expect(r.repository).toBe(g.repository);
    expect(r.transports.map((t) => t.transport)).toEqual(g.transports.map((t) => t.transport));
    for (const t of r.transports) {
      // Roots ignore the root filter and sum to the unfiltered total (D1):
      // nothing hidden. With a filter, the total is that root's.
      const sum = t.roots.reduce((n, x) => n + x.total, 0);
      if (r.root === null) expect(sum).toBe(t.calls.total);
      else expect(t.roots.find((x) => x.root === r.root)?.total ?? 0).toBe(t.calls.total);
      expect(Object.values(t.calls.by_status).reduce((n, x) => n + x, 0)).toBe(t.calls.total);
    }
  });

  it("reads server action rows (T024b)", () => {
    const r = parseEndpoints(raw(ENDPOINTS_GOLDENS["api-endpoints-all"]));
    const t = r.transports.find((x) => x.transport === "server_action");
    expect(t?.endpoints.map((e) => (e.transport === "server_action" ? [e.action, e.action_scope, e.caller_count] : null))).toEqual([
      ["t016dSave", "module", 2],
      ["t016dUnused", "module", 0],
      ["t016dPassed", "module", 0],
    ]);
    // t016dPassed is passed as a prop: may be called, not never called.
    expect(t?.endpoints[2]?.flags).toEqual(["unread_use"]);
    expect(t?.never_called).toBe(1);
  });

  it.each(Object.keys(DETAIL_GOLDENS) as (keyof typeof DETAIL_GOLDENS)[])("%s", (name) => {
    const g = DETAIL_GOLDENS[name];
    const d = parseEndpointDetail(raw(g));
    expect(d.endpoint.identity).toBe(g.endpoint.identity);
    expect(d.callers).toHaveLength(g.callers.length);
    expect(d.near_misses).toHaveLength(g.near_misses.length);
  });

  it.each(Object.keys(CALLS_GOLDENS) as (keyof typeof CALLS_GOLDENS)[])("%s", (name) => {
    const g = CALLS_GOLDENS[name];
    const c = parseCalls(raw(g));
    expect(c.calls).toHaveLength(g.calls.length);
    expect(c.total).toBe(g.total);
  });
});

describe("guards reject malformed shapes", () => {
  const endpointBreakers: [string, (r: Obj) => void][] = [
    ["another api_version", (r) => { r.api_version = 1; }],
    ["missing transports", (r) => { delete r.transports; }],
    ["an unknown transport", (r) => { ((r.transports as Obj[])[0] as Obj).transport = "grpc"; }],
    ["a negative count", (r) => { (((r.transports as Obj[])[0] as Obj).calls as Obj).total = -1; }],
    ["a non-count by_status", (r) => { ((((r.transports as Obj[])[0] as Obj).calls as Obj).by_status as Obj).linked = "9"; }],
    ["a row without id", (r) => { delete (((r.transports as Obj[])[0] as Obj).endpoints as Obj[])[0]?.id; }],
    ["flags not strings", (r) => { ((((r.transports as Obj[])[0] as Obj).endpoints as Obj[])[0] as Obj).flags = [1]; }],
    ["roots not a list", (r) => { ((r.transports as Obj[])[0] as Obj).roots = {}; }],
  ];
  it.each(endpointBreakers)("endpoints with %s", (_n, breakIt) => {
    const r = raw(ENDPOINTS_GOLDENS["api-endpoints-production"]) as unknown as Obj;
    breakIt(r);
    expect(() => parseEndpoints(r)).toThrow(ApiFormatError);
  });

  const detailBreakers: [string, (r: Obj) => void][] = [
    ["no endpoint", (r) => { delete r.endpoint; }],
    ["attributes not an object", (r) => { (r.endpoint as Obj).attributes = []; }],
    ["a caller edge without confidence", (r) => { delete (((r.callers as Obj[])[0] as Obj).edges as Obj[])[0]?.confidence; }],
    ["a string confidence", (r) => { ((((r.callers as Obj[])[0] as Obj).edges as Obj[])[0] as Obj).confidence = "0.9"; }],
    ["a bad transport", (r) => { r.transport = "ws"; }],
    ["file_state a number", (r) => { (r.endpoint as Obj).file_state = 3; }],
    ["source_root a number", (r) => { (((r.callers as Obj[])[0] as Obj).node as Obj).source_root = 1; }],
  ];
  it.each(detailBreakers)("detail with %s", (_n, breakIt) => {
    const r = raw(DETAIL_GOLDENS["api-endpoint-route"]) as unknown as Obj;
    breakIt(r);
    expect(() => parseEndpointDetail(r)).toThrow(ApiFormatError);
  });

  it("calls without targets", () => {
    const r = raw(CALLS_GOLDENS["api-calls-http-gaps"]) as unknown as Obj;
    delete ((r.calls as Obj[])[0] as Obj).targets;
    expect(() => parseCalls(r)).toThrow(ApiFormatError);
  });
});

describe("candidate_count_exact", () => {
  function nearMiss(exact: unknown) {
    const r = raw(DETAIL_GOLDENS["api-endpoint-route-ambiguous"]) as unknown as Obj;
    const n = (r.near_misses as Obj[])[0] as Obj;
    if (exact === undefined) delete n.candidate_count_exact;
    else n.candidate_count_exact = exact;
    return r;
  }
  it("reads true and false", () => {
    expect(parseEndpointDetail(nearMiss(true)).near_misses[0]?.candidate_count_exact).toBe(true);
    expect(parseEndpointDetail(nearMiss(false)).near_misses[0]?.candidate_count_exact).toBe(false);
  });
  it("treats an absent field as not exact (a lower bound)", () => {
    expect(parseEndpointDetail(nearMiss(undefined)).near_misses[0]?.candidate_count_exact).toBe(false);
  });
  it("rejects a non-boolean", () => {
    expect(() => parseEndpointDetail(nearMiss("yes"))).toThrow(ApiFormatError);
  });
});

describe("ide.caveat", () => {
  function withCaveat(caveat: unknown) {
    const r = raw(DETAIL_GOLDENS["api-endpoint-route"]) as unknown as Obj;
    const ide = (r.endpoint as Obj).ide as Obj;
    if (caveat !== undefined) ide.caveat = caveat;
    return r;
  }
  it("reads an absent field as null and keeps a string", () => {
    expect(parseEndpointDetail(withCaveat(undefined)).endpoint.ide?.caveat).toBeNull();
    expect(parseEndpointDetail(withCaveat("column_unit_assumed")).endpoint.ide?.caveat).toBe("column_unit_assumed");
  });
  it("rejects a non-string", () => {
    expect(() => parseEndpointDetail(withCaveat(1))).toThrow(ApiFormatError);
  });
});

describe("ide.line_only", () => {
  function withIde(lineOnly: unknown) {
    const r = raw(DETAIL_GOLDENS["api-endpoint-route"]) as unknown as Obj;
    const ide = (r.endpoint as Obj).ide as Obj;
    if (lineOnly !== undefined) ide.line_only = lineOnly;
    return r;
  }
  it("reads an absent field as false and true as true", () => {
    expect(parseEndpointDetail(withIde(undefined)).endpoint.ide?.line_only).toBe(false);
    expect(parseEndpointDetail(withIde(true)).endpoint.ide?.line_only).toBe(true);
  });
  it("rejects a non-boolean", () => {
    expect(() => parseEndpointDetail(withIde("yes"))).toThrow(ApiFormatError);
  });
});

describe("request paths", () => {
  it("encodes identities and roots as query values", () => {
    expect(endpointsPath({ scope: "production", root: null })).toBe("/api/v0/api/endpoints?scope=production&transport=all");
    expect(endpointsPath({ scope: "all", root: "src/frontend" })).toBe(
      "/api/v0/api/endpoints?scope=all&transport=all&root=src%2Ffrontend",
    );
    expect(endpointPath({ id: "route:a.ts#GET%20%2Fx", scope: "all", root: "." })).toBe(
      "/api/v0/api/endpoint?id=route%3Aa.ts%23GET%2520%252Fx&scope=all&root=.",
    );
  });
  it("keeps an empty status as an empty item", () => {
    expect(callsPath({ transport: "http", statuses: ["", "weird"], scope: "production", root: null })).toBe(
      "/api/v0/api/calls?transport=http&scope=production&status=,weird",
    );
  });
  it("refuses a status with a comma, which the server would split", () => {
    expect(() => callsPath({ transport: "tauri_ipc", statuses: ["a,b"], scope: "all", root: null })).toThrow(/comma/);
    expect(requestableStatuses(["", "a,b", "weird"])).toEqual({ requestable: ["", "weird"], unlistable: ["a,b"] });
  });
});

describe("index_changed", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });
  const changed = () =>
    new Response(JSON.stringify({ error: { code: "index_changed", message: "the index changed while reading; retry" } }), {
      status: 503,
    });

  it("is retried once", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(changed())
      .mockResolvedValueOnce(new Response(JSON.stringify(ENDPOINTS_GOLDENS["api-endpoints-production"]), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    const r = await httpClient.endpoints({ scope: "production", root: null }, {});
    expect(r.repository).toBe("fixture");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("fails after the second one", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValueOnce(changed()).mockResolvedValueOnce(changed());
    vi.stubGlobal("fetch", fetchMock);
    const err = await httpClient.endpoints({ scope: "production", root: null }, {}).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).code).toBe("index_changed");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("does not retry other errors", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(JSON.stringify({ error: { code: "store_error", message: "x" } }), { status: 500 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(httpClient.endpoints({ scope: "production", root: null }, {})).rejects.toBeInstanceOf(ApiError);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
