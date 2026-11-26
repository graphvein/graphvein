// The Explorer's state in the hash (T029):
//   #/explorer?view=spec&group=tag&op=<METHOD path key>&q=<filter>&route=<id>&edit=<editor>&prefix=<mount key>
// view (default operations), group (default file), op (the expanded
// operation a link points at), q (the list filter), route (a route
// identity a link from the API section points at: the Explorer opens the
// operation the document writes for it, then writes op instead) and edit
// (an editor a link from Settings opens: path-values scrolls to the path
// value editors, environments opens the try it environment picker).
// prefix (T040) is the key of a missing prefix a link from "What
// Graphvein couldn't read" points at: the Explorer opens that hop's value
// editor.

import { parseHashQuery } from "@/router";
import type { GroupBy } from "@/sections/explorer/model";

export type ExplorerView = "operations" | "spec";

/** ExplorerEdit is an editor a Settings link opens (T039 ST-F). */
export type ExplorerEdit = "path-values" | "environments";

export interface ExplorerParams {
  view: ExplorerView;
  group: GroupBy;
  op: string | null;
  q: string;
  route: string | null;
  edit: ExplorerEdit | null;
  prefix: string | null;
}

export function parseExplorerParams(hash: string): ExplorerParams {
  const q = parseHashQuery(hash);
  return {
    view: q.get("view") === "spec" ? "spec" : "operations",
    group: q.get("group") === "tag" ? "tag" : "file",
    op: q.get("op") || null,
    q: q.get("q") ?? "",
    route: q.get("route") || null,
    edit: q.get("edit") === "path-values" ? "path-values" : q.get("edit") === "environments" ? "environments" : null,
    prefix: q.get("prefix") || null,
  };
}

/** explorerHash writes params back; defaults are left out. */
export function explorerHash(p: ExplorerParams): string {
  const parts: string[] = [];
  if (p.view !== "operations") parts.push(`view=${p.view}`);
  if (p.group !== "file") parts.push(`group=${p.group}`);
  if (p.op !== null) parts.push(`op=${encodeURIComponent(p.op)}`);
  if (p.q !== "") parts.push(`q=${encodeURIComponent(p.q)}`);
  if (p.route !== null) parts.push(`route=${encodeURIComponent(p.route)}`);
  if (p.edit !== null) parts.push(`edit=${p.edit}`);
  if (p.prefix !== null) parts.push(`prefix=${encodeURIComponent(p.prefix)}`);
  return `#/explorer${parts.length > 0 ? `?${parts.join("&")}` : ""}`;
}

/** explorerRouteHash is the Explorer link of a route (the API section's
 * "Open in Explorer"): the Explorer opens the operation the generated
 * document writes for this route identity, or says why it has none. */
export function explorerRouteHash(route: string): string {
  return explorerHash({ view: "operations", group: "file", op: null, q: "", route, edit: null, prefix: null });
}

/** explorerEditHash is the Settings link to an Explorer editor: the path
 * value editors or the try it environment picker. */
export function explorerEditHash(edit: ExplorerEdit): string {
  return explorerHash({ view: "operations", group: "file", op: null, q: "", route: null, edit, prefix: null });
}

/** explorerPrefixHash is the Explorer link that opens the value editor of
 * one missing prefix (a mount hop's key, as GET
 * /api/v0/explorer/path-values lists it). */
export function explorerPrefixHash(key: string): string {
  return explorerHash({ view: "operations", group: "file", op: null, q: "", route: null, edit: null, prefix: key });
}
