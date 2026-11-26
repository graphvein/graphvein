// The Schema section (T018b2): every migration set the index holds, its
// final tables with every column fact, its migrations and diagnostics, and
// every place where the extraction is incomplete. It reads only
// /api/v0/schema/* and the shell's polled status and summary.
//
// Requests: the shell owns the one summary request; this section requests
// /sets once per index run (ADR 0048 known gap: /sets is about 1 s on a
// large store, so tabs, selection, filters and Include tests only read the
// loaded answer), and one table or migration detail for the selection.
// A detail read from another index run than /sets requests both again once;
// otherwise a banner says so and nothing of the two runs is mixed.
//
// Precision (VISION, principle 6): pages, cells and badges come from state.ts,
// entry.ts and cells.ts; none shows an unknown as a known value.

import { Suspense, lazy, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";

import { ApiError } from "@/api/client";
import { INDEX_CHANGED } from "@/api/endpoints";
import { attrInt, httpSchemaClient, type AssumptionCatalogue, type Overlap, type SchemaClient, type SchemaGroup, type SchemaSets, type SchemaSummary } from "@/api/schema";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";
import { runOf } from "@/hooks/useSchemaSummary";
import { useSchemaMigration } from "@/hooks/useSchemaMigration";
import type { ShellSchemaSets } from "@/hooks/useSchemaSets";
import { useSchemaTable } from "@/hooks/useSchemaTable";
import type { StatusView } from "@/hooks/useStatus";
import { formatShortTime } from "@/lib/time";
import { cn } from "@/lib/utils";
import { replaceHash, useHash } from "@/router";
import { SplitPane } from "@/sections/api/SplitPane";
import { AssumptionsTab } from "@/sections/schema/AssumptionsTab";
import { catalogueView, SchemaWhyContext, type CatalogueView, type SchemaWhy } from "@/sections/schema/catalogue";
import { Changed, FailedOnly, NoneFound, NotIndexed, TestsOnly, UnknownPage, UnreadStrips } from "@/sections/schema/EmptyStates";
import { ModelFilesTab } from "@/sections/schema/ModelFilesTab";
import type { OverlapTarget } from "@/sections/schema/OverlapNote";
import { PopoverContext, usePopoverHost } from "@/sections/schema/usePopover";
import { WhyPopoverHost } from "@/sections/schema/WhyPopover";
import { MigrationDetailView, MigrationList } from "@/sections/schema/MigrationsTab";
import { names as buildNames } from "@/sections/schema/names";
import { groupKeyOf, parseSchemaParams, schemaHash, type SchemaParams, type SchemaTab } from "@/sections/schema/params";

/** The tabs in order, and the element id of the one panel they control. */
const TABS: readonly SchemaTab[] = ["tables", "migrations", "assumptions", "overview"];
export const SCHEMA_TAB_PANEL_ID = "schema-tab-panel";
import { Banner, changedFiles, FreshnessBanner, OUTLINE_BUTTON, TabButton } from "@/sections/schema/parts";
import { optionCount, SetStrip, setLabel, tablesTotal, totalText, type TablesTotal } from "@/sections/schema/SetStrip";
import { hiddenTestSets, needsSets, objectsState, pageState, scopeTables, sourceCards, summaryPage } from "@/sections/schema/state";
import { TableDetailView } from "@/sections/schema/TableDetail";
import { TableList } from "@/sections/schema/TableList";

// Lazy chunks: the compact map (ELK worker client) and the Overview tab
// (React Flow). The schema home without the Overview imports no React Flow.
const MiniMap = lazy(() => import("@/sections/schema/MiniMap"));
const OverviewTab = lazy(() => import("@/sections/schema/OverviewTab"));

function errorText(err: Error): string {
  if (err instanceof ApiError && err.code === INDEX_CHANGED) return "The index changed twice while this view was loading. Retry when indexing has finished.";
  return err.message;
}

function Failure({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div role="alert" className="grid justify-items-start gap-2 rounded-xl border bg-card px-5 py-4 text-[13px]">
      <span>
        <b className="font-semibold">Could not load the schema.</b> {message}
      </span>
      <button type="button" className={OUTLINE_BUTTON} onClick={onRetry}>
        Retry
      </button>
    </div>
  );
}

function Loading({ label }: { label: string }) {
  return (
    <div className="grid gap-3.5" aria-busy="true">
      <span className="sr-only">{label}</span>
      <Skeleton className="h-20" />
      <Skeleton className="h-80" />
    </div>
  );
}

/** firstStatementMigration is the migration "Show statements" opens: the
 * group's first with opaque or unsupported statements, else its first. */
function firstStatementMigration(g: SchemaGroup): string | null {
  const m = g.migrations.find((x) => (attrInt(x.attributes, "opaque") ?? 0) > 0 || (attrInt(x.attributes, "unsupported") ?? 0) > 0) ?? g.migrations[0];
  return m?.id ?? null;
}

interface TitleProps {
  tab: SchemaTab | null;
  onTab: (t: SchemaTab) => void;
  /** tables is the sum over the visible groups; a group not extracted is
   * never counted as 0 (Review fixes R3). */
  tables: TablesTotal | null;
  migrations: number | null;
  /** The selected group's model files: the Migrations tab then reads
   * "Model files" (T018f 2.1); undefined for a migration group. */
  modelFiles?: number | null;
  /** The catalogue's assumptions in force, once loaded. */
  assumptions: number | null;
  hidden: number;
  tests: boolean | null;
  onTests: (on: boolean) => void;
  source: string | null | undefined;
  finishedAt: string | null;
}

/** TabCount is the Tables tab's total; a "+" or "?" carries its reason as a
 * tooltip and as text for screen readers. */
function TabCount({ t }: { t: { text: string; note: string | null } }) {
  return (
    <span className={cn("text-xs text-muted-foreground tabular-nums", t.note !== null && "italic")} title={t.note ?? undefined} data-testid="tab-tables-count">
      {t.text}
      {t.note !== null && <span className="sr-only"> ({t.note})</span>}
    </span>
  );
}

function TitleRow(p: TitleProps) {
  // Arrow keys move between the tabs (each one a visible button), Home and
  // End to the ends; moving selects, as a click does.
  const onTabKey = (e: ReactKeyboardEvent<HTMLButtonElement>) => {
    if (p.tab === null) return;
    const i = TABS.indexOf(p.tab);
    let next: SchemaTab | undefined;
    if (e.key === "ArrowRight") next = TABS[(i + 1) % TABS.length];
    else if (e.key === "ArrowLeft") next = TABS[(i + TABS.length - 1) % TABS.length];
    else if (e.key === "Home") next = TABS[0];
    else if (e.key === "End") next = TABS[TABS.length - 1];
    if (next === undefined) return;
    e.preventDefault();
    p.onTab(next);
    document.getElementById(`schema-tab-${next}`)?.focus();
  };
  const tabProps = (t: SchemaTab) => ({
    on: p.tab === t,
    onClick: () => {
      p.onTab(t);
    },
    testId: `tab-${t}`,
    id: `schema-tab-${t}`,
    controls: SCHEMA_TAB_PANEL_ID,
    onKeyDown: onTabKey,
  });
  return (
    <div className="flex flex-wrap items-center gap-x-3.5 gap-y-2" data-testid="schema-head">
      <h1 id="schema-title" className="m-0 text-[22px] font-semibold tracking-tight compact:text-xl">
        Schema
      </h1>
      {p.tab !== null && (
        <div role="tablist" aria-label="Schema views" className="inline-flex h-[34px] items-center rounded-lg bg-muted p-[3px] text-muted-foreground max-[600px]:h-auto max-[600px]:flex-wrap">
          <TabButton {...tabProps("tables")}>
            Tables {p.tables !== null && <TabCount t={totalText(p.tables)} />}
          </TabButton>
          <TabButton {...tabProps("migrations")}>
            {p.modelFiles !== undefined ? (
              <>
                Model files {p.modelFiles !== null && <span className="text-xs text-muted-foreground tabular-nums">{p.modelFiles}</span>}
              </>
            ) : (
              <>
                Migrations {p.migrations !== null && <span className="text-xs text-muted-foreground tabular-nums">{p.migrations}</span>}
              </>
            )}
          </TabButton>
          <TabButton {...tabProps("assumptions")}>
            Assumptions {p.assumptions !== null && <span className="text-xs text-muted-foreground tabular-nums">{p.assumptions}</span>}
          </TabButton>
          <TabButton {...tabProps("overview")}>
            Overview
          </TabButton>
        </div>
      )}
      <div className="flex-1" />
      {p.hidden > 0 && (
        <span className="text-[12.5px] text-muted-foreground" data-testid="hidden-tests">
          {p.hidden} test-only {p.hidden === 1 ? "set" : "sets"} hidden
        </span>
      )}
      {p.tests !== null && (
        <label className="inline-flex h-8 cursor-pointer items-center gap-2 rounded-md border border-input px-2.5 text-[13px] font-medium select-none has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring">
          <input type="checkbox" className="size-3.5 accent-brand" checked={p.tests} onChange={(e) => { p.onTests(e.target.checked); }} data-testid="include-tests" />
          Include tests
        </label>
      )}
      {p.source !== undefined && (
        <span className="text-[12.5px] text-muted-foreground" title="Where the selected group's schema comes from">
          Source <b className="font-semibold text-foreground">{p.source === "migrations" || p.source === "models" ? p.source : (p.source ?? "not a migration set")}</b>
        </span>
      )}
      {p.finishedAt !== null && (
        <span className="text-[12.5px] text-muted-foreground" title={p.finishedAt}>
          Data from the index of {formatShortTime(p.finishedAt)}
        </span>
      )}
    </div>
  );
}

/** noTablesText is what a view says for a group with no table to show: an
 * empty list is "No tables stored" only when the group's tables were
 * extracted (Review fixes R1, R3). */
function noTablesText(g: SchemaGroup): string {
  const st = objectsState(g);
  if (st === "not_extracted") return "Tables not extracted for this set; zero tables here is not an empty schema.";
  if (st === "unknown") return "Whether tables were extracted for this set is unknown; zero tables here is not an empty schema.";
  return "No tables stored for this group.";
}

function MapFallback() {
  return <div className="h-[264px] flex-none border-b" aria-busy="true"><span className="sr-only">Loading the map</span></div>;
}

export interface SchemaSectionProps {
  status: StatusView;
  summary: RequestResult<SchemaSummary>;
  /** The shell's /sets request (once per index run, kept across sections). */
  sets: ShellSchemaSets;
  client?: SchemaClient | undefined;
}

/** SchemaSection renders the Schema section of the shell. */
export function SchemaSection({ status, summary, sets: shellSets, client = httpSchemaClient }: SchemaSectionProps) {
  const hash = useHash();
  const params = useMemo(() => parseSchemaParams(hash), [hash]);
  const update = (patch: Partial<SchemaParams>) => {
    replaceHash(schemaHash({ ...parseSchemaParams(window.location.hash), ...patch }));
  };
  const [reload, setReload] = useState(0);
  const [reloadedFor, setReloadedFor] = useState<string | null>(null);
  const [changedRetry, setChangedRetry] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [panelOpen, setPanelOpen] = useState(false);
  const filterRef = useRef<HTMLInputElement>(null);
  const detailRef = useRef<HTMLDivElement>(null);

  const statusIndexed = status.kind === "ok" ? status.status.index.indexed : null;
  const sum = summary.state.kind === "ok" ? summary.state.data : null;
  const summaryRun = sum === null ? null : runOf(sum.index);
  const wantSets = sum !== null && needsSets(statusIndexed, sum);
  const setsReq = shellSets.request;
  const sets: SchemaSets | null = setsReq.state.kind === "ok" ? setsReq.state.data : null;
  const setsRun = sets === null ? null : runOf(sets.index);

  const page = sum === null ? null : sets !== null ? pageState(statusIndexed, sum, sets, params.tests) : summaryPage(statusIndexed, sum);
  const homeGroups = page?.kind === "home" ? page.groups : null;
  // Include tests per table for model groups (T018f O3).
  const scoped = useMemo(() => scopeTables(homeGroups ?? [], params.tests), [homeGroups, params.tests]);
  const groups = scoped.groups;

  // The assumption catalogue (T018f): the binary's, requested once the
  // summary is known and kept for the section.
  const catReq = useApiRequest<AssumptionCatalogue>(sum !== null ? `schema-assumptions:${String(reload)}` : null, (signal) => client.assumptions({ signal }));
  const indexVersion = sets?.index?.indexer.version ?? sum?.index?.indexer.version ?? null;
  const catalogue: CatalogueView = useMemo(
    () =>
      catReq.state.kind === "ok"
        ? catalogueView(catReq.state.data, indexVersion)
        : catReq.state.kind === "failed"
          ? { kind: "failed", message: errorText(catReq.state.error) }
          : { kind: "loading" },
    [catReq.state, indexVersion],
  );
  const popovers = usePopoverHost();

  // Selection (section 8): group from set=, else from the selection, else
  // the first visible group; a selection outside the visible groups falls
  // back to the default without a banner.
  const groupOfTable = useMemo(() => {
    const m = new Map<string, SchemaGroup>();
    for (const g of groups) for (const t of g.tables) m.set(t.node.identity, g);
    return m;
  }, [groups]);
  const groupOfMigration = useMemo(() => {
    const m = new Map<string, SchemaGroup>();
    for (const g of groups) for (const x of g.migrations) m.set(x.id, g);
    return m;
  }, [groups]);
  const explicit = params.set === null ? undefined : groups.find((g) => groupKeyOf(g) === params.set);
  const fromTable = params.sel === null ? undefined : groupOfTable.get(params.sel);
  // On the Migrations tab, a migration names its group; without one, the
  // selected table's group stays selected (a model group then shows its
  // Model files, T018f).
  const fromSel = params.tab === "migrations" ? (params.mig === null ? fromTable : groupOfMigration.get(params.mig)) : fromTable;
  const group: SchemaGroup | null = explicit ?? fromSel ?? groups[0] ?? null;
  const selTable = group === null ? null : (group.tables.find((t) => t.node.identity === params.sel) ?? (params.tab === "overview" ? null : (group.tables[0] ?? null)));
  const selMigration = group === null ? null : (group.migrations.find((m) => m.id === params.mig) ?? group.migrations[0] ?? null);

  const tableId = page?.kind === "home" && (params.tab === "tables" || (params.tab === "overview" && panelOpen)) ? (selTable?.node.identity ?? null) : null;
  const migId = page?.kind === "home" && params.tab === "migrations" ? (selMigration?.id ?? null) : null;
  const tableReq = useSchemaTable(client, tableId, setsRun, reload);
  const migReq = useSchemaMigration(client, migId, setsRun, reload);
  const tableRun = tableReq.state.kind === "ok" ? runOf(tableReq.state.data.index) : null;
  const migRun = migReq.state.kind === "ok" ? runOf(migReq.state.data.index) : null;

  // A detail from another run than /sets: request the detail again, once
  // per run seen (state adjusted during render), and /sets with it.
  const other = [tableRun, migRun].find((r) => r !== null && setsRun !== null && r !== setsRun) ?? null;
  if (other !== null && reloadedFor !== other) {
    setReloadedFor(other);
    setReload((r) => r + 1);
  }
  // Every reload of this section's details asks the shell for /sets again
  // too. An effect, because /sets is the shell's state, and a parent is
  // not updated while a child renders.
  const reloadSets = shellSets.reload;
  useEffect(() => {
    if (reload > 0) reloadSets();
  }, [reload, reloadSets]);
  const tableMismatch = tableRun !== null && setsRun !== null && tableRun !== setsRun;
  const migMismatch = migRun !== null && setsRun !== null && migRun !== setsRun;

  // summary said sets, /sets holds nothing: ask both again once (the
  // summary, the shell's, from an effect as above).
  if (page?.kind === "changed" && summaryRun !== null && changedRetry !== summaryRun) {
    setChangedRetry(summaryRun);
    setReload((r) => r + 1);
  }
  const retrySummary = summary.retry;
  useEffect(() => {
    if (changedRetry !== null) retrySummary();
  }, [changedRetry, retrySummary]);

  const changed = useMemo(() => changedFiles(status), [status]);
  const whyCtx: SchemaWhy = useMemo(
    () => ({
      catalogue,
      openEntry: (a: string | null) => {
        update({ tab: "assumptions", a });
      },
      openMigration: (id: string) => {
        update({ tab: "migrations", mig: id, set: null });
      },
    }),
    // update reads the hash when called; it has no state of its own.
    [catalogue],
  );
  // The other table of an overlap (T018f): from the loaded /sets, with
  // the "test, hidden" mark when Include tests hides it (O3).
  const overlapTarget = (o: Overlap): OverlapTarget => {
    let table = null;
    let label = setLabel({ set: o.set, source: o.source });
    for (const g of sets?.sets ?? []) {
      const t = g.tables.find((x) => x.node.identity === o.table);
      if (t !== undefined) {
        table = t;
        label = setLabel(g);
        break;
      }
    }
    return { table, groupLabel: label, hiddenTest: scoped.hiddenIds.has(o.table) || (table !== null && !groupOfTable.has(o.table) && table.node.attributes.scope === "test") };
  };
  const nm = useMemo(() => (sets === null ? null : buildNames(sets)), [sets]);
  const visibleTable = (id: string) => groupOfTable.has(id);
  const hiddenTestTable = (id: string) => scoped.hiddenIds.has(id);

  // Keyboard hints, each with a visible control: "/" focuses the filter,
  // "o" opens the IDE button of the detail. Not while typing.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return;
      const t = e.target;
      if (t instanceof HTMLElement && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable)) return;
      if (e.key === "/" && filterRef.current !== null) {
        e.preventDefault();
        filterRef.current.focus();
      } else if (e.key === "o") {
        const link = document.querySelector<HTMLAnchorElement>("[data-ide-primary]");
        if (link !== null) {
          e.preventDefault();
          link.click();
        }
      }
    };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
    };
  }, []);

  const selectTable = (id: string) => {
    update({ sel: id, set: null, tab: params.tab === "migrations" ? "tables" : params.tab });
  };
  const showStatements = (g: SchemaGroup | null, id: string) => {
    const mig = id !== "" ? id : g === null ? null : firstStatementMigration(g);
    update({ tab: "migrations", mig, set: null });
  };

  const cards = sets === null || sum === null ? [] : sourceCards(groups, sets, sum.sources_read, sets.unread_tools ?? sum.unread_tools);
  const setStrip = (g: SchemaGroup) => (
    <SetStrip
      group={g}
      cards={cards}
      onSelectGroup={(x) => {
        setPanelOpen(false);
        update({ set: groupKeyOf(x), sel: null, mig: null });
      }}
      onShowStatements={() => {
        showStatements(g, "");
      }}
      hiddenTests={scoped.hidden.get(g) ?? 0}
      onIncludeTests={() => {
        update({ tests: true });
      }}
    />
  );

  let body: ReactNode;
  const repo = sum?.repository ?? (status.kind === "ok" ? status.status.repository : "This repository");
  if (status.kind === "ok" && !status.status.index.indexed) {
    body = <NotIndexed repository={status.status.repository} />;
  } else if (summary.state.kind === "failed") {
    body = <Failure message={errorText(summary.state.error)} onRetry={summary.retry} />;
  } else if (sum === null || page === null) {
    body = <Loading label="Loading the schema" />;
  } else if (page.kind === "not_indexed") {
    body = <NotIndexed repository={repo} />;
  } else if (page.kind === "none") {
    body = <NoneFound summary={sum} />;
  } else if (page.kind === "unknown") {
    body =
      wantSets && setsReq.state.kind === "failed" ? (
        <Failure message={errorText(setsReq.state.error)} onRetry={setsReq.retry} />
      ) : (
        <UnknownPage summary={sum} found={page.found} reason={page.reason} current={status.kind === "ok" ? status.status.extractor.current : null} sets={sets} />
      );
  } else if (setsReq.state.kind === "failed") {
    body = <Failure message={errorText(setsReq.state.error)} onRetry={setsReq.retry} />;
  } else if (sets === null || nm === null) {
    body = <Loading label="Loading the migration sets" />;
  } else if (page.kind === "failed_only") {
    body = <FailedOnly sets={sets} />;
  } else if (page.kind === "changed") {
    body = <Changed sets={sets} />;
  } else if (page.kind === "tests_only") {
    body = (
      <TestsOnly
        hidden={page.hidden}
        sets={sets}
        onInclude={() => {
          update({ tests: true });
        }}
      />
    );
  } else {
    const inconsistent = groups.filter((g) => g.linked_without_edge > 0 || (g.unattached !== null && (g.unattached.columns > 0 || g.unattached.indexes > 0)));
    const mismatchBanner = (what: string) => (
      <div className="p-5">
        <Banner tone="warn" testId="run-mismatch">
          The {what} was read from another index run than the set list; the index may still be changing.{" "}
          <button type="button" className="font-medium text-brand-text underline underline-offset-[3px]" onClick={() => { setReload((r) => r + 1); }}>
            Reload
          </button>
        </Banner>
      </div>
    );
    // Q6 variant Y: the unreadable strips sit under the set strip.
    const strips = <UnreadStrips sets={sets} />;
    let content: ReactNode;
    if (group === null) {
      content = null;
    } else if (params.tab === "overview") {
      const panel =
        panelOpen && selTable !== null ? (
          tableReq.state.kind === "failed" ? (
            <div className="p-5"><Failure message={errorText(tableReq.state.error)} onRetry={tableReq.retry} /></div>
          ) : tableMismatch ? (
            mismatchBanner("table")
          ) : (
            <TableDetailView
              hiddenTest={hiddenTestTable}
              table={selTable}
              group={group}
              detail={tableReq.state.kind === "ok" ? tableReq.state.data : null}
              names={nm}
              changed={changed}
              multiSet={groups.length > 1}
              onTable={(id) => { update({ sel: id, set: null }); setPanelOpen(true); }}
              onMigration={(id) => { showStatements(group, id); }}
              visible={visibleTable}
              onClose={() => { setPanelOpen(false); }}
              overlapTarget={overlapTarget}
            />
          )
        ) : null;
      content = (
        <>
          {groups.length > 1 && (
            <label className="inline-flex items-center gap-2 text-[13px] text-muted-foreground">
              Set
              <select
                className="h-8 max-w-[420px] rounded-md border border-input bg-background px-2 font-mono text-[12.5px] text-foreground"
                value={groupKeyOf(group)}
                onChange={(e) => { setPanelOpen(false); update({ set: e.target.value, sel: null }); }}
                data-testid="set-switcher"
              >
                {groups.map((g) => (
                  <option key={groupKeyOf(g)} value={groupKeyOf(g)}>
                    {setLabel(g)} ({optionCount(g)})
                  </option>
                ))}
              </select>
            </label>
          )}
          {setStrip(group)}
          {strips}
          {objectsState(group) === "not_extracted" || group.tables.length === 0 ? (
            <div className="rounded-xl border bg-card px-6 py-8 text-center text-[13px] text-muted-foreground" data-testid="overview-empty">
              {noTablesText(group)}
            </div>
          ) : (
            <Suspense fallback={<Loading label="Loading the Overview" />}>
              <OverviewTab
                group={group}
                run={setsRun ?? "none"}
                selected={selTable?.node.identity ?? null}
                onSelect={(id) => { update({ sel: id, set: groupKeyOf(group) }); setPanelOpen(true); }}
                panel={panel}
                onClosePanel={() => { setPanelOpen(false); }}
              />
            </Suspense>
          )}
        </>
      );
    } else if (params.tab === "migrations" && group.source === "models") {
      content = (
        <>
          {setStrip(group)}
          {strips}
          <div className="flex min-h-[340px] flex-1">
            <ModelFilesTab group={group} onTable={selectTable} hiddenIds={scoped.hiddenIds} />
          </div>
        </>
      );
    } else if (params.tab === "migrations") {
      const detail = migReq.state.kind === "ok" && !migMismatch ? migReq.state.data : null;
      content = (
        <>
          {setStrip(group)}
          {strips}
          <div className="flex min-h-[340px] flex-1">
            <SplitPane
              label="Resize the migration list"
              stackBelow={560}
              list={
                <MigrationList
                  groups={groups}
                  selected={selMigration?.id ?? null}
                  selectedGroup={group}
                  onSelect={(id) => { update({ mig: id, set: null }); }}
                  onSelectGroup={(g) => { update({ set: groupKeyOf(g), mig: null }); }}
                  changed={changed}
                />
              }
              detail={
                <div ref={detailRef} className="min-h-0 flex-1 overflow-auto rounded-xl border bg-card" aria-live="polite">
                  {selMigration === null ? (
                    <div className="grid h-full place-items-center p-6 text-[13px] text-muted-foreground">No migration stored for this group.</div>
                  ) : migReq.state.kind === "failed" ? (
                    <div className="p-5"><Failure message={errorText(migReq.state.error)} onRetry={migReq.retry} /></div>
                  ) : migMismatch ? (
                    mismatchBanner("migration")
                  ) : (
                    <MigrationDetailView
                      migration={selMigration}
                      objects={objectsState(group)}
                      detail={detail?.migration.identity === selMigration.id ? detail : null}
                      onMigration={(id) => { update({ mig: id, set: null }); }}
                      onTable={selectTable}
                      visible={visibleTable}
                      changed={changed}
                    />
                  )}
                </div>
              }
            />
          </div>
        </>
      );
    } else {
      const detail = tableReq.state.kind === "ok" && !tableMismatch ? tableReq.state.data : null;
      const map =
        objectsState(group) === "not_extracted" || (objectsState(group) === "unknown" && group.tables.length === 0) ? (
          <div className="flex-none border-b px-3 py-3 text-xs text-muted-foreground" data-testid="mini-map-none">
            <b className="text-[13px] font-semibold text-foreground">Map</b>{" "}
            {objectsState(group) === "not_extracted" ? "Tables not extracted for this set; no map." : "Whether tables were extracted for this set is unknown; no map."}
          </div>
        ) : group.tables.length === 0 ? null : (
          <Suspense fallback={<MapFallback />}>
            <MiniMap group={group} run={setsRun ?? "none"} selected={selTable?.node.identity ?? null} onSelect={selectTable} onOverview={() => { update({ tab: "overview" }); }} />
          </Suspense>
        );
      content = (
        <>
          {setStrip(group)}
          {strips}
          <div className="flex min-h-[420px] flex-1">
            <SplitPane
              label="Resize the table list"
              stackBelow={560}
              list={
                <TableList
                  groups={groups}
                  query={query}
                  onQuery={setQuery}
                  selected={selTable?.node.identity ?? null}
                  selectedGroup={group}
                  onSelect={(id) => { selectTable(id); }}
                  onSelectGroup={(g) => { update({ set: groupKeyOf(g), sel: null }); }}
                  changed={changed}
                  filterRef={filterRef}
                  map={map}
                />
              }
              detail={
                <div ref={detailRef} className="min-h-0 flex-1 overflow-auto rounded-xl border bg-card" aria-live="polite" data-testid="detail-pane">
                  {params.tab === "assumptions" ? (
                    <AssumptionsTab
                      catalogue={catalogue}
                      repository={repo}
                      entry={params.a}
                      onEntry={(a) => { update({ a }); }}
                      onRetry={catReq.retry}
                    />
                  ) : selTable === null ? (
                    <div className="grid h-full place-items-center p-6 text-center text-[13px] text-muted-foreground" data-testid="no-table">
                      {noTablesText(group)}
                    </div>
                  ) : tableReq.state.kind === "failed" ? (
                    <div className="p-5"><Failure message={errorText(tableReq.state.error)} onRetry={tableReq.retry} /></div>
                  ) : tableMismatch ? (
                    mismatchBanner("table")
                  ) : (
                    <TableDetailView
                      hiddenTest={hiddenTestTable}
                      table={selTable}
                      group={group}
                      detail={detail?.table.identity === selTable.node.identity ? detail : null}
                      names={nm}
                      changed={changed}
                      multiSet={groups.length > 1}
                      onTable={selectTable}
                      onMigration={(id) => { showStatements(group, id); }}
                      visible={visibleTable}
                      overlapTarget={overlapTarget}
                    />
                  )}
                </div>
              }
            />
          </div>
        </>
      );
    }
    body = (
      <>
        {inconsistent.map((g) => (
          <Banner key={groupKeyOf(g)} tone="warn" testId="inconsistency">
            Index inconsistency in {setLabel(g)}:{" "}
            {[
              g.linked_without_edge > 0 && `${String(g.linked_without_edge)} ${g.linked_without_edge === 1 ? "column has" : "columns have"} a reference marked linked but no stored foreign key`,
              g.unattached !== null && g.unattached.columns > 0 && `${String(g.unattached.columns)} ${g.unattached.columns === 1 ? "column belongs" : "columns belong"} to no stored table`,
              g.unattached !== null && g.unattached.indexes > 0 && `${String(g.unattached.indexes)} ${g.unattached.indexes === 1 ? "index belongs" : "indexes belong"} to no stored table`,
            ]
              .filter(Boolean)
              .join("; ")}
            . Nothing is drawn or attached from them; re-index.
          </Banner>
        ))}
        {content === null && strips}
        <div role="tabpanel" id={SCHEMA_TAB_PANEL_ID} aria-labelledby={`schema-tab-${params.tab}`} className="flex min-h-0 flex-1 flex-col gap-3.5 compact:gap-3">
          {content}
        </div>
      </>
    );
  }

  const home = page?.kind === "home";
  const titleTables = home ? tablesTotal(groups) : null;
  const titleMigrations = home ? groups.reduce((n, g) => n + g.migrations.length, 0) : null;
  const showTests = sets !== null && (page?.kind === "home" || page?.kind === "tests_only");
  return (
    <PopoverContext.Provider value={popovers}>
    <SchemaWhyContext.Provider value={whyCtx}>
    <section aria-labelledby="schema-title" className="flex min-h-0 flex-1 flex-col gap-3.5 compact:gap-3" data-testid="schema-section">
      <TitleRow
        tab={home ? params.tab : null}
        onTab={(tab) => {
          setPanelOpen(false);
          update({ tab });
        }}
        tables={titleTables}
        migrations={titleMigrations}
        {...(home && group?.source === "models" ? { modelFiles: group.files?.length ?? null } : {})}
        assumptions={catalogue.kind === "ok" ? catalogue.data.assumptions.filter((a) => a.folded_into === null).length : null}
        hidden={sets !== null && showTests ? hiddenTestSets(sets, params.tests) : 0}
        tests={showTests ? params.tests : null}
        onTests={(tests) => {
          update({ tests });
        }}
        source={home ? (group?.source ?? null) : page?.kind === "tests_only" ? "migrations" : undefined}
        finishedAt={sets?.index?.finished_at ?? sum?.index?.finished_at ?? null}
      />
      <FreshnessBanner status={status} />
      {body}
      <WhyPopoverHost />
    </section>
    </SchemaWhyContext.Provider>
    </PopoverContext.Provider>
  );
}
