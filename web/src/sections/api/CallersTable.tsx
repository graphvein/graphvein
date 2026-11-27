// The callers of one endpoint (D3, D8). Precision rules:
// - callers come only from the response's `callers` (stored call edges);
// - those whose status is exactly "linked" come first; a caller with a
//   stored edge but another status (one alternative links, T016d F5) is in
//   a separate group with its reasons;
// - near misses (ambiguous or test-only calls that list this endpoint among
//   their candidates) are listed apart and not counted as callers;
// - dynamic and unmatched calls record no candidates and never appear
//   here unless the index stored an edge for them.

import { ArrowDown, CodeXml } from "lucide-react";
import { Fragment, useRef, type KeyboardEvent, type MouseEvent, type ReactNode, type Ref } from "react";

import { CATEGORY_LABEL, categoryOf, endpointNoun, rawLabel } from "@/api/categories";
import type { Caller, IdeStatus, NearMiss, PresentedNode, Reason, ScopeLabel, Transport } from "@/api/endpoints";
import { FLAG_WIDTH_PX, WrongFactFlag } from "@/components/WrongFactFlag";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { FamilyChip } from "@/sections/api/precision";
import { useReport, type WrongFact } from "@/shell/report";
import {
  BADGE,
  CategoryBadge,
  FileStateChip,
  IdeButton,
  TONE_CLASS,
  WrappedPath,
  callExpression,
  callerName,
  conditionalText,
  EdgeConfidence,
  isTestScope,
  statusOf,
  viaWrapper,
} from "@/sections/api/parts";

/** splitCallers separates linked callers from callers whose status is not
 * exactly "linked" (grouped by raw status, in order of first appearance). */
export function splitCallers(callers: Caller[]): { linked: Caller[]; others: { status: string; callers: Caller[] }[] } {
  const linked: Caller[] = [];
  const others = new Map<string, Caller[]>();
  for (const c of callers) {
    const s = statusOf(c.node);
    if (s === "linked") linked.push(c);
    else others.set(s, [...(others.get(s) ?? []), c]);
  }
  return { linked, others: [...others.entries()].map(([status, list]) => ({ status, callers: list })) };
}

/** shownNearMisses drops anything that is not an ambiguous or test-only
 * candidate: a dynamic or unmatched call is never a near miss. */
export function shownNearMisses(transport: Transport, nearMisses: NearMiss[]): NearMiss[] {
  return nearMisses.filter((n) => {
    const c = categoryOf(transport, statusOf(n.node));
    return c === "ambiguous" || c === "test_only";
  });
}

/** sameReason compares two reasons by code, text and detail. */
function sameReason(a: Reason, b: Reason): boolean {
  return a.code === b.code && a.text === b.text && (a.detail ?? "") === (b.detail ?? "");
}

/** sharedAssumptions are the assumption reasons (codes ending in
 * "_assumed": path_assumed, prefix_assumed, origin_assumed) that every
 * caller of the list carries with the identical text, when there are at
 * least two callers (T071 question 10, T073). They are said once above
 * the table and left out of the rows; a reason that differs on any caller
 * stays under each caller. Near misses keep their own reasons. */
export function sharedAssumptions(callers: Caller[]): Reason[] {
  const [first, ...rest] = callers;
  if (first === undefined || rest.length === 0) return [];
  const out: Reason[] = [];
  for (const r of first.reasons) {
    if (!r.code.endsWith("_assumed") || out.some((o) => sameReason(o, r))) continue;
    if (rest.every((c) => c.reasons.some((x) => sameReason(x, r)))) out.push(r);
  }
  return out;
}

/** ASSUMPTION_LEAD is how the server starts an assumption reason's text;
 * the shared note's lead replaces it. */
const ASSUMPTION_LEAD = "Assumption: ";

/** SharedAssumption says an assumption every caller carries once, above
 * the table: the lead (label row 71), then the reason's text and detail. */
function SharedAssumption({ reason, count }: { reason: Reason; count: number }) {
  const l = label(useLabelStyle(), "sharedAssumption", { count, code: reason.code });
  const text = reason.text.startsWith(ASSUMPTION_LEAD) ? reason.text.slice(ASSUMPTION_LEAD.length) : reason.text;
  return (
    <p className="m-0 mb-1.5 rounded-md border border-dashed px-2 py-1.5 text-[12.5px] text-muted-foreground" data-testid="shared-assumption" data-reason={reason.code} title={l.title}>
      <span className="font-medium text-foreground">{l.text}</span> {text}
      {reason.detail !== null && reason.detail !== "" ? (
        <>
          {" "}
          <span className="font-mono text-[11.5px] text-foreground/80 [overflow-wrap:anywhere]">{reason.detail}</span>
        </>
      ) : null}
    </p>
  );
}

function Reasons({ reasons }: { reasons: Reason[] }) {
  if (reasons.length === 0) return null;
  return (
    <ul className="mt-[3px] grid gap-0.5 text-[12.5px] text-muted-foreground">
      {reasons.map((r, i) => (
        <li key={`${r.code}-${String(i)}`} data-reason={r.code}>
          {r.text}
          {r.detail !== null && r.detail !== "" ? (
            <>
              {" "}
              <span className="font-mono text-[11.5px] text-foreground/80 [overflow-wrap:anywhere]">{r.detail}</span>
            </>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

function CallerCell({ node, owner, reasons, peeking }: { node: PresentedNode; owner: Caller["owner"]; reasons: Reason[]; peeking: boolean }) {
  const cond = conditionalText(node);
  return (
    <>
      <div className="flex flex-wrap items-center gap-1.5 font-medium [overflow-wrap:anywhere]">
        {callerName(owner, node)}
        {peeking && (
          <span className="rounded-[4px] border border-brand/35 bg-card px-[5px] text-[12px] leading-[17px] font-semibold text-brand-text">peeking</span>
        )}
        {viaWrapper(node) && <span className={cn(BADGE, TONE_CLASS.wrapper, "font-medium")}>via wrapper</span>}
        {cond !== null && (
          <span className={cn(BADGE, "border-border font-medium")} title={cond}>
            conditional
          </span>
        )}
        {isTestScope(node) && <span className={cn(BADGE, TONE_CLASS.other, "font-medium")}>test</span>}
        <FileStateChip state={node.file_state} />
      </div>
      <div className="mt-0.5 text-foreground/82">
        <WrappedPath file={node.file} line={node.span?.start_line ?? null} />
      </div>
      <div className="mt-[5px] rounded-[4px] border bg-background/70 px-[7px] py-[3px] font-mono text-[11.5px] [overflow-wrap:anywhere]">
        {callExpression(node)}
      </div>
      {cond !== null && <div className="mt-[3px] text-[12px] text-muted-foreground">Conditional: {cond}</div>}
      <Reasons reasons={reasons} />
    </>
  );
}

function SepRow({ children, rowRef, tone }: { children: ReactNode; rowRef?: Ref<HTMLTableRowElement> | undefined; tone: "dynamic" | "muted" }) {
  return (
    <tr ref={rowRef} className="scroll-mt-2">
      <td colSpan={4} className={cn("border-b-0 pt-3.5 pb-1.5 text-left text-[12.5px] font-semibold", tone === "dynamic" ? "text-dynamic" : "text-muted-foreground")}>
        {children}
      </td>
    </tr>
  );
}

/** scrollToRow scrolls the detail pane (the nearest element marked
 * data-scroll-container) so row is at its top. Element.scrollIntoView
 * would also scroll the page and the shell around the pane. */
export function scrollToRow(row: HTMLElement): void {
  const box = row.closest<HTMLElement>("[data-scroll-container]");
  if (box === null) {
    row.scrollIntoView({ block: "start" });
    return;
  }
  const top = row.getBoundingClientRect().top - box.getBoundingClientRect().top + box.scrollTop - 8;
  box.scrollTo({ top, behavior: "smooth" });
}

const TD = "border-b px-2 py-[9px] align-top first:pl-0 last:pr-0 last:text-right";

function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="rounded-[4px] border bg-muted px-1 font-mono text-[12px] leading-4 text-foreground/80">{children}</kbd>;
}

/** PEEK_PANEL_ID is the element id of the API section's code peek, so
 * rows and peek buttons can name it with aria-controls. */
export const PEEK_PANEL_ID = "api-code-peek";

/** PeekButton toggles the code peek of one row (T016e2 Q2). label is its
 * accessible name; it names the node, since a screen reader lists every
 * row's button. data-peek-opener lets focus return here on close. */
export function PeekButton({ pressed, onClick, label, opener }: { pressed: boolean; onClick: () => void; label: string; opener: string }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      aria-label={label}
      aria-controls={pressed ? PEEK_PANEL_ID : undefined}
      title={pressed ? "Close the code peek (Enter)" : "Peek source (Enter)"}
      data-testid="peek-button"
      data-peek-opener={opener}
      className={cn(
        "inline-flex size-6 flex-none items-center justify-center rounded-md [&_svg]:size-3.5",
        pressed ? "bg-brand text-brand-foreground hover:bg-brand/90" : "text-foreground hover:bg-accent",
      )}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
    >
      <CodeXml aria-hidden="true" />
    </button>
  );
}

/** peekLabel is the accessible name of a row's peek button: the caller and
 * where it is, so the buttons of different rows can be told apart. */
export function peekLabel(name: string, node: PresentedNode): string {
  const line = node.span?.start_line ?? null;
  const where = node.file === "" ? "" : ` (${node.file}${line !== null ? `:${String(line)}` : ""})`;
  return `Peek source of ${name}${where}`;
}

/** rowKeys handles the keyboard shortcuts of a peekable row while the
 * focus is on the row or on a control inside it: Up/Down move to the
 * previous or next peekable row in the same table (the peek follows while
 * it is open), Esc closes the peek, and Enter on the row itself toggles
 * it. Enter and Space on a link or button inside the row are left to that
 * control. */
function rowKeys(event: KeyboardEvent<HTMLTableRowElement>, id: string, peek: string | null, onPeek: (id: string | null) => void): void {
  const row = event.currentTarget;
  const onRow = event.target === row;
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    const rows = [...(row.closest("tbody")?.querySelectorAll<HTMLTableRowElement>("tr[data-peek-row]") ?? [])];
    const next = rows[rows.indexOf(row) + (event.key === "ArrowDown" ? 1 : -1)];
    event.preventDefault();
    if (next === undefined) return;
    next.focus();
    const nextId = next.getAttribute("data-peek-row");
    if (peek !== null && nextId !== null) onPeek(nextId);
  } else if (event.key === "Enter" && onRow) {
    event.preventDefault();
    onPeek(peek === id ? null : id);
  } else if (event.key === "Escape" && peek !== null) {
    event.preventDefault();
    onPeek(null);
  }
}

/** rowClick opens the row's peek, unless the click was on a link or
 * button inside it or the user was selecting text. */
function rowClick(event: MouseEvent<HTMLTableRowElement>, id: string, onPeek: (id: string | null) => void): void {
  const target = event.target as HTMLElement;
  if (target.closest("a, button") !== null) return;
  const selection = window.getSelection();
  if (selection !== null && !selection.isCollapsed && event.currentTarget.contains(selection.anchorNode)) return;
  onPeek(id);
}

/** peekRowProps makes a table row open the code peek: pointer cursor,
 * hover and selected styles, a roving tab stop, mouse and keyboard. */
function peekRowProps(id: string, peek: string | null, onPeek: ((id: string | null) => void) | undefined, tabStop: boolean) {
  if (onPeek === undefined) return {};
  const on = peek === id;
  return {
    "data-peek-row": id,
    "data-peeking": on ? "true" : undefined,
    "aria-expanded": on,
    "aria-controls": on ? PEEK_PANEL_ID : undefined,
    tabIndex: tabStop ? 0 : -1,
    onClick: (e: MouseEvent<HTMLTableRowElement>) => {
      rowClick(e, id, onPeek);
    },
    onKeyDown: (e: KeyboardEvent<HTMLTableRowElement>) => {
      rowKeys(e, id, peek, onPeek);
    },
    className: cn(
      "cursor-pointer outline-none focus-visible:[&>td]:shadow-[inset_0_2px_0_hsl(var(--ring)),inset_0_-2px_0_hsl(var(--ring))]",
      on
        ? "[&>td]:bg-brand-soft [&>td:first-child]:pl-2.5 [&>td:first-child]:shadow-[inset_3px_0_0_hsl(var(--brand))] [&>td:last-child]:pr-1.5"
        : "hover:[&>td]:bg-accent/70",
    ),
  };
}

export interface CallersTableProps {
  transport: Transport;
  callers: Caller[];
  nearMisses: NearMiss[];
  nearMissesIncomplete: number;
  ide: IdeStatus;
  /** The identity whose code peek is open, or null. */
  peek?: string | null | undefined;
  /** Opens (an identity) or closes (null) the code peek; without it rows
   * are not peekable. */
  onPeek?: ((id: string | null) => void) | undefined;
  /** T020: the calls' scope label (client calls or IPC calls), or null. */
  precision?: ScopeLabel | null;
  /** T022 review (serve --project): callers whose in-repository link
   * contradicts a scope declaration; their rows are marked. */
  conflicts?: Set<string>;
  /** The fact a caller row's wrong-fact flag reports (T040); no flag
   * without it, or while the public-repository switch is off. */
  factOf?: ((c: Caller) => WrongFact) | undefined;
}

/** CallersTable renders the Callers heading and table. */
export function CallersTable({ transport, callers, nearMisses, nearMissesIncomplete, ide, peek = null, onPeek, precision = null, conflicts, factOf }: CallersTableProps) {
  // The flag's column width is reserved only while the switch is on, so
  // the table is unchanged without it.
  const flags = useReport().url !== null && factOf !== undefined;
  const { linked, others } = splitCallers(callers);
  const near = shownNearMisses(transport, nearMisses);
  const shared = sharedAssumptions(callers);
  const ownReasons = (c: Caller) => (shared.length === 0 ? c.reasons : c.reasons.filter((r) => !shared.some((x) => sameReason(x, r))));
  const firstGap = useRef<HTMLTableRowElement>(null);
  const notFully = others.reduce((n, g) => n + g.callers.length, 0);
  const noun = endpointNoun(transport, 1);
  // One tab stop for all rows (roving): the peeking row, else the first.
  const ids = [...linked, ...others.flatMap((g) => g.callers), ...near].map((c) => c.node.identity);
  const tabStop = peek !== null && ids.includes(peek) ? peek : (ids[0] ?? null);

  const peekCell = (id: string, node: PresentedNode, name: string, fact?: () => WrongFact) => (
    <div className="flex items-center justify-end gap-0.5">
      {onPeek !== undefined && (
        <PeekButton
          label={peekLabel(name, node)}
          opener={id}
          pressed={peek === id}
          onClick={() => {
            onPeek(peek === id ? null : id);
          }}
        />
      )}
      <IdeButton ide={node.ide} status={ide} place={node.identity} />
      {flags && (fact !== undefined ? <WrongFactFlag fact={fact} /> : <span className="w-6 flex-none" />)}
    </div>
  );

  const callerRow = (c: Caller, highlight: boolean) => {
    const id = c.node.identity;
    const row = peekRowProps(id, peek, onPeek, tabStop === id);
    return (
    <tr key={id} data-caller={id} {...row} className={cn(flags && "group/fact", highlight && peek !== id && "[&>td]:bg-dynamic-soft/55", row.className)}>
      <td className={TD}>
        <CallerCell node={c.node} owner={c.owner} reasons={ownReasons(c)} peeking={peek === id} />
      </td>
      <td className={TD}>
        <CategoryBadge transport={transport} raw={statusOf(c.node)} />
        {conflicts?.has(id) === true && (
          <span
            className={cn(BADGE, TONE_CLASS.dynamic, "mt-1 block w-fit")}
            title="Scopes never override an in-repository link: the call keeps it, although its consumer's declaration names another scope of this repository."
            data-testid="declaration-conflict"
          >
            linked inside the repository; the declaration names another scope
          </span>
        )}
      </td>
      <td className={TD}>
        {c.edges.map((e, i) => (
          <EdgeConfidence key={i} edge={e} block size="sm" />
        ))}
      </td>
      <td className={TD}>{peekCell(id, c.node, callerName(c.owner, c.node), factOf !== undefined ? () => factOf(c) : undefined)}</td>
    </tr>
    );
  };

  const parts: string[] = [`${String(linked.length)} linked`];
  if (notFully > 0) parts.push(`${String(notFully)} not fully linked`);
  if (near.length > 0) parts.push(`${String(near.length)} ${near.length === 1 ? "near miss" : "near misses"}`);

  return (
    <div>
      <h4 className="m-0 mb-2 flex flex-wrap items-center gap-2 text-sm font-semibold">
        {callers.length === 0 ? "Near misses" : "Callers"}{" "}
        <span className="text-[13px] font-normal text-muted-foreground">
          {callers.length === 0 ? "not linked; this endpoint is among their recorded candidates" : parts.join(" · ")}
        </span>
        <FamilyChip transport={transport} which="calls" label={precision} testId="callers-precision" />
        {onPeek !== undefined && callers.length + near.length > 0 && (
          <span className="ml-auto inline-flex items-center gap-1 text-[12px] font-normal whitespace-nowrap text-muted-foreground" aria-hidden="true">
            <Kbd>↑</Kbd>
            <Kbd>↓</Kbd> select · <Kbd>Enter</Kbd> peek · <Kbd>Esc</Kbd> close
          </span>
        )}
        {callers.length > 0 && (notFully > 0 || near.length > 0) && (
          <button
            type="button"
            className="inline-flex items-center gap-1 text-[12.5px] font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]"
            onClick={() => {
              const row = firstGap.current;
              if (row !== null) scrollToRow(row);
            }}
          >
            <ArrowDown aria-hidden="true" className="size-3.5" />
            {notFully > 0 ? "Jump to not fully linked" : "Jump to near misses"}
          </button>
        )}
      </h4>
      {shared.map((r) => (
        <SharedAssumption key={`${r.code}-${r.text}`} reason={r} count={callers.length} />
      ))}
      {callers.length + near.length > 0 && (
        <table className="w-full table-fixed border-collapse text-[13px]">
          <colgroup>
            <col />
            <col className="w-[118px]" />
            <col className={onPeek !== undefined ? "w-[148px]" : "w-[132px]"} />
            <col style={{ width: `${String((onPeek !== undefined ? 64 : 38) + (flags ? FLAG_WIDTH_PX : 0))}px` }} />
          </colgroup>
          <thead>
            <tr className="text-left text-[12.5px] whitespace-nowrap text-muted-foreground">
              <th className="h-[34px] border-b pr-2 font-medium">Caller</th>
              <th className="h-[34px] border-b px-2 font-medium">Status</th>
              <th className="h-[34px] border-b px-2 font-medium">Confidence</th>
              <th className="h-[34px] border-b pl-2 font-medium">
                <span className="sr-only">Open</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {linked.map((c) => callerRow(c, false))}
            {others.map((g, i) => (
              <Fragment key={g.status}>
                <SepRow tone="dynamic" rowRef={i === 0 ? firstGap : undefined}>
                  Linked alternative of {g.status === "" || categoryOf(transport, g.status) === "other" ? `a call with status ${rawLabel(g.status)}` : `a ${CATEGORY_LABEL[categoryOf(transport, g.status)].toLowerCase()} call`}{" "}
                  <span className="font-normal text-muted-foreground">
                    · one alternative has a stored link to this {noun}; the call is not counted as linked
                  </span>
                </SepRow>
                {g.callers.map((c) => callerRow(c, true))}
              </Fragment>
            ))}
            {near.length > 0 && (
              <SepRow tone="muted" rowRef={notFully === 0 ? firstGap : undefined}>
                Near misses · not linked, not counted above{" "}
                <span className="font-normal">· this {noun} is among their recorded candidates</span>
              </SepRow>
            )}
            {near.map((n) => {
              const id = n.node.identity;
              const row = peekRowProps(id, peek, onPeek, tabStop === id);
              return (
              <tr key={id} data-near-miss={id} {...row}>
                <td className={TD}>
                  <CallerCell node={n.node} owner={n.owner} reasons={n.reasons} peeking={peek === id} />
                </td>
                <td className={TD}>
                  <CategoryBadge transport={transport} raw={statusOf(n.node)} />
                </td>
                <td className={TD}>
                  <div className="font-mono text-xs text-muted-foreground">not linked</div>
                  <div className="text-xs text-muted-foreground">
                    {n.relation === "ambiguous_candidate"
                      ? `1 of ${n.candidate_count_exact ? "" : "at least "}${String(n.candidate_count)} tied candidates`
                      : n.relation === "test_only_candidate"
                        ? "test-only candidate"
                        : n.relation}
                  </div>
                </td>
                <td className={TD}>{peekCell(id, n.node, callerName(n.owner, n.node))}</td>
              </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {nearMissesIncomplete > 0 && (
        <p className="mt-2.5 text-[12.5px] text-muted-foreground" data-testid="near-misses-incomplete">
          {nearMissesIncomplete} ambiguous or test-only {nearMissesIncomplete === 1 ? "call lists" : "calls list"} only
          their first 10 candidates; this endpoint may be among the unlisted ones.
        </p>
      )}
    </div>
  );
}
