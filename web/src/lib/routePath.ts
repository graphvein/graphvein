// A route's composed path as it is served (T025 revision). The stored
// template is normalized without a trailing "/" (docs/extract/routes.md);
// FastAPI paths record the real trailing slash as `trailing_slash: true`
// (ADR 0019), and two entries with the same template and different flags
// are distinct paths. Every view that shows a route's full path reads it
// through servedPath so the slash is never dropped.

/** servedPath is a stored `paths` entry's template with its trailing slash
 * restored, or null when the entry has no template. */
export function servedPath(entry: unknown): string | null {
  if (typeof entry !== "object" || entry === null) return null;
  const o = entry as Record<string, unknown>;
  if (typeof o.template !== "string") return null;
  return o.trailing_slash === true && o.template !== "/" ? `${o.template}/` : o.template;
}

// Paths whose prefix the code does not fix (T033, ADR 0084, rows UP-29 and
// UP-33): a route below a configured or unproven prefix stores the proven
// part in `unresolved_prefix_paths`; a Next.js route whose basePath is not
// read keeps a `paths` entry with `resolution: "unresolved"` that holds the
// template without the base (row NX-R4). Neither is a full path: every view
// shows them with "…" for the unknown prefix and never sorts, places or
// tries them as one.

/** PrefixUnknownPath is one path whose prefix is unknown, as shown. */
export interface PrefixUnknownPath {
  /** The display form: above + "…" + template ("…/api/x", "/v1/…/x"). */
  path: string;
  /** The stored reason code (route_prefix_unresolved,
   * route_mount_unresolved, nextjs_base_path_unresolved). */
  reason: string;
  /** The unresolved mount the entry comes from, or null. */
  hop: string | null;
  scope: "test" | "production";
}

/** PREFIX_REASON_TEXT words each reason, as the server does. */
export const PREFIX_REASON_TEXT: Record<string, string> = {
  route_prefix_unresolved: "a prefix on the way to the route is not a constant (for example read from configuration)",
  route_mount_unresolved: "the router is reached through a mount Graphvein does not prove",
  nextjs_base_path_unresolved: "the Next.js basePath is not read as a literal",
};

/** prefixReasonText is "prefix unknown: <why>" for a reason code. */
export function prefixReasonText(reason: string): string {
  const t = PREFIX_REASON_TEXT[reason];
  return t === undefined ? "prefix unknown" : `prefix unknown: ${t}`;
}

/** isFullPathEntry reports a stored `paths` entry that is a full composed
 * path (not a Next.js entry whose basePath is unresolved). */
export function isFullPathEntry(entry: unknown): boolean {
  return typeof entry !== "object" || entry === null || (entry as Record<string, unknown>).resolution !== "unresolved";
}

/** prefixDisplay is the display form of an unknown-prefix path. */
export function prefixDisplay(above: string, template: string, trailingSlash: boolean): string {
  const t = trailingSlash && template !== "/" ? `${template}/` : template;
  return above === "" ? `…${t}` : `${above.replace(/\/$/, "")}/…${t}`;
}

/** prefixUnknownPaths reads a route's paths whose prefix is unknown from
 * its stored attributes, in stored order, without repeats. */
export function prefixUnknownPaths(attrs: Record<string, unknown>): PrefixUnknownPath[] {
  const out: PrefixUnknownPath[] = [];
  const seen = new Set<string>();
  const add = (p: PrefixUnknownPath) => {
    const key = `${p.path}\u0000${p.reason}\u0000${p.scope}`;
    if (!seen.has(key)) {
      seen.add(key);
      out.push(p);
    }
  };
  const entries = (v: unknown): Record<string, unknown>[] =>
    Array.isArray(v) ? v.filter((e): e is Record<string, unknown> => typeof e === "object" && e !== null) : [];
  for (const e of entries(attrs.unresolved_prefix_paths)) {
    if (typeof e.template !== "string") continue;
    const above = typeof e.above === "string" ? e.above : "";
    add({
      path: prefixDisplay(above, e.template, e.trailing_slash === true),
      reason: typeof e.prefix_reason === "string" ? e.prefix_reason : "",
      hop: typeof e.hop === "string" ? e.hop : null,
      scope: e.scope === "test" ? "test" : "production",
    });
  }
  for (const e of entries(attrs.paths)) {
    if (typeof e.template !== "string" || isFullPathEntry(e)) continue;
    add({ path: prefixDisplay("", e.template, e.trailing_slash === true), reason: "nextjs_base_path_unresolved", hop: null, scope: e.scope === "test" ? "test" : "production" });
  }
  return out;
}

/** shownRoutePath is the path a view shows for a route from its stored
 * attributes: the first full composed path as served, else the first path
 * whose prefix the code does not fix ("…" for the prefix), else null. */
export function shownRoutePath(attrs: Record<string, unknown>): string | null {
  const paths = Array.isArray(attrs.paths) ? attrs.paths : [];
  for (const p of paths) {
    if (!isFullPathEntry(p)) continue;
    const s = servedPath(p);
    if (s !== null) return s;
  }
  return prefixUnknownPaths(attrs)[0]?.path ?? null;
}

// Path assumptions: a full composed path may rest on an
// assumption the extractor states on its entry: GR-V5 (a Go wrapper),
// RO-V1 (a FastAPI mount at an unknown prefix added earlier) or RO-V2 (an
// Express app or router mounted earlier whose middleware runs first). The
// texts mirror the server's routes.PathAssumptionText.

/** PATH_ASSUMPTION_TEXT words each path assumption, as the server does. */
export const PATH_ASSUMPTION_TEXT: Record<string, string> = {
  "GR-V5": "the path is served through a wrapper not proven to keep it: assumes the wrapper passes the request on unchanged",
  "RO-V1": "a mount at an unknown prefix may answer first: assumes the mount added before the route, whose prefix the code does not read, does not take this path",
  "RO-V2": "a mounted app's catch-all may answer first: assumes the app or router mounted earlier below this path's prefix passes the request on",
};

/** PathAssumptions are the assumptions one stored full path rests on, with
 * what they name (wrappers, earlier mounts). */
export interface PathAssumptions {
  ids: string[];
  names: string[];
}

/** pathAssumptions reads a stored `paths` entry's assumptions, or null when
 * it rests on none. */
export function pathAssumptions(entry: unknown): PathAssumptions | null {
  if (typeof entry !== "object" || entry === null) return null;
  const o = entry as Record<string, unknown>;
  const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : []);
  const ids = strings(o.assumptions);
  if (ids.length === 0) return null;
  return { ids, names: [...strings(o.served_through), ...strings(o.may_answer_first)] };
}

/** pathAssumptionTitle is the tooltip of a path's assumptions. */
export function pathAssumptionTitle(a: PathAssumptions): string {
  const texts = a.ids.map((id) => `${id}: ${PATH_ASSUMPTION_TEXT[id] ?? "an assumption of the route extraction"}`);
  return a.names.length > 0 ? `${texts.join("; ")} (${a.names.join(", ")})` : texts.join("; ");
}

/** shadowedPaths reads a route's withheld paths (route attribute
 * shadowed_paths): each path with the earlier mount that takes it. */
export function shadowedPaths(attrs: Record<string, unknown>): { path: string; by: string }[] {
  if (!Array.isArray(attrs.shadowed_paths)) return [];
  const out: { path: string; by: string }[] = [];
  for (const e of attrs.shadowed_paths as unknown[]) {
    if (typeof e !== "object" || e === null) continue;
    const o = e as Record<string, unknown>;
    if (typeof o.path === "string" && typeof o.by === "string") out.push({ path: o.path, by: o.by });
  }
  return out;
}

/** PATH_NOT_KNOWN marks a route shown by its pattern as written because no
 * path of any kind is stored for it (neither a full path nor an
 * unknown-prefix path); the server's search labels carry the same marker
 * (JS-4, ADR 0089 Revision 4). */
export const PATH_NOT_KNOWN = "(path not known)";

/** The words for a route with no composed path (ADR 0089 Revision 5), one
 * set for every view, from what the server knows: "shadowed" (an earlier
 * mount takes every composed path), "path withheld" (couldn't-read row
 * CR-44: the Go linker found the registration but another pattern or route
 * may answer its path), else "path not known". "Not mounted" is never
 * said: no stored fact proves a route unreachable from every app. */
export const NO_PATH_WORDS = {
  shadowed: "shadowed",
  withheld: "path withheld",
  unknown: "path not known",
} as const;

/** NO_PATH_TITLE explains each of NO_PATH_WORDS in a tooltip. */
export const NO_PATH_TITLE = {
  shadowed: "every composed path is taken first by a mount added before the route; the route is mounted, no path is stored",
  withheld: "the route is found, but another pattern of the ServeMux, its redirect, a forwarding closure or a route of the router it is mounted on may answer its path, so no path is claimed (Couldn't read: route paths withheld)",
  unknown: "no composed path is stored: the route may not be mounted, or a mount or prefix above it is not resolved; this is not proof that it is unreachable",
} as const;

/** NoPathKind is which of NO_PATH_WORDS applies to a route. */
export type NoPathKind = keyof typeof NO_PATH_WORDS;

/** noPathKind picks the words for a route with no composed path from the
 * server's facts: shadowed first (stored on the route), then withheld
 * (the server's path_withheld flag), else not known. */
export function noPathKind(shadowed: boolean, withheld: boolean): NoPathKind {
  if (shadowed) return "shadowed";
  return withheld ? "withheld" : "unknown";
}
