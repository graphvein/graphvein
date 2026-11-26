// T029 rows MT-20..MT-29: the table detail's "Routes that accept or return
// this model" and the route detail's "also the database table" note, read
// from the server's goldens (TestSchemaGoldens writes them from the
// route-model-tables fixture).
import { render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { parseEndpointDetail } from "@/api/endpoints";
import { parseModelRoutes, parseRouteModelTables, type ModelRoutes } from "@/api/modelTables";
import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { classLabel, ModelTablesNote, tableHash } from "@/sections/api/RouteShapeBlock";
import { absenceText, ModelRoutesPanel, modelUseText, routeApiHash } from "@/sections/schema/ModelRoutes";
import { parseSchemaParams } from "@/sections/schema/params";
import { AppShell } from "@/shell/AppShell";
import { DETAIL_GOLDENS, fakeClient, raw as rawDetail } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, setsGolden, summaryGolden, tableGolden } from "@/test/schemaFixtures";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
const enc = encodeURIComponent;
const TEAM = "table:app/models.py#Team";

function routes(name: "schema-table-model-routes" | "schema-table-model-routes-none"): ModelRoutes {
  const m = tableGolden(name).model_routes;
  if (m === null) throw new Error("no model_routes");
  return m;
}

describe("model_routes", () => {
  it("reads the server's list: the routes using the Hero table class, never its copies", () => {
    const m = routes("schema-table-model-routes");
    expect(m.state).toBe("listed");
    expect(m.class).toBe("class:app/models.py#Hero");
    expect(m.routes.map((r) => `${r.method ?? ""} ${r.path ?? ""}`)).toEqual(["GET /heroes", "POST /heroes", "GET /heroes/{hero_id}"]);
    expect(m.routes.flatMap((r) => r.uses.map((u) => [u.direction, u.section, u.row]))).toEqual([
      ["response", "responses", "MT-23"],
      ["request", "body", "MT-21"],
      ["response", "responses", "MT-23"],
    ]);
    expect(Object.keys(m.links)).toEqual(["app/main.py:10", "app/main.py:15", "app/main.py:20"]);
    expect(tableGolden("schema-table-complete").model_routes).toBeNull();
    expect(parseModelRoutes(undefined, "m")).toBeNull();
  });

  // JX-F2: a route without a full path shows its pattern marked as such.
  it("marks a route without a full path as its pattern, never as its path", () => {
    const m = routes("schema-table-model-routes");
    const first = m.routes[0];
    if (first === undefined) throw new Error("no route");
    const value: ModelRoutes = { ...m, routes: [{ ...first, path: null, pattern: "/heroes" }] };
    render(<ModelRoutesPanel value={value} ide={tableGolden("schema-table-model-routes").ide} tool="sqlmodel" />);
    const row = screen.getByTestId("model-route");
    expect(within(row).getByTestId("model-route-pattern-only").textContent).toBe("pattern as written, no full path");
  });

  it("lists each route with method, path, direction, chip and links to the API section and the Explorer", () => {
    render(<ModelRoutesPanel value={routes("schema-table-model-routes")} ide={tableGolden("schema-table-model-routes").ide} tool="sqlmodel" />);
    const rows = screen.getAllByTestId("model-route");
    expect(rows).toHaveLength(3);
    const post = rows[1];
    if (post === undefined) throw new Error("no row");
    expect(post.textContent).toContain("POST");
    expect(post.textContent).toContain("/heroes");
    expect(within(post).getByTestId("model-use").getAttribute("data-direction")).toBe("request");
    expect(within(post).getByTestId("model-use").textContent).toContain("accepts");
    expect(within(post).getByTestId("model-use").textContent).toContain("request body");
    expect(within(post).getByTestId("provenance-chip").textContent).toContain("main.py:10");
    const api = within(post).getByTestId("model-route-api").getAttribute("href") ?? "";
    expect(api).toBe(`#/api?sel=${enc("route:app/main.py#POST%20%2Fheroes")}&tab=overview`);
    expect(within(post).getByTestId("model-route-explorer").getAttribute("href")).toBe(`#/explorer?route=${enc("route:app/main.py#POST%20%2Fheroes")}`);
    expect(within(rows[0] as HTMLElement).getByTestId("model-use").textContent).toContain("returns");
    expect(within(rows[0] as HTMLElement).getByTestId("model-use").textContent).toContain("response 200");
    // The chips open the use's line in the editor, from the server's links.
    expect(screen.getAllByTestId("chip-ide-button")).toHaveLength(3);
    // 7 routes were not fully read: other uses are not ruled out.
    expect(screen.getByTestId("model-routes-others").textContent).toBe("7 of 7 routes have a request or response part that was not read, so other uses are not ruled out.");
  });

  it("never says none when routes were not fully read", () => {
    const m = routes("schema-table-model-routes-none");
    expect(m.state).toBe("none_found");
    render(<ModelRoutesPanel value={m} ide={null} tool="sqlmodel" />);
    const p = screen.getByTestId("model-routes-unknown");
    expect(p.textContent).toBe("No route whose request and response Graphvein read uses this model. 7 of 7 routes have a part that was not read, so a use there is not ruled out.");
    expect(screen.queryByTestId("model-routes-none")).toBeNull();
    expect(document.body.textContent).not.toContain("No route uses this model");
  });

  it("says none only for the proven state, and not known for the others", () => {
    const base = routes("schema-table-model-routes-none");
    const none = { ...base, state: "none", routes_not_fully_read: 0, routes_total: 2, routes_with_shape: 2 };
    render(<ModelRoutesPanel value={none} ide={null} tool="sqlmodel" />);
    expect(screen.getByTestId("model-routes-none").textContent).toBe("No route of this repository uses this model as a request or response (every request and response of the 2 routes was read).");
    expect(absenceText(none, "sqlmodel", "backend").text).toContain("No route of the repository backend uses this model");
    expect(absenceText({ ...base, state: "not_read" }, "sqlmodel").text).toBe("The routes' request and response shapes were not read, so the routes that use this model are not known.");
    expect(absenceText({ ...base, state: "no_declaration" }, "prisma").text).toContain("does not store for prisma tables");
    expect(absenceText({ ...base, state: "later" }, null)).toEqual({ text: "Unknown state (later): the routes that use this model are not known.", proven: false });
  });

  it("writes where a use is", () => {
    const u = routes("schema-table-model-routes").routes[0]?.uses[0];
    if (u === undefined) throw new Error("no use");
    expect(modelUseText(u)).toBe("response 200");
    expect(modelUseText({ ...u, section: "query", direction: "request", name: "filter", status: null })).toBe("query parameter filter");
    expect(modelUseText({ ...u, via: ["TeamList"], nested: true })).toBe("response 200, inside TeamList");
    expect(routeApiHash({ route: "route:a#GET%20%2F", scope: "test" })).toContain("tests=1");
  });

  it("the table detail shows the section for a model table and not for a migration table", async () => {
    const team = { ...tableGolden("schema-table-model"), model_routes: routes("schema-table-model-routes") };
    const client = fakeSchemaClient({ summary: summaryGolden("models"), sets: setsGolden("models"), tables: { [TEAM]: team } });
    window.history.replaceState(null, "", `#/schema?set=${enc("@models/sqlmodel/.")}&sel=${enc(TEAM)}`);
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
    const section = await screen.findByTestId("model-routes-section");
    await waitFor(() => {
      expect(within(section).getAllByTestId("model-route")).toHaveLength(3);
    });
    expect(section.textContent).toContain("Routes that accept or return this model");
    expect(section.textContent).toContain("same declaration: Team");
  });
});

describe("model_tables on the route detail", () => {
  const hero = { model: "Hero", class: "class:app/models.py#Hero", table: "table:app/models.py#Hero", name: "hero", set: "models/sqlmodel/.", tool: "sqlmodel", row: "MT-28" };

  it("is read when sent and null when absent", () => {
    expect(parseRouteModelTables([hero], "t")).toEqual([hero]);
    expect(parseRouteModelTables(undefined, "t")).toBeNull();
    const d = rawDetail(DETAIL_GOLDENS["api-endpoint-route"]) as Record<string, unknown>;
    // The golden route stores a shape without models: none is a table.
    expect(parseEndpointDetail(d).model_tables).toEqual([]);
    delete d.model_tables;
    expect(parseEndpointDetail(d).model_tables).toBeNull();
    d.model_tables = [hero];
    expect(parseEndpointDetail(d).model_tables).toEqual([hero]);
  });

  it("notes the model that is also a table, with a link to it in the Schema view", () => {
    render(<ModelTablesNote tables={[hero]} />);
    const note = screen.getByTestId("shape-model-table");
    // The class and its file, not the shape's internal model name.
    expect(note.textContent).toContain("The model Hero (app/models.py) is also the database table");
    expect(screen.getByTestId("shape-model-table-link").textContent).toBe("hero");
    const href = screen.getByTestId("shape-model-table-link").getAttribute("href") ?? "";
    expect(href).toBe(tableHash(hero));
    const p = parseSchemaParams(href);
    expect(p.set).toBe("@models/sqlmodel/.");
    expect(p.sel).toBe("table:app/models.py#Hero");
  });

  it("names a model by its class and file, not by the shape's qualified name", () => {
    expect(classLabel("class:app/dup/b.py#Item")).toBe("Item (app/dup/b.py)");
    render(<ModelTablesNote tables={[{ ...hero, model: "Item__app_dup_b", class: "class:app/dup/b.py#Item", table: "table:app/dup/b.py#Item", name: "item_b" }]} />);
    expect(screen.getByTestId("shape-model-table").textContent).toContain("The model Item (app/dup/b.py) is also the database table");
    expect(screen.getByTestId("shape-model-table").textContent).not.toContain("__");
  });

  it("shows nothing when no model is a table or the shape was not read", () => {
    const { container, rerender } = render(<ModelTablesNote tables={[]} />);
    expect(container.textContent).toBe("");
    rerender(<ModelTablesNote tables={null} />);
    expect(container.textContent).toBe("");
  });
});
