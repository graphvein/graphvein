// The Explorer section (T029, variant B of the T029 mockups): API
// documentation for a repository Graphvein indexed, built from the
// generated OpenAPI document (GET /api/v0/openapi) only. Operations are
// grouped (router file, or the tag of the committed spec operation that
// documents them) and collapsible, with method-coloured rows; an expanded
// operation shows parameters, request body, responses and security with a
// provenance chip on every field (variant C). The Spec tab shows the
// document itself with its download. Routes that are not in the document
// are listed below the operations, with the unknown part of their path and
// a guarded edit to supply it.
//
// It shows only what the document proves: a section that was not read
// says so, "none" appears only where a stored state proves it, and a
// partial section says what may be missing.

import { ChevronDown, ChevronRight, ChevronsDownUp, ChevronsUpDown, Lock, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { ApiError } from "@/api/client";
import { httpExplorerClient, type DocOperation, type ExplorerClient, type OpenApiDocument } from "@/api/openapi";
import { httpTryItClient, type TryItClient } from "@/api/tryit";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiRequest } from "@/hooks/useApiRequest";
import { useLabelStyle } from "@/hooks/useLabels";
import type { StatusView } from "@/hooks/useStatus";
import { label } from "@/lib/labels";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { replaceHash, useHash } from "@/router";
import { MethodBadge, methodRowTone, PathKey } from "@/sections/explorer/chips";
import { documentingOperations, emptyDocumentText, groupByFile, groupByTag, handlerName, matchesFilter, operationId, operationMarkers, routeAbsentText, routeOperations, type GroupBy, type OperationGroup } from "@/sections/explorer/model";
import { RouteLinksProvider, routeLinksCache } from "@/sections/explorer/chipLinks";
import { OperationDetail } from "@/sections/explorer/OperationDetail";
import { explorerHash, parseExplorerParams, type ExplorerParams } from "@/sections/explorer/params";
import { NotInDocument } from "@/sections/explorer/PathValues";
import { DownloadButtons, SpecView } from "@/sections/explorer/SpecView";
import { LabelChip } from "@/sections/explorer/tryit/dialogs";
import { EnvironmentBar } from "@/sections/explorer/tryit/EnvironmentBar";
import { TryItProvider, useTryIt } from "@/sections/explorer/tryit/TryIt";
import { useProject } from "@/shell/project";

/** OPEN_GROUPS_UP_TO: a document with at most this many operations opens
 * every group; a larger one starts collapsed so the groups fit on screen. */
export const OPEN_GROUPS_UP_TO = 40;

function runOf(status: StatusView): string {
  return status.kind === "ok" ? (status.status.index.last_run?.finished_at ?? "none") : "pending";
}

function Segmented<T extends string>({ value, options, onChange, label: name, disabled }: { value: T; options: { value: T; text: string; title?: string }[]; onChange: (v: T) => void; label: string; disabled?: readonly T[] }) {
  return (
    <div role="radiogroup" aria-label={name} className="inline-flex rounded-md bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          disabled={disabled?.includes(o.value) === true}
          title={o.title}
          onClick={() => {
            onChange(o.value);
          }}
          className={cn("h-7 rounded-[5px] px-3 text-[13px] whitespace-nowrap disabled:opacity-50", value === o.value ? "bg-background font-medium shadow-xs" : "text-muted-foreground hover:text-foreground")}
        >
          {o.text}
        </button>
      ))}
    </div>
  );
}

/** MemberSelect is project mode's member choice with each member's
 * operation count, read from that member's own document. */
function MemberSelect({ client, run }: { client: ExplorerClient; run: string }) {
  const project = useProject();
  const names = project?.project.members.map((m) => m.repository) ?? [];
  const counts = useApiRequest(project === null ? null : JSON.stringify(["member-counts", names, run]), async (signal) => {
    const out = new Map<string, number | string>();
    await Promise.all(
      names.map(async (n) => {
        try {
          out.set(n, (await client.memberDocument(n, { signal })).counts.operations);
        } catch (err) {
          if (signal.aborted) throw err;
          out.set(n, err instanceof ApiError && err.code !== undefined ? err.code : "not read");
        }
      }),
    );
    return out;
  });
  if (project === null) return null;
  const c = counts.state.kind === "ok" ? counts.state.data : null;
  return (
    <label className="flex items-center gap-2 text-[13px]" data-testid="member-select">
      <span className="text-muted-foreground">Member</span>
      <select
        value={project.repository}
        onChange={(e) => {
          project.select(e.target.value);
        }}
        className="h-8 rounded-md border bg-background px-2 text-[13px] font-medium"
      >
        {names.map((n) => {
          const v = c?.get(n);
          return (
            <option key={n} value={n}>
              {n}
              {v === undefined ? "" : typeof v === "number" ? ` · ${String(v)} operation${v === 1 ? "" : "s"}` : ` · ${v}`}
            </option>
          );
        })}
      </select>
      <span className="text-[12px] text-muted-foreground">one document per member</span>
    </label>
  );
}

/** LinkNote is a note a link into the Explorer puts on the operation it
 * opens (an equivalent registration written as another's operation), so
 * it is in view where the link lands instead of above the list. */
interface LinkNote {
  op: string;
  node: ReactNode;
}

function OperationRow({ op, doc, open, onToggle, claimScroll, note }: { op: DocOperation; doc: OpenApiDocument; open: boolean; onToggle: () => void; claimScroll: (id: string) => boolean; note: ReactNode }) {
  const style = useLabelStyle();
  const ref = useRef<HTMLLIElement>(null);
  // A link to this operation (op= or route= in the hash, "Open in
  // Explorer") brings it into view once, when its row first renders.
  useEffect(() => {
    const el = ref.current;
    if (!claimScroll(operationId(op)) || el === null) return;
    // Scroll the shell's main pane only: scrollIntoView would also move
    // the page and the shell around it (the top bar out of view).
    const box = el.closest("main");
    if (box !== null) box.scrollTop += el.getBoundingClientRect().top - box.getBoundingClientRect().top - 12;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, on mount
  }, []);
  const tryit = useTryIt();
  const env = tryit?.selected ?? null;
  const markers = operationMarkers(op, doc);
  const handler = handlerName(op);
  const auth = Array.isArray(op.auth.security_dependencies) && op.auth.security_dependencies.length > 0;
  const middleware = Array.isArray(op.auth.middleware) && op.auth.middleware.length > 0;
  const resolution = op.resolution === "set_by_you" ? label(style, "resolution", op.resolution) : null;
  const id = `op-${operationId(op).replace(/[^A-Za-z0-9_-]/g, "_")}`;
  return (
    <li ref={ref} className={cn("scroll-mt-3 overflow-hidden rounded-md border", methodRowTone(op.method))} data-testid="operation" data-route={op.route} data-op={operationId(op)}>
      {note}
      <button type="button" aria-expanded={open} aria-controls={id} onClick={onToggle} className="flex min-h-10 w-full items-center gap-3 px-2.5 py-1.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <MethodBadge method={op.method} />
        <span className="min-w-0 text-[14px] font-medium">
          <PathKey path={op.key} />
        </span>
        {handler !== null && <span className="min-w-0 truncate text-[13px] text-muted-foreground max-[1000px]:hidden">{handler}</span>}
        <span className="ml-auto flex flex-none flex-wrap items-center justify-end gap-1.5">
          {resolution !== null && (
            <span className="rounded-[5px] border border-brand/40 bg-brand-soft px-1.5 text-[11.5px] text-brand-text" title={resolution.title}>
              {resolution.text}
            </span>
          )}
          {markers.map((m) => (
              <span key={m.kind + m.text} title={m.title} data-marker={m.kind} className="rounded-[5px] border border-dynamic/40 bg-dynamic-soft px-1.5 text-[11.5px] text-dynamic">
                {m.text}
              </span>
            ))}
          {env !== null && (
            <span title={`requests go to ${env.name} (${env.baseUrl}), labelled ${env.label}`} data-testid="row-env-label">
              <LabelChip env={env} />
            </span>
          )}
          {(auth || middleware) && (
            <span title={auth ? "declares a security dependency (what the code declares, not proof the route is protected)" : "route middleware as written"} className="text-muted-foreground">
              <Lock aria-hidden="true" className="size-3.5" />
              <span className="sr-only">auth declared</span>
            </span>
          )}
          {open ? <ChevronDown aria-hidden="true" className="size-4" /> : <ChevronRight aria-hidden="true" className="size-4" />}
        </span>
      </button>
      {open && (
        <div id={id} className="bg-card">
          <OperationDetail op={op} doc={doc} />
        </div>
      )}
    </li>
  );
}

const METHOD_TEXT: Record<string, string> = { get: "text-m-get", post: "text-m-post", put: "text-m-put", patch: "text-m-patch", delete: "text-m-delete" };

/** MethodCounts summarises a collapsed group's methods ("GET 3 · POST 1"). */
function MethodCounts({ ops }: { ops: readonly DocOperation[] }) {
  const counts = new Map<string, number>();
  for (const m of ["get", "post", "put", "patch", "delete"]) {
    const n = ops.filter((o) => o.method === m).length;
    if (n > 0) counts.set(m, n);
  }
  for (const op of ops) if (!counts.has(op.method)) counts.set(op.method, ops.filter((o) => o.method === op.method).length);
  return (
    <span className="flex gap-2 font-mono text-[11px] max-[1100px]:hidden" aria-hidden="true">
      {[...counts.entries()].map(([m, n]) => (
        <span key={m} className={cn("font-semibold", METHOD_TEXT[m] ?? "text-foreground")}>
          {m.toUpperCase()} {n}
        </span>
      ))}
    </span>
  );
}

function Group({ group, doc, openGroup, onGroup, openOps, onOp, claimScroll, note }: { group: OperationGroup; doc: OpenApiDocument; openGroup: boolean; onGroup: () => void; openOps: ReadonlySet<string>; onOp: (id: string) => void; claimScroll: (id: string) => boolean; note: LinkNote | null }) {
  return (
    <section className="rounded-lg border bg-card" data-testid="operation-group" aria-label={group.title}>
      <button type="button" aria-expanded={openGroup} onClick={onGroup} className="flex w-full min-w-0 items-center gap-2 px-3 py-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring">
        {openGroup ? <ChevronDown aria-hidden="true" className="size-4 flex-none" /> : <ChevronRight aria-hidden="true" className="size-4 flex-none" />}
        <span className="text-[15px] font-semibold">{group.title}</span>
        <span className="min-w-0 truncate font-mono text-[12px] text-muted-foreground" title={group.subtitle}>
          {group.subtitle}
        </span>
        <span className="ml-auto flex flex-none items-center gap-2 text-[12.5px] text-muted-foreground tabular-nums">
          {!openGroup && <MethodCounts ops={group.operations} />}
          {group.operations.length} operation{group.operations.length === 1 ? "" : "s"}
        </span>
      </button>
      {openGroup && (
        <ul className="grid gap-1.5 px-2.5 pb-2.5">
          {group.operations.map((op) => (
            <OperationRow
              key={operationId(op)}
              op={op}
              doc={doc}
              open={openOps.has(operationId(op))}
              onToggle={() => {
                onOp(operationId(op));
              }}
              claimScroll={claimScroll}
              note={note !== null && note.op === operationId(op) ? note.node : null}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function Pill({ n, children, href, title, warn }: { n: number; children: ReactNode; href?: string; title?: string; warn?: boolean }) {
  const cls = cn("inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-[12.5px] whitespace-nowrap", warn === true ? "border-dashed border-dynamic/50 text-dynamic" : "bg-muted/50");
  const body = (
    <>
      <b className="font-semibold tabular-nums">{n}</b>
      {children}
    </>
  );
  return href !== undefined ? (
    <a href={href} className={cn(cls, "hover:underline")} title={title}>
      {body}
    </a>
  ) : (
    <span className={cls} title={title}>
      {body}
    </span>
  );
}

/** Coverage is the strip of counts below the toolbar: every count is the
 * document's own (x-graphvein.counts), of this repository or member. */
function Coverage({ doc }: { doc: OpenApiDocument }) {
  const c = doc.counts;
  const notIn = "#not-in-document";
  return (
    <div className="flex flex-wrap gap-1.5" data-testid="coverage">
      <Pill n={c.operations} title="operations in the generated document">
        operations
      </Pill>
      <Pill n={c.path_keys}>paths</Pill>
      <Pill n={c.with_shape} title="operations whose route has a stored request and response shape">
        with a stored shape
      </Pill>
      {c.unresolved > 0 && (
        <Pill n={c.unresolved} warn title="routes whose path has an unknown part: listed below, not in the document" href={notIn}>
          unknown path part
        </Pill>
      )}
      {c.conflicts > 0 && (
        <Pill n={c.conflicts} warn href={notIn}>
          conflicts
        </Pill>
      )}
      {c.unrepresentable > 0 && (
        <Pill n={c.unrepresentable} warn href={notIn}>
          not expressible in OpenAPI
        </Pill>
      )}
      {c.may_be_shadowed > 0 && (
        <Pill n={c.may_be_shadowed} warn title="operations another route of a first-match framework may answer first">
          may be shadowed
        </Pill>
      )}
      {c.set_by_you > 0 && <Pill n={c.set_by_you}>set by you</Pill>}
      {c.any_method > 0 && (
        <Pill n={c.any_method} href={notIn}>
          any-method routes
        </Pill>
      )}
      {doc.excludedTest.routes > 0 && (
        <Pill n={doc.excludedTest.routes} title="test-scope routes are left out of the document">
          test routes left out
        </Pill>
      )}
      <Pill n={doc.importedSpecs.length} title={doc.importedSpecs.map((s) => `${s.file}: ${String(s.operations)} operations`).join("\n") || "no OpenAPI document is committed in this repository"}>
        {doc.importedSpecs.length === 1 ? "committed spec" : "committed specs"}
      </Pill>
    </div>
  );
}

/** useTags reads the tags of the committed spec operations that document
 * the operations, only while grouping by tag. */
function useTags(client: ExplorerClient, doc: OpenApiDocument | null, on: boolean, run: string) {
  const ids = useMemo(() => (doc === null ? [] : documentingOperations(doc.operations)), [doc]);
  return useApiRequest(on && doc !== null && ids.length > 0 ? JSON.stringify(["op-tags", run, ids.length, ids[0] ?? ""]) : null, (signal) => client.operationTags(ids, { signal }));
}

/** PathValuesTarget answers a Settings link to the path value editors:
 * it scrolls to them once, or says why this document has none (they are
 * offered only on routes whose path has an unknown part). */
function PathValuesTarget({ doc }: { doc: OpenApiDocument }) {
  const editors = doc.unresolved.length > 0;
  useEffect(() => {
    if (!editors) return;
    const target = document.getElementById("not-in-document");
    if (target === null) return;
    if (typeof target.scrollIntoView === "function") target.scrollIntoView({ block: "start" });
    target.focus({ preventScroll: true });
  }, [editors]);
  return (
    <p role="status" className="rounded-lg border border-dashed px-3 py-2 text-[13px]" data-testid="path-values-target">
      {editors
        ? `Path values are entered below, under "Unknown path part": ${String(doc.unresolved.length)} route${doc.unresolved.length === 1 ? " has" : "s have"} a part of the path that is not read.`
        : `No route of ${doc.repository} has an unknown path part, so there is no path value to enter here${doc.userValues.length > 0 ? `; the ${String(doc.userValues.length)} value${doc.userValues.length === 1 ? "" : "s"} set by you ${doc.userValues.length === 1 ? "is" : "are"} listed below` : ""}.`}
    </p>
  );
}

function Body({ doc, params, set, client, run, onReload, linked, note }: { doc: OpenApiDocument; params: ExplorerParams; set: (p: Partial<ExplorerParams>) => void; client: ExplorerClient; run: string; onReload: () => void; linked: readonly string[]; note: LinkNote | null }) {
  const documented = doc.operations.some((o) => o.documentedBy.length > 0);
  const group: GroupBy = params.group === "tag" && documented ? "tag" : "file";
  const tags = useTags(client, doc, group === "tag", run);
  const filtered = useMemo(() => doc.operations.filter((o) => matchesFilter(o, params.q)), [doc, params.q]);
  const groups = useMemo(() => {
    if (group === "tag") return tags.state.kind === "ok" ? groupByTag(filtered, tags.state.data) : [];
    return groupByFile(filtered);
  }, [group, tags.state, filtered]);
  const defaultOpen = doc.operations.length <= OPEN_GROUPS_UP_TO || params.q !== "";
  const [toggled, setToggled] = useState<ReadonlySet<string>>(new Set());
  const [openOps, setOpenOps] = useState<ReadonlySet<string>>(() => new Set([...(params.op !== null ? [params.op] : []), ...linked]));
  // The operation a link points at scrolls into view once.
  const scrollTo = useRef<string | null>(params.op);
  const claimScroll = (id: string) => {
    if (scrollTo.current !== id) return false;
    scrollTo.current = null;
    return true;
  };
  const isOpen = (g: OperationGroup) => (toggled.has(g.id) ? !defaultOpen : defaultOpen) || (params.op !== null && g.operations.some((o) => operationId(o) === params.op) && !toggled.has(g.id));
  const setAll = (open: boolean) => {
    setToggled(new Set(open === defaultOpen ? [] : groups.map((g) => g.id)));
  };
  return (
    <>
      <EnvironmentBar openAtStart={params.edit === "environments"} />
      {params.edit === "path-values" && <PathValuesTarget doc={doc} />}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg border bg-card px-3 py-2" data-testid="explorer-toolbar">
        <MemberSelect client={client} run={run} />
        <label className="flex h-8 min-w-[220px] flex-1 items-center gap-2 rounded-md border bg-background px-2 text-[13px] max-[1100px]:min-w-[160px]">
          <Search aria-hidden="true" className="size-4 text-muted-foreground" />
          <input
            value={params.q}
            onChange={(e) => {
              set({ q: e.target.value });
            }}
            placeholder="Filter by path, method, handler or file"
            aria-label="Filter operations"
            className="min-w-0 flex-1 bg-transparent outline-none"
          />
        </label>
        <div className="flex items-center gap-2">
          <span className="text-[13px] text-muted-foreground">Group by</span>
          <Segmented<GroupBy>
            label="Group by"
            value={group}
            disabled={documented ? [] : ["tag"]}
            options={[
              { value: "file", text: "Router file" },
              { value: "tag", text: "OpenAPI tag", title: documented ? "the first tag of the committed spec operation that documents each route" : "no committed spec documents a route here" },
            ]}
            onChange={(v) => {
              set({ group: v });
            }}
          />
        </div>
        <div className="flex items-center gap-1">
          <Button
            size="icon-sm"
            variant="ghost"
            title="Expand all groups"
            aria-label="Expand all groups"
            onClick={() => {
              setAll(true);
            }}
          >
            <ChevronsUpDown aria-hidden="true" />
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            title="Collapse all groups"
            aria-label="Collapse all groups"
            onClick={() => {
              setAll(false);
            }}
          >
            <ChevronsDownUp aria-hidden="true" />
          </Button>
        </div>
      </div>
      <Coverage doc={doc} />
      {params.q !== "" && (
        <p className="text-[12.5px] text-muted-foreground" data-testid="filter-note">
          Showing {filtered.length} of {doc.operations.length} operations matching “{params.q}”.
        </p>
      )}
      {doc.operations.length === 0 ? (
        <div className="rounded-lg border border-dashed px-4 py-3 text-[13.5px]" data-testid="no-operations">
          {emptyDocumentText(doc)}
        </div>
      ) : group === "tag" && tags.state.kind !== "ok" ? (
        tags.state.kind === "failed" ? (
          <p className="text-[13px] text-unmatched">The committed spec operations could not be read: {tags.state.error.message}</p>
        ) : (
          <div className="grid gap-2" aria-busy="true">
            <span className="text-[13px] text-muted-foreground">Reading the tags of {documentingOperations(doc.operations).length} committed spec operations…</span>
            <Skeleton className="h-10" />
            <Skeleton className="h-10" />
          </div>
        )
      ) : filtered.length === 0 ? (
        <p className="text-[13px] text-muted-foreground">No operation matches “{params.q}”.</p>
      ) : (
        // One column no wider than the section: a group header's long
        // file path truncates instead of widening every group (about
        // 900 px wide).
        <div className="grid grid-cols-[minmax(0,1fr)] gap-2.5" data-testid="operation-groups">
          {groups.map((g) => (
            <Group
              key={g.id}
              group={g}
              doc={doc}
              openGroup={isOpen(g)}
              onGroup={() => {
                setToggled((t) => {
                  const n = new Set(t);
                  if (n.has(g.id)) n.delete(g.id);
                  else n.add(g.id);
                  return n;
                });
              }}
              openOps={openOps}
              claimScroll={claimScroll}
              note={note}
              onOp={(id) => {
                const n = new Set(openOps);
                const opening = !n.has(id);
                if (opening) n.add(id);
                else n.delete(id);
                setOpenOps(n);
                set({ op: opening ? id : params.op === id ? null : params.op });
              }}
            />
          ))}
        </div>
      )}
      <NotInDocument
        doc={doc}
        client={client}
        reloadKey={run}
        onApplied={onReload}
        focusPrefix={params.prefix}
        onPrefixDone={() => {
          set({ prefix: null });
        }}
      />
    </>
  );
}

/** ExplorerSection is the page. client replaces the requests in tests. */
export function ExplorerSection({ status, client = httpExplorerClient, tryItClient = httpTryItClient }: { status: StatusView; client?: ExplorerClient; tryItClient?: TryItClient }) {
  const hash = useHash();
  const params = parseExplorerParams(hash);
  const project = useProject();
  const run = runOf(status);
  const [reload, setReload] = useState(0);
  const answer = useApiRequest(status.kind === "ok" ? JSON.stringify(["openapi", run, reload]) : null, (signal) => client.document({ signal }));
  const set = (p: Partial<ExplorerParams>) => {
    replaceHash(explorerHash({ ...params, ...p }));
  };
  const doc = answer.state.kind === "ok" && answer.state.data.kind === "document" ? answer.state.data.document : null;
  // "Open in Explorer" from the API section names a route identity: open
  // the operations the document writes for it (the first in the hash), or
  // say why there is none. A route collapsed into another's operation as an
  // equivalent registration keeps route= beside op=, for the note.
  const linkedTo = doc !== null && params.route !== null ? routeOperations(doc, params.route) : { ops: [], equivalent: false };
  const routeOps = linkedTo.ops;
  const first = routeOps[0];
  const shown: ExplorerParams = first !== undefined && params.op === null ? { ...params, view: "operations", op: operationId(first), route: linkedTo.equivalent ? params.route : null } : params;
  const shownHash = explorerHash(shown);
  useEffect(() => {
    if (first !== undefined && params.op === null) replaceHash(shownHash);
  }, [first, params.op, shownHash]);
  const equivalentOf = linkedTo.equivalent && first !== undefined && shown.op === operationId(first) ? operationId(first) : null;
  // The chips' editor links: one endpoint detail per route and editor,
  // requested when an operation is opened, kept until the index, the
  // member or the client changes.
  const generation = doc !== null ? JSON.stringify([run, reload, doc.repository]) : null;
  const routeLinks = useMemo(() => {
    const load = client.routeLinks;
    if (load === undefined || generation === null) return null;
    return routeLinksCache((route, editor, signal) => load(route, editor, { signal }), generation);
  }, [client, generation]);
  return (
    <TryItProvider client={tryItClient} reloadKey={`${run}-${String(reload)}`}>
    <RouteLinksProvider source={routeLinks}>
    <div className="grid min-w-0 gap-3" data-testid="explorer">
      <header className="grid gap-1">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h1 className="text-[22px] font-semibold tracking-tight">Explorer</h1>
          {doc !== null && (
            <span className="rounded-md border bg-muted/60 px-2 py-0.5 text-[13px]" title={project !== null ? `member ${doc.repository} of project ${project.project.project}` : `repository ${doc.repository}`}>
              {doc.repository}
              {project !== null ? <span className="text-muted-foreground"> · member of {project.project.project}</span> : null}
            </span>
          )}
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <Segmented
              label="View"
              value={params.view}
              options={[
                { value: "operations", text: "Operations" },
                { value: "spec", text: "Spec" },
              ]}
              onChange={(v) => {
                set({ view: v });
              }}
            />
            {doc !== null && <DownloadButtons client={client} repository={doc.repository} />}
          </div>
        </div>
        {doc !== null && (
          <p className="text-[13px] text-muted-foreground" data-testid="explorer-summary">
            {doc.counts.operations} operation{doc.counts.operations === 1 ? "" : "s"} generated from {doc.index !== null ? `the index of ${formatShortTime(doc.index.finishedAt)}` : "an empty index"}
            {doc.importedSpecs.length > 0 ? ` · ${String(doc.operations.filter((o) => o.documentedBy.length > 0).length)} also documented by a committed spec` : ""}
            {project !== null ? ` · every count is of ${doc.repository}'s own document` : ""}
          </p>
        )}
      </header>
      {answer.state.kind === "failed" ? (
        <div role="alert" className="rounded-lg border border-unmatched/40 bg-unmatched-soft px-3 py-2 text-[13px]">
          The document could not be read: {answer.state.error.message}
          {answer.state.error instanceof ApiError && answer.state.error.code === "invalid_config" ? " Fix graphvein.yaml: the values set by you would otherwise be dropped silently." : ""}{" "}
          <button type="button" className="underline" onClick={answer.retry}>
            Retry
          </button>
        </div>
      ) : answer.state.kind === "ok" && answer.state.data.kind === "members" ? (
        <div className="rounded-lg border px-3 py-2 text-[13px]">
          Project {answer.state.data.list.project}: one document per member ({answer.state.data.list.members.map((m) => m.repository).join(", ")}). Choose a member in the sidebar.
        </div>
      ) : doc === null ? (
        <div className="grid gap-2" aria-busy="true" data-testid="explorer-loading">
          <Skeleton className="h-11" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
        </div>
      ) : shown.view === "spec" ? (
        <SpecView doc={doc} client={client} reloadKey={`${run}-${String(reload)}`} />
      ) : (
        <>
        {params.route !== null && first === undefined && (
          <div role="status" className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-dashed px-3 py-2 text-[13px]" data-testid="route-absent">
            <span>{routeAbsentText(doc, params.route)}</span>
            <button
              type="button"
              className="underline"
              onClick={() => {
                set({ route: null });
              }}
            >
              Dismiss
            </button>
          </div>
        )}
        <Body
          doc={doc}
          params={shown}
          linked={params.op === null ? routeOps.map(operationId) : []}
          note={
            params.route !== null && equivalentOf !== null
              ? {
                  op: equivalentOf,
                  node: (
                    <div role="status" className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-dashed bg-card px-3 py-2 text-[13px]" data-testid="route-equivalent">
                      <span>
                        Route {params.route} is an equivalent registration of {equivalentOf}: the document writes them as one operation, the first registration&apos;s, as the matcher picks it.
                      </span>
                      <button
                        type="button"
                        className="underline"
                        onClick={() => {
                          set({ route: null });
                        }}
                      >
                        Dismiss
                      </button>
                    </div>
                  ),
                }
              : null
          }
          set={set}
          client={client}
          run={`${run}-${String(reload)}`}
          onReload={() => {
            setReload((r) => r + 1);
          }}
        />
        </>
      )}
      {doc !== null && (
        <p className="text-[12px] text-muted-foreground">
          Callers and impact of each operation are in the <a href="#/api" className="underline">API section</a>. Generated from stored facts only: no code is run, and nothing is sent until you press Send in Try it out.
        </p>
      )}
    </div>
    </RouteLinksProvider>
    </TryItProvider>
  );
}

export default ExplorerSection;
