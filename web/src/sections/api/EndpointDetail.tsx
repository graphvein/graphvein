// The detail pane of one endpoint (D3, D8): header with the composed
// paths or the command, the stale note when its files changed, then the
// tabs of T016f UQ3 B: Overview (handler, OpenAPI), Callers N (the callers
// table or the never-called state) and Flow (the graph, drawn by the lazy
// Flow tab and passed in as `flow`).

import { ArrowUpRight, TriangleAlert } from "lucide-react";
import { lazy, Suspense, useRef, type KeyboardEvent, type ReactNode } from "react";

import { categoryCounts, CATEGORY_LABEL, neverCalledLabel, neverScoped, POSSIBLY_CALLED, TRANSPORT_LABEL } from "@/api/categories";
import type { EndpointDetail as Detail, PresentedNode, TransportSummary } from "@/api/endpoints";
import { NO_PATH_TITLE, NO_PATH_WORDS, isFullPathEntry, noPathKind, pathAssumptions, pathAssumptionTitle, prefixReasonText, prefixUnknownPaths, servedPath, shadowedPaths } from "@/lib/routePath";
import { cn } from "@/lib/utils";
import { explorerRouteHash } from "@/sections/explorer/params";
import { ProjectCallers } from "@/sections/api/ProjectCallers";
import { CallersTable, PeekButton, shownNearMisses, splitCallers } from "@/sections/api/CallersTable";
import { NeverCalled } from "@/sections/api/NeverCalled";
import { DEFAULT_TAB, DETAIL_TABS, type DetailTab, type GapCategory } from "@/sections/api/params";
import { FACT_PREFIX_UNKNOWN, FamilyChip, UnverifiedMark } from "@/sections/api/precision";
import { BADGE, IdeButton, Method, PathText, TONE_CLASS, TransportBadge, WrappedPath, callerName, EdgeConfidence, FactBadge } from "@/sections/api/parts";

import { itemsFor, VIEW_GROUPS } from "@/sections/changes/markers";
import { ChangeMarker } from "@/shell/ChangeMarker";
import { WrongFactFlag } from "@/components/WrongFactFlag";
import { useReport } from "@/shell/report";
import { callerFact, endpointFact, endpointText, whereOf } from "@/sections/api/wrongFact";
import { useChangeMarkers } from "@/shell/markers";

type Attrs = Record<string, unknown>;

// The Request and response block (T029) shares the Explorer's section
// pieces; it is its own chunk, loaded when an HTTP route's Overview opens.
const RouteShapeBlock = lazy(() => import("@/sections/api/RouteShapeBlock"));

function s(a: Attrs, key: string): string | null {
  const v = a[key];
  return typeof v === "string" ? v : null;
}

function list(a: Attrs, key: string): unknown[] {
  const v = a[key];
  return Array.isArray(v) ? v : [];
}

function text(v: unknown): string {
  return typeof v === "string" ? v : JSON.stringify(v);
}

function line(n: PresentedNode | null): number | null {
  return n?.span?.start_line ?? null;
}

function changed(n: PresentedNode | null | undefined): n is PresentedNode {
  return n !== null && n !== undefined && n.file_state !== null && n.file_state !== "unchanged";
}

/** StaleNote is M5's note for a file that changed since the index. */
export function StaleNote({ nodes }: { nodes: (PresentedNode | null | undefined)[] }) {
  const files = [...new Set(nodes.filter(changed).map((n) => `${n.file}\u0000${n.file_state ?? ""}`))];
  if (files.length === 0) return null;
  return (
    <div className="grid gap-1" data-testid="stale-note">
      {files.map((f) => {
        const [file = "", state] = f.split("\u0000");
        return (
          <div key={f} className="flex items-start gap-2 rounded-lg border border-stale/55 bg-stale/10 px-2.5 py-1.5 text-[12.5px]">
            <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 flex-none text-dynamic" />
            <div>
              <b className="font-semibold [overflow-wrap:anywhere]">
                {file} {state === "missing" ? "cannot be read now" : "changed since the last index"}
              </b>{" "}
              <span className="text-muted-foreground">· lines and callers may be outdated · run graphvein index</span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function Section({ title, aside, children, last }: { title: ReactNode; aside?: ReactNode; children: ReactNode; last?: boolean }) {
  return (
    <div className={cn("px-5 py-3", last !== true && "border-b")}>
      {title !== null && (
        <h4 className="m-0 mb-2 flex flex-wrap items-center gap-2 text-sm font-semibold">
          {title}
          {aside !== undefined && (
            <>
              <span className="flex-1" />
              {aside}
            </>
          )}
        </h4>
      )}
      {children}
    </div>
  );
}

function Grid({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-[96px_minmax(0,1fr)] items-baseline gap-x-3.5 gap-y-[5px] text-[13px]">{children}</div>;
}

function K({ children }: { children: ReactNode }) {
  return <div className="text-muted-foreground">{children}</div>;
}

function V({ children }: { children: ReactNode }) {
  return <div className="flex min-w-0 flex-wrap items-center gap-2">{children}</div>;
}

const OUTLINE = cn(BADGE, "border-border font-medium");
const SECONDARY = cn(BADGE, "bg-muted font-medium");

/** EndpointMarker is the selected endpoint's "changed since" marker, a
 * link to its item in Changes (the list row's marker is a decoration). */
function EndpointMarker({ id }: { id: string }) {
  const m = useChangeMarkers(VIEW_GROUPS.api);
  if (m.kind !== "ok") return null;
  return <ChangeMarker items={itemsFor(m.index, id)} baseText={m.baseText} className="mt-[3px]" />;
}

/** PathAssumptionChip names the assumptions a composed path rests on (unit
 * UP-F: GR-V5, RO-V1, RO-V2), with their texts in the tooltip. */
function PathAssumptionChip({ entry }: { entry: Attrs }) {
  const a = pathAssumptions(entry);
  if (a === null) return null;
  return (
    <span className={cn(BADGE, TONE_CLASS.dynamic)} title={pathAssumptionTitle(a)} data-testid="path-assumption">
      assumes {a.ids.join(", ")}
    </span>
  );
}

/** PathMarks are a composed path's marks: how it was resolved, a
 * condition, optional, its assumptions and test scope. */
function PathMarks({ p, d }: { p: Attrs; d: Detail }) {
  return (
    <>
      {s(p, "resolution") !== null && <span className={OUTLINE}>{s(p, "resolution")}</span>}
      {p.conditional === true && (
        <span className={OUTLINE}>
          conditional <UnverifiedMark label={d.precision} fact="conditional" />
        </span>
      )}
      {p.optional === true && <span className={OUTLINE}>optional</span>}
      <PathAssumptionChip entry={p} />
      {p.scope === "test" && (
        <span className={cn(BADGE, TONE_CLASS.other)}>
          test <UnverifiedMark label={d.precision} fact="scope" />
        </span>
      )}
    </>
  );
}

function HttpHeader({ d }: { d: Detail }) {
  const a = d.endpoint.attributes;
  // Full composed paths only: a path whose prefix the code does not fix
  // is shown with "…" for the prefix and its reason (T033 row UP-29).
  const paths = list(a, "paths").filter((p): p is Attrs => typeof p === "object" && p !== null && isFullPathEntry(p));
  // Unknown-prefix entries are shown beside full paths only in test scope
  // (UP-9 per scope: a route whose full paths are all test scope keeps
  // its production entries).
  const prefixed = paths.every((p) => p.scope === "test") ? prefixUnknownPaths(a) : [];
  const shadowed = paths.length === 0 && prefixed.length === 0 ? shadowedPaths(a) : [];
  const method = s(a, "method");
  const first = paths[0];
  const shownPath = first !== undefined ? servedPath(first) : null;
  const firstPrefixed = prefixed[0];
  // One composed path that is the title (T071 question 10, T073): it is
  // not printed again; its marks join the facts line as "1 composed path,
  // same as above". Several paths keep their list.
  const folded = paths.length === 1 && first !== undefined && pathAssumptions(first) !== null ? first : null;
  return (
    <>
      <div className="flex items-start gap-2.5">
        <span className={cn(OUTLINE, "mt-px")}>
          <Method method={method} className="text-xs" />
        </span>
        <h2 className="m-0 font-mono text-[17px] leading-[1.35] font-semibold tracking-[-0.01em] [overflow-wrap:anywhere]">
          {shownPath !== null ? (
            <PathText path={shownPath} />
          ) : firstPrefixed !== undefined ? (
            <PathText path={firstPrefixed.path} />
          ) : (
            <PathText path={s(a, "pattern") ?? d.endpoint.name} />
          )}
        </h2>
        {paths.length === 0 && firstPrefixed === undefined && shadowed.length === 0 && (
          <span className={cn(BADGE, TONE_CLASS.never, "mt-0.5")} title={NO_PATH_TITLE[noPathKind(false, d.path_withheld)]} data-testid="no-path-badge">
            {NO_PATH_WORDS[noPathKind(false, d.path_withheld)]}
          </span>
        )}
        {shadowed.length > 0 && (
          <span
            className={cn(BADGE, TONE_CLASS.dynamic, "mt-0.5")}
            title={`Every composed path is taken first by a mount added before the route: ${shadowed.map((x) => `${x.path} by ${x.by}`).join("; ")}`}
            data-testid="shadowed-badge"
          >
            shadowed
          </span>
        )}
        {paths.length === 0 && firstPrefixed !== undefined && (
          <span className={cn(BADGE, TONE_CLASS.dynamic, "mt-0.5")} title={prefixReasonText(firstPrefixed.reason)} data-testid="prefix-unknown-badge">
            prefix unknown
          </span>
        )}
        <EndpointMarker id={d.endpoint.identity} />
      </div>
      {firstPrefixed !== undefined && (
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-1 text-[12.5px] [overflow-wrap:anywhere]" data-testid="prefix-unknown-paths">
          <div className="text-xs text-muted-foreground">
            Served below a prefix the code does not fix: “…” stands for it, the rest is proven. Never a full path; the as-written pattern is{" "}
            <span className="font-mono">{s(a, "pattern") ?? d.endpoint.name}</span>.
            <UnverifiedMark label={d.precision} fact={FACT_PREFIX_UNKNOWN} />
          </div>
          {prefixed.map((p, i) => (
            <div key={i} className="flex min-w-0 flex-wrap items-center gap-1.5">
              <span className="min-w-0 font-mono [overflow-wrap:anywhere]">
                <PathText path={p.path} />
              </span>
              <span className="text-muted-foreground">{prefixReasonText(p.reason)}</span>
              {p.hop !== null && (
                <span className="min-w-0 rounded-md border px-[7px] font-mono text-xs leading-5 [overflow-wrap:anywhere] text-muted-foreground" title="the unresolved mount the path comes from">
                  {p.hop}
                </span>
              )}
              {p.scope === "test" && <span className={cn(BADGE, TONE_CLASS.other)}>test</span>}
            </div>
          ))}
        </div>
      )}
      {shadowed.length > 0 && (
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-1 text-[12.5px] [overflow-wrap:anywhere]" data-testid="shadowed-paths">
          <div className="text-xs text-muted-foreground">Mounted, but a mount added before the route takes every composed path first; no path is stored.</div>
          {shadowed.map((x, i) => (
            <div key={i} className="flex min-w-0 flex-wrap items-center gap-1.5">
              <span className="min-w-0 font-mono [overflow-wrap:anywhere]">
                <PathText path={x.path} />
              </span>
              <span className="text-muted-foreground">taken first by</span>
              <span className="min-w-0 font-mono text-xs [overflow-wrap:anywhere] text-muted-foreground">{x.by}</span>
            </div>
          ))}
        </div>
      )}
      {folded === null && (paths.length > 1 || paths.some((p) => pathAssumptions(p) !== null)) && (
        <div className="grid gap-1" data-testid="composed-paths">
          <div className="text-xs text-muted-foreground">
            {paths.length} composed {paths.length === 1 ? "path" : "paths"}
          </div>
          {paths.map((p, i) => (
            <div key={i} className="flex flex-wrap items-center gap-1.5 text-[12.5px]">
              <span className="font-mono">
                <PathText path={servedPath(p) ?? "(no template)"} />
              </span>
              <PathMarks p={p} d={d} />
            </div>
          ))}
        </div>
      )}
      {/* The facts line (T071, T073): transport, framework and repository
          as plain text, the folded composed path's marks, then the
          precision chip, the unverified count and the fact badge; one line
          where the pane is wide enough. */}
      <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-[12.5px]" data-testid="route-facts">
        <span className="inline-flex items-center gap-1.5 text-muted-foreground" data-testid="route-facts-text">
          <span aria-hidden="true" className="size-[7px] rounded-[2px] bg-http-bar" />
          <span>
            {[TRANSPORT_LABEL.http, s(a, "framework"), d.repository].filter((x): x is string => x !== null && x !== "").join(" · ")}
            {folded !== null && <span data-testid="composed-path-same"> · 1 composed path, same as above ·</span>}
          </span>
        </span>
        {folded !== null && <PathMarks p={folded} d={d} />}
        {a.scope === "test" && (
          <span className={cn(BADGE, TONE_CLASS.other)}>
            test scope <UnverifiedMark label={d.precision} fact="scope" />
          </span>
        )}
        <FamilyChip transport="http" which="endpoints" label={d.precision} testId="detail-precision" countUnverified />
        <FactBadge />
        <span className="text-muted-foreground">from the route registration</span>
      </div>
    </>
  );
}

function TauriHeader({ d }: { d: Detail }) {
  const a = d.endpoint.attributes;
  const registry = s(a, "registry");
  return (
    <>
      <div className="flex items-start gap-2.5">
        <h2 className="m-0 font-mono text-[17px] leading-[1.35] font-semibold [overflow-wrap:anywhere]">
          {s(a, "command") ?? d.endpoint.name}
        </h2>
        {s(a, "command") === null && <span className={cn(BADGE, TONE_CLASS.other, "mt-0.5")}>entry not resolved</span>}
        <EndpointMarker id={d.endpoint.identity} />
      </div>
      <div className="flex flex-wrap items-center gap-1.5 text-[12.5px]">
        <TransportBadge transport="tauri_ipc" />
        {s(a, "framework") !== null && <span className={SECONDARY}>{s(a, "framework")}</span>}
        <span className={SECONDARY}>{d.repository}</span>
        {registry !== null && <span className={OUTLINE}>registry: {registry}</span>}
        {a.cfg_gated === true && <span className={OUTLINE}>cfg-gated</span>}
        {a.test_only === true && <span className={cn(BADGE, TONE_CLASS.other)}>test only</span>}
        {a.unreachable === true && <span className={cn(BADGE, TONE_CLASS.other)}>unreachable</span>}
        {s(a, "plugin") !== null && <span className={OUTLINE}>plugin {s(a, "plugin")}</span>}
        <FamilyChip transport="tauri_ipc" which="endpoints" label={d.precision} testId="detail-precision" />
        <FactBadge />
        <span className="text-muted-foreground">from the command registration</span>
      </div>
    </>
  );
}

/** unreadUses is a server action's stored count of unread uses that may
 * reach it (T024b R1); 0 for every other endpoint. */
function unreadUses(d: Detail): number {
  const v = d.endpoint.attributes.unread_uses;
  return typeof v === "number" && v > 0 ? v : 0;
}

function ActionHeader({ d }: { d: Detail }) {
  const a = d.endpoint.attributes;
  const action = s(a, "action");
  const scope = s(a, "action_scope");
  return (
    <>
      <div className="flex items-start gap-2.5">
        <h2 className="m-0 font-mono text-[17px] leading-[1.35] font-semibold [overflow-wrap:anywhere]">{action ?? d.endpoint.name}</h2>
        {action === null && <span className={cn(BADGE, TONE_CLASS.other, "mt-0.5")}>anonymous</span>}
        <EndpointMarker id={d.endpoint.identity} />
      </div>
      <div className="flex flex-wrap items-center gap-1.5 text-[12.5px]">
        <TransportBadge transport="server_action" />
        {s(a, "framework") !== null && <span className={SECONDARY}>{s(a, "framework")}</span>}
        <span className={SECONDARY}>{d.repository}</span>
        {scope !== null && <span className={OUTLINE}>{scope === "inline" ? "inline 'use server'" : "'use server' module"}</span>}
        {a.scope === "test" && <span className={cn(BADGE, TONE_CLASS.other)}>test scope</span>}
        <FamilyChip transport="server_action" which="endpoints" label={d.precision} testId="detail-precision" />
        <FactBadge />
        <span className="text-muted-foreground">from the &apos;use server&apos; directive</span>
      </div>
    </>
  );
}

/** PeekTarget is a node of the detail pane whose code peek can open, with
 * the header text the peek shows for it. */
export interface PeekTarget {
  id: string;
  title: string;
  step: string;
}

/** peekTargets lists the peekable nodes of a detail in the order the pane
 * shows them: the handler, the callers (linked first, as the table) and
 * the near misses. */
export function peekTargets(d: Detail): PeekTarget[] {
  const out: PeekTarget[] = [];
  const h = d.handler.node;
  if (h !== null) out.push({ id: h.identity, title: h.qualified_name || h.name, step: "handler" });
  const { linked, others } = splitCallers(d.callers);
  const callers = [...linked, ...others.flatMap((g) => g.callers)];
  callers.forEach((c, i) => {
    out.push({ id: c.node.identity, title: callerName(c.owner, c.node), step: `caller ${String(i + 1)} of ${String(callers.length)}` });
  });
  const near = shownNearMisses(d.transport, d.near_misses);
  near.forEach((n, i) => {
    out.push({ id: n.node.identity, title: callerName(n.owner, n.node), step: `near miss ${String(i + 1)} of ${String(near.length)}` });
  });
  return out;
}

interface PeekProps {
  peek: string | null;
  onPeek: ((id: string | null) => void) | undefined;
}

function HandlerSection({ d, peek, onPeek }: { d: Detail } & PeekProps) {
  const h = d.handler;
  const http = d.transport === "http";
  const action = d.transport === "server_action";
  const name = h.node?.qualified_name ?? (h.state === "inline" ? "inline" : null);
  const edge = h.edge;
  const middleware = list(d.endpoint.attributes, "middleware");
  return (
    <Section
      title={
        <>
          Handler
          {name !== null && <span className="font-mono text-[13px] font-semibold">{name}</span>}
          <UnverifiedMark label={d.precision} fact="handler" />
        </>
      }
      aside={
        h.node !== null ? (
          <span className="flex items-center gap-1">
            {onPeek !== undefined && (
              <PeekButton
                label="Peek handler source"
                opener={h.node.identity}
                pressed={peek === h.node.identity}
                onClick={() => {
                  const id = h.node?.identity ?? null;
                  onPeek(id === null || peek === id ? null : id);
                }}
              />
            )}
            <IdeButton ide={h.node.ide} status={d.ide} label place={h.node.identity} />
          </span>
        ) : undefined
      }
    >
      <Grid>
        {!http && (
          <>
            <K>Registered</K>
            <V>
              <WrappedPath file={d.endpoint.file} line={line(d.endpoint)} />
              <span className="text-muted-foreground">{action ? "'use server' action" : "generate_handler! entry"}</span>
              <IdeButton ide={d.endpoint.ide} status={d.ide} place={d.endpoint.identity} />
            </V>
          </>
        )}
        <K>{http ? "Location" : "Handler"}</K>
        <V>
          {h.state === "resolved" && h.node !== null ? (
            <WrappedPath file={h.node.file} line={line(h.node)} />
          ) : h.state === "inline" ? (
            <span className="text-muted-foreground">Inline handler in the route registration</span>
          ) : h.state === "missing" ? (
            <span className="text-muted-foreground">The handler named by the {http ? "route" : action ? "action" : "entry"} is not in the index</span>
          ) : http ? (
            <span className="text-muted-foreground">Handler not resolved</span>
          ) : action ? (
            <span className="text-muted-foreground">An anonymous inline action: no function node</span>
          ) : (
            <span className="text-muted-foreground">Entry not resolved to a command function</span>
          )}
          {h.state === "unresolved" && h.expression !== null && <span className="font-mono text-xs">{h.expression}</span>}
        </V>
        {http && (
          <>
            <K>Route</K>
            <V>
              <WrappedPath file={d.endpoint.file} line={line(d.endpoint)} />
              <IdeButton ide={d.endpoint.ide} status={d.ide} place={d.endpoint.identity} />
            </V>
          </>
        )}
        {edge !== null && (
          <>
            <K>Link</K>
            <V>
              <EdgeConfidence edge={edge} />
              <span className="text-muted-foreground">{edge.kind}</span>
              <UnverifiedMark label={d.precision} fact="handler" />
            </V>
          </>
        )}
        {http && middleware.length > 0 && (
          <>
            <K>Middleware</K>
            <V>
              {middleware.map((m, i) => (
                <span key={i} className={cn(OUTLINE, "font-mono")}>
                  {text(m)}
                </span>
              ))}
              <UnverifiedMark label={d.precision} fact="middleware" />
            </V>
          </>
        )}
      </Grid>
    </Section>
  );
}

/** ShapeSection is the route's request and response shape (T029, rows
 * OX-40..OX-43), with a link to the same operation in the Explorer. */
function ShapeSection({ d }: { d: Detail }) {
  if (d.transport !== "http") return null;
  return (
    <Section
      title={
        <>
          Request and response
          {d.project !== null && <span className="text-xs font-normal text-muted-foreground">in {d.repository}</span>}
        </>
      }
      aside={
        <a
          href={explorerRouteHash(d.endpoint.identity)}
          className="inline-flex items-center gap-1 text-[12.5px] font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]"
          data-testid="open-in-explorer"
          title="The operation the generated OpenAPI document writes for this route, with Try it out"
        >
          Open in Explorer
          <ArrowUpRight aria-hidden="true" className="size-3.5" />
        </a>
      }
    >
      <Suspense fallback={<div className="h-16 animate-pulse rounded-md bg-muted" aria-busy="true" data-testid="route-shape-loading" />}>
        <RouteShapeBlock
          shape={d.shape}
          shapeError={d.shape_error}
          links={{ status: d.ide, links: d.shape_links }}
          modelTables={d.model_tables}
          facts={{ file: d.endpoint.file, line: line(d.endpoint) ?? 0, framework: s(d.endpoint.attributes, "framework"), attributes: d.endpoint.attributes }}
        />
      </Suspense>
    </Section>
  );
}

function OpenApiSection({ d }: { d: Detail }) {
  const o = d.openapi;
  if (o === null) return null;
  return (
    <Section title="OpenAPI">
      {o.operations.length === 0 ? (
        <span className={cn(BADGE, TONE_CLASS.never)} data-testid="openapi-state">
          {o.specs === 0 ? "No OpenAPI spec indexed" : "Not in any indexed spec"}
        </span>
      ) : (
        <div className="grid gap-2.5" data-testid="openapi-state">
          {o.operations.map((op) => {
            const a = op.node.attributes;
            const tags = list(a, "tags").map(text);
            const codes = list(a, "response_codes").map(text);
            return (
              <div key={op.node.identity} className="grid gap-1 text-[13px]">
                <div className="flex flex-wrap items-center gap-2">
                  <span className={cn(BADGE, TONE_CLASS.linked)}>Documented</span>
                  <span className="font-mono font-medium">{s(a, "operation_id") ?? op.node.name}</span>
                  <span className="font-mono text-xs">
                    <Method method={s(a, "method")} /> {s(a, "path") ?? ""}
                  </span>
                  {a.deprecated === true && <span className={cn(BADGE, TONE_CLASS.unmatched)}>deprecated</span>}
                  <span className="flex-1" />
                  <EdgeConfidence edge={op.edge} />
                  <IdeButton ide={op.node.ide} status={d.ide} place={op.node.identity} />
                </div>
                {s(a, "summary") !== null && s(a, "summary") !== "" && <div className="text-muted-foreground">{s(a, "summary")}</div>}
                <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                  <WrappedPath file={op.node.file} line={line(op.node)} />
                  {tags.map((t) => (
                    <span key={t} className={OUTLINE}>
                      {t}
                    </span>
                  ))}
                  {codes.length > 0 && <span className="font-mono">responses {codes.join(", ")}</span>}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </Section>
  );
}

export interface EndpointDetailProps {
  detail: Detail;
  summary: TransportSummary | undefined;
  openGap: (category: GapCategory) => void;
  /** The identity whose code peek is open, or null. */
  peek?: string | null | undefined;
  /** Opens or closes the code peek; without it nothing is peekable. */
  onPeek?: ((id: string | null) => void) | undefined;
  /** The open tab (DEFAULT_TAB when omitted). */
  tab?: DetailTab | undefined;
  /** Switches tabs; without it the strip is not shown. */
  onTab?: ((tab: DetailTab) => void) | undefined;
  /** The Flow tab's content (the lazy graph). */
  flow?: ReactNode;
  /** Controls at the right end of the tab row (the Flow tab's). */
  tabAside?: ReactNode;
}

const TAB_LABEL: Record<DetailTab, string> = { overview: "Overview", callers: "Callers", flow: "Flow" };

/** TAB_PANEL_ID is the element id of the open tab's panel. */
export const TAB_PANEL_ID = "endpoint-tab-panel";

function TabStrip({ tab, onTab, callers, projectCallers = 0, aside }: { tab: DetailTab; onTab: (t: DetailTab) => void; callers: number; projectCallers?: number; aside: ReactNode }) {
  // On the Flow tab the row (with the hop control) sticks to the top of
  // the pane if the pane ever scrolls (a very short split).
  const sticky = tab === "flow";
  const refs = useRef<Partial<Record<DetailTab, HTMLButtonElement | null>>>({});
  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    const i = DETAIL_TABS.indexOf(tab);
    let next: DetailTab | undefined;
    if (e.key === "ArrowRight") next = DETAIL_TABS[(i + 1) % DETAIL_TABS.length];
    else if (e.key === "ArrowLeft") next = DETAIL_TABS[(i + DETAIL_TABS.length - 1) % DETAIL_TABS.length];
    else if (e.key === "Home") next = DETAIL_TABS[0];
    else if (e.key === "End") next = DETAIL_TABS[DETAIL_TABS.length - 1];
    if (next === undefined) return;
    e.preventDefault();
    onTab(next);
    refs.current[next]?.focus();
  };
  return (
    <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-1.5 border-b px-5", sticky && "sticky top-0 z-[6] bg-card")}>
      <div role="tablist" aria-label="Endpoint views" className="flex gap-1">
        {DETAIL_TABS.map((t) => (
          <button
            key={t}
            ref={(el) => {
              refs.current[t] = el;
            }}
            type="button"
            role="tab"
            id={`endpoint-tab-${t}`}
            aria-selected={tab === t}
            aria-controls={TAB_PANEL_ID}
            tabIndex={tab === t ? 0 : -1}
            data-testid={`tab-${t}`}
            className={cn(
              "-mb-px inline-flex h-10 flex-none items-center gap-1.5 border-b-2 px-2 text-[13.5px] whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-ring",
              tab === t ? "border-brand font-semibold text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
            )}
            onClick={() => {
              onTab(t);
            }}
            onKeyDown={onKeyDown}
          >
            {TAB_LABEL[t]}
            {t === "callers" && <span className="inline-flex h-5 flex-none items-center rounded-[4px] border border-transparent bg-muted px-1.5 font-mono text-xs leading-none whitespace-nowrap text-muted-foreground" data-testid="caller-count">{callers}</span>}
            {t === "callers" && projectCallers > 0 && (
              <span className="inline-flex h-5 flex-none items-center rounded-[4px] border border-dashed border-brand/40 px-1.5 font-mono text-xs leading-none whitespace-nowrap text-brand-text" title={`${String(projectCallers)} from other repositories of the project`} data-testid="project-caller-count">
                +{projectCallers}
              </span>
            )}
          </button>
        ))}
      </div>
      <span className="flex-1" />
      {aside}
    </div>
  );
}

/** EndpointDetailView renders a loaded endpoint. */
export function EndpointDetailView({ detail: d, summary, openGap, peek = null, onPeek, tab = DEFAULT_TAB, onTab, flow, tabAside }: EndpointDetailProps) {
  // The header is a flag group only while the switch is on (markup unchanged off).
  const flags = useReport().url !== null;
  const near = shownNearMisses(d.transport, d.near_misses);
  const counts = summary !== undefined ? categoryCounts(d.transport, summary.calls.by_status) : undefined;
  const dynamicCount = counts?.get("dynamic")?.count ?? 0;
  const wrapperCount = counts?.get("wrapper")?.count ?? 0;
  const isFlow = tab === "flow";
  const headSticky = onTab !== undefined && peek !== null && !isFlow;
  // Project mode: calls of other members linked to this route. They are
  // not callers inside this repository, but the route is not "never
  // called" in the project either (T022 B9).
  const projectCallers = d.project?.callers.length ?? 0;
  // Calls of other members the link pass left ambiguous
  // (or incomplete) with this route among the candidates: not callers, but
  // the route is not "no linked caller" either.
  const possibleCallers = d.project?.possible_callers.filter((p) => p.kind === "calls_route").length ?? 0;
  const neverInRepo = d.callers.length === 0 && projectCallers === 0 && possibleCallers === 0;
  const possiblyOnly = d.callers.length === 0 && projectCallers === 0 && possibleCallers > 0;
  const conflicts = new Set((d.project?.declaration_conflicts ?? []).map((c) => c.call));
  return (
    <div data-testid="endpoint-detail" data-endpoint={d.endpoint.identity} data-tab={tab} className={cn(isFlow && "flex flex-1 flex-col")}>
      {/* While the code peek is open the pane is short: the route header and
       * the tabs stay at its top while the rows scroll under them (the API
       * section sizes the split so they fit, ApiSection.tsx). Otherwise the
       * wrapper adds no box, so the Flow tab's own sticky tab row still
       * sticks. */}
      <div data-detail-head="" className={headSticky ? "sticky top-0 z-[6] bg-card" : "contents"}>
        <div className={cn(flags && "group/fact", "grid gap-2.5 px-5 pt-3.5 pb-3", onTab === undefined && "border-b")}>
          <div className="flex items-start gap-1.5">
            <div className="grid min-w-0 flex-1 gap-2.5">{d.transport === "http" ? <HttpHeader d={d} /> : d.transport === "server_action" ? <ActionHeader d={d} /> : <TauriHeader d={d} />}</div>
            <WrongFactFlag fact={() => endpointFact(d)} />
          </div>
          <StaleNote nodes={[d.endpoint, d.handler.node]} />
          {!d.ide.available && (
            <div className="text-[12.5px] text-muted-foreground">IDE links are unavailable: {d.ide.error ?? "configuration error"}</div>
          )}
        </div>
        {onTab !== undefined && <TabStrip tab={tab} onTab={onTab} callers={d.callers.length} projectCallers={projectCallers} aside={isFlow ? tabAside : null} />}
      </div>
      <div
        id={onTab !== undefined ? TAB_PANEL_ID : undefined}
        role={onTab !== undefined ? "tabpanel" : undefined}
        aria-labelledby={onTab !== undefined ? `endpoint-tab-${tab}` : undefined}
        className={cn(isFlow && "flex flex-1 flex-col")}
      >
      {tab === "flow" && flow}
      {tab === "overview" && (
        <>
          <HandlerSection d={d} peek={peek} onPeek={onPeek} />
          <ShapeSection d={d} />
          <OpenApiSection d={d} />
        </>
      )}
      {tab === "callers" && (
      <Section
        title={
          neverInRepo ? (
            <>
              Callers{" "}
              {unreadUses(d) > 0 ? (
                <span className={cn(BADGE, TONE_CLASS.other)}>may be called</span>
              ) : (
                <span className={cn(BADGE, TONE_CLASS.never)}>{neverCalledLabel(d.scope, undefined, d.project !== null)}</span>
              )}
            </>
          ) : possiblyOnly ? (
            <>
              Callers{" "}
              <span className={cn(BADGE, TONE_CLASS.other)} data-testid="possibly-called-badge">
                {POSSIBLY_CALLED}
              </span>
            </>
          ) : d.callers.length === 0 ? (
            "Callers"
          ) : null
        }
        last
      >
        {d.callers.length === 0 && unreadUses(d) > 0 && (
          <p className="text-[13px] text-muted-foreground" data-testid="may-be-called">
            No linked use, but {unreadUses(d) === 1 ? "1 unread use" : `${String(unreadUses(d))} unread uses`} (an action passed as a prop or
            an argument, or read through a member) may reach this action; it is not listed as having no linked caller.
          </p>
        )}
        {d.callers.length === 0 && projectCallers > 0 && (
          <p className="m-0 text-[12.5px] text-muted-foreground" data-testid="no-repo-callers">
            No caller inside {d.repository}; {projectCallers} {projectCallers === 1 ? "call" : "calls"} of other repositories {projectCallers === 1 ? "is" : "are"} linked to this route below.
          </p>
        )}
        {neverInRepo && unreadUses(d) === 0 && (
          <NeverCalled
            transport={d.transport}
            repository={d.repository}
            scope={d.scope}
            root={d.root}
            dynamicCount={dynamicCount}
            neverCount={summary !== undefined ? neverScoped(summary).count : 0}
            project={d.project !== null}
            onReviewDynamic={() => {
              openGap("dynamic");
            }}
            onAllNever={() => {
              openGap("never");
            }}
          />
        )}
        {(d.callers.length > 0 || near.length > 0 || d.near_misses_incomplete > 0) && (
          <div className={cn(d.callers.length === 0 && "mt-4")}>
            <CallersTable
              transport={d.transport}
              callers={d.callers}
              nearMisses={d.near_misses}
              nearMissesIncomplete={d.near_misses_incomplete}
              ide={d.ide}
              peek={peek}
              onPeek={onPeek}
              precision={d.callers_precision}
              conflicts={conflicts}
              factOf={(c) => callerFact(d, c, null)}
            />
          </div>
        )}
        <ProjectCallers project={d.project} repository={d.repository} route={{ text: endpointText(d), repository: d.repository, where: whereOf(d.endpoint) }} />
        {d.transport === "tauri_ipc" && wrapperCount > 0 && (
          <p className="mt-2.5 text-[12.5px] leading-[1.7] text-muted-foreground">
            <span className={cn(BADGE, TONE_CLASS.wrapper, "mr-1.5 font-medium")}>{CATEGORY_LABEL.wrapper.toLowerCase()}</span>
            {wrapperCount === 1 ? "The inner invoke site of a wrapper is" : `The ${String(wrapperCount)} inner invoke sites of wrappers are`} not
            callers of this command; the wrapper's call sites are.{" "}
            <button
              type="button"
              className="font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]"
              onClick={() => {
                openGap("wrapper");
              }}
            >
              See {wrapperCount} wrapper {wrapperCount === 1 ? "site" : "sites"}
            </button>
          </p>
        )}
      </Section>
      )}
      </div>
    </div>
  );
}
