// The Start here examples (T045, claim rows HP-1 to HP-3 and HP-D),
// picked from the server's API section goldens.
import { describe, expect, it } from "vitest";

import type { ApiClient, EndpointsResponse } from "@/api/endpoints";
import { parseApiParams } from "@/sections/api/params";
import { confidenceNumber } from "@/lib/labels";
import { factExample, inferredCandidates, loadExamples, unknownExample } from "@/sections/help/examples";
import { detailGolden, endpointsGolden, fakeClient, type Request } from "@/test/apiFixtures";

const signal = () => new AbortController().signal;

function onlyTransports(res: EndpointsResponse, keep: string[]): EndpointsResponse {
  return { ...res, transports: res.transports.filter((t) => keep.includes(t.transport)) };
}

describe("Start here examples", () => {
  it("picks the first full HTTP route as the Fact, else the first endpoint", () => {
    const res = endpointsGolden("api-endpoints-production");
    const fact = factExample(res);
    expect(fact).toMatchObject({ subject: "GET /t016d-api/t016d-users", label: "Fact · 1.0", detail: "from its registration", row: "HP-1" });
    const p = parseApiParams(fact?.href ?? "");
    expect(p.sel).toBe("route:server/app.ts#GET%20%2Ft016d-api%2Ft016d-users");
    expect(p.tab).toBe("overview");

    const actions = factExample(onlyTransports(res, ["server_action"]));
    expect(actions).toMatchObject({ subject: "t016dSave", detail: "from its 'use server' directive" });
    expect(factExample({ ...res, transports: [] })).toBeNull();
  });

  it("picks the first inferred caller of the first called routes, at most three", async () => {
    const requests: Request[] = [];
    const ex = await loadExamples(fakeClient(requests), signal());
    // The first called route has no detail in the fake client (404): it is
    // skipped, and the next one's first caller is the example.
    expect(ex.inferred).toMatchObject({ subject: "GET /t016d-items", label: "Inferred · 0.9", detail: "called from root-call.ts:2", row: "HP-2" });
    const p = parseApiParams(ex.inferred?.href ?? "");
    expect(p.sel).toBe("route:server/app.ts#GET%20%2Ft016d-items");
    expect(p.tab).toBe("callers");
    expect(p.peek).toBe(detailGolden("api-endpoint-route").callers[0]?.node.identity);
    expect(requests.filter((r) => r.kind === "endpoint")).toHaveLength(2);
    expect(inferredCandidates(endpointsGolden("api-endpoints-production"))).toHaveLength(3);
  });

  it("writes the stored confidence in the one number format", () => {
    expect(confidenceNumber(1)).toBe("1.0");
    expect(confidenceNumber(0.7)).toBe("0.7");
    expect(confidenceNumber(0.875)).toBe("0.875");
  });

  it("counts dynamic HTTP calls as the Unknown, only when positive", () => {
    const res = endpointsGolden("api-endpoints-production");
    const unknown = unknownExample(res);
    expect(unknown).toMatchObject({ subject: "3 calls whose URL is built at runtime", label: "Unknown", row: "HP-3" });
    expect(parseApiParams(unknown?.href ?? "").gap).toEqual({ transport: "http", category: "dynamic" });
    const none = structuredClone(res);
    for (const t of none.transports) if (t.transport === "http") t.calls.by_status.dynamic = 0;
    expect(unknownExample(none)).toBeNull();
  });

  it("ignores callers without an inferred edge and other transports' counts", async () => {
    const res = endpointsGolden("api-endpoints-production");
    const detail = detailGolden("api-endpoint-route");
    for (const c of detail.callers) for (const e of c.edges) e.provenance = "fact";
    const client: ApiClient = {
      endpoints: () => Promise.resolve(structuredClone(res)),
      endpoint: () => Promise.resolve(structuredClone(detail)),
      calls: () => Promise.reject(new Error("not used")),
    };
    const ex = await loadExamples(client, signal());
    expect(ex.inferred).toBeNull();
    // Every called route was read: the index provably has no Inferred
    // example (the page then says "none of this kind yet").
    const called = res.transports.flatMap((t) => t.endpoints).filter((r) => r.transport === "http" && r.caller_count > 0).length;
    expect(ex.inferredSearchedAll).toBe(called <= 3);
    // A failed detail request proves nothing.
    const failing: ApiClient = { ...client, endpoint: () => Promise.reject(new Error("down")) };
    expect((await loadExamples(failing, signal())).inferredSearchedAll).toBe(false);
    // Tauri has dynamic calls too; only HTTP's count is the example.
    expect(unknownExample(onlyTransports(res, ["tauri_ipc"]))).toBeNull();
  });
});
