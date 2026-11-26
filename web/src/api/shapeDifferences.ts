// shape_differences of a link (T029 W4, rows SD-6..SD-12 of
// docs/contract/claims/shape-differences.md): what a linked frontend call
// sends compared with what the route reads. The server sends it on the
// callers of GET /api/v0/api/endpoint (and the project's callers) and on
// the calls_route edges of GET /api/v0/api/flow, only where both sides
// are fully read. Absent means nothing was compared: the UI then shows
// nothing, never "no differences".

import { ApiFormatError, array, bool, object, parseShapeLinks, str, type IdeLink, type JSONObject } from "@/api/endpoints";

/** One side of a difference: where it is known and how. */
export interface DifferenceSide {
  file: string;
  line: number;
  rule: string;
  /** literal, typed (the call) or fact (the route). */
  kind: string;
}

/** One key one side has and the other does not. */
export interface ShapeDifference {
  row: string;
  section: string;
  /** The key's path in the body ("address.zip") or the query name. */
  field: string;
  direction: "sent_not_read" | "read_not_sent";
  text: string;
  call: DifferenceSide;
  route: DifferenceSide;
}

/** The comparison of one link. */
export interface ShapeDifferences {
  crossRepository: boolean;
  compared: string[];
  differences: ShapeDifference[];
  callLinks: Record<string, IdeLink>;
  routeLinks: Record<string, IdeLink>;
}

function line(obj: JSONObject, path: string): number {
  const v = obj.line;
  if (typeof v !== "number" || !Number.isInteger(v) || v < 1) throw new ApiFormatError(`${path}.line is not a line`);
  return v;
}

function side(value: unknown, path: string): DifferenceSide {
  const o = object(value, path);
  return { file: str(o, "file", path), line: line(o, path), rule: str(o, "rule", path), kind: str(o, "kind", path) };
}

/** parseShapeDifferences reads an optional shape_differences member:
 * null when absent (nothing compared). A direction this version does not
 * know is refused, so a new kind of difference is never shown in the
 * words of another. */
export function parseShapeDifferences(value: unknown, path: string): ShapeDifferences | null {
  if (value === undefined || value === null) return null;
  const o = object(value, path);
  return {
    crossRepository: o.cross_repository === undefined ? false : bool(o, "cross_repository", path),
    compared: array(o.compared, `${path}.compared`).map((c, i) => {
      if (typeof c !== "string") throw new ApiFormatError(`${path}.compared[${String(i)}] is not a string`);
      return c;
    }),
    differences: array(o.differences, `${path}.differences`).map((d, i) => {
      const q = `${path}.differences[${String(i)}]`;
      const dobj = object(d, q);
      const direction = str(dobj, "direction", q);
      if (direction !== "sent_not_read" && direction !== "read_not_sent") throw new ApiFormatError(`${q}.direction ${JSON.stringify(direction)} is not known`);
      return {
        row: str(dobj, "row", q),
        section: str(dobj, "section", q),
        field: str(dobj, "field", q),
        direction,
        text: str(dobj, "text", q),
        call: side(dobj.call, `${q}.call`),
        route: side(dobj.route, `${q}.route`),
      };
    }),
    callLinks: parseShapeLinks(o.call_links, `${path}.call_links`),
    routeLinks: parseShapeLinks(o.route_links, `${path}.route_links`),
  };
}

/** hasShapeDifferences says whether a comparison has a difference to show. */
export function hasShapeDifferences(d: ShapeDifferences | null | undefined): d is ShapeDifferences {
  return d !== null && d !== undefined && d.differences.length > 0;
}

/** LinkedCallDifferences are the differences of one linked call of a
 * route, for a view that lists the route's links (the Explorer). */
export interface LinkedCallDifferences {
  /** The call's name and site, as the callers table shows them. */
  name: string;
  file: string;
  line: number | null;
  diffs: ShapeDifferences;
}
