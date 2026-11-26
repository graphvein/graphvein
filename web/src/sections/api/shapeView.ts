// The route detail's "Request and response" block (T029, rows OX-40..OX-43
// of docs/contract/claims/try-it.md): the sections of a route's stored
// shape as GET /api/v0/api/endpoint serves it, in the Explorer's views
// (sections/explorer/model.ts) so both places show the same states, chips
// and words. Pure functions, tested without rendering.
//
// The Explorer's rule holds here too: absence is shown only where the
// shape proves it. A route without a stored shape (or with one that could
// not be read) reads "not read" in every section; "None, proven" comes only
// from a stored provably_none state with its evidence, and becomes "None
// besides the security scheme" in Headers, Query and Cookies when a scheme
// that applies reads, or may read, there.

import type { RouteShape, ShapeChain, ShapeParam, ShapeProvenance, ShapeScheme } from "@/api/routeShape";
import { classRead, componentRead, SECTION_KEYS, shapeTypeText, type ItemView, type SchemeRead, type SectionKey, type SectionStatus, type SectionView } from "@/sections/explorer/model";

/** RouteFacts are the stored facts of the route the block needs besides
 * its shape: where it is registered (the fallback provenance of a name a
 * security dependency reads) and its auth attributes as stored. */
export interface RouteFacts {
  file: string;
  line: number;
  framework: string | null;
  attributes: Record<string, unknown>;
}

/** registration is the provenance of a fact of the route's registration. */
export function registration(r: RouteFacts): ShapeProvenance {
  return { kind: "fact", rule: `${r.framework ?? "route"} registration`, file: r.file, line: r.line, chain: [], confidence: 1 };
}

function stored(shape: RouteShape, key: SectionKey): SectionStatus {
  const s = shape[key];
  if (s.state === "provably_none") return { kind: "provably_none", evidence: s.evidence };
  if (s.state === "partial") return { kind: "partial", reason: s.reason };
  if (s.state === "complete") return { kind: "complete", evidence: s.evidence };
  return { kind: "not_read" };
}

function base(name: string, type: string, provenance: ShapeProvenance): ItemView {
  return { name, type, schema: null, presence: null, access: null, provenance, inDocument: true, row: null, why: null, notes: [] };
}

function schemaRef(schema: ShapeParam["schema"]): ItemView["schema"] {
  if (schema === null) return null;
  return schema.type === "object" || schema.type === "ref" || schema.type === "array" ? { kind: "shape", schema } : null;
}

/** shapeItems lists a section's stored items in the Explorer's words. */
export function shapeItems(shape: RouteShape, key: SectionKey): ItemView[] {
  switch (key) {
    case "path":
    case "query":
    case "header":
    case "cookie":
      return shape[key].items.map((p) => ({ ...base(p.name, shapeTypeText(p.schema), p.provenance), schema: schemaRef(p.schema), presence: p.presence, access: p.access }));
    case "body":
      return shape.body.items.map((c) => ({ ...base(c.content_type ?? "content type not read", c.schema === null ? "schema not read" : shapeTypeText(c.schema), c.provenance), schema: schemaRef(c.schema) }));
    case "responses":
      return shape.responses.items
        .map((r) => ({
          ...base(r.status, r.schema === null ? "body not read" : r.schema.type === "null" ? "no body (proven empty)" : shapeTypeText(r.schema), r.provenance),
          schema: schemaRef(r.schema),
          notes: r.content_type !== null ? [r.content_type] : [],
        }))
        .sort((a, b) => a.name.localeCompare(b.name));
    case "security":
      return shape.security.items.map((req) =>
        base(
          req.schemes.length === 0 ? "anonymous" : req.schemes.map((s) => s.name).join(" + "),
          req.schemes.length === 0 ? "no credentials (a proven alternative)" : req.schemes.length === 1 ? "scheme" : "all of these schemes",
          req.provenance,
        ),
      );
  }
}

/** schemeComponent is a stored scheme in the Security Scheme Object's
 * field names, for the Explorer's componentRead. */
function schemeComponent(s: ShapeScheme): Record<string, unknown> {
  return { type: s.type, in: s.in, name: s.param_name };
}

/** depProvenance is where a FastAPI security dependency is declared (its
 * chain's first step), else the route's registration. */
function depProvenance(dep: Record<string, unknown>, r: RouteFacts): ShapeProvenance {
  const chain = dep.chain;
  const step: unknown = Array.isArray(chain) ? chain[0] : undefined;
  if (typeof step === "object" && step !== null) {
    const o = step as Record<string, unknown>;
    const span = typeof o.span === "object" && o.span !== null ? (o.span as Record<string, unknown>) : null;
    if (typeof o.file === "string" && span !== null && typeof span.start_line === "number") {
      return { ...registration(r), file: o.file, line: span.start_line };
    }
  }
  return registration(r);
}

/** shapeSchemeReads lists the security schemes that apply to the route and
 * what each reads (the Explorer's schemeReads over the stored shape): the
 * schemes its stored requirements name, described by the shape's
 * security_schemes, then its declared FastAPI security dependencies. A
 * scheme nothing describes reads from a location not known, never
 * "nothing". */
export function shapeSchemeReads(shape: RouteShape | null, r: RouteFacts): SchemeRead[] {
  const out = new Map<string, SchemeRead>();
  const raw = r.attributes.security_dependencies;
  const deps = (Array.isArray(raw) ? raw : []).filter((d): d is Record<string, unknown> => typeof d === "object" && d !== null);
  const depClass = (name: string): string | null => {
    for (const d of deps) if ((d.scheme_name === name || d.class === name) && typeof d.class === "string") return d.class;
    return null;
  };
  const add = (name: string, provenance: ShapeProvenance) => {
    if (out.has(name)) return;
    const component = shape?.security_schemes.find((s) => s.name === name);
    const cls = depClass(name);
    const read = component !== undefined ? componentRead(schemeComponent(component)) : cls !== null ? classRead(cls) : { location: null, name: null };
    out.set(name, { scheme: name, ...read, provenance });
  };
  for (const req of shape?.security.items ?? []) for (const s of req.schemes) add(s.name, req.provenance);
  for (const d of deps) {
    const name = typeof d.scheme_name === "string" ? d.scheme_name : typeof d.class === "string" ? d.class : null;
    if (name !== null) add(name, depProvenance(d, r));
  }
  return [...out.values()];
}

/** routeShapeViews is every section of the route detail's block, in the
 * Explorer's order. shape null (none stored, or not readable) is every
 * section not read; the names a security dependency reads are still
 * listed, since the route's auth attributes are stored facts of their own. */
export function routeShapeViews(shape: RouteShape | null, r: RouteFacts): Record<SectionKey, SectionView> {
  const reads = shapeSchemeReads(shape, r);
  const out = {} as Record<SectionKey, SectionView>;
  for (const key of SECTION_KEYS) {
    const status: SectionStatus = shape === null ? { kind: "not_read" } : stored(shape, key);
    const items = shape === null ? [] : shapeItems(shape, key);
    const view: SectionView = { key, status, items };
    if (key === "header" || key === "query" || key === "cookie") {
      const schemeRows: ItemView[] = reads
        .filter((x) => x.location === key)
        .map((x) => ({ ...base(x.name ?? "name not stored", "read by the security scheme (see Security)", x.provenance ?? registration(r)), inDocument: false, readBy: x.scheme }));
      view.items = [...schemeRows, ...items];
      const here = reads.filter((x) => x.location === key || x.location === null);
      if (status.kind === "provably_none" && here.length > 0) view.status = { kind: "none_besides_scheme", evidence: status.evidence, schemes: here.map((x) => x.scheme) };
    }
    out[key] = view;
  }
  return out;
}

/** depthCapChains are the followed chains cut at the run's depth limit
 * (SM-23): shown as "depth cap reached (N)" with where to raise it. */
export function depthCapChains(shape: RouteShape | null): ShapeChain[] {
  return shape === null ? [] : shape.follow.chains.filter((c) => c.stop === "depth_cap");
}
