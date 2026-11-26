// Cross-repository callers of a route (T022 B9; serve --project only): the
// calls and OpenAPI operations of other project members the stored link
// pass linked to this route, and the calls it left ambiguous with this
// route among the candidates. Precision rules:
// - every link shows its repository, the assumption it rests on and its
//   provenance in the label table's words ("Inferred across
//   repositories", technical "inferred 0.7");
// - a stale link stays listed, marked stale with its reasons;
// - an ambiguous call is never shown as a caller: it is listed apart as
//   "may be called by" (T022 review), with the number of candidate
//   routes, as is a call left incomplete by a provider without a stored
//   index;
// - in-repository callers whose link a scope declaration contradicts are
//   listed with that mark (T022 review);
// - these are project records, not stored edges of this repository, so
//   they never change the in-repository callers or their count.

import type { IdeStatus } from "@/api/endpoints";
import type { PossibleCaller, ProjectEndpoint, ProjectLink } from "@/api/project";
import { ProjectLinkChip, useProjectLinkText } from "@/components/precision/ProvChip";
import { RepoBadge, StaleLinkChip } from "@/components/RepoBadge";
import { cn } from "@/lib/utils";
import { WrongFactFlag } from "@/components/WrongFactFlag";
import { useReport } from "@/shell/report";
import { FileStateChip, IdeButton, WrappedPath, callExpression, callerName } from "@/sections/api/parts";
import { projectCallerFact } from "@/sections/api/wrongFact";

/** linkedIde is the editor status of a node presented through another
 * member's checkout: the server's own (ide_status: the browser's choice,
 * then the member's graphvein.yaml, then the built-in default), so the
 * link is named and asked for like every other editor link. An older
 * server sends none: the link is usable, its source unstated. */
export function linkedIde(l: ProjectLink): IdeStatus {
  return l.ide_status ?? { available: true, error: null, label: null, editor: null, source: null };
}

/** RouteRef names the route of the detail, for the flag's report. */
export interface RouteRef {
  text: string;
  repository: string;
  where: string;
}

function LinkRow({ l, what, route }: { l: ProjectLink; what: string; route?: RouteRef | undefined }) {
  // The chip's own words, for the flag's report (read through the chip
  // module, T041).
  const provText = useProjectLinkText(l);
  const name = l.node !== null ? callerName(l.owner, l.node) : l.identity;
  // The row is a flag group only while the switch is on (markup unchanged off).
  const flags = useReport().url !== null;
  return (
    <li className={cn(flags && "group/fact", "grid gap-1 border-b border-dashed py-2 last:border-b-0", l.stale && "opacity-90")} data-testid="project-caller" data-repository={l.repository} data-stale={l.stale ? "true" : "false"}>
      <div className="flex flex-wrap items-center gap-1.5">
        <RepoBadge repository={l.repository} />
        <span className="font-medium [overflow-wrap:anywhere]">{name}</span>
        {l.node !== null && <FileStateChip state={l.node.file_state} />}
        <ProjectLinkChip link={l} />
        {l.stale && <StaleLinkChip reasons={l.stale_reasons} />}
        <span className="flex-1" />
        {l.node !== null && l.node.ide !== null && <IdeButton ide={l.node.ide} status={linkedIde(l)} place={l.node.identity} />}
        {route !== undefined && what === "call" && <WrongFactFlag fact={() => projectCallerFact(route, l, provText)} />}
      </div>
      {l.node !== null ? (
        <>
          <div className="text-foreground/82">
            <WrappedPath file={l.node.file} line={l.node.span?.start_line ?? null} />
          </div>
          <div className="w-fit max-w-full rounded-[4px] border border-dashed border-brand/40 bg-background/70 px-[7px] py-[3px] font-mono text-[11.5px] [overflow-wrap:anywhere]">{callExpression(l.node)}</div>
        </>
      ) : (
        <div className="text-[12.5px] text-dynamic">{l.repository} no longer stores this {what}; the link is from the last link pass.</div>
      )}
      <div className="text-[12.5px] text-muted-foreground" data-testid="project-assumption">
        Assumption: <span className="italic">{l.assumption}</span>
      </div>
      {l.stale && l.stale_reasons.length > 0 && <div className="text-[12px] text-dynamic">Stale: {l.stale_reasons.join("; ")}</div>}
    </li>
  );
}

/** possibleNote words why a possible caller is not a link (T022 review):
 * ambiguous across repositories with its candidates, or not linked
 * because a candidate provider has no stored index. */
export function possibleNote(a: PossibleCaller): string {
  const what = a.kind === "documents_route" ? "may document it" : "may call it";
  if (a.status === "ambiguous") {
    return `${what} (${a.wording}; ${String(a.candidate_count)} candidate routes in ${a.candidate_repositories.join(", ")})`;
  }
  return `${what} (${a.wording}${a.reason === "" ? "" : `; ${a.reason}`})`;
}

/** possibleHeading counts possible callers: calls, and OpenAPI operations
 * that may document the route. */
function possibleHeading(possible: PossibleCaller[]): string {
  const calls = possible.filter((a) => a.kind !== "documents_route").length;
  const ops = possible.length - calls;
  const parts: string[] = [];
  if (calls > 0) parts.push(`May be called by ${String(calls)} ${calls === 1 ? "call" : "calls"}`);
  if (ops > 0) parts.push(`${calls > 0 ? "and documented" : "May be documented"} by ${String(ops)} ${ops === 1 ? "operation" : "operations"}`);
  return `${parts.join(" ")} of other repositories`;
}

/** ProjectCallers renders the cross-repository part of an endpoint's
 * Callers tab; nothing on a single-repository server. */
export function ProjectCallers({ project, repository, route }: { project: ProjectEndpoint | null; repository: string; route?: RouteRef }) {
  if (project === null) return null;
  const { callers, operations, possible_callers: possible, declaration_conflicts: conflicts } = project;
  const pass = project.link_pass;
  return (
    <section className="mt-4 grid gap-2" aria-label="Callers in other repositories" data-testid="project-callers">
      <h3 className="m-0 flex flex-wrap items-baseline gap-x-2 text-[13px] font-semibold">
        Callers in other repositories
        <small className="text-[12px] font-normal text-muted-foreground">
          {callers.length} {callers.length === 1 ? "call" : "calls"} linked by project {project.project}, pass {pass.pass}
        </small>
      </h3>
      {pass.state === "stale" && (
        <p className="m-0 text-[12.5px] text-dynamic" data-testid="project-pass-stale">
          The link pass is stale ({pass.stale_reasons.join("; ")}): its links are shown and marked until the project is indexed again.
        </p>
      )}
      {pass.state === "none" && <p className="m-0 text-[12.5px] text-muted-foreground">No link pass is stored for project {project.project}: run graphvein index --project.</p>}
      {callers.length === 0 && pass.state !== "none" && (
        <p className="m-0 text-[12.5px] text-muted-foreground">No call of another repository is linked to this route.</p>
      )}
      {callers.length > 0 && (
        <ul className="m-0 grid list-none rounded-md border border-dashed border-brand/40 px-3 py-0">
          {callers.map((l) => (
            <LinkRow key={`${l.repository}\u0000${l.identity}`} l={l} what="call" route={route} />
          ))}
        </ul>
      )}
      {operations.length > 0 && (
        <>
          <h3 className="m-0 mt-1 text-[13px] font-semibold">Documented in other repositories</h3>
          <ul className="m-0 grid list-none rounded-md border border-dashed border-brand/40 px-3 py-0">
            {operations.map((l) => (
              <LinkRow key={`${l.repository}\u0000${l.identity}`} l={l} what="operation" />
            ))}
          </ul>
        </>
      )}
      {possible.length > 0 && (
        <div className="text-[12.5px]" data-testid="project-possible">
          <span className="font-medium">{possibleHeading(possible)}</span>{" "}
          <span className="text-muted-foreground">(never linked to {repository}, so not counted as callers; counted as a blind spot):</span>
          <ul className="mt-1 grid gap-0.5 pl-3">
            {possible.map((a) => (
              <li key={`${a.repository}\u0000${a.identity}`} className="flex flex-wrap items-center gap-1.5" data-testid="project-possible-caller" data-status={a.status}>
                <RepoBadge repository={a.repository} />
                <span className="font-mono text-[11.5px] [overflow-wrap:anywhere]">{a.identity}</span>
                <span className="text-muted-foreground">{possibleNote(a)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {conflicts.length > 0 && (
        <div className="text-[12.5px]" data-testid="project-declaration-conflicts">
          <span className="font-medium">
            {conflicts.length} in-repository {conflicts.length === 1 ? "caller is" : "callers are"} linked inside the repository; the declaration names another scope:
          </span>
          <ul className="mt-1 grid gap-0.5 pl-3">
            {conflicts.map((c) => (
              <li key={c.call} className="grid gap-0.5">
                <span className="font-mono text-[11.5px] [overflow-wrap:anywhere]">{c.call}</span>
                <span className="text-muted-foreground">
                  {c.consumer} declares {c.declared.join("; ")}; the link reaches {c.target_scope === "" ? "the default scope" : `scope ${c.target_scope}`}.
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
