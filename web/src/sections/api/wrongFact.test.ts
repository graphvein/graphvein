import { describe, expect, it } from "vitest";

import type { EndpointDetail, PresentedNode } from "@/api/endpoints";
import { endpointFact, endpointText } from "@/sections/api/wrongFact";

// Synthetic routes in dozzle's shapes (T040 JX-F2): every API route sits
// under the non-constant mount r.Route(base, ...), so its stored pattern
// is the part below the mount ("/rules") and its proven path is kept with
// "…" for the unknown prefix.
function node(attributes: Record<string, unknown>): PresentedNode {
  return {
    identity: "route:internal/web/routes.go#GET%20%2Frules",
    kind: "route",
    level: 3,
    name: "GET /rules",
    qualified_name: "",
    file: "internal/web/routes.go",
    span: { start_line: 253, start_column: 1, end_line: 253, end_column: 40 },
    content_hash: "h",
    attributes,
    ide: null,
    file_state: "unchanged",
    source_root: null,
  };
}

function detail(attributes: Record<string, unknown>): EndpointDetail {
  // Only the fields the fact reads; the rest of the detail is not used.
  return { repository: "dozzle", endpoint: node(attributes), handler: { state: "unresolved", expression: null, node: null, edge: null } } as unknown as EndpointDetail;
}

const PREFIXED = {
  method: "GET",
  pattern: "/rules",
  paths: [],
  unresolved_prefix_paths: [{ above: "", template: "/api/notifications/rules", hop: "mount:internal/web/routes.go#createRouter%20r.Route%28base%29", prefix_reason: "route_prefix_unresolved", scope: "production" }],
};

describe("endpointText", () => {
  it("writes a route without a full path as its unknown-prefix path, never as its pattern", () => {
    expect(endpointText(detail(PREFIXED))).toBe("GET …/api/notifications/rules (prefix unknown)");
    const fact = endpointFact(detail(PREFIXED)).fact;
    expect(fact).toContain("GET …/api/notifications/rules");
    expect(fact).not.toContain("GET /rules");
  });

  it("keeps the known part above the unknown prefix", () => {
    const a = { ...PREFIXED, unresolved_prefix_paths: [{ above: "/v1", template: "/rules/{id}", prefix_reason: "route_mount_unresolved", scope: "production" }] };
    expect(endpointText(detail(a))).toBe("GET /v1/…/rules/{id} (prefix unknown)");
  });

  it("writes a full path as served", () => {
    expect(endpointText(detail({ method: "GET", pattern: "/rules", paths: [{ template: "/api/rules", scope: "production" }] }))).toBe("GET /api/rules");
  });

  it("marks a pattern without any path as path not known, or shadowed", () => {
    expect(endpointText(detail({ method: "GET", pattern: "/rules", paths: [] }))).toBe("GET /rules (path not known)");
    // owncast (JX-R2 A2): a catch-all on the root router whose path is
    // withheld is mounted; the fact never says "not mounted".
    const owncast = endpointText(detail({ method: "ANY", pattern: "/*", paths: [] }));
    expect(owncast).toBe("ANY /* (path not known)");
    expect(owncast).not.toContain("not mounted");
    // JX-F4: with the server's path_withheld mark (row CR-44) the fact
    // says the path is withheld.
    expect(endpointText({ ...detail({ method: "ANY", pattern: "/*", paths: [] }), path_withheld: true })).toBe("ANY /* (path withheld)");
    expect(endpointText(detail({ method: "GET", pattern: "/rules", paths: [], shadowed_paths: [{ path: "/rules", by: "app.use" }] }))).toBe("GET /rules (pattern as written, shadowed)");
  });
});
