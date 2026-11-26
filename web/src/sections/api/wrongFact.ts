// The facts the wrong-fact flag reports from the API section (T040): the
// endpoint of the detail header and each caller row. Plain functions, so
// the prefilled text is tested.

import type { Caller, EdgeRef, EndpointDetail, PresentedNode } from "@/api/endpoints";
import type { ProjectLink } from "@/api/project";
import { NO_PATH_WORDS, PATH_NOT_KNOWN, isFullPathEntry, prefixUnknownPaths, servedPath, shadowedPaths } from "@/lib/routePath";
import { confidenceNumber, storedEdge } from "@/lib/labels";
import { callerName, callExpression } from "@/sections/api/parts";
import type { WrongFact } from "@/shell/report";

/** whereOf is a node's file:line ("file" without a span). */
export function whereOf(node: PresentedNode, repository?: string): string {
  const at = node.span !== null ? `${node.file}:${String(node.span.start_line)}` : node.file;
  return repository !== undefined ? `${repository} ${at}` : at;
}

function edgeText(e: EdgeRef): string {
  return `${e.kind} ${storedEdge(e)}`;
}

/** endpointText is the endpoint as the header shows it: method and the
 * first served path; else its unknown-prefix path ("…/api/x", with "…"
 * for the prefix the code does not fix); else the stored pattern, marked
 * "(path not known)" so it is never read as the path, shadowed when the
 * stored attributes prove it, or "(path withheld)" when the server marks
 * the registration withheld (path_withheld, row CR-44). "Not mounted" is
 * not said: no stored fact proves it (ADR 0089 Revision 5). */
export function endpointText(d: EndpointDetail): string {
  const a = d.endpoint.attributes;
  const method = typeof a.method === "string" ? `${a.method} ` : "";
  const paths = Array.isArray(a.paths) ? a.paths.filter((p): p is Record<string, unknown> => typeof p === "object" && p !== null && isFullPathEntry(p)) : [];
  const first = paths[0];
  const served = first !== undefined ? servedPath(first) : null;
  if (served !== null) return `${method}${served}`.trim();
  const prefixed = prefixUnknownPaths(a)[0];
  if (prefixed !== undefined) return `${method}${prefixed.path} (prefix unknown)`.trim();
  const pattern = typeof a.pattern === "string" ? a.pattern : d.endpoint.name;
  if (shadowedPaths(a).length > 0) return `${method}${pattern} (pattern as written, shadowed)`.trim();
  if (d.path_withheld) return `${method}${pattern} (${NO_PATH_WORDS.withheld})`.trim();
  return `${method}${pattern} ${PATH_NOT_KNOWN}`.trim();
}

/** endpointFact is the detail header's fact: the endpoint and its handler. */
export function endpointFact(d: EndpointDetail): WrongFact {
  const where = [whereOf(d.endpoint)];
  let fact = `${d.endpoint.kind.replace(/_/g, " ")} ${endpointText(d)} (${d.repository})`;
  const h = d.handler.node;
  if (h !== null) {
    fact += ` is handled by ${h.qualified_name !== "" ? h.qualified_name : h.name}`;
    where.push(whereOf(h));
  } else {
    fact += `, handler ${d.handler.state.replace(/_/g, " ")}`;
  }
  const rule = d.handler.edge !== null ? `stored ${d.endpoint.kind} node; handler edge ${edgeText(d.handler.edge)}` : `stored ${d.endpoint.kind} node`;
  return { fact, where, rule, shown: null };
}

/** callerFact is a caller row's fact: the call and the edges that link it. */
export function callerFact(d: EndpointDetail, c: Caller, shown: string | null): WrongFact {
  return {
    fact: `${callerName(c.owner, c.node)} calls ${endpointText(d)} (${callExpression(c.node)})`,
    where: [whereOf(c.node), whereOf(d.endpoint)],
    rule: c.edges.length > 0 ? c.edges.map(edgeText).join("; ") : null,
    shown,
  };
}

/** projectCallerFact is a cross-repository caller's fact: the project
 * link, its assumption and both sides. */
export function projectCallerFact(route: { text: string; repository: string; where: string }, l: ProjectLink, shown: string): WrongFact {
  const name = l.node !== null ? callerName(l.owner, l.node) : l.identity;
  return {
    fact: `${name} (${l.repository}) calls ${route.text} (${route.repository})`,
    where: [l.node !== null ? whereOf(l.node, l.repository) : `${l.repository} ${l.identity}`, `${route.repository} ${route.where}`],
    rule: `${l.kind} project link, ${l.provenance} ${confidenceNumber(l.confidence)}; assumption: ${l.assumption}`,
    shown,
  };
}
