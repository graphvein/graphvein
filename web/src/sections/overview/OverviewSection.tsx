// The Overview (T064, design record T061 D1 variant A): the home page at
// #/overview, opened by "#/" and the sidebar's logo. It answers "what is
// this repository" from the answers the other sections already request
// (no new endpoint): the status, /architecture, /api/endpoints, the shell's
// schema summary and /unread.
//
// Precision (T061 D1): "none found in the indexed files", never "none in
// this repository"; "not read yet" for a tool found but not read; no number
// the endpoints do not serve (no file total, no language line: open
// questions 1 and 2 left them out). A part that has not answered shows a
// skeleton, a failed one says so; nothing is shown as zero meanwhile.

import { ArrowRight, Boxes, Folder, Info, Monitor, Server, SquareTerminal, type LucideIcon } from "lucide-react";
import { useState, type ReactNode } from "react";

import { httpArchitectureClient, type ArchitectureClient, type ArchitectureResponse, type Component } from "@/api/architecture";
import { TRANSPORT_LABEL, callNoun, endpointNoun, linkedCount, linkedPercent, neverCalledLabel, neverScoped } from "@/api/categories";
import { httpClient, type ApiClient, type EndpointsResponse, type TransportSummary } from "@/api/endpoints";
import type { SchemaSummary } from "@/api/schema";
import { fetchUnread, type UnreadCounts, type UnreadFetcher } from "@/api/unread";
import { ParseFailedNotice } from "@/components/ParseFailedNotice";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiRequest, type RequestResult, type RequestState } from "@/hooks/useApiRequest";
import { useLabelStyle } from "@/hooks/useLabels";
import type { StatusView } from "@/hooks/useStatus";
import { formatCount } from "@/lib/count";
import { label } from "@/lib/labels";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { helpHash } from "@/router";
import { KindTextPlain, mainKind } from "@/sections/architecture/kindText";
import { DEFAULT_SHOW, buildMap, endpointName, placement } from "@/sections/architecture/model";
import { filesWhere, isEmptyTransport, noneFoundSentence } from "@/sections/api/coverage";
import { schemaEntryTag } from "@/sections/schema/entry";
import { noneTitle } from "@/sections/schema/EmptyStates";
import { refusalReason, toolLabel } from "@/sections/schema/tools";
import { LabelBanner } from "@/shell/LabelControl";
import { useProject } from "@/shell/project";

/** ROWS_MAX is how many components are listed as rows with their counts;
 * more are listed as tiles (name and files). */
export const ROWS_MAX = 4;
/** TILES_MAX caps the tiles; the rest is "+ N more" on the map. */
export const TILES_MAX = 12;
/** LINKS_MAX caps the drawn links listed. */
export const LINKS_MAX = 4;

function plural(n: number, one: string, many: string): string {
  return `${formatCount(n)} ${n === 1 ? one : many}`;
}

const KIND_ICON: Record<string, LucideIcon> = { server: Server, client: Monitor, binary: SquareTerminal, unassigned_files: Folder };

function Card({ title, sub, testId, children }: { title: string; sub?: ReactNode; testId: string; children: ReactNode }) {
  return (
    <section aria-label={title} data-testid={testId} className="grid min-w-0 grid-cols-[minmax(0,1fr)] content-start gap-3 rounded-xl border bg-card p-4 text-[13px] shadow-[0_1px_2px_rgba(0,0,0,.04)]">
      <h2 className="m-0 flex flex-wrap items-baseline gap-x-2 text-[15px] font-semibold">
        {title}
        {sub !== undefined && <span className="text-[13px] font-normal text-muted-foreground">{sub}</span>}
      </h2>
      {children}
    </section>
  );
}

function More({ href, children, testId }: { href: string; children: ReactNode; testId?: string }) {
  return (
    <a href={href} data-testid={testId} className="inline-flex w-fit items-center gap-1 font-medium text-brand-text underline-offset-4 hover:underline">
      {children}
      <ArrowRight aria-hidden="true" className="size-3.5" />
    </a>
  );
}

function Chip({ children }: { children: ReactNode }) {
  return <span className="inline-flex h-6 items-center rounded-md border px-2 text-xs whitespace-nowrap text-muted-foreground tabular-nums">{children}</span>;
}

/** Pending is a part's state other than an answer: a skeleton while it is
 * requested, the failure with a retry. */
function Pending<T>({ request, what }: { request: RequestResult<T>; what: string }) {
  if (request.state.kind === "failed") {
    return (
      <p className="m-0 text-muted-foreground" role="alert">
        {what} could not be loaded: {request.state.error.message}.{" "}
        <button type="button" className="font-medium text-brand-text underline" onClick={request.retry}>
          Retry
        </button>
      </p>
    );
  }
  return <Skeleton className="h-16" />;
}

function dirText(c: Component): string {
  return c.directory === "." || c.directory === "" ? "./" : `${c.directory}/`;
}

/** componentChips are a component row's counts; zero counts other than
 * files are left out (a zero is served, but says nothing about the part). */
function componentChips(c: Component): string[] {
  const out: string[] = [];
  if (c.counts.routes > 0) out.push(plural(c.counts.routes, "route", "routes"));
  if (c.counts.ipc_commands > 0) out.push(plural(c.counts.ipc_commands, "IPC command", "IPC commands"));
  if (c.counts.client_calls > 0) out.push(plural(c.counts.client_calls, "client call", "client calls"));
  if (c.counts.tables > 0) out.push(plural(c.counts.tables, "table", "tables"));
  out.push(plural(c.counts.files, "file", "files"));
  return out;
}

function ComponentRow({ c }: { c: Component }) {
  const kind = mainKind(c);
  const Icon = KIND_ICON[kind] ?? Boxes;
  return (
    <li className="flex items-center gap-3 border-b py-2.5 first:pt-0 last:border-b-0" data-testid="overview-component">
      <span className="grid size-8 flex-none place-items-center rounded-md bg-brand-soft text-brand-text">
        <Icon aria-hidden="true" className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2">
          <b className="font-semibold break-all">{c.name}</b>
          <KindTextPlain c={c} kind={kind} className="text-xs text-muted-foreground" />
        </div>
        <div className="truncate font-mono text-xs text-muted-foreground">{[dirText(c), ...c.manifests.map((m) => m.file.split("/").pop() ?? m.file)].join(" · ")}</div>
      </div>
      <div className="flex flex-wrap justify-end gap-1.5">
        {componentChips(c).map((t) => (
          <Chip key={t}>{t}</Chip>
        ))}
      </div>
    </li>
  );
}

function ComponentTile({ c }: { c: Component }) {
  const kind = mainKind(c);
  const Icon = KIND_ICON[kind] ?? Boxes;
  return (
    <li className="flex min-w-0 items-center gap-2 rounded-md border px-2.5 py-1.5" data-testid="overview-component" title={`${c.name} · ${dirText(c)}`}>
      <Icon aria-hidden="true" className="size-4 flex-none text-muted-foreground" />
      <b className="min-w-0 flex-1 truncate font-semibold">{c.name}</b>
      <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums">{plural(c.counts.files, "file", "files")}</span>
    </li>
  );
}

/** linksIntoLibraries splits libraries.links_in by the component the
 * edges come from, largest first, with the server's rule: the in-process
 * links between a folded and an unfolded component, their count when the
 * library is the target and their back count when it is the source. */
export function linksIntoLibraries(r: ArchitectureResponse): { name: string; count: number }[] {
  const folded = new Set(r.components.filter((c) => c.visibility === "folded").map((c) => c.id));
  const by = new Map<string, number>();
  const add = (id: string, n: number) => {
    if (n === 0) return;
    const name = endpointName(r, id);
    by.set(name, (by.get(name) ?? 0) + n);
  };
  for (const l of r.links) {
    if (l.family !== "in_process") continue;
    if (folded.has(l.target) && !folded.has(l.source)) add(l.source, l.count);
    if (folded.has(l.source) && !folded.has(l.target)) add(l.target, l.back_count);
  }
  return [...by.entries()].map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
}

/** cardLines is the default map's lines between two cards: the lines into
 * the libraries band (on by default since T067) are left out, the band is
 * named on its own row. */
export function cardLines(r: ArchitectureResponse) {
  return buildMap(r, DEFAULT_SHOW, null).edges.filter((e) => e.type === "link" && e.band === undefined);
}

/** PartsCard is the components on the default map, the workspace libraries
 * in its band, the components hidden on it and the lines between the cards
 * (/architecture). */
export function PartsCard({ request }: { request: RequestResult<ArchitectureResponse> }) {
  const style = useLabelStyle();
  if (request.state.kind !== "ok") {
    return (
      <Card title="Parts" testId="overview-parts">
        <Pending request={request} what="The parts" />
      </Card>
    );
  }
  const r = request.state.data;
  const shown = r.components.filter((c) => placement(c, DEFAULT_SHOW) === "card");
  const folded = r.components.filter((c) => c.visibility === "folded");
  const hiddenTests = r.components.filter((c) => c.visibility === "hidden" && c.hidden_by === "tests").length;
  const hiddenDocs = r.components.filter((c) => c.visibility === "hidden" && c.hidden_by === "docs").length;
  const hiddenOther = r.components.filter((c) => c.visibility === "hidden" && c.hidden_by !== "tests" && c.hidden_by !== "docs").length;
  const edges = cardLines(r);
  const sub = [plural(shown.length, "component on the map", "components on the map")];
  if (folded.length > 0) sub.push(plural(folded.length, "workspace library in the band", "workspace libraries in the band"));
  sub.push(plural(r.links.length, "link", "links"));
  const hidden: string[] = [];
  if (hiddenTests > 0) hidden.push(`tests ${plural(hiddenTests, "component", "components")}`);
  if (hiddenDocs > 0) hidden.push(`docs ${plural(hiddenDocs, "component", "components")}`);
  if (hiddenOther > 0) hidden.push(`configured ${plural(hiddenOther, "component", "components")}`);
  const intoLibs = linksIntoLibraries(r);
  const rows = shown.length <= ROWS_MAX;
  const listed = rows ? shown : shown.slice(0, TILES_MAX);
  return (
    <Card title="Parts" sub={sub.join(" · ")} testId="overview-parts">
      {shown.length === 0 ? (
        <p className="m-0 text-muted-foreground">No component on the default map.</p>
      ) : rows ? (
        <ul className="m-0 grid list-none p-0">
          {listed.map((c) => (
            <ComponentRow key={c.id} c={c} />
          ))}
        </ul>
      ) : (
        <ul className="m-0 grid list-none grid-cols-[repeat(auto-fill,minmax(210px,1fr))] gap-2 p-0">
          {listed.map((c) => (
            <ComponentTile key={c.id} c={c} />
          ))}
          {shown.length > listed.length && (
            <li className="flex items-center px-2.5 text-muted-foreground">
              <a href="#/architecture" className="underline-offset-4 hover:underline">
                + {formatCount(shown.length - listed.length)} more on the map
              </a>
            </li>
          )}
        </ul>
      )}
      {folded.length > 0 && (
        <div className="flex items-center gap-3 border-t pt-2.5" data-testid="overview-libraries">
          <span className="grid size-8 flex-none place-items-center rounded-md bg-muted text-muted-foreground">
            <Boxes aria-hidden="true" className="size-4" />
          </span>
          <div className="min-w-0 flex-1">
            <div>
              <b className="font-semibold">Workspace libraries</b> <span className="text-xs text-muted-foreground">in the band below the cards</span>
            </div>
            <div className="truncate font-mono text-xs text-muted-foreground">
              {folded
                .slice(0, 3)
                .map((c) => dirText(c))
                .join(" · ")}
              {folded.length > 3 && ` · + ${formatCount(folded.length - 3)} more`}
            </div>
          </div>
          <div className="flex flex-wrap justify-end gap-1.5">
            <Chip>{plural(folded.length, "package", "packages")}</Chip>
            {(r.libraries.links_in > 0 || r.libraries.links_out > 0) && (
              <>
                <Chip>{plural(r.libraries.links_in, "edge in", "edges in")}</Chip>
                <Chip>{formatCount(r.libraries.links_out)} out</Chip>
              </>
            )}
          </div>
        </div>
      )}
      {r.libraries.links_in > 0 && (
        <div className="flex gap-2.5 rounded-lg border border-brand/40 bg-brand-soft px-3 py-2.5" data-testid="overview-libraries-note">
          <Info aria-hidden="true" className="mt-0.5 size-4 flex-none" />
          <p className="m-0">
            <b className="font-semibold">
              {plural(r.libraries.links_in, "in-process edge ends", "in-process edges end")} in the workspace libraries
            </b>
            {intoLibs.length > 0 && ` (${intoLibs.slice(0, 2).map((x) => `${formatCount(x.count)} from ${x.name}`).join(", ")}${intoLibs.length > 2 ? ", more from the others" : ""})`}
            {". The map draws them as lines into the libraries band below the cards. "}
            <More href="#/architecture">Libraries band</More>
          </p>
        </div>
      )}
      {edges.length > 0 && (
        <ul className="m-0 grid list-none gap-1.5 border-t p-0 pt-2.5" data-testid="overview-links">
          {edges.slice(0, LINKS_MAX).map((e) => {
            const head = e.links[0];
            const names = head === undefined ? "" : `${endpointName(r, head.source)} → ${endpointName(r, head.target)}`;
            return (
              <li key={e.id} className="flex flex-wrap items-center gap-x-2 gap-y-1">
                {e.families.map((f) => {
                  const l = label(style, "linkFamily", { family: f, row: e.links.find((x) => x.family === f)?.row ?? "" });
                  return (
                    <span key={f} title={l.title} className="rounded-[4px] bg-muted px-1.5 py-0.5 text-[11px] font-semibold">
                      {l.text}
                    </span>
                  );
                })}
                <b className="font-semibold">{names}</b>
                <span className="text-muted-foreground">
                  {formatCount(e.count)}
                  {e.families.length === 1 && e.families[0] === "http" ? (e.count === 1 ? " call linked" : " calls linked") : ""}
                  {e.both ? " both ways" : ""}
                </span>
              </li>
            );
          })}
          {edges.length > LINKS_MAX && <li className="text-muted-foreground">+ {formatCount(edges.length - LINKS_MAX)} more lines on the map</li>}
        </ul>
      )}
      <div className="flex flex-wrap items-center justify-between gap-2 text-muted-foreground">
        <span data-testid="overview-hidden">{hidden.length > 0 ? `Hidden on the map: ${hidden.join(" · ")}` : ""}</span>
        <More href="#/architecture">Open the map</More>
      </div>
    </Card>
  );
}


function Big({ value, text, tone }: { value: string; text: string; tone?: "linked" }) {
  return (
    <div className="grid min-w-0 gap-0.5">
      <b className={cn("text-[24px] leading-none font-semibold tabular-nums", tone === "linked" && "text-fresh")}>{value}</b>
      <span className="text-muted-foreground">{text}</span>
    </div>
  );
}

function TransportNumbers({ t, scope }: { t: TransportSummary; scope: string }) {
  const never = neverScoped(t);
  const linked = linkedCount(t.calls.by_status);
  const pct = linkedPercent(linked, t.calls.total);
  const frameworks = t.frameworks.length > 0 ? ` · ${t.frameworks.join(", ")}` : "";
  return (
    <div className="flex flex-wrap gap-x-8 gap-y-3" data-testid={`overview-transport-${t.transport}`}>
      <Big value={formatCount(t.endpoint_count)} text={`${TRANSPORT_LABEL[t.transport]} ${endpointNoun(t.transport, t.endpoint_count)}${frameworks}`} />
      <Big value={formatCount(t.calls.total)} text={callNoun(t.transport, t.calls.total)} />
      <Big value={formatCount(linked)} text={`linked to ${endpointNoun(t.transport, 1) === "route" ? "a route" : `an ${endpointNoun(t.transport, 1)}`}${pct === null ? "" : ` (${String(pct)} %)`}`} tone="linked" />
      <Big value={formatCount(never.count)} text={`${endpointNoun(t.transport, never.count)} with ${neverCalledLabel(scope, undefined, never.project).toLowerCase()}`} />
    </div>
  );
}

/** rootKey normalises a root or component directory for comparison. */
function rootKey(dir: string): string {
  const d = dir.replace(/\/+$/, "");
  return d === "" || d === "." ? "." : d;
}

/** hiddenRootLines name the calls of roots whose component the default map
 * hides (langflow's docs: their calls count in the totals above but have
 * no card on the map). */
export function hiddenRootLines(t: TransportSummary, arch: ArchitectureResponse | null): string[] {
  if (arch === null) return [];
  const out: string[] = [];
  for (const r of t.roots) {
    if (r.total === 0) continue;
    const c = arch.components.find((x) => rootKey(x.directory) === rootKey(r.root));
    if (c === undefined || c.visibility !== "hidden") continue;
    out.push(`${plural(r.total, callNoun(t.transport, 1), callNoun(t.transport, 2))} from ${dirText(c)}, hidden on the map (${c.hidden_by ?? "configured"})`);
  }
  return out;
}

/** InterfacesCard is the routes and calls per transport (/api/endpoints):
 * the numbers of each transport that has any, then one line for those with
 * none found. */
export function InterfacesCard({ request, arch }: { request: RequestResult<EndpointsResponse>; arch: ArchitectureResponse | null }) {
  if (request.state.kind !== "ok") {
    return (
      <Card title="Interfaces" testId="overview-interfaces">
        <Pending request={request} what="The routes and calls" />
      </Card>
    );
  }
  const r = request.state.data;
  // The API page's wording (T065): "none found" once, where it holds.
  const full = r.transports.filter((t) => !isEmptyTransport(t));
  const empty = r.transports.filter(isEmptyTransport);
  const notDrawn = arch?.not_drawn ?? {};
  const inCode: string[] = [];
  if ((notDrawn.calls ?? 0) > 0) inCode.push(plural(notDrawn.calls ?? 0, "call", "calls"));
  if ((notDrawn.imports ?? 0) > 0) inCode.push(plural(notDrawn.imports ?? 0, "import", "imports"));
  return (
    <Card title="Interfaces" testId="overview-interfaces">
      {full.length === 0 ? (
        <>
          <p className="m-0" data-testid="overview-no-interfaces">
            {noneFoundSentence(
              r.transports.map((t) => t.transport),
              r.scope,
              false,
            )}
          </p>
          {inCode.length > 0 && <p className="m-0 text-muted-foreground">In-code calls are counted, not drawn: {inCode.join(" and ")} inside components.</p>}
          <More href="#/architecture">Open the map</More>
        </>
      ) : (
        <>
          {full.map((t) => (
            <div key={t.transport} className="grid gap-2">
              <TransportNumbers t={t} scope={r.scope} />
              {hiddenRootLines(t, arch).map((line) => (
                <p key={line} className="m-0 text-muted-foreground">
                  {line}
                </p>
              ))}
            </div>
          ))}
          <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-2.5 text-muted-foreground">
            <span data-testid="overview-none-found">{empty.length > 0 ? `${empty.map((t) => TRANSPORT_LABEL[t.transport]).join(", ")}: none found ${filesWhere(r.scope)}` : ""}</span>
            <More href="#/api">Open the API</More>
          </div>
        </>
      )}
    </Card>
  );
}

/** DataCard is the schema state (the shell's /schema/summary answer):
 * sets and model groups, or "none found" with what that does not mean, or
 * why it is not known. */
export function DataCard({ summary }: { summary: RequestResult<SchemaSummary> }) {
  if (summary.state.kind !== "ok") {
    return (
      <Card title="Data" testId="overview-data">
        <Pending request={summary} what="The schema summary" />
      </Card>
    );
  }
  const s = summary.state.data;
  const models = s.sources_read?.includes("models") === true;
  let body: ReactNode;
  if (s.found === "sets") {
    const parts: string[] = [];
    if (s.sets !== null && s.sets > 0) parts.push(plural(s.sets, "migration set", "migration sets"));
    if (s.model_groups !== null && s.model_groups > 0) parts.push(plural(s.model_groups, "group of model tables", "groups of model tables"));
    const tag = schemaEntryTag(s);
    body = (
      <p className="m-0">
        {parts.length > 0 ? `${parts.join(" and ")} found.` : "Migration sets found."}
        {tag !== null && <span className="text-muted-foreground"> {tag.title}.</span>}
      </p>
    );
  } else if (s.found === "none") {
    const unread = s.unread_tools ?? [];
    // T070: a model source found and refused is "found, not read", as on
    // the Schema page, never "none found".
    const refused = s.refused_sources ?? [];
    body = (
      <>
        <p className="m-0">
          {refused.length > 0 ? (
            <b className="font-semibold">{noneTitle(s)}.</b>
          ) : (
            <>
              <b className="font-semibold">No migration set{models && " or model table"} that Graphvein reads was found in the indexed files.</b>{" "}
              <span className="text-muted-foreground">This does not mean there is no database.</span>
            </>
          )}
        </p>
        {refused.length > 0 && (
          <ul className="m-0 grid gap-0.5 pl-5 [overflow-wrap:anywhere]" data-testid="overview-refused">
            {refused.map((r, i) => (
              <li key={i}>
                {toolLabel(r.tool)} models <span className="font-mono">{r.file}</span>: found, not read <span className="text-muted-foreground">({refusalReason(r)})</span>
              </li>
            ))}
          </ul>
        )}
        {unread.length > 0 && (
          <ul className="m-0 grid gap-0.5 pl-5" data-testid="overview-unread-tools">
            {unread.map((t, i) => (
              <li key={i}>
                {toolLabel(t.tool)}
                {t.path !== null && <span className="font-mono"> {t.path}</span>}
                {t.files !== null && ` (${plural(t.files, "file", "files")})`}: found, not read yet
              </li>
            ))}
          </ul>
        )}
      </>
    );
  } else if (s.found === "unknown" && s.unknown_reason === "not_indexed") {
    body = <p className="m-0 text-muted-foreground">Not indexed yet.</p>;
  } else {
    body = <p className="m-0 text-muted-foreground">{schemaEntryTag(s)?.title ?? `Schema state "${s.found}" is not known to this UI`}.</p>;
  }
  return (
    <Card title="Data" testId="overview-data">
      {body}
      <More href="#/schema">Schema</More>
    </Card>
  );
}

/** unreadLines are the Could-not-read card's lines: [count, words]. A count
 * the server marks not available (null) is left out; the files lines are
 * always listed, the others only above zero. */
export function unreadLines(c: UnreadCounts): { value: number; text: string }[] {
  const v = (k: string): number | null => c[k] ?? null;
  const out: { value: number; text: string }[] = [];
  const notRead = v("files_not_read");
  const syntax = v("files_syntax_errors");
  if (notRead !== null) out.push({ value: notRead, text: `${notRead === 1 ? "file" : "files"} not read${syntax !== null ? ` · ${formatCount(syntax)} read with syntax errors` : ""}` });
  const noFact = v("files_without_facts");
  if (noFact !== null) out.push({ value: noFact, text: `${noFact === 1 ? "file" : "files"} with no fact stored` });
  const dynamic = v("dynamic_calls");
  const wrapper = v("wrapper_calls");
  if (dynamic !== null && wrapper !== null && dynamic + wrapper > 0) {
    out.push({ value: dynamic + wrapper, text: `client ${dynamic + wrapper === 1 ? "call" : "calls"} not resolved (${formatCount(dynamic)} dynamic, ${formatCount(wrapper)} through a wrapper)` });
  }
  const unplaced = v("unplaced_routes");
  if (unplaced !== null && unplaced > 0) out.push({ value: unplaced, text: `${unplaced === 1 ? "route" : "routes"} without a full path` });
  const sections = v("routes_with_sections_not_read");
  if (sections !== null && sections > 0) out.push({ value: sections, text: `${sections === 1 ? "route" : "routes"} with parts of their shape not read` });
  const unknown = v("unresolved_calls_unknown");
  const external = v("unresolved_calls_external");
  if (unknown !== null && unknown > 0) out.push({ value: unknown, text: `in-code ${unknown === 1 ? "call" : "calls"} to unknown code${external !== null ? ` · ${formatCount(external)} to external code` : ""}` });
  return out;
}

function UnreadCard({ state, retry }: { state: RequestState<UnreadCounts>; retry: () => void }) {
  return (
    <Card title="What Graphvein could not read" testId="overview-unread">
      {state.kind === "ok" ? (
        <ul className="m-0 grid list-none gap-1 p-0">
          {unreadLines(state.data).map((l) => (
            <li key={l.text} className="grid grid-cols-[48px_1fr] gap-2">
              <b className="text-right font-semibold tabular-nums">{formatCount(l.value)}</b>
              <span className="text-muted-foreground">{l.text}</span>
            </li>
          ))}
        </ul>
      ) : (
        <Pending request={{ state, retry }} what="The counts" />
      )}
      <More href="#/unread">Couldn&apos;t read</More>
    </Card>
  );
}

function StartHere({ routes, librariesOnly }: { routes: boolean; librariesOnly: boolean }) {
  const line = (href: string, text: string, where: string) => (
    <li className="flex flex-wrap items-baseline gap-x-2">
      <More href={href}>{text}</More>
      <span className="text-muted-foreground">{where}</span>
    </li>
  );
  return (
    <Card title="Start here" testId="overview-start">
      <ul className="m-0 grid list-none gap-1.5 p-0">
        {librariesOnly ? line("#/architecture", "How the components use the libraries", "the map's libraries band") : line("#/architecture", "How the parts connect", "the map")}
        {routes && line("#/api", "Who calls a route", "API › a route › Callers")}
        {line("#/changes", "What a change reaches", "Changes")}
        {line(helpHash("start-here"), "Start here", "the Help guide")}
      </ul>
    </Card>
  );
}

export interface OverviewSectionProps {
  status: StatusView;
  /** The shell's schema summary request (one, shared with the sidebar). */
  summary: RequestResult<SchemaSummary>;
  client?: ApiClient | undefined;
  architectureClient?: ArchitectureClient | undefined;
  unreadFetcher?: UnreadFetcher | undefined;
}

/** OverviewSection is the #/overview page. In project mode it is the
 * member's overview (T061 open question 3). */
export function OverviewSection({ status, summary, client = httpClient, architectureClient = httpArchitectureClient, unreadFetcher = fetchUnread }: OverviewSectionProps) {
  const project = useProject();
  const okRun = status.kind === "ok" ? (status.status.index.last_run?.finished_at ?? "") : null;
  // Every request waits for the first status and is made again for a new
  // run; a status withdrawn while it is checked again keeps the answers.
  const [run, setRun] = useState<string | null>(okRun);
  if (okRun !== null && okRun !== run) setRun(okRun);
  const arch = useApiRequest(run === null ? null : JSON.stringify(["overview-architecture", run]), (signal) => architectureClient.overview("production", { signal }));
  const endpoints = useApiRequest(run === null ? null : JSON.stringify(["overview-endpoints", run]), (signal) => client.endpoints({ scope: "production", root: null }, { signal }));
  const member = project?.repository;
  const unread = useApiRequest(run === null ? null : JSON.stringify(["overview-unread", run, member ?? null]), async (signal) => {
    const a = await unreadFetcher({ scope: "production", ...(member !== undefined ? { member } : {}) }, { signal });
    return a.kind === "page" ? a.page.counts : a.project.totals;
  });
  const s = status.kind === "ok" ? status.status : null;
  const archData = arch.state.kind === "ok" ? arch.state.data : null;
  const routes = endpoints.state.kind === "ok" && endpoints.state.data.transports.some((t) => t.transport === "http" && t.endpoint_count > 0);
  const librariesOnly = archData !== null && archData.libraries.links_in > 0 && cardLines(archData).length === 0;
  const pending: RequestState<UnreadCounts> = unread.state.kind === "idle" ? { kind: "loading" } : unread.state;
  return (
    <div className="grid gap-3.5" data-testid="overview-page">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h1 className="m-0 text-[22px] leading-tight font-semibold tracking-[-0.01em]" data-testid="overview-title">
          {s !== null ? s.repository : <Skeleton className="inline-block h-6 w-40 align-middle" />}
        </h1>
        {s !== null && (
          <span className="text-[13px] text-muted-foreground" data-testid="overview-run">
            {s.index.last_run !== null ? `indexed ${formatShortTime(s.index.last_run.finished_at)}` : "not indexed yet"} · {s.extractor.current}
          </span>
        )}
      </div>
      {s !== null && <ParseFailedNotice count={s.index.parse_failed ?? 0} className="rounded-md border bg-dynamic-soft px-3 py-2" />}
      <div className="grid grid-cols-[minmax(0,1fr)_minmax(280px,340px)] items-start gap-3.5 max-[1100px]:grid-cols-1">
        <div className="grid min-w-0 gap-3.5">
          <PartsCard request={arch} />
          <InterfacesCard request={endpoints} arch={archData} />
          <DataCard summary={summary} />
        </div>
        <div className="grid min-w-0 gap-3.5">
          <LabelBanner />
          <UnreadCard state={pending} retry={unread.retry} />
          <StartHere routes={routes} librariesOnly={librariesOnly} />
        </div>
      </div>
    </div>
  );
}
