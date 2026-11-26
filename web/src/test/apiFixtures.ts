// The Go server's API section goldens (internal/server/testdata), imported
// so the TypeScript guards and views are tested against what the server
// really sends; a changed golden changes what these tests parse.
import callsHttp from "../../../internal/server/testdata/api-calls-http-gaps.golden.json";
import callsTauri from "../../../internal/server/testdata/api-calls-tauri-gaps.golden.json";
import endpointAction from "../../../internal/server/testdata/api-endpoint-action.golden.json";
import endpointCommand from "../../../internal/server/testdata/api-endpoint-command.golden.json";
import endpointAmbiguous from "../../../internal/server/testdata/api-endpoint-route-ambiguous.golden.json";
import endpointNever from "../../../internal/server/testdata/api-endpoint-route-never.golden.json";
import endpointTestOnly from "../../../internal/server/testdata/api-endpoint-route-test-only.golden.json";
import endpointRoute from "../../../internal/server/testdata/api-endpoint-route.golden.json";
import endpointsAll from "../../../internal/server/testdata/api-endpoints-all.golden.json";
import endpointsProduction from "../../../internal/server/testdata/api-endpoints-production.golden.json";
import endpointsRoot from "../../../internal/server/testdata/api-endpoints-root.golden.json";
import statusesCalls from "../../../internal/server/testdata/api-statuses-calls.golden.json";
import statusesCommand from "../../../internal/server/testdata/api-statuses-endpoint-command.golden.json";
import statusesRoute from "../../../internal/server/testdata/api-statuses-endpoint-route.golden.json";
import statusesEndpoints from "../../../internal/server/testdata/api-statuses-endpoints.golden.json";
import upEndpoint from "../../../internal/server/testdata/unknown-prefix-endpoint.golden.json";
import upEndpoints from "../../../internal/server/testdata/unknown-prefix-endpoints.golden.json";
import upNextEndpoint from "../../../internal/server/testdata/unknown-prefix-next-endpoint.golden.json";

import { ApiError } from "@/api/client";
import {
  parseCalls,
  parseEndpointDetail,
  parseEndpoints,
  type ApiClient,
  type CallsParams,
  type CallsResponse,
  type EndpointDetail,
  type EndpointsResponse,
} from "@/api/endpoints";

export const ENDPOINTS_GOLDENS = {
  "api-endpoints-production": endpointsProduction,
  "api-endpoints-all": endpointsAll,
  "api-endpoints-root": endpointsRoot,
  "api-statuses-endpoints": statusesEndpoints,
  "unknown-prefix-endpoints": upEndpoints,
} as const;

export const DETAIL_GOLDENS = {
  "api-endpoint-route": endpointRoute,
  "api-endpoint-route-never": endpointNever,
  "api-endpoint-route-ambiguous": endpointAmbiguous,
  "api-endpoint-route-test-only": endpointTestOnly,
  "api-endpoint-command": endpointCommand,
  "api-endpoint-action": endpointAction,
  "api-statuses-endpoint-route": statusesRoute,
  "api-statuses-endpoint-command": statusesCommand,
  "unknown-prefix-endpoint": upEndpoint,
  "unknown-prefix-next-endpoint": upNextEndpoint,
} as const;

export const CALLS_GOLDENS = {
  "api-calls-http-gaps": callsHttp,
  "api-calls-tauri-gaps": callsTauri,
  "api-statuses-calls-http": statusesCalls.http,
  "api-statuses-calls-tauri": statusesCalls.tauri_ipc,
} as const;

/** raw returns a mutable deep copy of a golden's JSON. */
export function raw<T>(golden: T): T {
  return structuredClone(golden);
}

export function endpointsGolden(name: keyof typeof ENDPOINTS_GOLDENS): EndpointsResponse {
  return parseEndpoints(raw(ENDPOINTS_GOLDENS[name]));
}

export function detailGolden(name: keyof typeof DETAIL_GOLDENS): EndpointDetail {
  return parseEndpointDetail(raw(DETAIL_GOLDENS[name]));
}

export function callsGolden(name: keyof typeof CALLS_GOLDENS): CallsResponse {
  return parseCalls(raw(CALLS_GOLDENS[name]));
}

/** A request the fake client received. */
export type Request = { kind: "endpoints" | "endpoint" | "calls"; params: unknown };

/** fakeClient answers from the fixture goldens and records requests:
 * production -> api-endpoints-production, all -> api-endpoints-all; the
 * detail of any identity found in a detail golden, else 404; calls from
 * the gap goldens filtered by status and root. */
export function fakeClient(requests: Request[] = []): ApiClient {
  const details = Object.values(DETAIL_GOLDENS)
    .slice(0, 5)
    .map((g) => parseEndpointDetail(raw(g)));
  return {
    endpoints: (p) => {
      requests.push({ kind: "endpoints", params: p });
      return Promise.resolve(endpointsGolden(p.scope === "production" ? "api-endpoints-production" : "api-endpoints-all"));
    },
    endpoint: (p) => {
      requests.push({ kind: "endpoint", params: p });
      const d = details.find((x) => x.endpoint.identity === p.id);
      if (d === undefined) return Promise.reject(new ApiError("http", "not_found: no API endpoint", 404, "not_found"));
      return Promise.resolve(structuredClone(d));
    },
    calls: (p: CallsParams) => {
      requests.push({ kind: "calls", params: p });
      const res = callsGolden(p.transport === "http" ? "api-calls-http-gaps" : "api-calls-tauri-gaps");
      res.calls = res.calls.filter((c) => {
        const s = typeof c.node.attributes.status === "string" ? c.node.attributes.status : "";
        return p.statuses.includes(s) && (p.root === null || c.node.source_root === p.root);
      });
      res.total = res.calls.length;
      res.statuses = [...p.statuses];
      res.root = p.root;
      return Promise.resolve(res);
    },
  };
}
