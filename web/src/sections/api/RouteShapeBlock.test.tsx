// The route detail's Request and response block (T029, OX-40..OX-43).
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import golden from "../../../../internal/server/testdata/route-shape.golden.json";

import { parseRouteShape, type RouteShape, type ShapeProvenance } from "@/api/routeShape";
import { EndpointDetailView } from "@/sections/api/EndpointDetail";
import { RouteShapeBlock } from "@/sections/api/RouteShapeBlock";
import type { RouteFacts } from "@/sections/api/shapeView";
import { detailGolden } from "@/test/apiFixtures";

function shape(): RouteShape {
  const s = parseRouteShape(structuredClone(golden), "shape");
  if (s === null) throw new Error("golden shape not read");
  return s;
}

const facts: RouteFacts = { file: "app.py", line: 3, framework: "fastapi", attributes: {} };
const fact: ShapeProvenance = { kind: "fact", rule: "SH-T1", file: "app.py", line: 3, chain: [], confidence: 1 };

describe("RouteShapeBlock", () => {
  it("shows each read section with its items and chips, and the sections not read in one line", () => {
    render(<RouteShapeBlock shape={shape()} shapeError={null} facts={facts} />);
    const path = screen.getByTestId("section-path");
    expect(path.getAttribute("data-state")).toBe("complete");
    expect(within(path).getByText("id")).toBeTruthy();
    expect(within(path).getAllByTestId("provenance-chip").length).toBeGreaterThan(0);
    expect(screen.getByTestId("section-header").getAttribute("data-state")).toBe("partial");
    expect(screen.getByTestId("section-header").textContent).toContain("dependencies may read more headers");
    // The 200 response's model expands one level, through the shape's models.
    expect(within(screen.getByTestId("section-responses")).getByTestId("schema-fields").textContent).toContain('"kind"');
    // Cookies and the request body are not read: named in the shared line,
    // never as an empty section or "none".
    expect(screen.getByTestId("shape-not-read").textContent).toBe("Not read by Graphvein: Cookies, Request body");
    expect(screen.queryByTestId("section-cookie")).toBeNull();
    expect(screen.queryByTestId("section-none")).toBeNull();
    expect(screen.getByTestId("shape-depth-cap").textContent).toContain("depth cap reached (4); raise index.shape_depth in graphvein.yaml");
  });

  it("lets a long schema field type wrap anywhere, so it never widens its column (layout checked in a browser)", () => {
    // jsdom does no layout: this pins the class that lets a spec reference
    // such as docs/openapi/openapi.json#/components/schemas/Properties
    // wrap, which keeps the Explorer's two columns from overlapping.
    render(<RouteShapeBlock shape={shape()} shapeError={null} facts={facts} />);
    const types = within(screen.getByTestId("section-responses")).getAllByTestId("schema-field-type");
    expect(types.length).toBeGreaterThan(0);
    for (const t of types) expect(t.className).toContain("[overflow-wrap:anywhere]");
  });

  it("says not read for every section of a route without a shape, and never none", () => {
    render(<RouteShapeBlock shape={null} shapeError={null} facts={facts} />);
    expect(screen.getByTestId("shape-none")).toBeTruthy();
    expect(screen.getByTestId("shape-not-read").textContent).toBe("Not read by Graphvein: Path, Query, Headers, Cookies, Request body, Responses, Security");
    expect(screen.queryByTestId("section-item")).toBeNull();
    expect(document.body.textContent).not.toMatch(/None/);
  });

  it("says why a stored shape could not be read", () => {
    render(<RouteShapeBlock shape={null} shapeError="invalid route shape: SM-19" facts={facts} />);
    expect(screen.getByTestId("shape-error").textContent).toContain("invalid route shape: SM-19. Every section reads as not read.");
  });

  it("shows None, proven with its evidence, and None besides the security scheme where the scheme reads", () => {
    const s = shape();
    s.body = { state: "provably_none", reason: null, evidence: fact, items: [] };
    s.header = { state: "provably_none", reason: null, evidence: fact, items: [] };
    render(<RouteShapeBlock shape={s} shapeError={null} facts={facts} />);
    const body = screen.getByTestId("section-body");
    expect(within(body).getByTestId("section-none").textContent).toContain("app.py:3");
    const header = screen.getByTestId("section-header");
    expect(header.getAttribute("data-state")).toBe("none_besides_scheme");
    expect(within(header).getByTestId("read-by-scheme").textContent).toBe("read by the security scheme HTTPBearer (see Security)");
    expect(within(header).queryByTestId("section-none")).toBeNull();
  });
});

describe("EndpointDetailView: Request and response (T029)", () => {
  it("shows the block in an HTTP route's Overview with the Explorer link", async () => {
    const d = detailGolden("api-endpoint-route");
    render(<EndpointDetailView detail={d} summary={undefined} openGap={() => undefined} tab="overview" />);
    expect(await screen.findByTestId("route-shape")).toBeTruthy();
    expect(screen.getByTestId("open-in-explorer").getAttribute("href")).toBe(`#/explorer?route=${encodeURIComponent(d.endpoint.identity)}`);
  });

  it("is not shown for a Tauri command", () => {
    render(<EndpointDetailView detail={detailGolden("api-endpoint-command")} summary={undefined} openGap={() => undefined} tab="overview" />);
    expect(screen.queryByTestId("open-in-explorer")).toBeNull();
  });

  it("names the member in project mode", async () => {
    const d = detailGolden("api-endpoint-route");
    d.project = { callers: [], possible_callers: [], declaration_conflicts: [] } as unknown as NonNullable<typeof d.project>;
    render(<EndpointDetailView detail={d} summary={undefined} openGap={() => undefined} tab="overview" />);
    await screen.findByTestId("route-shape");
    expect(screen.getByText(`in ${d.repository}`)).toBeTruthy();
  });
});
