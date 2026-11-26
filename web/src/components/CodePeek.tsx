// The code peek panel (T016e2, ADR 0044): the current source around one
// stored node, the exact span highlighted when the server proved it, and
// "Open in editor". It requests GET /api/v0/source itself from a node
// identity, so any view (the API detail pane now, the Flow tab and trace
// lanes later) only passes an identity and optional context.
//
// Precision (VISION, principle 6):
// - the highlight is drawn only from source.highlight, and only for the
//   states exact and text_match (the guard drops it otherwise); the UI
//   computes no span and never marks the stored line of a changed file;
// - text_match always shows the "file changed" banner, changed and
//   span_invalid a high-contrast banner over the plain current lines;
// - an answer read from another index run than the surrounding view is
//   not shown.
// Text is rendered as text nodes; styling uses classes only (ADR 0041 CSP).

import {
  Binary,
  ChevronDown,
  ChevronUp,
  CircleHelp,
  DatabaseX,
  FileLock,
  FileWarning,
  FileX,
  MapPinOff,
  TriangleAlert,
  X,
  type LucideIcon,
} from "lucide-react";
import { useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import { ApiError } from "@/api/client";
import type { IdeStatus, PresentedNode, ScopeLabel } from "@/api/endpoints";
import {
  DEFAULT_CONTEXT,
  HIGHLIGHT_STATES,
  MAX_CONTEXT,
  httpSourceClient,
  isKnownState,
  type SourceClient,
  type SourceHighlight,
  type SourceResponse,
  type SourceView,
} from "@/api/source";
import { EditorLink } from "@/components/EditorLink";
import { IdeCaveat, IdeMark } from "@/components/IdeParts";
import { PrecisionChip } from "@/components/precision/PrecisionChip";
import { familyName, fileChip, scopeChip } from "@/components/precision/words";
import { Skeleton } from "@/components/ui/skeleton";
import { useSource } from "@/hooks/useSource";
import { useEditor } from "@/hooks/useEditor";
import { presentIde } from "@/lib/ide";
import { cn } from "@/lib/utils";

/** CONTEXT_STEP is how many lines each "Show more" adds on each side. */
export const CONTEXT_STEP = 8;

/** Lines kept above the anchor line when the panel scrolls to it. */
const ANCHOR_LINES = 3;

export interface CodePeekProps {
  /** The canonical identity of the stored node to show. */
  id: string;
  /** Replaces the HTTP client in tests. */
  client?: SourceClient | undefined;
  /** The finished_at of the index run the surrounding view shows ("none"
   * without an index). An answer of another run is not shown. Omit to
   * skip the check. */
  expectedRun?: string | undefined;
  /** The polled status says the index is stale. */
  stale?: boolean | undefined;
  /** A name for the header instead of the node's qualified name (the
   * caller's function, for a call node). */
  title?: string | undefined;
  /** Where the node sits in the surrounding view ("caller 1 of 3"). */
  step?: string | undefined;
  /** A token that forces a new request, such as the view's reload count. */
  reload?: number | undefined;
  /** Closes the panel (close button and Esc). */
  onClose?: (() => void) | undefined;
  /** The element id of the panel, for aria-controls on what opens it. */
  panelId?: string | undefined;
  className?: string | undefined;
}

/** callsColumn is the coverage column of the code calls label: its row,
 * or for row D the family. */
function callsColumn(label: ScopeLabel | null): string {
  return label !== null && label.row !== null && label.row !== "D" ? label.row : "code_calls";
}

/** runKey names the index run of an answer, as the API section does. */
function runKey(r: SourceResponse): string {
  return r.index?.finished_at ?? "none";
}

/** kindLabel turns a node kind into words: client_call -> Client call. */
export function kindLabel(kind: string): string {
  if (kind === "ipc_call") return "IPC call";
  if (kind === "action_call") return "Action use";
  const words = kind.replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

type Tone = "exact" | "warn" | "none";

interface StateInfo {
  tone: Tone;
  label: string;
  detail: string;
}

/** stateInfo is the chip text of a source state; unknown states keep their
 * raw name. */
export function stateInfo(state: string): StateInfo {
  switch (state) {
    case "exact":
      return { tone: "exact", label: "Exact", detail: "file unchanged since index" };
    case "text_match":
      return { tone: "warn", label: "Text match", detail: "file changed since index" };
    case "changed":
      return { tone: "warn", label: "Changed", detail: "span not shown" };
    case "span_invalid":
      return { tone: "warn", label: "Span invalid", detail: "span not shown" };
    case "missing":
      return { tone: "none", label: "Missing", detail: "file not found" };
    case "not_regular":
      return { tone: "none", label: "Not a regular file", detail: "not read" };
    case "too_large":
      return { tone: "none", label: "Too large", detail: "not read" };
    case "binary":
      return { tone: "none", label: "Not text", detail: "not shown" };
    case "unreadable":
      return { tone: "none", label: "Unreadable", detail: "read failed" };
    case "no_location":
      return { tone: "none", label: "No location", detail: "nothing to show" };
    case "not_indexed":
      return { tone: "none", label: "Not indexed", detail: "not read" };
    default:
      return { tone: "none", label: `State ${state}`, detail: "not highlighted" };
  }
}

const CHANGED_REASON: Record<string, string> = {
  span_out_of_range: "the stored span is past the end of the file now",
  span_text_differs: "the text at the stored span is not the indexed text",
  span_not_unique: "the indexed text occurs more than once in the file now",
  empty_span: "the span is empty, so its text cannot be checked",
};

function formatSize(bytes: number | null): string {
  if (bytes === null) return "larger than 4 MiB";
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

interface Empty {
  icon: LucideIcon;
  title: string;
  text: string;
}

function emptyState(s: SourceView): Empty {
  switch (s.state) {
    case "missing":
      return { icon: FileX, title: "File not found", text: `${s.file.path} was deleted or moved since the last index. Nothing to show.` };
    case "not_regular":
      return {
        icon: FileLock,
        title: "Not a regular file",
        text: "A part of the path is a symlink or not a regular file or directory now, so Graphvein does not read it (the indexer reads only regular files).",
      };
    case "too_large":
      return { icon: FileWarning, title: "File too large", text: `The file is ${formatSize(s.file.size)} now, above the 4 MiB limit of the code peek.` };
    case "binary":
      return { icon: Binary, title: "Not a text file", text: "The file now contains a NUL byte or is not valid UTF-8, so it would not be indexed." };
    case "unreadable":
      return { icon: FileWarning, title: "File cannot be read", text: s.reason ?? "The file could not be opened or read." };
    case "no_location":
      return { icon: MapPinOff, title: "No source location", text: "This node has no file or span in the index (a synthetic node)." };
    case "not_indexed":
      return {
        icon: DatabaseX,
        title: "File not in the index",
        text: "The node's file is not a stored file of the index (a store inconsistency), so nothing was read. Run graphvein index.",
      };
    default:
      return {
        icon: CircleHelp,
        title: `Unknown state ${s.state}`,
        text: "This version of the UI does not know this source state; nothing is shown.",
      };
  }
}

/** linePart is how one returned line is drawn: whether it is in the
 * highlighted band and which UTF-16 range of it is marked. */
export interface LinePart {
  on: boolean;
  start: number;
  end: number;
}

/** lineParts places the server's highlight on line n (text t). A line is
 * banded when it holds at least one highlighted character or line feed:
 * every line before end_line (its line feed is in the span), end_line only
 * when end_column > 0 or the span is on one line. Columns are clamped to
 * the line; nothing is computed beyond the server's range. */
export function lineParts(n: number, t: string, h: SourceHighlight | null): LinePart {
  if (h === null || n < h.start_line || n > h.end_line) return { on: false, start: 0, end: 0 };
  if (n === h.end_line && n !== h.start_line && h.end_column === 0) return { on: false, start: 0, end: 0 };
  const start = n === h.start_line ? Math.min(h.start_column, t.length) : 0;
  const end = n === h.end_line ? Math.min(h.end_column, t.length) : t.length;
  return { on: true, start, end: Math.max(start, end) };
}

function StateChip({ state }: { state: string }) {
  const info = stateInfo(state);
  return (
    <span className="inline-flex items-center gap-1.5 text-[12.5px] whitespace-nowrap text-muted-foreground" data-testid="peek-state" data-state={state}>
      <span
        aria-hidden="true"
        className={cn("size-[7px] rounded-full", info.tone === "exact" ? "bg-linked" : info.tone === "warn" ? "bg-stale" : "bg-never")}
      />
      <b className={cn("font-semibold", info.tone === "exact" ? "text-linked" : info.tone === "warn" ? "text-dynamic" : "text-foreground")}>
        {info.label}
      </b>
      <span>· {info.detail}</span>
    </span>
  );
}

function PathLine({ path, loc }: { path: string; loc: string | null }) {
  const segs = path.split("/");
  return (
    <div className="min-w-0 flex-1 font-mono text-[12px] leading-[1.45] text-foreground/80 [overflow-wrap:anywhere]" data-testid="peek-path">
      {segs.map((seg, i) => (
        <span key={i} className="inline-block max-w-full [overflow-wrap:anywhere]">
          {seg}
          {i < segs.length - 1 ? "/" : null}
          {i === segs.length - 1 && loc !== null ? <span className="text-muted-foreground">:{loc}</span> : null}
        </span>
      ))}
    </div>
  );
}

const IDE_BASE =
  "inline-flex h-7 flex-none items-center gap-1.5 whitespace-nowrap rounded-md border border-input bg-background px-2.5 text-[12.5px] font-medium shadow-xs [&_svg]:size-3.5";

function PeekIdeButton({ node, status, enabled, why }: { node: PresentedNode | null; status: IdeStatus | null; enabled: boolean; why: string }) {
  const editor = useEditor();
  const link = node?.ide ?? null;
  const view = presentIde(status, enabled ? link : null, editor, why);
  if (view.href === null) {
    // A peek that cannot be trusted (span mismatch, no lines) keeps its own
    // reason; the editor-off and unavailable reasons come first.
    return (
      <button type="button" disabled title={view.title} aria-label={view.name} className={cn(IDE_BASE, "cursor-not-allowed text-muted-foreground opacity-55 shadow-none")}>
        <IdeMark view={view} />
        {view.text}
      </button>
    );
  }
  return (
    <EditorLink href={view.href} place={node?.identity ?? view.href} label={view.text} detail={view.title} aria-label={view.name} className={cn(IDE_BASE, "hover:bg-accent")} data-testid="peek-ide">
      <IdeMark view={view} />
      {view.text}
      <IdeCaveat view={view} />
    </EditorLink>
  );
}

function Banner({ tone, children, testId }: { tone: "amber" | "strong"; children: ReactNode; testId: string }) {
  return (
    <div
      role="note"
      data-testid={testId}
      className={cn(
        "mx-4 mt-2 flex flex-none items-start gap-2 rounded-lg px-2.5 py-[7px] text-[12.5px] leading-[1.45]",
        tone === "amber" ? "border border-stale/55 bg-stale/10" : "bg-stale text-stale-foreground shadow-[0_0_0_3px_hsl(var(--stale)/.22)]",
      )}
    >
      <TriangleAlert aria-hidden="true" className={cn("mt-px size-[15px] flex-none", tone === "amber" && "text-dynamic")} />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function StateBanner({ s, storedLine }: { s: SourceView; storedLine: number | null }) {
  if (s.state === "text_match" && s.highlight !== null) {
    const h = s.highlight;
    const at = h.start_line === h.end_line ? `line ${String(h.start_line)}` : `lines ${String(h.start_line)}–${String(h.end_line)}`;
    return (
      <Banner tone="amber" testId="peek-banner">
        <b className="font-semibold">File changed since the last index;</b> this span's text was found unchanged at {at}. Run graphvein
        index to refresh.
      </Banner>
    );
  }
  if (s.state === "changed") {
    const why = s.reason !== null ? (CHANGED_REASON[s.reason] ?? s.reason) : null;
    return (
      <Banner tone="strong" testId="peek-banner">
        <b className="font-semibold">File changed since the last index; lines may have moved. Span not shown.</b>{" "}
        {storedLine !== null ? `Showing the current lines around stored line ${String(storedLine)}` : "Showing the current lines"}
        {why !== null ? ` (${why})` : ""}. Run graphvein index to locate it.
      </Banner>
    );
  }
  if (s.state === "span_invalid") {
    return (
      <Banner tone="strong" testId="peek-banner">
        <b className="font-semibold">The stored span does not fit this unchanged file. Span not shown.</b> The indexer recorded a range
        outside the file or inside a character; showing the current lines around stored line {storedLine ?? "?"}.
      </Banner>
    );
  }
  if (!isKnownState(s.state)) {
    return (
      <Banner tone="strong" testId="peek-banner">
        <b className="font-semibold">Unknown source state {s.state}.</b> This version of the UI does not know it; nothing is highlighted.
      </Banner>
    );
  }
  return null;
}

function MoreButton({ dir, onClick, busy }: { dir: "above" | "below"; onClick: () => void; busy: boolean }) {
  const Icon = dir === "above" ? ChevronUp : ChevronDown;
  return (
    <button
      type="button"
      disabled={busy}
      onClick={onClick}
      className={cn(
        "flex h-[26px] w-full items-center gap-1.5 px-3.5 text-left font-sans text-[12px] font-medium text-brand-text hover:bg-accent disabled:cursor-wait disabled:opacity-60",
        dir === "above" ? "border-b" : "border-t",
      )}
    >
      <Icon aria-hidden="true" className="size-[13px]" />
      Show more {dir}
      <span className="font-normal text-muted-foreground">· {CONTEXT_STEP} lines</span>
    </button>
  );
}

function CodeLines({ s, onMore, busy, canMore }: { s: SourceView; onMore: (dir: "above" | "below") => void; busy: boolean; canMore: boolean }) {
  const first = s.first_line ?? 1;
  const last = first + s.lines.length - 1;
  const total = s.file.line_count;
  // Only exact and text_match carry a highlight (parseSource drops it
  // otherwise); this check keeps that true for hand-built views too.
  const h = HIGHLIGHT_STATES.includes(s.state) ? s.highlight : null;
  const cut = new Set(s.cut_lines);
  const moreAbove = canMore && first > 1;
  const moreBelow = canMore && total !== null && last < total;
  return (
    <div className="rounded-md border bg-code-bg font-mono text-[12px] leading-[19px] [tab-size:4]" data-testid="peek-code">
      {moreAbove && (
        <MoreButton
          dir="above"
          busy={busy}
          onClick={() => {
            onMore("above");
          }}
        />
      )}
      {s.lines.map((t, i) => {
        const n = first + i;
        const p = lineParts(n, t, h);
        return (
          <div
            key={n}
            data-line={n}
            data-on={p.on ? "true" : undefined}
            className={cn("grid grid-cols-[46px_minmax(0,1fr)] pr-2.5", p.on && "bg-code-hl shadow-[inset_3px_0_0_hsl(var(--brand))]")}
          >
            <span aria-hidden="true" className={cn("pr-2.5 text-right select-none", p.on ? "font-semibold text-brand-text" : "text-muted-foreground")}>
              {n}
            </span>
            <span className="min-w-0 whitespace-pre-wrap [overflow-wrap:anywhere]">
              {p.on ? (
                <>
                  {t.slice(0, p.start)}
                  <mark className="rounded-[2px] bg-code-sp py-px text-inherit" data-testid="peek-mark">
                    {t.slice(p.start, p.end)}
                  </mark>
                  {t.slice(p.end)}
                </>
              ) : (
                t
              )}
              {cut.has(n) && (
                <span className="font-sans text-muted-foreground" title="This line is longer than 4,096 bytes; the rest is not shown" data-cut="true">
                  {" "}
                  … line cut
                </span>
              )}
              {t === "" && !cut.has(n) ? " " : null}
            </span>
          </div>
        );
      })}
      {moreBelow && (
        <MoreButton
          dir="below"
          busy={busy}
          onClick={() => {
            onMore("below");
          }}
        />
      )}
    </div>
  );
}

function EmptyBody({ s }: { s: SourceView }) {
  const e = emptyState(s);
  const Icon = e.icon;
  return (
    <div className="grid justify-items-start gap-2 rounded-lg border border-dashed p-[18px] text-[13px]" data-testid="peek-empty">
      <div className="grid size-8 place-items-center rounded-lg bg-never-soft text-never">
        <Icon aria-hidden="true" className="size-4" />
      </div>
      <b className="text-sm font-semibold">{e.title}</b>
      <span className="text-muted-foreground [overflow-wrap:anywhere]">{e.text}</span>
    </div>
  );
}

function failureText(err: Error): string {
  if (err instanceof ApiError && err.status === 404) return "This node is no longer in the index; a re-index removed or renamed it.";
  if (err instanceof ApiError && err.code === "index_changed") return "The index changed twice while the source was loading. Retry when indexing has finished.";
  return err.message;
}

/** CodePeek shows the source of node id. It keeps its size in every state
 * (loading, failure, no source), so the layout around it does not jump. */
export function CodePeek({ id, client = httpSourceClient, expectedRun, stale, title, step, reload = 0, onClose, panelId, className }: CodePeekProps) {
  const [ctx, setCtx] = useState({ id, context: DEFAULT_CONTEXT, dir: null as "above" | "below" | null });
  // A new node starts at the default context (state derived from props).
  if (ctx.id !== id) setCtx({ id, context: DEFAULT_CONTEXT, dir: null });
  const context = ctx.id === id ? ctx.context : DEFAULT_CONTEXT;
  const req = useSource(client, id, context, reload);
  // Keep the previous answer of the same node visible while "Show more"
  // loads, so the code does not blink; another node or reload drops it.
  const [held, setHeld] = useState<{ id: string; reload: number; data: SourceResponse } | null>(null);
  if (req.state.kind === "ok" && held?.data !== req.state.data) setHeld({ id, reload, data: req.state.data });
  const shown =
    req.state.kind === "ok" ? req.state.data : req.state.kind === "loading" && held !== null && held.id === id && held.reload === reload ? held.data : null;
  const busy = req.state.kind === "loading";
  const mismatch = shown !== null && expectedRun !== undefined && runKey(shown) !== expectedRun;

  const body = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const box = body.current;
    if (box === null || shown === null || mismatch) return;
    if (ctx.dir === "above") {
      box.scrollTop = 0;
      return;
    }
    if (ctx.dir === "below") {
      box.scrollTop = box.scrollHeight;
      return;
    }
    const anchor = shown.source.highlight?.start_line ?? shown.node.span?.start_line ?? null;
    if (anchor === null) return;
    const line = box.querySelector<HTMLElement>(`[data-line="${String(Math.max(shown.source.first_line ?? 1, anchor - ANCHOR_LINES))}"]`);
    if (line === null) return;
    box.scrollTop += line.getBoundingClientRect().top - box.getBoundingClientRect().top - 2;
  }, [shown, mismatch, ctx.dir]);

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === "Escape" && onClose !== undefined) {
      event.preventDefault();
      event.stopPropagation();
      onClose();
    }
  };

  const node = shown?.node ?? null;
  const s = shown?.source ?? null;
  const name = title ?? (node !== null ? node.qualified_name || node.name : null);
  const hasLines = s !== null && s.lines.length > 0;
  const h = s !== null && HIGHLIGHT_STATES.includes(s.state) ? s.highlight : null;
  const loc = h !== null ? `${String(h.start_line)}:${String(h.start_column + 1)}` : node?.span !== null && node?.span !== undefined ? String(node.span.start_line) : null;
  const ideWhy = s === null ? "Loading" : hasLines ? "" : `No source to open: ${emptyState(s).title.toLowerCase()}`;

  let content: ReactNode;
  if (req.state.kind === "failed") {
    content = (
      <div role="alert" className="grid justify-items-start gap-2 rounded-lg border border-dashed p-[18px] text-[13px]" data-testid="peek-failed">
        <span>
          <b className="font-semibold">Could not load the source.</b> {failureText(req.state.error)}
        </span>
        <button type="button" className="h-7 rounded-md border border-input bg-background px-2.5 text-[12.5px] font-medium hover:bg-accent" onClick={req.retry}>
          Retry
        </button>
      </div>
    );
  } else if (shown === null || s === null) {
    content = (
      <div className="grid gap-1.5" aria-busy="true">
        <span className="sr-only">Loading the source</span>
        {Array.from({ length: 8 }, (_, i) => (
          <Skeleton key={i} className={cn("h-3.5", i % 3 === 0 ? "w-2/3" : i % 3 === 1 ? "w-5/6" : "w-1/2")} />
        ))}
      </div>
    );
  } else if (mismatch) {
    content = (
      <div role="alert" className="grid justify-items-start gap-2 rounded-lg border border-dashed p-[18px] text-[13px]" data-testid="peek-mismatch">
        <span>
          <b className="font-semibold">The source was read from another index run than this view.</b> The index may still be changing;
          retry when indexing has finished.
        </span>
        <button type="button" className="h-7 rounded-md border border-input bg-background px-2.5 text-[12.5px] font-medium hover:bg-accent" onClick={req.retry}>
          Retry
        </button>
      </div>
    );
  } else if (hasLines) {
    content = (
      <>
        <CodeLines
          s={s}
          busy={busy}
          canMore={context < MAX_CONTEXT}
          onMore={(dir) => {
            setCtx({ id, context: Math.min(MAX_CONTEXT, context + CONTEXT_STEP), dir });
          }}
        />
        {s.truncated && (
          <p className="m-0 text-[12px] text-muted-foreground" data-testid="peek-truncated">
            The span is longer than 400 lines; its first lines are shown. Open the file in the editor for the rest.
          </p>
        )}
        {context >= MAX_CONTEXT && (
          <p className="m-0 text-[12px] text-muted-foreground">Showing at most {MAX_CONTEXT} lines of context on each side.</p>
        )}
      </>
    );
  } else {
    content = <EmptyBody s={s} />;
  }

  return (
    <section
      id={panelId}
      aria-label={name !== null ? `Code peek: ${name}` : "Code peek"}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      data-testid="code-peek"
      data-node={id}
      data-state={mismatch ? "mismatch" : (s?.state ?? (req.state.kind === "failed" ? "failed" : "loading"))}
      aria-busy={busy}
      className={cn("flex min-h-0 flex-col overflow-hidden bg-card outline-none", className)}
    >
      <div className="grid flex-none gap-1 border-b px-4 py-2">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-muted-foreground">
          {node !== null && <span className="rounded-md bg-muted px-[7px] text-[12px] leading-5 font-medium text-foreground">{kindLabel(node.kind)}</span>}
          <h3 className="m-0 min-w-0 font-mono text-[14px] leading-[1.35] font-semibold text-foreground [overflow-wrap:anywhere]">
            {name ?? <span className="inline-block h-4 w-40 animate-pulse rounded-md bg-accent align-middle" />}
          </h3>
          {step !== undefined && <span className="whitespace-nowrap">{step}</span>}
          {stale === true && (
            <span className="rounded-[4px] border border-dynamic/45 px-[5px] text-[12px] leading-4 font-semibold text-dynamic" title="The polled status says files changed since the index was built">
              index stale
            </span>
          )}
          {s !== null && !mismatch && s.file.label.language !== null && (
            <PrecisionChip
              state={fileChip(s.file.label)}
              scope={s.file.label.language}
              focus={{ language: s.file.label.language }}
              indexer={shown?.index?.indexer.name ?? null}
              testId="peek-precision"
            />
          )}
          {shown !== null && !mismatch && (
            // The file label never covers the calls of a function or
            // method; the server sends their label beside it (T020 O-S1-5).
            <PrecisionChip
              state={scopeChip(shown.code_calls_precision)}
              scope={familyName("code_calls")}
              focus={{ column: callsColumn(shown.code_calls_precision) }}
              testId="peek-calls-precision"
            />
          )}
          <span className="flex-1" />
          {s !== null && !mismatch && <StateChip state={s.state} />}
          {onClose !== undefined && (
            <button
              type="button"
              aria-label="Close code peek (Esc)"
              title="Close (Esc)"
              className="grid size-7 flex-none place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
              onClick={onClose}
            >
              <X aria-hidden="true" className="size-4" />
            </button>
          )}
        </div>
        <div className="flex min-w-0 items-center gap-2.5">
          <PathLine path={node?.file ?? s?.file.path ?? ""} loc={loc} />
          <PeekIdeButton node={node} status={shown?.ide ?? null} enabled={s !== null && !mismatch && hasLines} why={ideWhy} />
        </div>
      </div>
      {s !== null && !mismatch && <StateBanner s={s} storedLine={node?.span?.start_line ?? null} />}
      <div
        ref={body}
        tabIndex={0}
        role="region"
        aria-label={s !== null && hasLines ? `Source lines ${String(s.first_line ?? 1)} to ${String((s.first_line ?? 1) + s.lines.length - 1)} of ${s.file.path}` : "Source"}
        className="grid min-h-0 flex-1 content-start gap-2 overflow-auto px-4 pt-2 pb-2.5 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        data-testid="peek-body"
      >
        {content}
      </div>
    </section>
  );
}
