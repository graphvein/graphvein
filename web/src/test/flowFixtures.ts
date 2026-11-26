// The Go server's flow goldens (internal/server/testdata, ADR 0045),
// imported so the Flow tab's guard and graph building are tested against
// what the server really sends.
import flowCommand from "../../../internal/server/testdata/flow-command.golden.json";
import flowGraph from "../../../internal/server/testdata/flow-graph.golden.json";
import flowAmbiguous from "../../../internal/server/testdata/flow-route-ambiguous.golden.json";
import flowLiteralOrigin from "../../../internal/server/testdata/flow-route-literal-origin.golden.json";
import flowNever from "../../../internal/server/testdata/flow-route-never.golden.json";
import flowSuffix from "../../../internal/server/testdata/flow-route-suffix.golden.json";
import flowTables from "../../../internal/server/testdata/flow-tables.golden.json";
import flowTestOnly from "../../../internal/server/testdata/flow-route-test-only.golden.json";
import flowRoute from "../../../internal/server/testdata/flow-route.golden.json";

import { ApiError } from "@/api/client";
import { parseFlow, type FlowClient, type FlowParams, type FlowResponse } from "@/api/flow";

export const FLOW_GOLDENS = {
  "flow-route": flowRoute,
  "flow-route-ambiguous": flowAmbiguous,
  "flow-route-literal-origin": flowLiteralOrigin,
  "flow-route-never": flowNever,
  "flow-route-suffix": flowSuffix,
  "flow-route-test-only": flowTestOnly,
  "flow-command": flowCommand,
  "flow-graph": flowGraph,
  "flow-tables": flowTables,
} as const;

export type FlowGoldenName = keyof typeof FLOW_GOLDENS;

/** flowGolden parses a golden (a fresh copy each call). */
export function flowGolden(name: FlowGoldenName): FlowResponse {
  return parseFlow(structuredClone(FLOW_GOLDENS[name]));
}

/** fakeFlowClient answers the flow of any endpoint found in a golden of
 * the api-section fixture, else 404, and records the requests. */
export function fakeFlowClient(requests: FlowParams[] = []): FlowClient {
  const all = (Object.keys(FLOW_GOLDENS) as FlowGoldenName[]).filter((n) => n !== "flow-graph" && n !== "flow-tables").map(flowGolden);
  return {
    flow: (p) => {
      requests.push(p);
      const f = all.find((x) => x.endpoint.identity === p.id);
      if (f === undefined) return Promise.reject(new ApiError("http", "not_found: no API endpoint", 404, "not_found"));
      return Promise.resolve({ ...structuredClone(f), scope: p.scope, root: p.root });
    },
  };
}
