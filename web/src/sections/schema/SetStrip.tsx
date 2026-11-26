// The set strip (D3, D5, T018b2 section 4): the selected group's name and
// tool, counts counted from the loaded lists, the common state stated once,
// the other states with their full badge, and the table-list line.

import { useCallback, useEffect, useState, type ReactNode } from "react";

import type { SchemaGroup } from "@/api/schema";
import { cn } from "@/lib/utils";
import { completenessBadge, notModeledReason, tableListReason, tableListRule } from "@/sections/schema/cells";
import { CompletenessBadge, HalfCircle, OUTLINE_BUTTON } from "@/sections/schema/parts";
import { commonState, objectsState, stateCounts } from "@/sections/schema/state";
import { Check, ChevronDown, TriangleAlert } from "lucide-react";
import { RestsOn } from "@/sections/schema/RestsOn";
import { SourceCards } from "@/sections/schema/SourceCards";
import type { SourceCard } from "@/sections/schema/state";
import { WhyButton, type Why } from "@/sections/schema/WhyPopover";
import { dialectsText } from "@/sections/schema/Dialects";

/** setLabel is a group's heading ("No migration set" for set null); a
 * model group's set is a key, not a directory, so it has no slash. */
export function setLabel(g: Pick<SchemaGroup, "set" | "source">): string {
  if (g.set === null) return "No migration set";
  return g.source === "models" ? g.set : `${g.set}/`;
}

/** fieldText shows a set-level field: a field listed in conflicts reads
 * "conflicting values" (never one of them), null "unknown". */
function fieldText(g: SchemaGroup, key: "tool" | "dialect" | "package"): { text: string; title?: string; unknown: boolean } {
  if (g.conflicts.includes(key)) return { text: "conflicting values", title: `The group's migrations disagree on ${key} (or some lack it): ${g.conflicts.join(", ")}`, unknown: true };
  const v = g[key];
  return v === null ? { text: `${key} unknown`, unknown: true } : { text: v, unknown: false };
}

/** GroupDialects is a model group's run-time dialects (ADR 0065): "runs
 * on mysql, postgres or sqlite" where the API gives the list, "dialects
 * differ between tables" where it reports a conflict, nothing otherwise
 * (a single-dialect group reads as before). */
export function GroupDialects({ g }: { g: Pick<SchemaGroup, "dialects" | "conflicts"> }) {
  if (g.dialects !== null && g.dialects.length > 0) {
    return (
      <span title="The dialect is chosen at run time; dialect-dependent facts are shown per dialect" data-testid="group-dialects">
        {" · "}runs on {dialectsText(g.dialects)}
      </span>
    );
  }
  if (g.conflicts.includes("dialects")) {
    return (
      <i title="The tables of this group state different run-time dialects, or some state none" data-testid="group-dialects">
        {" · "}dialects differ between tables
      </i>
    );
  }
  return null;
}

/** groupCounts are the strip's numbers, counted from the lists. Tables,
 * columns and FK pairs are null when the group's tables were not extracted
 * (or that is unknown): an empty list is then no count (Review fixes R1). */
export function groupCounts(g: SchemaGroup): { tables: number | null; columns: number | null; pairs: number | null; migrations: number } {
  const extracted = objectsState(g) === "extracted";
  return {
    tables: extracted ? g.tables.length : null,
    columns: extracted ? g.tables.reduce((n, t) => n + t.columns.length, 0) : null,
    pairs: extracted ? g.foreign_keys.length : null,
    migrations: g.migrations.length,
  };
}

/** notCountedText is the text of a count that is not one. */
export function notCountedText(g: SchemaGroup): string {
  return objectsState(g) === "not_extracted" ? "not extracted" : "unknown";
}

/** TablesTotal is a sum of table counts over groups. n counts only the
 * extracted groups; a group not extracted (or unknown) is never counted as
 * 0, so the sum is a lower bound whenever one of them is in it
 * (review fix). */
export interface TablesTotal {
  n: number;
  /** counted is the number of extracted groups in n. */
  counted: number;
  notExtracted: number;
  unknown: number;
}

/** tablesTotal sums the tables of the extracted groups and counts the rest. */
export function tablesTotal(groups: readonly SchemaGroup[]): TablesTotal {
  const t: TablesTotal = { n: 0, counted: 0, notExtracted: 0, unknown: 0 };
  for (const g of groups) {
    const st = objectsState(g);
    if (st === "extracted") {
      t.n += g.tables.length;
      t.counted++;
    }
    else if (st === "not_extracted") t.notExtracted++;
    else t.unknown++;
  }
  return t;
}

/** totalText is a tab-sized total: "51", "51+" when some groups are not
 * counted, "?" when no group is; note explains the "+" or "?" ("3 groups not
 * extracted", "1 group unknown"), null for a plain count. */
export function totalText(t: TablesTotal): { text: string; note: string | null } {
  const groups = (k: number) => `${String(k)} ${k === 1 ? "group" : "groups"}`;
  const parts = [t.notExtracted > 0 && `${groups(t.notExtracted)} not extracted`, t.unknown > 0 && `${groups(t.unknown)} unknown`].filter((x): x is string => typeof x === "string");
  if (parts.length === 0) return { text: String(t.n), note: null };
  return { text: t.counted > 0 ? `${String(t.n)}+` : "?", note: parts.join(", ") };
}

/** optionCount is a group's table count in the set switcher. */
export function optionCount(g: SchemaGroup): string {
  const n = groupCounts(g).tables;
  if (n === null) return notCountedText(g) === "not extracted" ? "not extracted" : "tables unknown";
  return `${String(n)} ${n === 1 ? "table" : "tables"}`;
}

/** StateLine is the common-state sentence plus the differing states. */
export function StateLine({ g }: { g: SchemaGroup }) {
  const n = g.tables.length;
  const common = commonState(g.tables);
  const counts = stateCounts(g.tables);
  const k = common === null ? 0 : (counts.get(common) ?? 0);
  const all = k === n;
  const others = [...counts.entries()].filter(([state]) => state !== common);
  if (n === 0) {
    const st = objectsState(g);
    // Not extracted: the table-list line below says so with its reasons.
    if (st === "not_extracted") return null;
    const text =
      st === "unknown"
        ? `Whether tables were extracted is unknown (objects ${g.objects ?? "null"}).`
        : g.table_list === "complete"
          ? "No tables in this group."
          : "No table stored; the table list is not complete, so this is not an empty schema.";
    return (
      <span className={cn("text-[13px] text-muted-foreground", st !== "extracted" && "italic")} data-testid="state-line-empty">
        {text}
      </span>
    );
  }
  return (
    <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px]" data-testid="state-line">
      {common !== null && (
        <span className="inline-flex items-center gap-1.5">
          {common === "complete" ? <Check aria-hidden="true" className="size-3.5 text-linked" /> : <HalfCircle className="text-muted-foreground" />}
          <span>
            <b className="font-semibold">{all ? `All ${String(n)}` : `${String(k)} of ${String(n)}`}</b> {n === 1 && all ? "table" : "tables"}{" "}
            {common === "complete" ? "complete" : "complete as far as the migrations show"}
          </span>
        </span>
      )}
      {others.map(([state, c]) => (
        <span key={state} className="inline-flex items-center gap-1">
          <CompletenessBadge c={{ ...completenessBadge(state, {}), text: completenessBadge(state, {}).text.replace(/ · .*$/, "") }} />
          <span className="tabular-nums">{c}</span>
        </span>
      ))}
    </span>
  );
}

/** TableListLine is the table_list sentence with one line per reason. */
export function TableListLine({ g, onShowStatements, why }: { g: SchemaGroup; onShowStatements: () => void; why?: ReactNode }) {
  const rule = tableListRule(g.table_list);
  if (rule === "table_list.complete")
    return (
      <span className="inline-flex items-center gap-1.5">
        <span className="inline-flex w-fit items-center gap-1 rounded-md bg-linked-soft px-1.5 text-xs font-semibold text-linked" data-testid="table-list" data-rule={rule}>
          <Check aria-hidden="true" className="size-3" />
          Table list complete
        </span>
        {why}
      </span>
    );
  if (rule === "table_list.not_modeled")
    return (
      <div className="grid gap-0.5 text-[13px]" data-testid="table-list" data-rule={rule}>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-flex w-fit items-center gap-1 rounded-md border border-dynamic/50 bg-dynamic-soft px-1.5 text-xs font-semibold text-dynamic">
            <TriangleAlert aria-hidden="true" className="size-3" />
            Tables not extracted
          </span>
          {why}
        </span>
        {g.not_modeled_reasons.map((r) => {
          const c = notModeledReason(r);
          return (
            <span key={r} className={cn("text-muted-foreground", c.unknown && "italic")} data-rule={c.rule}>
              {c.text}
            </span>
          );
        })}
        <span className="text-muted-foreground">Zero tables here is not an empty schema.</span>
      </div>
    );
  if (rule === "table_list.may_be_incomplete") {
    const byKind = g.opaque_by_kind !== null && typeof g.opaque_by_kind === "object" ? Object.entries(g.opaque_by_kind as Record<string, unknown>) : [];
    return (
      <div className="grid gap-0.5 text-[13px]" data-testid="table-list" data-rule={rule}>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-flex w-fit items-center gap-1 rounded-md border border-dynamic/50 bg-dynamic-soft px-1.5 text-xs font-semibold text-dynamic">
            <TriangleAlert aria-hidden="true" className="size-3" />
            Table list may be incomplete
          </span>
          {why}
        </span>
        <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span className="grid gap-0.5">
            {g.table_list_reasons.map((r) => {
              const c = tableListReason(r, g.opaque_total, g.tool);
              const title = r === "opaque_statements" && byKind.length > 0 ? byKind.map(([k, v]) => `${k} ${String(v)}`).join(", ") : undefined;
              return (
                <span key={r} className={cn("text-muted-foreground", c.unknown && "italic")} title={title} data-rule={c.rule}>
                  {c.text}
                </span>
              );
            })}
          </span>
          {g.migrations.length > 0 && (
            <button type="button" className={OUTLINE_BUTTON} onClick={onShowStatements} data-testid="show-statements">
              Show statements
            </button>
          )}
        </span>
      </div>
    );
  }
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        className="inline-flex w-fit items-center gap-1 rounded-md border border-dashed border-muted-foreground/70 px-1.5 text-xs font-semibold text-muted-foreground"
        title={`table_list: ${g.table_list ?? "null"}`}
        data-testid="table-list"
        data-rule={rule}
      >
        Table list {g.set === null ? "not known (tables from another producer)" : `unknown (${g.table_list ?? "null"})`}
      </span>
      {why}
    </span>
  );
}

/** groupSource is the contract of a group's rows. */
export function groupSource(g: Pick<SchemaGroup, "source">): string {
  return g.source === "models" ? "models" : "migrations";
}

/** tableListWhy is the table-list line's popover: the line's reasons and
 * what the group's facts rest on. */
export function tableListWhy(g: SchemaGroup): Why {
  const rule = tableListRule(g.table_list);
  const reasons = rule === "table_list.not_modeled" ? g.not_modeled_reasons.map((r) => notModeledReason(r).text) : g.table_list_reasons.map((r) => tableListReason(r, g.opaque_total, g.tool).text);
  const head =
    rule === "table_list.complete"
      ? "Table list complete"
      : rule === "table_list.may_be_incomplete"
        ? "Table list may be incomplete"
        : rule === "table_list.not_modeled"
          ? "Tables not extracted"
          : "Table list unknown";
  return {
    kind: "table_list",
    title: `${head}: ${setLabel(g)}`,
    source: groupSource(g),
    lead:
      rule === "table_list.complete"
        ? "Every table this group's source defines is listed, under the assumptions below."
        : rule === "table_list.unknown"
          ? `The API sent table_list ${g.table_list ?? "null"}, which this view does not read as complete.`
          : "Tables the list does not show may exist:",
    reasons: reasons.map((text) => ({ text, row: null, where: null })),
    assumptions: { heading: `Facts in ${setLabel(g)} rest on`, ids: g.rests_on },
    entry: null,
  };
}

/** pinsText writes a model group's library pins ("SQLModel 0.0.39 ·
 * ..."), or null when they are not an object of strings. */
export function pinsText(pins: unknown): string | null {
  if (pins === null || typeof pins !== "object" || Array.isArray(pins)) return null;
  const NAMES: Record<string, string> = { sqlmodel: "SQLModel", sqlalchemy: "SQLAlchemy", pydantic: "Pydantic" };
  const parts = Object.entries(pins as Record<string, unknown>).map(([k, v]) => `${NAMES[k] ?? k} ${typeof v === "string" ? v : "unknown"}`);
  return parts.length === 0 ? null : parts.join(" · ");
}

export interface SetStripProps {
  group: SchemaGroup;
  /** The strip's cards (state.sourceCards). */
  cards: readonly SourceCard[];
  onSelectGroup: (g: SchemaGroup) => void;
  onShowStatements: () => void;
  /** Test-scope tables of this group Include tests hides (O3). */
  hiddenTests: number;
  onIncludeTests: () => void;
}

/** useOverflowCue reports whether a scroller has content below its visible
 * part, so the strip can show that more lines follow (T018f Revision 2,
 * C8) instead of cutting a line in half without a sign. */
export function useOverflowCue(): { attach: (el: HTMLDivElement | null) => void; more: boolean; scrollMore: () => void } {
  const [el, setEl] = useState<HTMLDivElement | null>(null);
  const [more, setMore] = useState(false);
  const check = useCallback(() => {
    if (el === null) return;
    setMore(el.scrollHeight - el.scrollTop - el.clientHeight > 1);
  }, [el]);
  useEffect(() => {
    if (el === null) return;
    // Measured after layout (and outside the effect's own run).
    const frame = requestAnimationFrame(check);
    el.addEventListener("scroll", check);
    window.addEventListener("resize", check);
    // ResizeObserver is missing in some test environments; the scroll and
    // resize events still keep the cue current there.
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(check);
    ro?.observe(el);
    for (const child of el.children) ro?.observe(child);
    return () => {
      cancelAnimationFrame(frame);
      el.removeEventListener("scroll", check);
      window.removeEventListener("resize", check);
      ro?.disconnect();
    };
  });
  const scrollMore = useCallback(() => {
    if (el !== null) el.scrollBy({ top: Math.max(24, el.clientHeight - 24), behavior: "smooth" });
  }, [el]);
  return { attach: setEl, more, scrollMore };
}

/** SetStrip renders the source cards and the selected group's lines. Its
 * height is fixed on wide windows (the info column scrolls), so the boxes
 * below it never move between groups or states (T018f 2.5). When the lines
 * do not fit, a fade and a "more" button over the column's bottom edge say
 * so (C8); both are absolutely placed, so showing them moves no box. */
export function SetStrip({ group: g, cards, onSelectGroup, onShowStatements, hiddenTests, onIncludeTests }: SetStripProps) {
  const c = groupCounts(g);
  const fields = (["tool", "dialect", "package"] as const).map((k) => ({ k, ...fieldText(g, k) }));
  const pins = g.source === "models" ? pinsText(g.pins) : null;
  const source = groupSource(g);
  const { attach, more, scrollMore } = useOverflowCue();
  return (
    <div
      className="grid flex-none grid-cols-[minmax(0,max-content)_minmax(360px,1fr)] overflow-hidden rounded-xl border bg-card min-[1000px]:h-[128px] max-[1000px]:grid-cols-1"
      data-testid="set-strip"
      data-set={g.set ?? ""}
      data-source={g.source ?? ""}
    >
      <SourceCards cards={cards} selected={g} onSelect={onSelectGroup} />
      <div className="relative min-h-0 min-w-0 border-l max-[1000px]:border-t max-[1000px]:border-l-0">
        <div ref={attach} className="grid h-full min-h-0 content-start gap-1 overflow-y-auto px-4 py-2" data-testid="strip-info">
          <TableListLine
            g={g}
            onShowStatements={onShowStatements}
            why={
              <WhyButton why={() => tableListWhy(g)} className="text-xs text-brand-text underline underline-offset-[3px]" testId="table-list-why" label="Why: the table list">
                why
              </WhyButton>
            }
          />
          <StateLine g={g} />
          <RestsOn ids={g.rests_on} source={source} group={setLabel(g)} testId="group-rests-on" />
          {g.platform !== null && (
            <span className="text-xs text-muted-foreground" title={g.platform.objects.join("; ")} data-testid="group-platform">
              Assumes the {g.platform.name === "supabase" ? "Supabase" : g.platform.name} platform objects ({g.platform.assumption}): {g.platform.objects.join("; ")}
            </span>
          )}
          {g.platform?.assumptions.map((a) => (
            <span key={`${a.id}:${a.text}`} className="text-xs text-muted-foreground" data-testid="group-platform-assumption">
              Assumes ({a.id}): {a.text}
            </span>
          ))}
          <span className="text-xs text-muted-foreground">
            {g.source === "models" ? (
              <>
                {pins !== null ? (
                  <>Read as {pins}{g.assumptions?.includes("V1") === true && " (V1)"}</>
                ) : (
                  <i>{g.pins_conflict === true ? "Library pins differ between tables" : "Library pins unknown"}</i>
                )}
                <GroupDialects g={g} />
              </>
            ) : g.set !== null ? (
              fields.map((f, i) => (
                <span key={f.k}>
                  {i > 0 && " · "}
                  <span className={cn(f.unknown && "italic")} title={f.title}>
                    {f.text}
                  </span>
                </span>
              ))
            ) : (
              "Tables from another producer, not from a migration set"
            )}
            {" · "}
            <span className={cn(c.columns === null && "italic")} data-testid="count-columns">
              {c.columns ?? notCountedText(g)}
            </span>{" "}
            columns ·{" "}
            <span className={cn(c.pairs === null && "italic")} data-testid="count-pairs">
              {c.pairs ?? notCountedText(g)}
            </span>{" "}
            FK pairs
          </span>
          {hiddenTests > 0 && (
            <span className="text-xs text-muted-foreground" data-testid="hidden-test-tables">
              {hiddenTests} test {hiddenTests === 1 ? "table" : "tables"} hidden ·{" "}
              <button type="button" className="text-brand-text underline underline-offset-[3px]" onClick={onIncludeTests}>
                Include tests
              </button>
            </span>
          )}
        </div>
        {more && (
          <>
            <div aria-hidden="true" className="pointer-events-none absolute right-0 bottom-0 left-0 h-7 bg-gradient-to-t from-card to-transparent" data-testid="strip-fade" />
            <button
              type="button"
              className="absolute right-2 bottom-1 inline-flex items-center gap-0.5 rounded-md border bg-card px-1.5 text-[11.5px] text-brand-text shadow-sm hover:bg-accent"
              onClick={scrollMore}
              title="More lines below: scroll the group's lines"
              data-testid="strip-more"
            >
              more <ChevronDown aria-hidden="true" className="size-3" />
            </button>
          </>
        )}
      </div>
    </div>
  );
}
