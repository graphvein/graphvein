// The per-fact "why" popover (T018f R-5, 2.2): what a fact rests on, in one
// click. It shows the stored reason, the contract row (id, form and claim
// from the catalogue), the assumption texts, the source line as an IDE link
// under the browser's editor choice (ADR 0050), and a link to the entry in
// the Assumptions tab. One at a time (usePopover); `position: fixed`, placed
// beside its trigger and kept in the viewport; a bottom sheet below 600 px.
// Focus moves into it on open, Tab cycles inside, Escape or the close
// button closes it and returns focus to the trigger, a click outside
// closes it.

import { ExternalLink, X } from "lucide-react";
import { useContext, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";

import type { IdeLink } from "@/api/endpoints";
import { EditorLink } from "@/components/EditorLink";
import { useEditor } from "@/hooks/useEditor";
import { useLabel } from "@/hooks/useLabels";
import { EDITOR_OFF, ideLabel } from "@/lib/ide";
import { cn } from "@/lib/utils";
import { contractRow, useSchemaWhy, type CatalogueView } from "@/sections/schema/catalogue";
import { rowEntry } from "@/sections/schema/params";
import { PopoverContext, usePopoverTrigger, WHY_POPOVER_ID } from "@/sections/schema/usePopover";

/** A source line: an IDE link (/table, /migration) or, for facts only
 * /sets holds, text with a link to the migration in the Migrations tab. */
export interface WhySource {
  label: string;
  link: IdeLink | null;
  /** A migration id to open in the Migrations tab, or null. */
  migration?: string | null;
}

/** One reason: the stored text, the row or precondition id behind it and
 * where it is. */
export interface WhyReason {
  text: string;
  row: string | null;
  where: WhySource | null;
}

/** Why is the content of one popover. Every field is optional except the
 * kind (a data attribute for the tests and the measure check), the title
 * and the contract the row ids belong to. */
export interface Why {
  kind: "cell" | "state" | "rests" | "row" | "source" | "unread" | "table_list" | "overlap" | "verification";
  title: string;
  /** "migrations" or "models": the contract of the row ids. */
  source: string;
  lead?: ReactNode;
  reasons?: WhyReason[];
  rows?: string[];
  assumptions?: { heading: string; ids: string[] | null };
  sources?: WhySource[];
  /** The Assumptions tab entry the footer opens; null: the tab's top. */
  entry?: string | null;
  entryLabel?: string;
}

/** IdChip is a contract-row or assumption id in its quiet chip. */
export function IdChip({ id, kind }: { id: string; kind: "row" | "assumption" }) {
  return (
    <span
      className={cn(
        "inline-flex h-[18px] items-center rounded-[4px] px-1 font-mono text-[11px] font-semibold whitespace-nowrap",
        kind === "row" ? "bg-dynamic-soft text-dynamic" : "border border-dashed border-muted-foreground/70 text-foreground",
      )}
    >
      {id}
    </span>
  );
}

/** SourceLink is a source line under the editor choice: the IDE link, or
 * the text alone when links are off or there is none. */
export function SourceLink({ s, onMigration }: { s: WhySource; onMigration?: (id: string) => void }) {
  const { off } = useEditor();
  const mig = s.migration ?? null;
  return (
    <span className="inline-flex max-w-full min-w-0 flex-wrap items-baseline gap-x-2">
      {s.link !== null && !off ? (
        <EditorLink
          href={s.link.link}
          place={s.label}
          label={ideLabel(s.link.link)}
          detail={s.link.link}
          className="inline-flex max-w-full min-w-0 items-center gap-0.5 font-mono text-[12.5px] [overflow-wrap:anywhere] text-brand-text underline underline-offset-[3px]"
          data-testid="why-source-link"
        >
          {s.label}
          <ExternalLink aria-hidden="true" className="size-3" />
        </EditorLink>
      ) : (
        <span className="max-w-full min-w-0 font-mono text-[12.5px] [overflow-wrap:anywhere]" {...(off && s.link !== null ? { title: EDITOR_OFF } : {})} data-testid="why-source-text">
          {s.label}
        </span>
      )}
      {mig !== null && onMigration !== undefined && (
        <button type="button" className="text-[12.5px] text-brand-text underline underline-offset-[3px]" onClick={() => { onMigration(mig); }}>
          in the Migrations tab
        </button>
      )}
    </span>
  );
}

function Heading({ children }: { children: ReactNode }) {
  return <div className="mt-1 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">{children}</div>;
}

/** RowLine is one contract row: id, form and claim from the catalogue. */
function RowLine({ c, source, id }: { c: CatalogueView; source: string; id: string }) {
  const row = contractRow(c, source, id);
  return (
    <div className="flex min-w-0 items-baseline gap-1.5" data-testid="why-row" data-row={id}>
      <IdChip id={id} kind="row" />
      {row !== null ? (
        <span className="min-w-0 [overflow-wrap:anywhere]">
          {row.branch !== null && (
            <>
              <span className="font-mono text-[12.5px]">{row.branch}</span> ·{" "}
            </>
          )}
          {row.summary}
        </span>
      ) : c.kind === "ok" ? (
        <span className="text-muted-foreground italic">not in this build&apos;s catalogue</span>
      ) : (
        <span className="text-muted-foreground italic">row text unavailable{c.kind === "failed" ? " (the catalogue did not load)" : ""}</span>
      )}
    </div>
  );
}

/** AssumptionLine is one assumption: id, title and text. */
export function AssumptionLine({ c, id }: { c: CatalogueView; id: string }) {
  const a = c.kind === "ok" ? c.byId.get(id) : undefined;
  return (
    <div className="flex min-w-0 items-baseline gap-1.5" data-testid="why-assumption" data-assumption={id}>
      <IdChip id={id} kind="assumption" />
      {a !== undefined ? (
        <span className="min-w-0 [overflow-wrap:anywhere]">
          <b className="font-semibold">{a.title}.</b> <span className="text-muted-foreground">{a.text}</span>
        </span>
      ) : c.kind === "ok" ? (
        <span className="text-muted-foreground italic">not in this build&apos;s catalogue</span>
      ) : (
        <span className="text-muted-foreground italic">assumption text unavailable</span>
      )}
    </div>
  );
}

/** SHEET_BELOW: windows narrower than this get a bottom sheet. */
export const SHEET_BELOW = 600;
const WIDTH = 380;
const GAP = 6;
const MARGIN = 8;

function place(trigger: HTMLElement, box: HTMLElement): { left: number; top: number } {
  const r = trigger.getBoundingClientRect();
  const h = box.offsetHeight;
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  let left = Math.min(Math.max(MARGIN, r.left), vw - WIDTH - MARGIN);
  if (left < MARGIN) left = MARGIN;
  let top = r.bottom + GAP;
  if (top + h > vh - MARGIN) top = r.top - GAP - h;
  if (top < MARGIN) top = Math.max(MARGIN, vh - MARGIN - h);
  return { left, top };
}

function focusables(el: HTMLElement): HTMLElement[] {
  return [...el.querySelectorAll<HTMLElement>("a[href], button:not([disabled]), [tabindex]:not([tabindex='-1'])")];
}

/** WhyPopoverHost renders the open popover, if any, into document.body. */
export function WhyPopoverHost() {
  const api = useContext(PopoverContext);
  const open = api.open;
  if (open === null) return null;
  return createPortal(<WhyPopover key={open.id} trigger={open.trigger} why={open.why()} onClose={api.close} />, document.body);
}

function WhyPopover({ trigger, why, onClose }: { trigger: HTMLElement; why: Why; onClose: (restore: boolean) => void }) {
  const ctx = useSchemaWhy();
  const c = ctx.catalogue;
  const ref = useRef<HTMLDivElement>(null);
  const [sheet, setSheet] = useState(() => window.innerWidth < SHEET_BELOW);
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null);

  useLayoutEffect(() => {
    const update = () => {
      const narrow = window.innerWidth < SHEET_BELOW;
      setSheet(narrow);
      if (!narrow && ref.current !== null) setPos(place(trigger, ref.current));
    };
    update();
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
    };
  }, [trigger]);

  // Focus moves to the first focusable element on open.
  useEffect(() => {
    const el = ref.current;
    // preventScroll: the popover may still sit at its off-screen measuring
    // place, and focusing it must not scroll the page (C1).
    if (el !== null) (focusables(el)[0] ?? el).focus({ preventScroll: true });
  }, []);

  // Escape closes and refocuses the trigger; a click outside closes.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose(true);
      }
    };
    const onDown = (e: PointerEvent | globalThis.MouseEvent) => {
      const t = e.target;
      if (!(t instanceof Node)) return;
      if (ref.current?.contains(t) === true || trigger.contains(t)) return;
      onClose(false);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [onClose, trigger]);

  // Tab cycles inside the popover.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Tab" || ref.current === null) return;
    const list = focusables(ref.current);
    const first = list[0];
    const last = list[list.length - 1];
    if (first === undefined || last === undefined) return;
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus({ preventScroll: true });
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus({ preventScroll: true });
    }
  };

  const go = (entry: string | null) => {
    onClose(false);
    ctx.openEntry(entry);
  };
  const onMigration = (id: string) => {
    onClose(false);
    ctx.openMigration(id);
  };
  const entry = why.entry === undefined ? null : why.entry;
  const titleId = `${WHY_POPOVER_ID}-title`;
  // grid-cols-[minmax(0,1fr)] and min-w-0: a grid item's min-width is auto,
  // so a long path or mono text would widen the column past the box and
  // clip (T018f Revision 2, C1); horizontal overflow stays hidden.
  const style = sheet ? undefined : pos === null ? { left: -10000, top: 0 } : { left: pos.left, top: pos.top };
  return (
    <div
      ref={ref}
      id={WHY_POPOVER_ID}
      role="dialog"
      aria-labelledby={titleId}
      data-testid="why-popover"
      data-kind={why.kind}
      data-sheet={sheet ? "true" : "false"}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      style={style}
      className={cn(
        "fixed z-50 grid grid-cols-[minmax(0,1fr)] content-start gap-0 overflow-x-hidden overflow-y-auto border bg-popover text-[13px] text-popover-foreground shadow-[0_10px_30px_rgba(0,0,0,.18)] outline-none",
        sheet ? "right-0 bottom-0 left-0 max-h-[60vh] rounded-t-2xl" : "max-h-[70vh] w-[380px] rounded-xl",
      )}
    >
      <div className="flex min-w-0 items-start gap-2 border-b px-3.5 py-2.5">
        <h2 id={titleId} className="m-0 min-w-0 flex-1 text-[14px] font-semibold [overflow-wrap:anywhere]">
          {why.title}
        </h2>
        <button type="button" className="grid size-6 flex-none place-items-center rounded-md hover:bg-accent" aria-label="Close" title="Close (Esc)" aria-keyshortcuts="Escape" onClick={() => { onClose(true); }} data-testid="why-close">
          <X aria-hidden="true" className="size-4" />
        </button>
      </div>
      <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-1.5 px-3.5 py-2.5">
        {why.lead !== undefined && (
          <div className="min-w-0 [overflow-wrap:anywhere]" data-testid="why-lead">
            {why.lead}
          </div>
        )}
        {why.reasons !== undefined && why.reasons.length > 0 && (
          <>
            <Heading>Reasons</Heading>
            <ul className="m-0 grid min-w-0 grid-cols-[minmax(0,1fr)] gap-1 pl-0">
              {why.reasons.map((r, i) => (
                <li key={i} className="min-w-0 list-none [overflow-wrap:anywhere]" data-testid="why-reason">
                  {r.row !== null && <IdChip id={r.row} kind="row" />} {r.text}
                  {r.where !== null && (
                    <>
                      {" · "}
                      <SourceLink s={r.where} onMigration={onMigration} />
                    </>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}
        {why.rows !== undefined && why.rows.length > 0 && (
          <>
            <Heading>{why.rows.length === 1 ? "Contract row" : "Contract rows"}</Heading>
            {why.rows.map((id) => (
              <RowLine key={id} c={c} source={why.source} id={id} />
            ))}
          </>
        )}
        {why.assumptions !== undefined && (
          <>
            <Heading>{why.assumptions.heading}</Heading>
            {why.assumptions.ids === null ? (
              <span className="text-muted-foreground italic">Not recorded for this index; re-index.</span>
            ) : why.assumptions.ids.length === 0 ? (
              <span className="text-muted-foreground">None of the catalogue&apos;s assumptions.</span>
            ) : (
              why.assumptions.ids.map((id) => <AssumptionLine key={id} c={c} id={id} />)
            )}
          </>
        )}
        {why.sources !== undefined && why.sources.length > 0 && (
          <>
            <Heading>Source</Heading>
            {why.sources.map((s, i) => (
              <div key={i} className="min-w-0">
                <SourceLink s={s} onMigration={onMigration} />
              </div>
            ))}
          </>
        )}
        {c.kind === "ok" && c.versionNote !== null && (
          <p className="m-0 text-xs text-muted-foreground" data-testid="why-version-note">
            {c.versionNote}
          </p>
        )}
        {c.kind === "failed" && (
          <p className="m-0 text-xs text-muted-foreground italic" data-testid="why-catalogue-failed">
            The assumption catalogue did not load: ids are shown without their texts.
          </p>
        )}
      </div>
      <div className="min-w-0 border-t px-3.5 py-2">
        <button type="button" className="max-w-full text-left text-[13px] [overflow-wrap:anywhere] text-brand-text underline underline-offset-[3px]" onClick={() => { go(entry); }} data-testid="why-entry">
          {why.entryLabel ?? (entry === null ? "The Assumptions tab" : `${entryName(entry)} in the Assumptions tab`)} →
        </button>
      </div>
    </div>
  );
}

/** entryName writes an Assumptions tab entry for a link label. */
function entryName(entry: string): string {
  const m = /^row:[^:]*:(.*)$/.exec(entry);
  return m?.[1] ?? entry;
}

/** WhyButton is a popover trigger: a button around its content (a chip,
 * an unknown cell, a badge, a "why" link). */
export function WhyButton({ why, children, className, label, testId, title, dataId }: { why: () => Why; children: ReactNode; className?: string; label?: string; testId?: string; title?: string; dataId?: string }) {
  const props = usePopoverTrigger(why);
  return (
    <button type="button" {...props} aria-label={label} title={title} className={cn("cursor-pointer rounded-[4px] text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none", className)} data-testid={testId} data-id={dataId}>
      {children}
    </button>
  );
}

/** AssumptionChip is one id of a "Rests on" line or a row chip: it opens
 * the assumption's popover. */
export function AssumptionChip({ id, source, testId = "rests-chip" }: { id: string; source: string; testId?: string }) {
  const { catalogue } = useSchemaWhy();
  const a = catalogue.kind === "ok" ? catalogue.byId.get(id) : undefined;
  // T025 row 17 (lib/labels.ts): the catalogue title in the plain style,
  // the id in the technical style; an id the catalogue lacks stays raw.
  const l = useLabel("assumptionChip", { id, title: a?.title });
  return (
    <WhyButton
      testId={testId}
      label={l.aria ?? l.text}
      className={cn(
        "inline-flex h-[18px] items-center rounded-[4px] border border-dashed border-muted-foreground/70 px-1 text-[11px] font-semibold whitespace-nowrap hover:bg-accent",
        l.text === id && "font-mono",
      )}
      dataId={id}
      title={l.title}
      why={() => ({
        kind: "rests",
        title: a !== undefined ? `${id} · ${a.title}` : id,
        source,
        assumptions: { heading: "Assumption", ids: [id] },
        entry: id,
      })}
    >
      {l.text}
    </WhyButton>
  );
}

/** rowEntryOf is the Assumptions tab entry of a contract row. */
export function rowEntryOf(source: string, id: string): string {
  return rowEntry(source, id);
}
