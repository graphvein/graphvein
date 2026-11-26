// The Assumptions tab (T018f R-4, 2.1): the single catalogue of what
// Graphvein assumes about the environment, grouped by source. Each entry
// shows its id, title and text and the contract rows that rest on it
// (collapsible); an assumption folded into another is listed under its
// target. Each source also lists its contract rows (form, claim and the
// assumptions they rest on), so a row id in a popover has an entry too.
// The deep link #/schema?tab=assumptions&a=E2 (or a=row:<source>:<id>)
// scrolls to the entry and highlights it.

import { useEffect, useRef } from "react";

import type { Assumption } from "@/api/schema";
import { cn } from "@/lib/utils";
import { rowsResting, type CatalogueView } from "@/sections/schema/catalogue";
import { rowEntry } from "@/sections/schema/params";
import { OUTLINE_BUTTON } from "@/sections/schema/parts";
import { IdChip } from "@/sections/schema/WhyPopover";

const SOURCE_LABEL: Record<string, string> = { migrations: "Migrations", models: "Models", table_access: "Code to tables" };

function sourceLabel(s: string): string {
  if (s.startsWith("models/")) return `Models: ${s.slice("models/".length)} contract`;
  return SOURCE_LABEL[s] ?? s;
}

/** entryDomId is the element id of an entry. */
export function entryDomId(entry: string): string {
  return `assumption-${entry.replace(/[^A-Za-z0-9_-]/g, "_")}`;
}

function Entry({ a, folded, data, selected, onRow }: { a: Assumption; folded: Assumption[]; data: Extract<CatalogueView, { kind: "ok" }>["data"]; selected: string | null; onRow: (entry: string) => void }) {
  const rows = rowsResting(data, a.id);
  const on = selected === a.id;
  return (
    <li
      id={entryDomId(a.id)}
      className={cn("grid list-none grid-cols-[52px_minmax(140px,200px)_1fr] gap-x-3 gap-y-1 border-b border-l-[3px] px-3 py-2 max-[700px]:grid-cols-[52px_1fr]", on ? "border-l-brand bg-brand-soft" : "border-l-transparent")}
      data-testid="assumption-entry"
      data-entry={a.id}
      data-selected={on ? "true" : "false"}
    >
      <span>
        <IdChip id={a.id} kind="assumption" />
      </span>
      <b className="font-semibold">{a.title}</b>
      <span className="text-muted-foreground max-[700px]:col-span-full">{a.text}</span>
      <details className="col-start-2 col-end-[-1] text-[12.5px] max-[700px]:col-start-1" open={on}>
        <summary className="cursor-pointer text-muted-foreground">
          Rows that rest on it <span className="tabular-nums">({rows.length})</span>
        </summary>
        <span className="mt-1 flex flex-wrap gap-1" data-testid="assumption-rows">
          {rows.map((r) => (
            <button key={`${r.source}:${r.id}`} type="button" className="rounded-[4px] hover:brightness-95" title={`${sourceLabel(r.source)} row ${r.id}`} onClick={() => { onRow(rowEntry(r.source, r.id)); }}>
              <IdChip id={r.id} kind="row" />
            </button>
          ))}
          {rows.length === 0 && <span className="text-muted-foreground">No row of this build&apos;s contracts lists it.</span>}
        </span>
      </details>
      {folded.length > 0 && (
        <ul className="col-start-2 col-end-[-1] m-0 grid gap-1 pl-0 max-[700px]:col-start-1" data-testid="folded">
          {folded.map((f) => (
            <li key={f.id} id={entryDomId(f.id)} className={cn("list-none rounded-md border border-dashed px-2 py-1 text-[12.5px]", selected === f.id && "border-brand bg-brand-soft")} data-testid="assumption-entry" data-entry={f.id} data-folded-into={a.id}>
              <IdChip id={f.id} kind="assumption" /> <b className="font-semibold">{f.title}</b> <span className="text-muted-foreground">folded into {a.id}: {f.text}</span>
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

export interface AssumptionsTabProps {
  catalogue: CatalogueView;
  repository: string;
  /** The entry to scroll to and highlight (`a`). */
  entry: string | null;
  onEntry: (entry: string) => void;
  onRetry: () => void;
}

/** AssumptionsTab renders the catalogue. */
export function AssumptionsTab({ catalogue: c, repository, entry, onEntry, onRetry }: AssumptionsTabProps) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (entry === null || c.kind !== "ok") return;
    const el = document.getElementById(entryDomId(entry));
    if (el === null) return;
    const d = el.closest("details");
    if (d !== null) d.open = true;
    // Scroll only the nearest scroller (the detail pane), never the page,
    // so nothing outside the pane moves (T018f 2.5).
    let p = el.parentElement;
    while (p !== null && !/(auto|scroll)/.test(getComputedStyle(p).overflowY)) p = p.parentElement;
    if (p !== null) {
      const pr = p.getBoundingClientRect();
      const er = el.getBoundingClientRect();
      p.scrollTop += er.top - pr.top - p.clientHeight / 2 + er.height / 2;
    }
  }, [entry, c.kind]);

  let body;
  if (c.kind === "loading") body = <p className="m-0 px-5 text-[13px] text-muted-foreground" aria-busy="true">Loading the assumption catalogue…</p>;
  else if (c.kind === "failed")
    body = (
      <div role="alert" className="grid justify-items-start gap-2 px-5 text-[13px]">
        <span>
          <b className="font-semibold">Could not load the assumption catalogue.</b> {c.message} Chips and popovers still show the ids.
        </span>
        <button type="button" className={OUTLINE_BUTTON} onClick={onRetry}>
          Retry
        </button>
      </div>
    );
  else {
    const data = c.data;
    // Each model tool's rows follow the shared Models assumptions.
    const rank = (src: string): string => (src.startsWith("models/") ? `models\u0001${src}` : src);
    const sources = [...new Set([...data.assumptions.map((a) => a.source), ...Object.keys(data.rows)])].sort((a, b) => (rank(a) < rank(b) ? -1 : rank(a) > rank(b) ? 1 : 0));
    const known = new Set(data.assumptions.map((a) => a.id));
    body = (
      <div className="grid gap-4">
        {sources.map((src) => {
          const inForce = data.assumptions.filter((a) => a.source === src && (a.folded_into === null || !known.has(a.folded_into)));
          const rows = Object.entries(data.rows[src] ?? {});
          return (
            <section key={src} className="grid gap-1" data-testid="assumption-source" data-source={src}>
              <h3 className="m-0 px-5 text-[14px] font-semibold">{sourceLabel(src)}</h3>
              <ul className="m-0 grid border-t pl-0 text-[13px]">
                {inForce.map((a) => (
                  <Entry key={a.id} a={a} folded={data.assumptions.filter((f) => f.folded_into === a.id)} data={data} selected={entry} onRow={onEntry} />
                ))}
              </ul>
              <details className="px-5 text-[13px]" open={entry?.startsWith(`row:${src}:`) === true}>
                <summary className="cursor-pointer text-muted-foreground">
                  Contract rows <span className="tabular-nums">({rows.length})</span>: the row id on an unknown fact or a claim names one of these
                </summary>
                <ul className="m-0 mt-1 grid pl-0" data-testid="contract-rows">
                  {rows.map(([id, r]) => {
                    const e = rowEntry(src, id);
                    return (
                      <li key={id} id={entryDomId(e)} className={cn("grid list-none grid-cols-[72px_minmax(120px,240px)_1fr_auto] gap-x-3 border-b py-1 max-[700px]:grid-cols-[72px_1fr]", entry === e && "bg-brand-soft")} data-testid="contract-row" data-entry={e}>
                        <span>
                          <IdChip id={id} kind="row" />
                        </span>
                        <span className="font-mono text-[12.5px] [overflow-wrap:anywhere]">{r.branch ?? ""}</span>
                        <span>{r.summary}</span>
                        <span className="flex flex-wrap gap-0.5">
                          {r.assumptions.length === 0 ? (
                            <span className="text-xs text-muted-foreground">rests on none</span>
                          ) : (
                            r.assumptions.map((a) => (
                              <button key={a} type="button" className="rounded-[4px]" onClick={() => { onEntry(a); }} title={`${a} in this tab`}>
                                <IdChip id={a} kind="assumption" />
                              </button>
                            ))
                          )}
                        </span>
                      </li>
                    );
                  })}
                </ul>
              </details>
            </section>
          );
        })}
        {entry !== null && !entryKnown(data, entry) && (
          <p className="m-0 px-5 text-[13px] text-muted-foreground italic" data-testid="entry-unknown">
            {entry} is not in this build&apos;s catalogue.
          </p>
        )}
      </div>
    );
  }
  return (
    <div ref={ref} className="grid content-start gap-2 py-4" data-testid="assumptions-tab">
      <div className="grid gap-1 px-5">
        <h2 className="m-0 flex items-baseline gap-2 text-xl font-semibold">
          Assumptions <span className="text-[13px] font-normal text-muted-foreground">for {repository}</span>
        </h2>
        <p className="m-0 text-[13px] text-muted-foreground">
          What Graphvein assumes about the environment, per source. Each fact the schema view shows as known names the assumptions it rests on (its &quot;Rests on&quot; line,
          a row chip or a popover).
        </p>
        {c.kind === "ok" && c.versionNote !== null && (
          <p className="m-0 text-[13px] text-dynamic" data-testid="catalogue-version-note">
            {c.versionNote}
          </p>
        )}
      </div>
      {body}
    </div>
  );
}

function entryKnown(data: Extract<CatalogueView, { kind: "ok" }>["data"], entry: string): boolean {
  if (data.assumptions.some((a) => a.id === entry)) return true;
  const m = /^row:([^:]*):(.*)$/.exec(entry);
  return m !== null && data.rows[m[1] ?? ""]?.[m[2] ?? ""] !== undefined;
}
