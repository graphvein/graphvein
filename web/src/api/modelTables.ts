// Routes that accept or return a database model (T029, rows MT-20..MT-29
// of docs/contract/claims/route-model-types.md): the model_routes field of
// GET /api/v0/schema/table (model tables) and the model_tables field of
// GET /api/v0/api/endpoint (HTTP routes with a shape). The server proves
// that a shape's model and the table are the same declaration node; these
// guards only check the JSON types. A state this UI does not know is kept
// as its raw string and shown as unknown, never as "none".

import { ApiFormatError, array, bool, object, parseShapeLinks, str, strOrNull, type IdeLink, type JSONObject } from "@/api/endpoints";
import { parseProvenance, type ShapeProvenance } from "@/api/routeShape";

/** One place of a route's shape that refers to the model (MT-21..MT-24). */
export interface ModelUse {
  /** request (body or a parameter) or response; kept raw. */
  direction: string;
  /** The shape section: body, responses, path, query, header or cookie. */
  section: string;
  /** The parameter's name (parameters only). */
  name: string | null;
  /** The response status (responses only). */
  status: string | null;
  content_type: string | null;
  /** The models the reference was reached through, outermost first. */
  via: string[];
  /** True when the reference is inside an object's fields. */
  nested: boolean;
  provenance: ShapeProvenance;
  row: string;
}

/** One route whose shape uses the model (MT-25). */
export interface ModelRoute {
  route: string;
  method: string | null;
  path: string | null;
  pattern: string | null;
  framework: string | null;
  scope: unknown;
  /** The name the route's shape gives the model. */
  model: string;
  uses: ModelUse[];
}

/** model_routes of a model table (MT-25..MT-27). */
export interface ModelRoutes {
  /** listed | none | none_found | not_read | no_declaration; kept raw. */
  state: string;
  row: string;
  /** The class node the table is declared by. */
  class: string | null;
  routes: ModelRoute[];
  routes_total: number;
  routes_with_shape: number;
  routes_not_fully_read: number;
  /** The editor link of each use's "file:line"; empty when none. */
  links: Record<string, IdeLink>;
}

/** One model of a route's shape that is also a database table (MT-28). */
export interface RouteModelTable {
  model: string;
  class: string;
  table: string;
  name: string | null;
  set: string | null;
  tool: string | null;
  row: string;
}

function nonNegative(o: JSONObject, key: string, path: string): number {
  const v = o[key];
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) throw new ApiFormatError(`${path}.${key} is not a count`);
  return v;
}

function strings(value: unknown, path: string): string[] {
  return array(value, path).map((v, i) => {
    if (typeof v !== "string") throw new ApiFormatError(`${path}[${String(i)}] is not a string`);
    return v;
  });
}

function parseUse(value: unknown, path: string): ModelUse {
  const o = object(value, path);
  return {
    direction: str(o, "direction", path),
    section: str(o, "section", path),
    name: strOrNull(o, "name", path),
    status: strOrNull(o, "status", path),
    content_type: strOrNull(o, "content_type", path),
    via: strings(o.via, `${path}.via`),
    nested: bool(o, "nested", path),
    provenance: parseProvenance(o.provenance, `${path}.provenance`),
    row: str(o, "row", path),
  };
}

/** parseModelRoutes reads model_routes; absent (a migration table, or an
 * older server) is null. */
export function parseModelRoutes(value: unknown, path: string): ModelRoutes | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  return {
    state: str(o, "state", path),
    row: str(o, "row", path),
    class: strOrNull(o, "class", path),
    routes: array(o.routes, `${path}.routes`).map((r, i) => {
      const rp = `${path}.routes[${String(i)}]`;
      const ro = object(r, rp);
      return {
        route: str(ro, "route", rp),
        method: strOrNull(ro, "method", rp),
        path: strOrNull(ro, "path", rp),
        pattern: strOrNull(ro, "pattern", rp),
        framework: strOrNull(ro, "framework", rp),
        scope: ro.scope,
        model: str(ro, "model", rp),
        uses: array(ro.uses, `${rp}.uses`).map((u, j) => parseUse(u, `${rp}.uses[${String(j)}]`)),
      };
    }),
    routes_total: nonNegative(o, "routes_total", path),
    routes_with_shape: nonNegative(o, "routes_with_shape", path),
    routes_not_fully_read: nonNegative(o, "routes_not_fully_read", path),
    links: parseShapeLinks(o.links, `${path}.links`),
  };
}

/** parseRouteModelTables reads model_tables; absent (no shape, or an
 * older server) is null. */
export function parseRouteModelTables(value: unknown, path: string): RouteModelTable[] | null {
  if (value === undefined || value === null) return null;
  return array(value, path).map((v, i) => {
    const p = `${path}[${String(i)}]`;
    const o = object(v, p);
    return {
      model: str(o, "model", p),
      class: str(o, "class", p),
      table: str(o, "table", p),
      name: strOrNull(o, "name", p),
      set: strOrNull(o, "set", p),
      tool: strOrNull(o, "tool", p),
      row: str(o, "row", p),
    };
  });
}
