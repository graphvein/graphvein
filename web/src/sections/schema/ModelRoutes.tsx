// "Routes that accept or return this model" in a model table's detail
// (T029, rows MT-20..MT-29 of docs/contract/claims/route-model-types.md):
// the routes whose stored request/response shape uses the table's model,
// the same declaration node as the table, never a class that only copies
// its fields. Each route links to its API section detail and to its
// Explorer operation; each use carries the shape item's provenance chip.
// "No route uses this model" is said only when the server proved it; when
// some routes were not fully read, the panel says how many instead.

import { ArrowUpRight, CircleSlash } from "lucide-react";

import type { IdeStatus } from "@/api/endpoints";
import type { ModelRoute, ModelRoutes, ModelUse } from "@/api/modelTables";
import { Skeleton } from "@/components/ui/skeleton";
import { apiHash, DEFAULT_PARAMS } from "@/sections/api/params";
import { ChipLinksProvider } from "@/sections/explorer/chipLinks";
import { MethodBadge, PathKey, ProvenanceChip } from "@/sections/explorer/chips";
import { explorerRouteHash } from "@/sections/explorer/params";

/** routeApiHash opens the route's Overview (its request and response) in
 * the API section; a test route needs "Include tests". */
export function routeApiHash(r: Pick<ModelRoute, "route" | "scope">): string {
  return apiHash({ ...DEFAULT_PARAMS, sel: r.route, tab: "overview", tests: r.scope === "test" });
}

/** modelUseText says where a use is: "request body", "query parameter q",
 * "response 200", followed by "inside TeamList" for a nested use. */
export function modelUseText(u: ModelUse): string {
  let where: string;
  if (u.section === "body") where = "request body";
  else if (u.section === "responses") where = `response ${u.status ?? "status unknown"}`;
  else if (["path", "query", "header", "cookie"].includes(u.section)) where = `${u.section} parameter${u.name !== null ? ` ${u.name}` : ""}`;
  else where = `${u.direction} ${u.section}`;
  if (u.via.length > 0) return `${where}, inside ${u.via.join(" → ")}`;
  if (u.nested) return `${where}, inside an object`;
  return where;
}

/** absenceText is what the panel says when no route is listed, by state.
 * Only "none" says that no route uses the model, and only of the
 * repository whose routes were read (named in project mode). */
export function absenceText(m: ModelRoutes, tool: string | null, repository: string | null = null): { text: string; proven: boolean } {
  const of = repository === null ? "this repository" : `the repository ${repository}`;
  const unread = `${String(m.routes_not_fully_read)} of ${String(m.routes_total)} route${m.routes_total === 1 ? "" : "s"}`;
  switch (m.state) {
    case "none":
      return { text: `No route of ${of} uses this model as a request or response (every request and response of the ${String(m.routes_total)} route${m.routes_total === 1 ? "" : "s"} was read).`, proven: true };
    case "none_found":
      return { text: `No route whose request and response Graphvein read uses this model. ${unread} have a part that was not read, so a use there is not ruled out.`, proven: false };
    case "not_read":
      return { text: "The routes' request and response shapes were not read, so the routes that use this model are not known.", proven: false };
    case "no_declaration":
      return { text: `Routes are linked to a model table only through the declaration it is defined by, which Graphvein does not store for ${tool ?? "this tool's"} tables, so the routes that use this model are not known.`, proven: false };
    default:
      return { text: `Unknown state (${m.state}): the routes that use this model are not known.`, proven: false };
  }
}

const LINK = "inline-flex items-center gap-0.5 text-[12px] font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]";

function RouteRow({ r }: { r: ModelRoute }) {
  // Without a full path the stored pattern is shown marked as the
  // pattern, never as if it were the route's path (T040 JX-F2).
  const path = r.path ?? r.pattern;
  const patternOnly = r.path === null && r.pattern !== null;
  return (
    <li className="grid list-none gap-1 border-b py-1.5 last:border-b-0" data-testid="model-route">
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        {r.method !== null ? <MethodBadge method={r.method} className="h-5 w-[54px] text-[10.5px]" /> : <span className="text-xs text-muted-foreground italic">method unknown</span>}
        {path !== null ? <PathKey path={path} /> : <span className="text-muted-foreground italic">path not read</span>}
        {patternOnly && (
          <span className="text-[11.5px] text-muted-foreground" title="No full path is stored for this route; this is its pattern as written in the code" data-testid="model-route-pattern-only">
            pattern as written, no full path
          </span>
        )}
        {r.scope === "test" && <span className="rounded-[4px] bg-muted px-1 text-[10.5px] text-muted-foreground">test</span>}
        <span className="flex-1" />
        <a href={routeApiHash(r)} className={LINK} data-testid="model-route-api" title="The route in the API section: its request and response, callers and flow">
          API
          <ArrowUpRight aria-hidden="true" className="size-3" />
        </a>
        <a href={explorerRouteHash(r.route)} className={LINK} data-testid="model-route-explorer" title="The operation the generated OpenAPI document writes for this route">
          Explorer
          <ArrowUpRight aria-hidden="true" className="size-3" />
        </a>
      </div>
      <ul className="m-0 grid gap-1 pl-0">
        {r.uses.map((u, i) => (
          <li key={i} className="flex list-none flex-wrap items-center gap-x-2 gap-y-1 pl-1 text-[12.5px]" data-testid="model-use" data-direction={u.direction}>
            <span className={u.direction === "request" ? "font-medium text-http" : u.direction === "response" ? "font-medium text-ipc" : "font-medium"}>{u.direction === "request" ? "accepts" : u.direction === "response" ? "returns" : u.direction}</span>
            <span className="text-muted-foreground">{modelUseText(u)}</span>
            <ProvenanceChip p={u.provenance} />
          </li>
        ))}
      </ul>
    </li>
  );
}

export interface ModelRoutesPanelProps {
  /** The table detail's model_routes; undefined while it loads. */
  value: ModelRoutes | null | undefined;
  ide: IdeStatus | null;
  tool: string | null;
  /** The member repository in project mode (named in "none"); null on a
   * single-repository server. */
  repository?: string | null;
}

/** ModelRoutesPanel is the body of the section; nothing for a table the
 * server sends no model_routes for (a migration table). */
export function ModelRoutesPanel({ value, ide, tool, repository = null }: ModelRoutesPanelProps) {
  if (value === undefined) return <Skeleton className="h-4 w-1/2" />;
  if (value === null) return null;
  const listed = value.state === "listed" && value.routes.length > 0;
  const absence = listed ? null : absenceText(value, tool, repository);
  const others = listed && value.routes_not_fully_read > 0;
  return (
    <ChipLinksProvider value={ide === null ? null : { status: ide, links: value.links }}>
      <div className="grid gap-1.5" data-testid="model-routes" data-state={value.state}>
        {listed && (
          <ul className="m-0 grid pl-0" data-testid="model-route-list">
            {value.routes.map((r) => (
              <RouteRow key={`${r.route}|${r.model}`} r={r} />
            ))}
          </ul>
        )}
        {absence !== null &&
          (absence.proven ? (
            <p className="m-0 text-[13px] text-muted-foreground" data-testid="model-routes-none" title={`Row ${value.row}`}>
              {absence.text}
            </p>
          ) : (
            <p
              className="m-0 flex items-start gap-2 rounded-md border border-dashed bg-[repeating-linear-gradient(135deg,transparent_0_6px,hsl(var(--muted))_6px_12px)] px-2.5 py-1.5 text-[12.5px] text-muted-foreground"
              data-testid="model-routes-unknown"
              title={`Row ${value.row}. Absence here is not a claim that there is none.`}
            >
              <CircleSlash aria-hidden="true" className="mt-0.5 size-3.5 flex-none" />
              <span>{absence.text}</span>
            </p>
          ))}
        {others && (
          <p className="m-0 text-[12px] text-muted-foreground" data-testid="model-routes-others">
            {value.routes_not_fully_read} of {value.routes_total} route{value.routes_total === 1 ? "" : "s"} have a request or response part that was not read, so other uses are not ruled out.
          </p>
        )}
      </div>
    </ChipLinksProvider>
  );
}
