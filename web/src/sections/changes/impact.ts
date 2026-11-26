// Pure helpers of the Impact tab: paths grouped by relation and wording,
// a long fan-out folded by directory (V3 mockup "fan-out folding"), and the
// location a path's last step was read from. Nothing here adds or drops a
// path: folded paths stay one click away and are counted.

import type { ImpactPath } from "@/api/changes";
import { fileLine, identityParts } from "@/sections/changes/words";

export interface PathGroup {
  key: string;
  relation: string;
  wording: string;
  paths: ImpactPath[];
}

/** groupPaths groups paths by relation and wording in first-seen order;
 * inside a group by distance, then node. */
export function groupPaths(paths: readonly ImpactPath[]): PathGroup[] {
  const groups = new Map<string, PathGroup>();
  for (const p of paths) {
    const key = `${p.relation}\u0000${p.wording}`;
    let g = groups.get(key);
    if (g === undefined) {
      g = { key, relation: p.relation, wording: p.wording, paths: [] };
      groups.set(key, g);
    }
    g.paths.push(p);
  }
  for (const g of groups.values()) g.paths.sort((a, b) => a.distance - b.distance || (a.node < b.node ? -1 : a.node > b.node ? 1 : 0));
  return [...groups.values()];
}

/** FOLD_AT: a group with more paths than this shows SHOW_FIRST and folds
 * the rest by directory. */
export const FOLD_AT = 10;
export const SHOW_FIRST = 8;

export interface Fold {
  dir: string;
  paths: ImpactPath[];
}

/** dirOf is the directory of a node's file ("(root)" for a top-level
 * file, "(no file)" for an identity without one). */
export function dirOf(node: string): string {
  const p = identityParts(node);
  if (p === null || p.file === "" || p.file === ".") return "(no file)";
  const slash = p.file.lastIndexOf("/");
  return slash === -1 ? "(root)" : `${p.file.slice(0, slash)}/`;
}

/** foldPaths splits a group's paths into the ones shown and folds by
 * directory, largest first. */
export function foldPaths(paths: readonly ImpactPath[]): { shown: ImpactPath[]; folds: Fold[] } {
  if (paths.length <= FOLD_AT) return { shown: [...paths], folds: [] };
  const shown = paths.slice(0, SHOW_FIRST);
  const byDir = new Map<string, ImpactPath[]>();
  for (const p of paths.slice(SHOW_FIRST)) {
    const d = dirOf(p.node);
    const list = byDir.get(d) ?? [];
    list.push(p);
    byDir.set(d, list);
  }
  const folds = [...byDir.entries()].map(([dir, ps]) => ({ dir, paths: ps }));
  folds.sort((a, b) => b.paths.length - a.paths.length || (a.dir < b.dir ? -1 : 1));
  return { shown, folds };
}

/** pathLocation is where the path's last step was read ("file:line"),
 * or null when the step carries no evidence. */
export function pathLocation(p: ImpactPath): string | null {
  const last = p.steps.at(-1);
  const ev = last?.evidence[0];
  if (ev === undefined) return null;
  return fileLine(ev.file, ev.span);
}
