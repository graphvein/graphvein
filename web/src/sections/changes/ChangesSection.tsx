// The Changes section (T019b, V3 "Global compare, three panes" with V1's
// density): the summary of the comparison the compare chip chose, the
// group's items in the list pane and one item in the item pane with its
// Change, Impact and Raw tabs. The groups are in the sidebar under
// "Changes" (a select below 600 px, where one pane shows at a time with a
// back step). It reads only /api/v0/changes/* and the shell's status.
//
// Every state keeps the summary box and the panes in place: the branch
// progress card covers the summary box, the item tabs swap inside the
// item pane, the compare menu floats (T019 Revision 2: zero layout shift).

import type { ReactNode } from "react";

import type { ChangesSummary, GroupCount } from "@/api/changes";
import type { StatusView } from "@/hooks/useStatus";
import { useApiRequest } from "@/hooks/useApiRequest";
import { cn } from "@/lib/utils";
import { replaceHash, useHash } from "@/router";
import { NotExtractedChip } from "@/sections/changes/chips";
import { ItemDetail, lookupQuery } from "@/sections/changes/ItemDetail";
import { ItemList } from "@/sections/changes/ItemList";
import { changesHash, parseChangesParams, type ChangesParams } from "@/sections/changes/params";
import { refusalOf, type Refusal } from "@/sections/changes/refusal";
import { RefCard, SUMMARY_MIN, SummaryBar } from "@/sections/changes/SummaryBar";
import { usePaged } from "@/sections/changes/usePaged";
import { baseLabel, groupEntries, groupLabel, plural, PRINCIPLE } from "@/sections/changes/words";
import { OUTLINE_BUTTON } from "@/sections/schema/parts";
import { DEFAULT_BASE, useCheckedAnswer, useCompare, type CompareState } from "@/shell/compare";

/** defaultGroup is the group the section opens with: the first group with
 * items, else the first group. */
export function defaultGroup(summary: ChangesSummary | null): string | null {
  if (summary === null) return null;
  const gs = summary.summary.groups;
  return (gs.find((g) => g.items > 0 && !summary.summary.not_extracted.includes(g.group)) ?? gs[0])?.group ?? null;
}

const CARD = "flex min-h-0 flex-col overflow-hidden rounded-xl border bg-card max-[599px]:overflow-visible";

function Card({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <div className={cn(CARD, "grid flex-1 content-start justify-items-start gap-2 overflow-auto px-5 py-5 text-[13px]")} data-testid={testId}>
      {children}
    </div>
  );
}

function RefusalCard({ r, c }: { r: Refusal; c: CompareState }) {
  return (
    <Card testId="changes-refusal">
      <h2 className="m-0 text-base font-semibold">{r.title}</h2>
      {r.code !== "first_run" && <code className="rounded bg-muted px-1.5 font-mono text-[11.5px]">{r.code}</code>}
      <p className="m-0 max-w-[62ch]">{r.body}</p>
      <div className="flex flex-wrap gap-2">
        {r.actions.map((a) => (
          <button
            key={a}
            type="button"
            className={OUTLINE_BUTTON}
            onClick={() => {
              if (a === "previous") c.setBase(DEFAULT_BASE);
              else if (a === "retry") c.summary.retry();
              else c.setMenuOpen(true);
            }}
          >
            {a === "pick" ? "Pick another base" : a === "branch" ? "Compare with a branch" : a === "retry" ? "Try again" : "Compare with the previous run"}
          </button>
        ))}
      </div>
      <p className="m-0 text-xs text-muted-foreground">The compare chip in the top bar shows the refused base; markers are hidden in every view until a comparison answers.</p>
    </Card>
  );
}

function NothingChanged({ data, label }: { data: ChangesSummary; label: string }) {
  const s = data.summary;
  const moves = s.groups.reduce((n, g) => n + g.moves, 0);
  const hidden = s.groups.reduce((n, g) => n + g.hidden_tests, 0);
  return (
    <Card testId="changes-nothing">
      <h2 className="m-0 text-base font-semibold">Nothing changed since {label}</h2>
      <p className="m-0 max-w-[62ch]">
        No fact differs between the sides under contract {s.contract_version}
        {s.partial ? "; one side has knowledge gaps (see the banner), so facts there are unknown, not unchanged" : ", and both sides are complete"}. {plural(moves, "fact")} moved (only
        their lines changed); moves are counted, not listed.
        {hidden > 0 ? ` ${plural(hidden, "test-scope item")} hidden.` : ""}
      </p>
      {s.not_extracted.length > 0 && (
        <p className="m-0 flex flex-wrap items-center gap-1.5">
          Not compared: {s.not_extracted.map((g) => <NotExtractedChip key={g}>{groupLabel(g)}</NotExtractedChip>)}
        </p>
      )}
    </Card>
  );
}

function Panes({ params, summary, setParams }: { params: ChangesParams; summary: ChangesSummary; setParams: (p: Partial<ChangesParams>) => void }) {
  const c = useCompare();
  const s = summary.summary;
  const entries = groupEntries(s.groups, s.not_extracted);
  const groupId = params.group ?? defaultGroup(summary) ?? "";
  const group: GroupCount | null = s.groups.find((g) => g.group === groupId) ?? null;
  const notExtracted = s.not_extracted.includes(groupId);
  const listKey = notExtracted || groupId === "" || c.comparison === null ? null : `items#${c.comparison}#${groupId}#${params.change ?? ""}#${params.q}#${String(params.tests)}`;
  const raw = usePaged(listKey, (cursor, signal) =>
    c.client.items({ base: c.choice.base, group: groupId, change: params.change, q: params.q === "" ? null : params.q, tests: params.tests, cursor }, { signal }),
  );
  // An answer from another index run than the summary shows as loading
  // while the summary reloads (R5-2).
  const firstState = useCheckedAnswer(c, raw.first.state, raw.more);
  const paged = { ...raw, first: { ...raw.first, state: firstState }, more: firstState.kind === "ok" ? raw.more : [] };
  const listed = paged.first.state.kind === "ok" ? [...paged.first.state.data.items, ...paged.more.flatMap((m) => m.items)] : [];
  const selId = params.sel ?? listed[0]?.id ?? null;
  const inList = listed.find((i) => i.id === selId);
  // A selection not on the loaded pages (a deep link, a move hint) is
  // looked up by its identity across every group.
  const lookupKey = selId !== null && inList === undefined && paged.first.state.kind !== "loading" && c.comparison !== null ? `lookup#${c.comparison}#${selId}` : null;
  const lookupReq = useApiRequest(lookupKey, (signal) => c.client.items({ base: c.choice.base, q: lookupQuery(selId ?? ""), tests: true, limit: 50 }, { signal }));
  const lookup = { ...lookupReq, state: useCheckedAnswer(c, lookupReq.state) };
  const item = inList ?? (lookup.state.kind === "ok" ? lookup.state.data.items.find((i) => i.id === selId) : undefined);

  const back = (
    <div className="flex items-center gap-1.5 text-[12.5px] min-[600px]:hidden">
      <button
        type="button"
        className="text-brand-text underline-offset-2 hover:underline"
        onClick={() => {
          setParams({ pane: "list" });
        }}
      >
        {"‹"} {groupLabel(groupId)}
      </button>
      <span className="text-muted-foreground">› item</span>
    </div>
  );

  return (
    <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)] gap-3 max-[599px]:flex max-[599px]:flex-col" data-testid="changes-panes">
      <div className={cn(CARD, params.pane === "item" && "max-[599px]:hidden")} data-testid="list-pane">
        <ItemList
          key={groupId}
          groupId={groupId}
          group={group}
          notExtracted={notExtracted}
          entries={entries}
          change={params.change}
          q={params.q}
          tests={params.tests}
          sel={selId}
          paged={paged}
          onGroup={(g) => {
            setParams({ group: g, change: null, q: "", sel: null, pane: "list" });
          }}
          onChange={(ch) => {
            setParams({ change: ch, sel: null });
          }}
          onQuery={(q) => {
            setParams({ q, sel: null });
          }}
          onTests={(t) => {
            setParams({ tests: t, sel: null });
          }}
          onSelect={(id) => {
            setParams({ sel: id, pane: "item" });
          }}
        />
      </div>
      <div className={cn(CARD, params.pane === "list" && "max-[599px]:hidden")} data-testid="item-pane">
        {item !== undefined ? (
          <ItemDetail
            item={item}
            tab={params.tab}
            depth={params.depth}
            onTab={(t) => {
              setParams({ tab: t, sel: item.id });
            }}
            onDepth={(d) => {
              setParams({ depth: d, sel: item.id });
            }}
            onOpen={(id) => {
              setParams({ sel: id, tab: "change", pane: "item" });
            }}
            back={back}
          />
        ) : (
          <div className="grid content-start gap-2 p-4 text-[12.5px] text-muted-foreground" data-testid="item-empty">
            {back}
            {selId === null
              ? notExtracted
                ? "Nothing to show: this group is not extracted."
                : paged.first.state.kind === "loading"
                  ? "Loading…"
                  : "No item to show."
              : lookup.state.kind === "loading" || paged.first.state.kind === "loading"
                ? "Loading the item…"
                : lookup.state.kind === "failed"
                  ? `Could not load the item: ${lookup.state.error.message}`
                  : "This item is not part of the comparison."}
          </div>
        )}
      </div>
    </div>
  );
}

/** ChangesSection is the page. */
export function ChangesSection({ status }: { status: StatusView }) {
  const c = useCompare();
  const hash = useHash();
  const params = parseChangesParams(hash);
  const setParams = (p: Partial<ChangesParams>) => {
    replaceHash(changesHash({ ...params, ...p }));
  };
  const runs = c.bases.state.kind === "ok" ? c.bases.state.data.runs : [];
  const label = baseLabel(c.choice.base, runs);
  const s = c.summary.state;

  let summaryBox: ReactNode = null;
  let body: ReactNode;
  if (status.kind !== "ok") {
    body = (
      <Card testId="changes-waiting">
        <p className="m-0 text-muted-foreground">{status.kind === "failed" ? "The server did not answer; see the banner above." : "Checking the server…"}</p>
      </Card>
    );
  } else if (status.status.index.last_run === null) {
    body = (
      <Card testId="changes-not-indexed">
        <h2 className="m-0 text-base font-semibold">Nothing indexed yet</h2>
        <p className="m-0">Index the repository first; the change journal starts with the second index run.</p>
      </Card>
    );
  } else if (!c.choice.on) {
    body = (
      <Card testId="changes-off">
        <h2 className="m-0 text-base font-semibold">Compare is off</h2>
        <p className="m-0 max-w-[62ch]">No comparison is requested and no view shows markers. Turn it on to see what changed since {label}.</p>
        <button
          type="button"
          className={OUTLINE_BUTTON}
          onClick={() => {
            c.setOn(true);
          }}
        >
          Turn compare on
        </button>
      </Card>
    );
  } else if (s.kind === "failed") {
    body = <RefusalCard r={refusalOf(s.error, { base: c.choice.base, baseText: label, runs: c.bases.state.kind === "ok" ? runs : null, retention: c.bases.state.kind === "ok" ? c.bases.state.data.retention : null })} c={c} />;
  } else if (s.kind === "ok") {
    summaryBox = <SummaryBar data={s.data} runs={runs} onRecheck={(base) => { const spec = base.slice(4).split(".."); c.startRef(spec[0] ?? "", spec[1] ?? null); }} />;
    body = s.data.summary.total === 0 ? <NothingChanged data={s.data} label={label} /> : <Panes params={params} summary={s.data} setParams={setParams} />;
  } else {
    summaryBox = (
      <div aria-busy="true" className="grid gap-1.5">
        <span className="sr-only">Loading the comparison</span>
        <div className="h-5 w-3/5 animate-pulse rounded bg-muted" />
        <div className="h-5 w-2/5 animate-pulse rounded bg-muted" />
      </div>
    );
    body = <div className={cn(CARD, "flex-1 animate-pulse bg-muted/40")} aria-hidden="true" />;
  }

  const showBox = summaryBox !== null || c.ref !== null;
  return (
    <section aria-label="Changes" className="flex min-h-0 flex-1 flex-col gap-3" data-testid="changes-section">
      {showBox && (
        <div className={cn("relative flex-none", SUMMARY_MIN)} data-testid="summary-box">
          <div className={cn(c.ref !== null && "invisible")}>{summaryBox}</div>
          {c.ref !== null && <RefCard progress={c.ref} onDismiss={c.dismissRef} />}
        </div>
      )}
      {body}
      <p className="m-0 flex-none text-[11.5px] text-muted-foreground max-[599px]:hidden">
        <b>Principle:</b> {PRINCIPLE}
      </p>
    </section>
  );
}
