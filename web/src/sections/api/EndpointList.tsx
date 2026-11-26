// The endpoint list (left pane): one group per transport with a sticky
// header, fixed-height rows in a windowed listbox, a filter box and a
// footer with counts and the key hint.

import { CircleHelp, FlaskConical, GitBranch, Layers, ListFilter, Settings2, Unplug } from "lucide-react";
import { useMemo } from "react";

import { POSSIBLY_CALLED, TRANSPORT_LABEL, endpointNoun, isNeverRow, isPossiblyCalledRow, neverCalledLabel } from "@/api/categories";
import type { EndpointRow, Transport, TransportSummary } from "@/api/endpoints";
import { VirtualList, type VirtualItem } from "@/components/VirtualList";
import { NO_PATH_TITLE, NO_PATH_WORDS, noPathKind } from "@/lib/routePath";
import { cn } from "@/lib/utils";
import { Method, PathText, TransportBadge } from "@/sections/api/parts";
import type { ChangeItem } from "@/api/changes";
import { itemsFor, markerTitle, VIEW_GROUPS } from "@/sections/changes/markers";
import { groupHash } from "@/sections/changes/params";
import { ChangeMarker } from "@/shell/ChangeMarker";
import { useChangeMarkers } from "@/shell/markers";

export const ROW_HEIGHT = 34;
export const HEADER_HEIGHT = 36;

type ListItem =
  | (VirtualItem & { kind: "header"; summary: TransportSummary; shown: number; never: number; possibly: number })
  | (VirtualItem & { kind: "row"; row: EndpointRow });

/** endpointLabel is the text a row stands for (search, aria-label). */
export function endpointLabel(row: EndpointRow): string {
  if (row.transport === "http") {
    // A path whose prefix the code does not fix is shown with "…" for the
    // prefix (T033 row UP-29), never as a full path.
    const path = row.path ?? row.prefix_unknown?.path ?? row.pattern ?? row.name;
    return `${row.method ?? "?"} ${path}`;
  }
  if (row.transport === "server_action") return row.action ?? row.name;
  return row.command ?? row.name;
}

/** rowAriaLabel is the accessible name of a list row: transport,
 * endpoint and how many callers it has in scope (neverCalledLabel). */
export function rowAriaLabel(row: EndpointRow, scope: string): string {
  const n = row.caller_count;
  const p = row.project_caller_count ?? 0;
  const callers = isPossiblyCalledRow(row)
    ? POSSIBLY_CALLED
    : n === 0 && p === 0
      ? mayBeCalled(row)
        ? MAY_BE_CALLED
        : neverCalledLabel(scope, undefined, row.project_caller_count !== null).toLowerCase()
      : n === 1
        ? "1 caller"
        : `${String(n)} callers`;
  const project = p > 0 ? `, ${String(p)} from other repositories` : "";
  return `${TRANSPORT_LABEL[row.transport]} ${endpointLabel(row)}, ${callers}${project}`;
}

/** MAY_BE_CALLED labels a server action without a linked use that an
 * unread use (a prop, an argument, a member read) may reach (T024b R1):
 * it is never shown as having no linked caller. */
export const MAY_BE_CALLED = "may be called (unread use)";

/** mayBeCalled reports a row the server flags unread_use. */
export function mayBeCalled(row: EndpointRow): boolean {
  return row.flags.includes("unread_use");
}

/** matchesFilter searches method, path, pattern, command, name and file. */
export function matchesFilter(row: EndpointRow, q: string): boolean {
  if (q === "") return true;
  const hay = [endpointLabel(row), row.name, row.file, row.transport === "http" ? (row.pattern ?? "") : (row.handler ?? "")]
    .join(" ")
    .toLowerCase();
  return hay.includes(q.toLowerCase());
}

/** PREFIX_UNKNOWN marks a route path whose prefix the code does not fix
 * (T033 row UP-29); the row's tooltip names the reason. */
export const PREFIX_UNKNOWN = "prefix unknown";

/** noPathText is the marker and tooltip of an http row with neither a
 * full path nor an unknown-prefix path, from the server's flags: shadowed,
 * path_withheld (row CR-44), else "path not known"; never "not mounted"
 * (ADR 0089 Revision 5). */
export function noPathText(row: EndpointRow): { text: string; title: string } {
  const kind = noPathKind(row.flags.includes("shadowed"), row.flags.includes("path_withheld"));
  return { text: NO_PATH_WORDS[kind], title: NO_PATH_TITLE[kind] };
}

const FLAG_ICONS: { flag: string; label: string; Icon: typeof FlaskConical }[] = [
  { flag: "conditional", label: "Conditional: some composed paths depend on a runtime condition", Icon: GitBranch },
  { flag: "shadowed", label: "Shadowed: a mount added before the route takes every composed path first; the route is mounted, no path is stored", Icon: Layers },
  { flag: "path_withheld", label: `Path withheld: ${NO_PATH_TITLE.withheld}`, Icon: Layers },
  { flag: "path_assumed", label: "Path assumption: a composed path rests on an assumption (a mount at an unknown prefix or a mounted app's catch-all may answer first, or a wrapper); the endpoint names it", Icon: CircleHelp },
  { flag: "test_paths", label: "Test paths: some composed paths are in test scope", Icon: FlaskConical },
  { flag: "cfg_gated", label: "cfg-gated: registered only in some build configurations", Icon: Settings2 },
  { flag: "test_only", label: "Test only", Icon: FlaskConical },
  { flag: "unreachable", label: "Unreachable registration", Icon: Unplug },
];

function Flags({ row }: { row: EndpointRow }) {
  const shown = FLAG_ICONS.filter((f) => row.flags.includes(f.flag));
  if (row.scope === "test" && !shown.some((f) => f.flag === "test_only" || f.flag === "test_paths")) {
    shown.push({ flag: "scope_test", label: "Test scope", Icon: FlaskConical });
  }
  if (shown.length === 0) return null;
  return (
    <span className="flex items-center gap-1 text-muted-foreground">
      {shown.map(({ flag, label, Icon }) => (
        <span key={flag} title={label} role="img" aria-label={label}>
          <Icon aria-hidden="true" className="size-3.5" />
        </span>
      ))}
    </span>
  );
}

function CallerCount({ n, scope, may = false, project = null, possible = null }: { n: number; scope: string; may?: boolean; project?: number | null; possible?: number | null }) {
  if (n === 0 && may) {
    return <span className="rounded-[4px] bg-other-soft px-1.5 text-[11.5px] leading-[18px] whitespace-nowrap text-other">{MAY_BE_CALLED}</span>;
  }
  // Project mode (T022 B9): calls of other repositories linked to the
  // route; the route then has a linked caller in the project.
  if (n === 0 && (project ?? 0) === 0 && possible !== null && possible > 0) {
    // T022 review: only ambiguous or incomplete calls of other
    // repositories may reach it.
    return (
      <span
        className="rounded-[4px] bg-other-soft px-1.5 text-[11.5px] leading-[18px] whitespace-nowrap text-other"
        title={`${String(possible)} ${possible === 1 ? "call" : "calls"} of other repositories may call it; not linked`}
        data-testid="possibly-called"
      >
        {POSSIBLY_CALLED}
      </span>
    );
  }
  if (project !== null && project > 0) {
    return (
      <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={`${String(n)} in this repository, ${String(project)} from other repositories of the project`}>
        {n} <span className="text-brand-text">+{project}</span> {n + project === 1 ? "caller" : "callers"}
      </span>
    );
  }
  return n > 0 ? (
    <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums">
      {n} {n === 1 ? "caller" : "callers"}
    </span>
  ) : (
    <span className="rounded-[4px] bg-never-soft px-1.5 text-[11.5px] leading-[18px] whitespace-nowrap text-never">{neverCalledLabel(scope, undefined, project !== null).toLowerCase()}</span>
  );
}

function Row({ row, scope, selected, mark }: { row: EndpointRow; scope: string; selected: boolean; mark: Mark | null }) {
  // The marker follows the path in the same cell and shrinks before it:
  // the path keeps at least about half the cell, cut with an ellipsis,
  // and the row's tooltip has the full text (T019 Revision 5, R5-6). In a
  // cell narrower than 200 px (a side-by-side window) the marker is a dot.
  const marker = mark === null ? null : <ChangeMarker items={mark.items} baseText={mark.baseText} as="decoration" className="max-w-[45%] min-w-[3.5rem] flex-[0_1_auto]" shrinkInCell />;
  const cls = cn(
    "grid h-full cursor-pointer items-center gap-2 border-b border-border/70 px-3 text-[13px] hover:bg-accent/70",
    selected && "bg-brand-soft shadow-[inset_2px_0_0_hsl(var(--brand))] hover:bg-brand-soft",
  );
  if (row.transport === "http") {
    return (
      <div
        className={cn(cls, "grid-cols-[54px_minmax(0,1fr)_auto_auto]")}
        title={
          row.path !== null
            ? endpointLabel(row)
            : row.prefix_unknown !== null
              ? `${endpointLabel(row)} · ${row.prefix_unknown.reason_text}`
              : `${endpointLabel(row)} · ${noPathText(row).title}`
        }
      >
        <Method method={row.method} />
        <span className="@container/cell flex min-w-0 items-center gap-1.5">
          <span className="min-w-0 flex-[1_1_auto] truncate font-mono text-[12.5px]" data-testid="endpoint-path">
            {row.path !== null ? (
              <PathText path={row.path} />
            ) : row.prefix_unknown !== null ? (
              <span>
                <PathText path={row.prefix_unknown.path} />{" "}
                <span className="font-sans text-xs text-muted-foreground" data-testid="prefix-unknown">
                  {PREFIX_UNKNOWN}
                </span>
              </span>
            ) : (
              <>
                <PathText path={row.pattern ?? row.name} />{" "}
                <span className="font-sans text-xs text-muted-foreground" data-testid="no-path">
                  {noPathText(row).text}
                </span>
              </>
            )}
          </span>
          {marker}
        </span>
        <Flags row={row} />
        <CallerCount n={row.caller_count} scope={scope} may={mayBeCalled(row)} project={row.project_caller_count} possible={row.project_possible_caller_count} />
      </div>
    );
  }
  return (
    <div className={cn(cls, "grid-cols-[minmax(0,1fr)_auto_auto]")} title={endpointLabel(row)}>
      <span className="@container/cell flex min-w-0 items-center gap-1.5">
        <span className="min-w-0 flex-[1_1_auto] truncate font-mono text-[12.5px]" data-testid="endpoint-path">
          {row.transport === "server_action" ? (
            (row.action ?? <span className="text-muted-foreground">anonymous inline action</span>)
          ) : (
            row.command ?? <span className="text-muted-foreground">{row.name} (entry not resolved)</span>
          )}
        </span>
        {marker}
      </span>
      <Flags row={row} />
      <CallerCount n={row.caller_count} scope={scope} may={mayBeCalled(row)} project={row.project_caller_count} possible={row.project_possible_caller_count} />
    </div>
  );
}

const HEADER_EDGE: Record<Transport, string> = {
  http: "shadow-[inset_3px_0_0_hsl(var(--http-bar))]",
  tauri_ipc: "shadow-[inset_3px_0_0_hsl(var(--ipc-bar))]",
  server_action: "shadow-[inset_3px_0_0_hsl(var(--action-bar))]",
};

function Header({
  summary,
  shown,
  never,
  possibly,
  repository,
  scope,
  filtered,
}: {
  summary: TransportSummary;
  shown: number;
  never: number;
  possibly: number;
  repository: string;
  scope: string;
  filtered: boolean;
}) {
  return (
    <div
      className={cn(
        "flex h-full items-center gap-2 border-b bg-card pr-3 pl-[9px] text-[12.5px] whitespace-nowrap",
        HEADER_EDGE[summary.transport],
      )}
    >
      <TransportBadge transport={summary.transport} />
      <span className="truncate text-muted-foreground">
        {repository}
        {summary.frameworks.length > 0 ? ` · ${summary.frameworks.join(", ")}` : ""}
      </span>
      <span className="ml-auto flex items-center gap-2.5 tabular-nums">
        <span>
          <b className="font-semibold">{shown}</b>
          {filtered ? <span className="text-muted-foreground"> of {summary.endpoints.length}</span> : null}{" "}
          <span className="text-muted-foreground">{endpointNoun(summary.transport, summary.endpoints.length)}</span>
        </span>
        <span className="text-muted-foreground">{neverCalledLabel(scope, never, summary.never_called_in_project !== null)}</span>
        {possibly > 0 && (
          <span className="text-muted-foreground" data-testid="possibly-called-count">
            {possibly} {POSSIBLY_CALLED}
          </span>
        )}
      </span>
    </div>
  );
}

export interface EndpointListProps {
  transports: TransportSummary[];
  repository: string;
  /** The scope the caller counts were read in (the endpoints answer's scope). */
  scope: string;
  query: string;
  onQuery: (q: string) => void;
  selected: string | null;
  onSelect: (id: string) => void;
}

/** optionId is the DOM id of an endpoint's list option. */
export function optionId(id: string): string {
  return `ep-${encodeURIComponent(id).replace(/%/g, "_")}`;
}

/** Mark is a row's "changed since" marker. */
interface Mark {
  items: ChangeItem[];
  baseText: string;
}

/** EndpointList renders the list card. */
export function EndpointList(props: EndpointListProps) {
  const { transports, query } = props;
  const markers = useChangeMarkers(VIEW_GROUPS.api);
  const markOf = (id: string): Mark | null => {
    if (markers.kind !== "ok") return null;
    const items = itemsFor(markers.index, id);
    return items.length === 0 ? null : { items, baseText: markers.baseText };
  };
  // Endpoints the base had and the current index lacks are no rows here;
  // test-scope ones count only with Include tests on, like the rows
  // (R5-6).
  const gone = markers.kind === "ok" ? markers.items.filter((i) => i.record === "node" && (i.kind === "route" || i.kind === "ipc_command") && i.change === "removed" && (props.scope === "all" || !i.test)) : [];
  const items = useMemo(() => {
    const out: ListItem[] = [];
    for (const t of transports) {
      if (t.endpoints.length === 0) continue;
      const rows = t.endpoints.filter((r) => matchesFilter(r, query));
      out.push({ key: `h:${t.transport}`, kind: "header", summary: t, shown: rows.length, never: rows.filter((r) => isNeverRow(r) && !mayBeCalled(r)).length, possibly: rows.filter(isPossiblyCalledRow).length });
      for (const row of rows) out.push({ key: row.id, kind: "row", row });
    }
    return out;
  }, [transports, query]);
  const shown = items.filter((i) => i.kind === "row").length;
  const groups = items.filter((i) => i.kind === "header").length;
  const emptyTransport = transports.length === 1 && transports[0]?.endpoints.length === 0 ? transports[0] : undefined;

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card shadow-[0_1px_2px_rgba(0,0,0,.04)]">
      <div className="flex items-center gap-2 border-b p-2">
        <label className="flex h-[30px] min-w-0 flex-1 items-center gap-2 rounded-md border border-input px-2.5 text-[13px] text-muted-foreground focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/50">
          <ListFilter aria-hidden="true" className="size-3.5 flex-none" />
          <input
            type="search"
            aria-label="Filter routes and commands"
            placeholder="Filter routes and commands"
            className="min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-muted-foreground"
            value={query}
            onChange={(e) => {
              props.onQuery(e.target.value);
            }}
          />
        </label>
      </div>
      <VirtualList
        items={items}
        rowHeight={ROW_HEIGHT}
        headerHeight={HEADER_HEIGHT}
        selectedKey={props.selected}
        onSelect={props.onSelect}
        optionId={optionId}
        ariaLabel="Endpoints"
        optionLabel={(item) => {
          if (item.kind !== "row") return "";
          const mark = markOf(item.row.id);
          return rowAriaLabel(item.row, props.scope) + (mark === null ? "" : `, ${markerTitle(mark.items, mark.baseText).replace(/: open in Changes$/, "")}`);
        }}
        renderRow={(item, selected) => (item.kind === "row" ? <Row row={item.row} scope={props.scope} selected={selected} mark={markOf(item.row.id)} /> : null)}
        renderHeader={(item) =>
          item.kind === "header" ? (
            <Header summary={item.summary} shown={item.shown} never={item.never} possibly={item.possibly} repository={props.repository} scope={props.scope} filtered={query !== ""} />
          ) : null
        }
        empty={
          <div className="px-4 py-8 text-center text-[13px] text-muted-foreground">
            {emptyTransport !== undefined
              ? `No ${TRANSPORT_LABEL[emptyTransport.transport]} ${endpointNoun(emptyTransport.transport, 2)} in this repository`
              : query !== ""
                ? `No route or command matches "${query}"`
                : "No routes or commands in this repository"}
          </div>
        }
      />
      <div className="flex min-h-[30px] flex-none flex-wrap items-center gap-x-2.5 gap-y-0.5 border-t px-3 py-1.5 text-xs text-muted-foreground" data-testid="endpoints-footer">
        <span>
          <b className="font-semibold text-foreground">{shown}</b> {query !== "" ? "matching" : "endpoints"}
        </span>
        <span>·</span>
        <span>
          {groups} {groups === 1 ? "group" : "groups"}
        </span>
        {gone.length > 0 && markers.kind === "ok" && (
          <>
            <span>·</span>
            <a href={groupHash(gone[0]?.group ?? "routes")} className="text-brand-text underline underline-offset-2" data-testid="endpoints-gone" title={`Endpoints the ${markers.baseText} had that the current index lacks; they are listed in Changes`}>
              {gone.length} {gone.length === 1 ? "endpoint" : "endpoints"} no longer in the index since {markers.baseText}
            </a>
          </>
        )}
        <span className="flex-1" />
        <span className="max-[1400px]:hidden">
          <Kbd>↑</Kbd> <Kbd>↓</Kbd> <Kbd>PgUp</Kbd> <Kbd>PgDn</Kbd> move
        </span>
      </div>
    </div>
  );
}

/** Kbd is a key cap. */
export function Kbd({ children }: { children: string }) {
  return (
    <kbd className="rounded-[4px] border border-b-2 bg-muted px-[5px] font-mono text-[11px] leading-4 font-medium text-muted-foreground">
      {children}
    </kbd>
  );
}
