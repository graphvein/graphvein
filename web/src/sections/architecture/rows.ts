// The rows of a component expanded in place (AG11, AG14): its routes,
// IPC commands, calls and tables from GET /api/v0/architecture/component,
// grouped by file (level 3) with the nodes as level 4. Above LIST_ROWS rows
// the card becomes a filterable, scrollable list, and the component keeps
// one bundled line into its header (the map's line; rows draw none).
//
// Counts on a row come from the overview's link members only ("2 in": two
// stored client calls link to this route); a row with no member shows no
// count rather than "0", since members are listed for HTTP and IPC links
// only.

import type { ComponentResponse, Link } from "@/api/architecture";
import type { PresentedNode } from "@/api/endpoints";
import { shownRoutePath } from "@/lib/routePath";

/** The expanded card turns into a filterable list above this many rows
 * (AG14: "above about 12 rows"). */
export const LIST_ROWS = 12;

export interface FileRow {
  type: "file";
  key: string;
  level: 3;
  file: string;
  /** The file relative to the component directory (the card names it). */
  display: string;
  /** e.g. "5 routes" */
  text: string;
}

export interface NodeRow {
  type: "node";
  key: string;
  level: 4;
  node: PresentedNode;
  method: string | null;
  /** The route's full path, the call's URL template or the node name. */
  text: string;
  /** "2 in" (callers among link members) or "linked" for a call. */
  note: string | null;
}

export interface GroupRow {
  type: "group";
  key: string;
  text: string;
}

export type Row = FileRow | NodeRow | GroupRow;

/** firstTemplate is the first stored path as served (servedPath keeps a
 * FastAPI trailing slash). */
function firstTemplate(n: PresentedNode): string | null {
  // A path whose prefix the code does not fix keeps its "…" (T033 UP-33).
  return shownRoutePath(n.attributes);
}

function attr(n: PresentedNode, key: string): string | null {
  const v = n.attributes[key];
  return typeof v === "string" ? v : null;
}

const GROUPS: { key: "routes" | "commands" | "calls" | "tables"; one: string; many: string }[] = [
  { key: "routes", one: "route", many: "routes" },
  { key: "commands", one: "IPC command", many: "IPC commands" },
  { key: "calls", one: "call", many: "calls" },
  { key: "tables", one: "table", many: "tables" },
];

/** buildRows lists a component's content: one group per node family,
 * files as level 3 headers, nodes as level 4 rows. */
export function buildRows(d: ComponentResponse, links: readonly Link[]): Row[] {
  const incoming = new Map<string, number>();
  const outgoing = new Set<string>();
  for (const l of links) {
    for (const m of l.members) {
      incoming.set(m.target, (incoming.get(m.target) ?? 0) + 1);
      outgoing.add(m.source);
    }
  }
  const rows: Row[] = [];
  for (const g of GROUPS) {
    const list = d[g.key];
    if (list.length === 0) continue;
    rows.push({ type: "group", key: `g:${g.key}`, text: `${String(list.length)} ${list.length === 1 ? g.one : g.many}` });
    const byFile = new Map<string, PresentedNode[]>();
    for (const n of list) {
      const f = byFile.get(n.file) ?? [];
      f.push(n);
      byFile.set(n.file, f);
    }
    for (const [file, nodes] of byFile) {
      rows.push({ type: "file", key: `f:${g.key}:${file}`, level: 3, file, display: relative(file, d.component.directory), text: `${String(nodes.length)} ${nodes.length === 1 ? g.one : g.many}` });
      for (const n of nodes) {
        const inbound = incoming.get(n.identity);
        let note: string | null = null;
        if (inbound !== undefined) note = `${String(inbound)} in`;
        else if (outgoing.has(n.identity)) note = "linked";
        rows.push({
          type: "node",
          key: n.identity,
          level: 4,
          node: n,
          method: attr(n, "method"),
          text: g.key === "tables" ? n.name : (firstTemplate(n) ?? attr(n, "command") ?? n.name),
          note,
        });
      }
    }
  }
  return rows;
}

/** relative strips the component directory from a repository path. */
export function relative(file: string, dir: string): string {
  if (dir === "." || dir === "") return file;
  return file.startsWith(`${dir}/`) ? file.slice(dir.length + 1) : file;
}

/** filterRows keeps the node rows matching q (text, file or name) and the
 * group and file headers above them. */
export function filterRows(rows: readonly Row[], q: string): Row[] {
  const s = q.trim().toLowerCase();
  if (s === "") return [...rows];
  const out: Row[] = [];
  let group: GroupRow | null = null;
  let file: FileRow | null = null;
  for (const r of rows) {
    if (r.type === "group") {
      group = r;
      file = null;
      continue;
    }
    if (r.type === "file") {
      file = r;
      continue;
    }
    const hay = `${r.method ?? ""} ${r.text} ${r.node.file} ${r.node.qualified_name}`.toLowerCase();
    if (!hay.includes(s)) continue;
    if (group !== null) {
      out.push(group);
      group = null;
    }
    if (file !== null) {
      out.push(file);
      file = null;
    }
    out.push(r);
  }
  return out;
}

/** nodeRowCount is the number of level 4 rows (the AG14 threshold counts
 * every row, headers included). */
export function isList(rows: readonly Row[]): boolean {
  return rows.length > LIST_ROWS;
}
