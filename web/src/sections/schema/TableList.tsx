// The table list (D2, D3, D5, T018b2 section 4): the visible groups'
// tables in API order, with group headers only when more than one group is
// visible, a client-side filter over table and column names ("/"), and per
// row the quiet glyph when the table is in its group's common state or the
// full badge otherwise. Rows are buttons (click selects, Up/Down move);
// above 200 rows the list is windowed (VirtualList).

import { ListFilter } from "lucide-react";
import { useRef, type KeyboardEvent, type ReactNode, type RefObject } from "react";

import type { SchemaGroup, SchemaTable } from "@/api/schema";
import { VirtualList, type VirtualItem } from "@/components/VirtualList";
import { cn } from "@/lib/utils";
import { Kbd } from "@/sections/api/EndpointList";
import { completenessBadge } from "@/sections/schema/cells";
import { UnverifiedMark } from "@/sections/schema/Verification";
import { ChangedMark, CompletenessBadge, fileLine, HalfCircle, QuietGlyph, tableName } from "@/sections/schema/parts";
import { groupKeyOf } from "@/sections/schema/params";
import { setLabel } from "@/sections/schema/SetStrip";
import { commonState, objectsState } from "@/sections/schema/state";
import { VIEW_GROUPS } from "@/sections/changes/markers";
import { NodeMarker } from "@/shell/ChangeMarker";

/** VIRTUAL_ABOVE: more rows than this are windowed. */
export const VIRTUAL_ABOVE = 200;
const ROW_H = 46;
const HEADER_H = 34;

/** matches says whether a table or one of its columns contains q. */
export function matches(t: SchemaTable, q: string): boolean {
  if (q === "") return true;
  const s = q.toLowerCase();
  if (tableName(t.node).toLowerCase().includes(s)) return true;
  return t.columns.some((c) => {
    const n = c.node.attributes.name;
    return (typeof n === "string" ? n : c.node.name).toLowerCase().includes(s);
  });
}

type Item = (VirtualItem & { kind: "header"; group: SchemaGroup; shown: number }) | (VirtualItem & { kind: "row"; group: SchemaGroup; table: SchemaTable });

/** listItems builds the list: a header per group when more than one is
 * visible, then its matching tables. */
export function listItems(groups: readonly SchemaGroup[], q: string): Item[] {
  const out: Item[] = [];
  for (const g of groups) {
    const rows = g.tables.filter((t) => matches(t, q));
    if (groups.length > 1) out.push({ key: `h:${groupKeyOf(g)}`, kind: "header", group: g, shown: rows.length });
    for (const t of rows) out.push({ key: t.node.identity, kind: "row", group: g, table: t });
  }
  return out;
}

function StateMark({ t, group }: { t: SchemaTable; group: SchemaGroup }) {
  const c = completenessBadge(t.completeness, t.node.attributes);
  const common = commonState(group.tables);
  if ((c.tone === "complete" && common === "complete") || (c.tone === "in_source" && common === "complete_in_source")) return <QuietGlyph c={c} />;
  return <CompletenessBadge c={c.tone === "in_source" ? { ...c, text: c.text.replace(/ · .*$/, "") } : c} />;
}

/** columnsUnknown: a table that is not complete (or complete as far as the
 * migrations show) with no stored column has unknown columns, not zero. */
export function columnsUnknown(t: SchemaTable): boolean {
  return t.columns.length === 0 && t.completeness !== "complete" && t.completeness !== "complete_in_source";
}

/** headerCount is a group header's table count; a group whose tables were
 * not extracted (or that is unknown) has no count. */
function headerCount(g: SchemaGroup, shown: number): string {
  const st = objectsState(g);
  if (st === "not_extracted") return "not extracted";
  if (st === "unknown") return "unknown";
  const n = g.tables.length;
  return `${shown === n ? String(n) : `${String(shown)} of ${String(n)}`} ${n === 1 ? "table" : "tables"}`;
}

/** OverlapMark is the list row marker of a same-name model/migration
 * pair (T018f 2.1): "⇄ <other group>". */
function OverlapMark({ t }: { t: SchemaTable }) {
  if (t.overlaps === null || t.overlaps.length === 0) return null;
  const labels = [...new Set(t.overlaps.map((o) => setLabel({ set: o.set, source: o.source })))];
  return (
    <span className="ml-1.5 rounded-[3px] bg-ipc-soft px-1 font-sans text-[11px] font-medium text-ipc" title="A table of the same name in another source; not merged, not compared" data-testid="overlap-mark">
      ⇄ {labels.join(", ")}
    </span>
  );
}

/** classOf is a model table's class name, or null. */
function classOf(t: SchemaTable): string | null {
  const c = t.node.attributes.class;
  if (typeof c !== "string" || t.node.attributes.source !== "model") return null;
  return c.slice(c.lastIndexOf("#") + 1);
}

function Row({ t, group, selected, changed }: { t: SchemaTable; group: SchemaGroup; selected: boolean; changed: boolean }) {
  const cls = classOf(t);
  return (
    <span className="flex min-w-0 flex-1 items-center gap-2">
      <span className="grid min-w-0 flex-1">
        <span className={cn("truncate font-mono text-[13px]", selected && "font-semibold text-brand-text")}>
          {tableName(t.node)}
          <OverlapMark t={t} />
          <UnverifiedMark t={t} tool={group.tool} />
          {t.node.attributes.scope === "test" && <span className="ml-1.5 rounded-[3px] bg-dynamic-soft px-1 font-sans text-[11px] font-semibold text-dynamic">test</span>}
        </span>
        <span className="flex items-center gap-1.5 truncate text-xs text-muted-foreground">
          {cls !== null && `class ${cls} · `}
          {fileLine(t.node.file, t.node.span?.start_line)}
          <ChangedMark show={changed} />
        </span>
      </span>
      <NodeMarker id={t.node.identity} groups={VIEW_GROUPS.tables} as="decoration" />
      <span className={cn("flex-none text-xs text-muted-foreground tabular-nums", columnsUnknown(t) && "italic")} data-testid="row-columns">
        {columnsUnknown(t) ? "columns unknown" : `${String(t.columns.length)} col`}
      </span>
      <span className="flex flex-none items-center" data-testid="row-state">
        <StateMark t={t} group={group} />
      </span>
    </span>
  );
}

function GroupHeader({ g, shown, selected, onSelect }: { g: SchemaGroup; shown: number; selected: boolean; onSelect: () => void }) {
  return (
    <button
      type="button"
      className={cn("flex h-full w-full items-center gap-2 border-y border-l-4 bg-muted/60 px-3 text-left text-[12.5px]", selected ? "border-l-brand" : "border-l-transparent")}
      onClick={onSelect}
      title="Show this set's strip and map"
      data-testid="group-header"
    >
      <span className="truncate font-mono font-semibold">{setLabel(g)}</span>
      {g.dialect !== null && <span className="text-muted-foreground">{g.dialect}</span>}
      {g.dialects !== null && g.dialects.length > 0 && <span className="truncate text-muted-foreground" title="Run-time dialects" data-testid="group-header-dialects">{g.dialects.join(", ")}</span>}
      {g.test_only === true && <span className="rounded-[3px] bg-dynamic-soft px-1 text-[11px] font-semibold text-dynamic">test</span>}
      <span className="flex-1" />
      <span className={cn("whitespace-nowrap text-muted-foreground tabular-nums", objectsState(g) !== "extracted" && "italic")} title={objectsState(g) === "extracted" ? undefined : "Tables of this set were not extracted (or whether they were is unknown); this is no count"} data-testid="group-header-count">
        {headerCount(g, shown)}
      </span>
    </button>
  );
}

function EmptyGroupNote({ g }: { g: SchemaGroup }) {
  if (g.tables.length > 0) return null;
  return (
    <div className="border-b px-4 py-2 text-xs text-muted-foreground">
      {objectsState(g) === "not_extracted"
        ? "Tables not extracted; zero tables here is not an empty schema."
        : objectsState(g) === "unknown"
          ? "Whether tables were extracted is unknown."
          : "No tables stored for this group."}
    </div>
  );
}

export interface TableListProps {
  groups: SchemaGroup[];
  query: string;
  onQuery: (q: string) => void;
  selected: string | null;
  selectedGroup: SchemaGroup | null;
  onSelect: (id: string, group: SchemaGroup) => void;
  onSelectGroup: (group: SchemaGroup) => void;
  changed: Set<string>;
  filterRef: RefObject<HTMLInputElement | null>;
  /** The map above the list (lazy). */
  map: ReactNode;
}

/** TableList renders the list column: map, filter, rows and legend. */
export function TableList({ filterRef, map, ...props }: TableListProps) {
  const { groups, query, selected, changed } = props;
  const items = listItems(groups, query);
  const rows = items.filter((i) => i.kind === "row");
  const boxRef = useRef<HTMLDivElement>(null);
  const anyComplete = groups.some((g) => commonState(g.tables) === "complete");
  const anyInSource = groups.some((g) => commonState(g.tables) === "complete_in_source");

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    const buttons = [...(boxRef.current?.querySelectorAll<HTMLButtonElement>("button[data-table-row]") ?? [])];
    const i = buttons.findIndex((b) => b === document.activeElement);
    const next = buttons[i === -1 ? 0 : event.key === "ArrowDown" ? Math.min(buttons.length - 1, i + 1) : Math.max(0, i - 1)];
    if (next === undefined) return;
    event.preventDefault();
    next.focus();
    next.click();
  };

  let list: ReactNode;
  if (rows.length > VIRTUAL_ABOVE) {
    list = (
      <VirtualList
        items={items}
        rowHeight={ROW_H}
        headerHeight={HEADER_H}
        selectedKey={selected}
        onSelect={(key) => {
          const it = items.find((x) => x.key === key);
          if (it?.kind === "row") props.onSelect(key, it.group);
        }}
        optionId={(key) => `tbl-${encodeURIComponent(key).replace(/%/g, "_")}`}
        ariaLabel="Tables"
        optionLabel={(it) =>
          it.kind === "row" ? `${tableName(it.table.node)}, ${columnsUnknown(it.table) ? "columns unknown" : `${String(it.table.columns.length)} columns`}, ${it.table.completeness}` : ""
        }
        renderRow={(it, sel) => (it.kind === "row" ? <span className="flex h-full items-center px-3"><Row t={it.table} group={it.group} selected={sel} changed={changed.has(it.table.node.file)} /></span> : null)}
        renderHeader={(it) => (it.kind === "header" ? <GroupHeader g={it.group} shown={it.shown} selected={props.selectedGroup === it.group} onSelect={() => { props.onSelectGroup(it.group); }} /> : null)}
        empty={<div className="px-4 py-8 text-center text-[13px] text-muted-foreground">No table or column matches &quot;{query}&quot;</div>}
      />
    );
  } else {
    list = (
      <div ref={boxRef} className="min-h-0 flex-1 overflow-auto" onKeyDown={onKeyDown} data-testid="table-list-rows">
        {groups.map((g) => {
          const gi = items.filter((x) => x.group === g);
          const header = gi.find((x) => x.kind === "header");
          return (
            <div key={groupKeyOf(g)} role="group" aria-label={setLabel(g)}>
              {header?.kind === "header" && (
                <div className="h-[34px]">
                  <GroupHeader g={g} shown={header.shown} selected={props.selectedGroup === g} onSelect={() => { props.onSelectGroup(g); }} />
                </div>
              )}
              <EmptyGroupNote g={g} />
              {gi.map((it) =>
                it.kind === "row" ? (
                  <button
                    key={it.key}
                    type="button"
                    data-table-row={it.key}
                    aria-current={selected === it.key ? "true" : undefined}
                    className={cn(
                      "flex w-full items-center border-b border-l-[3px] px-3 py-1.5 text-left hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none focus-visible:ring-inset",
                      selected === it.key ? "border-l-brand bg-brand-soft" : "border-l-transparent",
                    )}
                    onClick={() => {
                      props.onSelect(it.key, it.group);
                    }}
                    data-testid="table-row"
                    data-name={tableName(it.table.node)}
                  >
                    <Row t={it.table} group={it.group} selected={selected === it.key} changed={changed.has(it.table.node.file)} />
                  </button>
                ) : null,
              )}
            </div>
          );
        })}
        {rows.length === 0 && query !== "" && <div className="px-4 py-8 text-center text-[13px] text-muted-foreground">No table or column matches &quot;{query}&quot;</div>}
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card shadow-[0_1px_2px_rgba(0,0,0,.04)]" data-testid="list-column">
      {map}
      <div className="flex items-center gap-2 border-b p-2">
        <label className="flex h-[30px] min-w-0 flex-1 items-center gap-2 rounded-md border border-input px-2.5 text-[13px] text-muted-foreground focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/50">
          <ListFilter aria-hidden="true" className="size-3.5 flex-none" />
          <input
            ref={filterRef}
            type="search"
            aria-label="Filter tables and columns"
            aria-keyshortcuts="/"
            title="Filter tables and columns (/)"
            placeholder="Filter tables and columns"
            className="min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-muted-foreground"
            value={query}
            onChange={(e) => {
              props.onQuery(e.target.value);
            }}
          />
          <Kbd>/</Kbd>
        </label>
      </div>
      {list}
      <div className="flex h-[30px] flex-none items-center gap-3 overflow-hidden border-t px-3 text-xs whitespace-nowrap text-muted-foreground">
        {anyInSource && (
          <span className="inline-flex items-center gap-1">
            <HalfCircle /> = complete as far as the migrations show
          </span>
        )}
        {anyComplete && <span>✓ = complete</span>}
        <span className="flex-1" />
        <span>
          <Kbd>↑</Kbd> <Kbd>↓</Kbd> move
        </span>
      </div>
    </div>
  );
}
