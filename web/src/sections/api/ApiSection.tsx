// The API section (T016d2): every HTTP route and Tauri IPC command, who
// calls each one, how sure each link is, and where the graph is
// incomplete. It consumes only /api/v0/api/* and the shell's polled
// /api/v0/status. State that belongs in a link (transport, tests, root,
// selection, open sheet) lives in the hash (params.ts).
//
// Freshness (D9): the view never checks freshness itself. When the polled
// status names a newer last run than the loaded data, or an endpoint or
// calls answer names another run than the list, everything is requested
// again; while it loads the old answer is not shown, and answers of two
// runs are never shown together. A stale or unknown status adds a banner;
// every presented node carries file_state.
//
// The detail pane has three tabs (T016f UQ3 B): Overview, Callers and Flow.
// The Flow tab's graph is a lazy chunk (React Flow and the ELK worker load
// only when it opens); its data, filters and selection live here, so the
// code peek below it works as for the Callers tab. While the Flow tab is
// open the endpoint list collapses to a rail whose list overlays the pane.

import { ChevronRight, CircleAlert, TriangleAlert, X } from "lucide-react";
import { Suspense, lazy, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { ApiError } from "@/api/client";
import { TRANSPORT_LABEL, endpointNoun, gapStatuses } from "@/api/categories";
import {
  INDEX_CHANGED,
  httpClient,
  requestableStatuses,
  type ApiClient,
  type CallsParams,
  type EndpointRow,
  type IndexRun,
  type Scope,
  type Transport,
  type TransportSummary,
} from "@/api/endpoints";
import { httpFlowClient, type FlowClient } from "@/api/flow";
import { httpSourceClient, type SourceClient } from "@/api/source";
import { CodePeek } from "@/components/CodePeek";
import { Skeleton } from "@/components/ui/skeleton";
import { MIN_TOP, VerticalSplit } from "@/components/VerticalSplit";
import { useCalls } from "@/hooks/useCalls";
import { useEndpointDetail } from "@/hooks/useEndpointDetail";
import { useEndpoints } from "@/hooks/useEndpoints";
import { useFlow } from "@/hooks/useFlow";
import { useLabelStyle } from "@/hooks/useLabels";
import type { StatusView } from "@/hooks/useStatus";
import { cn } from "@/lib/utils";
import { replaceHash, useHash } from "@/router";
import { PEEK_PANEL_ID } from "@/sections/api/CallersTable";
import { CoverageCards } from "@/sections/api/CoverageCards";
import type { CoverageActions } from "@/sections/api/coverage";
import { EndpointDetailView, peekTargets } from "@/sections/api/EndpointDetail";
import { EndpointList, endpointLabel } from "@/sections/api/EndpointList";
import { FlowControls } from "@/sections/api/flow/FlowControls";
import { buildGraph, flowPeek, type FlowPeek, type GraphOptions } from "@/sections/api/flow/graph";
import { FILTER_TIERS } from "@/sections/api/flow/tiers";
import { GapSheet } from "@/sections/api/GapSheet";
import { LinkDifferences, hasDifferences } from "@/sections/api/LinkDifferences";
import { FLOW_PEEK_CLOSED, apiHash, parseApiParams, type ApiParams, type DetailTab, type GapCategory } from "@/sections/api/params";
import { MIN_SPLIT_WIDTH, SplitPane } from "@/sections/api/SplitPane";
import { SummaryStrip } from "@/sections/api/SummaryStrip";
import { Toolbar, rootOptions } from "@/sections/api/Toolbar";

// The Flow tab's chunk: imported on first use only (D5 gate: the entry
// chunk contains neither React Flow nor ELK).
const FlowTab = lazy(() => import("@/sections/api/flow/FlowTab"));

function FlowLoading({ label }: { label: string }) {
  return (
    <div className="grid flex-1 grid-cols-4 items-center gap-8 bg-canvas p-8" aria-busy="true" data-testid="flow-loading">
      <span className="sr-only">{label}</span>
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="grid gap-3">
          <Skeleton className="h-12" />
          {i % 2 === 0 && <Skeleton className="h-12" />}
        </div>
      ))}
    </div>
  );
}

function errorText(err: Error): string {
  if (err instanceof ApiError && err.code === INDEX_CHANGED) {
    return "The index changed twice while this view was loading. Retry when indexing has finished.";
  }
  return err.message;
}

/** runKey names the index run an answer was read from. */
function runKey(index: IndexRun | null): string {
  return index?.finished_at ?? "none";
}

/** compareRuns orders two run keys by finish time: negative when a is
 * older, 0 when they are the same run, positive when a is newer. "none"
 * (no index) is older than any run; keys that are not timestamps are
 * ordered as text so the order stays stable. */
export function compareRuns(a: string, b: string): number {
  if (a === b) return 0;
  if (a === "none") return -1;
  if (b === "none") return 1;
  const ta = Date.parse(a);
  const tb = Date.parse(b);
  if (!Number.isNaN(ta) && !Number.isNaN(tb) && ta !== tb) return ta < tb ? -1 : 1;
  return a < b ? -1 : 1;
}

function mismatchText(what: string, list: string, other: string): string {
  return `The ${what} was read from another index run than the list (list ${list}, ${what} ${other}); the index may still be changing. Reload when indexing has finished.`;
}

function isGone(err: Error): boolean {
  return err instanceof ApiError && err.status === 404;
}

function Banner({ tone, children, testId }: { tone: "warn" | "info"; children: ReactNode; testId?: string }) {
  const Icon = tone === "warn" ? TriangleAlert : CircleAlert;
  return (
    <div
      role="note"
      data-testid={testId}
      className={cn(
        "flex items-start gap-2 rounded-lg border px-3 py-2 text-[13px]",
        tone === "warn" ? "border-stale/60 bg-stale/12" : "border-border bg-muted/50",
      )}
    >
      <Icon aria-hidden="true" className={cn("mt-0.5 size-4 flex-none", tone === "warn" ? "text-dynamic" : "text-muted-foreground")} />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function Failure({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div role="alert" className="grid justify-items-start gap-2 rounded-xl border bg-card px-5 py-4 text-[13px]">
      <span>
        <b className="font-semibold">Could not load the API section.</b> {message}
      </span>
      <button type="button" className="h-7 rounded-md border border-input bg-background px-2.5 text-[12.5px] font-medium hover:bg-accent" onClick={onRetry}>
        Retry
      </button>
    </div>
  );
}

function FreshnessBanner({ status }: { status: StatusView }) {
  if (status.kind === "loading") return null;
  if (status.kind === "ok") {
    const f = status.status.freshness;
    if (f.state === "fresh" || f.state === "not_indexed") return null;
    if (f.state === "stale") {
      const n = f.changed_count ?? f.changed_files.length;
      return (
        <Banner tone="warn" testId="stale-banner">
          <b className="font-semibold">The index is stale: {n === 1 ? "1 file changed" : `${String(n)} files changed`} since it was built.</b>{" "}
          Counts, callers and lines below describe the last index; files that changed are marked. Run graphvein index to update.
        </Banner>
      );
    }
  }
  return (
    <Banner tone="warn" testId="stale-banner">
      <b className="font-semibold">Freshness is unknown.</b> The data below may not match the working tree.
    </Banner>
  );
}

/** ListRail is the endpoint list collapsed to a vertical strip while the
 * Flow tab is open (as the PNG), with the button that shows the list. */
function ListRail({
  transports,
  selected,
  open,
  onToggle,
}: {
  transports: TransportSummary[];
  selected: EndpointRow | undefined;
  open: boolean;
  onToggle: (open: boolean) => void;
}) {
  const t = transports.find((x) => x.transport === selected?.transport) ?? transports[0];
  const sel = selected === undefined ? null : endpointLabel(selected);
  return (
    <div className="flex w-[46px] flex-none flex-col items-center gap-2.5 rounded-xl border bg-card py-2.5 text-[12.5px] text-muted-foreground" data-testid="list-rail">
      <button
        type="button"
        aria-expanded={open}
        aria-label={open ? "Hide the endpoint list" : "Show the endpoint list"}
        title={open ? "Hide the endpoint list" : "Show the endpoint list"}
        className="grid size-7 place-items-center rounded-md hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
        onClick={() => {
          onToggle(!open);
        }}
      >
        {open ? <X aria-hidden="true" className="size-4" /> : <ChevronRight aria-hidden="true" className="size-4" />}
      </button>
      <div className="flex rotate-180 items-center gap-2 whitespace-nowrap [writing-mode:vertical-rl]">
        {t !== undefined && (
          <span>
            {TRANSPORT_LABEL[t.transport]} · {t.endpoint_count} {endpointNoun(t.transport, t.endpoint_count)}
          </span>
        )}
        {sel !== null && <span className="font-mono text-foreground">{sel}</span>}
      </div>
    </div>
  );
}

/** ListLayout is the list/detail split, or on the Flow tab (and where the
 * split's two minimums do not fit) the rail with the list as an overlay
 * over the detail pane. */
function ListLayout({
  railed,
  rail,
  railOpen,
  onCloseRail,
  list,
  detail,
}: {
  railed: boolean;
  rail: ReactNode;
  railOpen: boolean;
  onCloseRail: () => void;
  list: ReactNode;
  detail: ReactNode;
}) {
  if (!railed) return <SplitPane list={list} detail={detail} />;
  return (
    <div className="relative flex min-h-0 min-w-0 flex-1 gap-3">
      {rail}
      {railOpen && (
        <div
          className="absolute inset-y-0 left-[58px] z-20 flex w-[min(460px,calc(100%-70px))] flex-col shadow-[0_8px_24px_rgba(0,0,0,.18)]"
          data-testid="list-overlay"
          onKeyDown={(e) => {
            if (e.key === "Escape") onCloseRail();
          }}
        >
          {list}
        </div>
      )}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">{detail}</div>
    </div>
  );
}

export interface ApiSectionProps {
  status: StatusView;
  /** Replaces the HTTP client in tests. */
  client?: ApiClient | undefined;
  /** Replaces the source client of the code peek in tests. */
  sourceClient?: SourceClient | undefined;
  /** Replaces the Flow tab's client in tests. */
  flowClient?: FlowClient | undefined;
}

/** PEEK_HEIGHT_KEY is the localStorage key of the code peek height. */
export const PEEK_HEIGHT_KEY = "graphvein.api.peekHeight";

/** FLOW_PEEK_HEIGHT_KEY keeps the Flow tab's peek height apart: the graph
 * needs a taller top pane than the callers table. */
export const FLOW_PEEK_HEIGHT_KEY = "graphvein.api.flowPeekHeight";

/** On the Flow tab the peek starts at this share of the split (the callers
 * table's 54 % left the canvas about 250 px at 1440x900), and the top pane
 * keeps at least FLOW_MIN_TOP px, so the tab row, the canvas with its zoom
 * buttons and the legend fit without scrolling. Both stay resizable. */
export const FLOW_PEEK_FRACTION = 0.26;
export const FLOW_MIN_TOP = 400;

/** PEEK_ROW_ROOM is the room kept under the route header and tabs (sticky
 * while the peek is open) for the first line of the opened row, and
 * SPLIT_HANDLE the resize handle's height, which minTop includes. */
export const PEEK_ROW_ROOM = 32;
const SPLIT_HANDLE = 12;

/** PEEK_MIN_BOTTOM is the least peek height on the Overview and Callers
 * tabs. Below the split's usual MIN_BOTTOM so that a tall route header
 * (about 280 px in a 900 px wide window) still fits above the peek; the
 * peek scrolls its own code. */
export const PEEK_MIN_BOTTOM = 120;

/** peekSplitLimits keeps the route header and tabs (headHeight, measured;
 * 0 before the first measurement) fully visible above the code peek on
 * the Overview and Callers tabs. */
export function peekSplitLimits(headHeight: number): { minTop?: number; minBottom?: number } {
  if (headHeight <= 0) return {};
  return { minTop: Math.max(MIN_TOP, Math.ceil(headHeight) + SPLIT_HANDLE + PEEK_ROW_ROOM), minBottom: PEEK_MIN_BOTTOM };
}

/** findPeekRow finds the row of id inside box, else its peek button (the
 * handler has no row). Attribute comparison, so identities need no CSS
 * escaping. */
function findPeekRow(box: HTMLElement | null, id: string): HTMLElement | null {
  if (box === null) return null;
  for (const attr of ["data-peek-row", "data-peek-opener"]) {
    for (const el of box.querySelectorAll<HTMLElement>(`[${attr}]`)) {
      if (el.getAttribute(attr) === id) return el;
    }
  }
  return null;
}

/** ApiSection renders the API section of the shell. */
export function ApiSection({ status, client = httpClient, sourceClient = httpSourceClient, flowClient = httpFlowClient }: ApiSectionProps) {
  const hash = useHash();
  const params = useMemo(() => parseApiParams(hash), [hash]);
  const update = (patch: Partial<ApiParams>) => {
    const next = { ...params, ...patch };
    // The Architecture back link belongs to the Flow tab of the endpoint
    // it was opened for.
    if (next.sel !== params.sel || next.tab !== "flow") next.back = null;
    replaceHash(apiHash(next));
  };
  const [query, setQuery] = useState("");
  const [notice, setNotice] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const [reloadedFor, setReloadedFor] = useState<string | null>(null);
  const scope: Scope = params.tests ? "all" : "production";

  const endpoints = useEndpoints(client, scope, params.root, reload);
  const data = endpoints.state.kind === "ok" ? endpoints.state.data : null;

  const detailClient = useMemo<ApiClient>(
    () => ({
      ...client,
      endpoint: (p, o) =>
        client.endpoint(p, o).catch((err: unknown) => {
          if (err instanceof Error && isGone(err) && o.signal?.aborted !== true) {
            setNotice(`The selected endpoint is no longer in the index (${p.id}); a re-index removed or renamed it.`);
            replaceHash(apiHash({ ...parseApiParams(window.location.hash), sel: null, peek: null }));
          }
          throw err;
        }),
    }),
    [client],
  );
  const detail = useEndpointDetail(detailClient, params.sel, scope, params.root, reload);
  const flowOpen = params.tab === "flow" && params.sel !== null;
  const flow = useFlow(flowClient, flowOpen ? params.sel : null, scope, params.root, reload);

  const gap = params.gap;
  const gapSummary = gap === null ? undefined : data?.transports.find((t) => t.transport === gap.transport);
  // A status with a comma cannot be requested (requestableStatuses).
  const { requestable: statuses, unlistable } = requestableStatuses(
    gap === null || gap.category === "never" || gapSummary === undefined ? [] : gapStatuses(gap.transport, gap.category, gapSummary.calls.by_status),
  );
  const callsParams: CallsParams | null =
    gap === null || gap.category === "never" || gapSummary === undefined || statuses.length === 0
      ? null
      : {
          transport: gap.transport,
          statuses,
          scope,
          root: params.root,
          // Project mode: the counts are the project's, so is the list.
          ...(gapSummary.calls.repository_by_status !== null ? { statusScope: "project" as const } : {}),
        };
  const calls = useCalls(client, callsParams, reload);

  // D9: the index changed when the polled status names a newer run than
  // the list, or the endpoint or calls answer names another run than the
  // list. Request everything again, once per newest run seen. (State
  // adjusted during render, as React documents for values derived from
  // props; the mismatched answer is never committed while that reload is
  // pending.)
  const dataRun = data === null ? null : runKey(data.index);
  const statusRun = status.kind === "ok" ? runKey(status.status.index.last_run) : null;
  const detailRun = detail.state.kind === "ok" ? runKey(detail.state.data.index) : null;
  const callsRun = calls.state.kind === "ok" ? runKey(calls.state.data.index) : null;
  const flowRun = flow.state.kind === "ok" ? runKey(flow.state.data.index) : null;
  let target: string | null = null;
  if (dataRun !== null) {
    const seen: [string | null, boolean][] = [
      [statusRun, true],
      [detailRun, false],
      [callsRun, false],
      [flowRun, false],
    ];
    for (const [run, onlyNewer] of seen) {
      if (run === null) continue;
      const c = compareRuns(run, dataRun);
      if (c === 0 || (onlyNewer && c < 0)) continue;
      const t = c > 0 ? run : dataRun;
      if (target === null || compareRuns(t, target) > 0) target = t;
    }
  }
  if (target !== null && reloadedFor !== target) {
    setReloadedFor(target);
    setReload((r) => r + 1);
  }
  const reloadAll = () => {
    setReload((r) => r + 1);
  };
  const detailMismatch = dataRun !== null && detailRun !== null && compareRuns(detailRun, dataRun) !== 0;
  const callsMismatch = dataRun !== null && callsRun !== null && compareRuns(callsRun, dataRun) !== 0;
  const flowMismatch = dataRun !== null && flowRun !== null && compareRuns(flowRun, dataRun) !== 0;

  const shown = data === null ? [] : data.transports.filter((t) => params.transport === "all" || t.transport === params.transport);
  const actions: CoverageActions = {
    openGap: (transport: Transport, category: GapCategory, root?: string | null) => {
      update({ gap: { transport, category }, ...(root !== undefined ? { root } : {}) });
    },
    setRoot: (root) => {
      update({ root });
    },
    activeGap: gap,
    root: params.root,
  };

  // The code peek (T016e2): one node of the selected endpoint at a time,
  // named by the hash's `peek` (T016i D4), so a reload or a shared link
  // opens it again. Every change of `sel` removes it. Only a node the
  // loaded detail shows can be peeked: while the detail loads (or failed)
  // the value is kept and nothing is shown, and an identity that is not a
  // target of the detail is ignored, like an unknown `node=`.
  const peekId = flowOpen ? null : params.peek;
  const targets = detail.state.kind === "ok" && !detailMismatch ? peekTargets(detail.state.data) : [];
  const detailPeek = peekId === null ? undefined : targets.find((t) => t.id === peekId);

  // The Flow tab's graph and selection (hash: node or edge). Selecting
  // opens the peek; closing it keeps the selection (D7) and writes
  // peek=0, until another card or line is selected.
  const flowData = flowOpen && flow.state.kind === "ok" && !flowMismatch ? flow.state.data : null;
  const labelStyle = useLabelStyle();
  const flowOptions = useMemo<GraphOptions>(
    () => ({ hops: params.hops, nearMisses: params.nm, tiers: new Set(params.tiers), labels: labelStyle }),
    [params.hops, params.nm, params.tiers, labelStyle],
  );
  const flowGraph = useMemo(() => (flowData === null ? null : buildGraph(flowData, flowOptions)), [flowData, flowOptions]);
  // The Flow tab lays out the 2-hop graph and draws the 1-hop one at its
  // places (T016i D3), so a hop change moves no card.
  const flowLayoutGraph = useMemo(
    () => (flowData === null ? null : flowOptions.hops === 2 ? flowGraph : buildGraph(flowData, { ...flowOptions, hops: 2 })),
    [flowData, flowOptions, flowGraph],
  );
  // A node named by the hash may be folded into another card (an owner in
  // its call's card): select that card ("Open in Flow tab" names chain
  // members by identity).
  const flowNode = params.node === null || flowGraph === null ? params.node : (flowGraph.nodes.find((n) => n.id === params.node || n.members.includes(params.node ?? ""))?.id ?? params.node);
  const flowTarget = flowData === null || flowGraph === null ? null : flowPeek(flowData, flowGraph, flowNode, params.edge);
  const flowPeekOpen = flowTarget !== null && params.peek !== FLOW_PEEK_CLOSED;
  const peekTarget: FlowPeek | undefined = flowOpen
    ? flowPeekOpen
      ? flowTarget
      : undefined
    : detailPeek === undefined
      ? undefined
      : { ...detailPeek, facts: null, differences: null };
  const peekOpen = peekTarget !== undefined;
  const topRef = useRef<HTMLDivElement>(null);
  // The element that opened the peek (a row, its button or the handler's
  // button): focus returns to it when the peek closes.
  const openerRef = useRef<HTMLElement | null>(null);
  const setPeek = (id: string | null) => {
    if (id !== null) {
      const active = document.activeElement;
      const box = topRef.current;
      openerRef.current = active instanceof HTMLElement && box !== null && box.contains(active) ? active : null;
    }
    update({ peek: id });
  };
  const closePeek = () => {
    if (flowOpen) {
      const node = params.node;
      update({ peek: FLOW_PEEK_CLOSED });
      // Focus returns to the selected card.
      requestAnimationFrame(() => {
        const box = topRef.current;
        if (box === null || node === null) return;
        if (document.activeElement !== null && box.contains(document.activeElement)) return;
        for (const el of box.querySelectorAll<HTMLElement>("[data-flow-card]")) if (el.getAttribute("data-flow-card") === node) el.focus();
      });
      return;
    }
    const was = peekId;
    const opener = openerRef.current;
    openerRef.current = null;
    setPeek(null);
    // Focus returns to the element that opened the peek, else to the
    // node's row or button (the opener may have been re-rendered away).
    // Focus already in the detail pane (closed from a row or a button)
    // stays where it is.
    if (was !== null) {
      requestAnimationFrame(() => {
        const box = topRef.current;
        if (box !== null && document.activeElement !== null && box.contains(document.activeElement)) return;
        const el = opener !== null && opener.isConnected ? opener : findPeekRow(topRef.current, was);
        el?.focus();
      });
    }
  };
  // The height of the route header and tabs, sticky at the top of the
  // detail pane while the peek is open (EndpointDetailView): the split
  // keeps them fully visible above the peek (peekSplitLimits).
  const [headHeight, setHeadHeight] = useState(0);
  // Opening the peek shrinks the detail pane: keep the opened row visible
  // below the sticky header. The pane settles later (the coverage cards
  // collapse and the split measures its new height), so the row is
  // revealed again whenever the pane's or the header's size changes while
  // this peek is open.
  useLayoutEffect(() => {
    if (peekId === null || flowOpen) return;
    const box = topRef.current;
    if (box === null) return;
    const id = peekId;
    const head = () => box.querySelector<HTMLElement>("[data-detail-head]");
    const reveal = () => {
      const row = findPeekRow(box, id);
      if (row === null) return;
      const r = row.getBoundingClientRect();
      const b = box.getBoundingClientRect();
      const top = b.top + (head()?.offsetHeight ?? 0);
      if (r.bottom > b.bottom) box.scrollTop += Math.min(r.bottom - b.bottom + 8, r.top - top - 8);
      else if (r.top < top) box.scrollTop -= top - r.top + 8;
    };
    reveal();
    const observer = new ResizeObserver(() => {
      setHeadHeight(head()?.offsetHeight ?? 0);
      reveal();
    });
    observer.observe(box);
    const h = head();
    if (h !== null) observer.observe(h);
    return () => {
      observer.disconnect();
    };
  }, [peekId, flowOpen]);

  const [railOpen, setRailOpen] = useState(false);
  // The width of the list/detail area: narrower than the split's two
  // minimums (a window about 900 px wide), the list folds into the rail as
  // on the Flow tab, so the detail keeps a usable width.
  const [splitBox, setSplitBox] = useState<HTMLDivElement | null>(null);
  const [splitWidth, setSplitWidth] = useState(0);
  useLayoutEffect(() => {
    if (splitBox === null) return;
    const observer = new ResizeObserver(() => {
      setSplitWidth(splitBox.clientWidth);
    });
    observer.observe(splitBox);
    return () => {
      observer.disconnect();
    };
  }, [splitBox]);
  const narrow = splitWidth > 0 && splitWidth < MIN_SPLIT_WIDTH;
  const selectedRow = data?.transports.flatMap((t) => t.endpoints).find((r) => r.id === params.sel);

  const detailSummary =
    detail.state.kind === "ok" ? data?.transports.find((t) => detail.state.kind === "ok" && t.transport === detail.state.data.transport) : undefined;

  let detailBody: ReactNode;
  if (params.sel === null) {
    detailBody = (
      <div className="grid h-full place-items-center px-6 py-10 text-center text-[13px] text-muted-foreground">
        Select a route or command to see its handler and callers.
      </div>
    );
  } else if (detail.state.kind === "ok" && detailMismatch) {
    detailBody = (
      <div className="grid gap-3 p-5" aria-busy="true">
        <Banner tone="warn" testId="run-mismatch">
          {mismatchText("endpoint", dataRun, detailRun)}{" "}
          <button type="button" className="font-medium text-brand-text underline underline-offset-[3px]" onClick={reloadAll}>
            Reload
          </button>
        </Banner>
        <Skeleton className="h-7 w-2/3" />
        <Skeleton className="h-24" />
      </div>
    );
  } else if (detail.state.kind === "ok") {
    const d = detail.state.data;
    let flowBody: ReactNode = null;
    if (flowOpen) {
      if (flow.state.kind === "failed") {
        flowBody = (
          <div className="p-5">
            <Failure message={errorText(flow.state.error)} onRetry={flow.retry} />
          </div>
        );
      } else if (flowMismatch) {
        flowBody = (
          <div className="grid gap-3 p-5" aria-busy="true">
            <Banner tone="warn" testId="flow-run-mismatch">
              {mismatchText("flow", dataRun, flowRun)}{" "}
              <button type="button" className="font-medium text-brand-text underline underline-offset-[3px]" onClick={reloadAll}>
                Reload
              </button>
            </Banner>
          </div>
        );
      } else if (flowData !== null && flowGraph !== null && flowLayoutGraph !== null) {
        flowBody = (
          <Suspense fallback={<FlowLoading label="Loading the Flow tab" />}>
            <FlowTab
              flow={flowData}
              graph={flowGraph}
              layoutGraph={flowLayoutGraph}
              options={flowOptions}
              selectedNode={flowNode}
              selectedEdge={params.edge}
              back={params.back}
              peekOpen={flowPeekOpen}
              onSelect={({ node, edge }) => {
                update({ node, edge, peek: null });
              }}
              onTogglePeek={() => {
                if (flowPeekOpen) closePeek();
                else update({ peek: null });
              }}
              onHops={(hops) => {
                update({ hops });
              }}
              onTiers={(tiers) => {
                update({ tiers: FILTER_TIERS.filter((t) => tiers.includes(t)) });
              }}
              onGap={(category) => {
                update({ gap: { transport: d.transport, category } });
              }}
              onCallers={() => {
                update({ tab: "callers", node: null, edge: null, peek: null });
              }}
            />
          </Suspense>
        );
      } else {
        flowBody = <FlowLoading label="Loading the flow" />;
      }
    }
    detailBody = (
      <EndpointDetailView
        detail={d}
        summary={detailSummary}
        openGap={(category) => {
          update({ gap: { transport: d.transport, category } });
        }}
        peek={peekOpen ? peekId : null}
        onPeek={(id) => {
          if (id === null) closePeek();
          else setPeek(id);
        }}
        tab={params.tab}
        onTab={(tab: DetailTab) => {
          // Switching to or from the Flow tab clears its selection and the
          // peek; Overview <-> Callers keeps the peek (T016i D4).
          const crossesFlow = (tab === "flow") !== (params.tab === "flow");
          update({ tab, node: null, edge: null, ...(crossesFlow ? { peek: null } : {}) });
        }}
        flow={flowBody}
        tabAside={
          <FlowControls
            hops={params.hops}
            nearMisses={params.nm}
            onHops={(hops) => {
              update({ hops });
            }}
            onNearMisses={(nm) => {
              update({ nm });
            }}
          />
        }
      />
    );
  } else if (detail.state.kind === "failed") {
    detailBody = (
      <div className="p-5">
        <Failure message={errorText(detail.state.error)} onRetry={detail.retry} />
      </div>
    );
  } else {
    detailBody = (
      <div className="grid gap-3 p-5" aria-busy="true">
        <span className="sr-only">Loading the endpoint</span>
        <Skeleton className="h-7 w-2/3" />
        <Skeleton className="h-5 w-1/2" />
        <Skeleton className="h-24" />
        <Skeleton className="h-40" />
      </div>
    );
  }

  let body: ReactNode;
  if (endpoints.state.kind === "failed") {
    body = <Failure message={errorText(endpoints.state.error)} onRetry={endpoints.retry} />;
  } else if (data === null) {
    body = (
      <div className="grid gap-3.5" aria-busy="true">
        <span className="sr-only">Loading the API section</span>
        <div className="grid grid-cols-2 gap-3.5 compact:grid-cols-1">
          <Skeleton className="h-36 compact:h-10" />
          <Skeleton className="h-36 compact:h-10" />
        </div>
        <Skeleton className="h-80" />
      </div>
    );
  } else if (data.index === null) {
    body = (
      <div className="rounded-xl border bg-card px-6 py-10 text-center text-[13px] text-muted-foreground" data-testid="not-indexed">
        {data.repository} has no index yet. Run <span className="font-mono text-foreground">graphvein index</span> and reload.
      </div>
    );
  } else {
    const inconsistent = shown.filter((t) => t.calls.linked_without_edge > 0);
    const unknownScope = shown.reduce((n, t) => n + t.calls.unknown_scope, 0);
    body = (
      <>
        {inconsistent.map((t) => (
          <Banner key={t.transport} tone="warn" testId="inconsistency">
            {t.calls.linked_without_edge} {t.calls.linked_without_edge === 1 ? "call is" : "calls are"} marked linked but{" "}
            {t.calls.linked_without_edge === 1 ? "has" : "have"} no stored link; re-index.
          </Banner>
        ))}
        {unknownScope > 0 && (
          <Banner tone="info">
            {unknownScope} {unknownScope === 1 ? "call has" : "calls have"} a scope value this UI does not know; such calls are only
            counted with Include tests.
          </Banner>
        )}
        {!flowOpen && (
          <>
            <CoverageCards transports={shown} repository={data.repository} scope={data.scope} actions={actions} collapsed={peekOpen} />
            <SummaryStrip transports={shown} scope={data.scope} actions={actions} shown={peekOpen} />
          </>
        )}
        <div ref={setSplitBox} className="flex min-h-[340px] flex-1">
          <ListLayout
            railed={flowOpen || narrow}
            rail={<ListRail transports={shown} selected={selectedRow} open={railOpen} onToggle={setRailOpen} />}
            railOpen={railOpen}
            onCloseRail={() => {
              setRailOpen(false);
            }}
            list={
              <EndpointList
                transports={shown}
                repository={data.repository}
                scope={data.scope}
                query={query}
                onQuery={setQuery}
                selected={params.sel}
                onSelect={(id) => {
                  setNotice(null);
                  setRailOpen(false);
                  update({ sel: id, node: null, edge: null, peek: null });
                }}
              />
            }
            detail={
              <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card shadow-[0_1px_2px_rgba(0,0,0,.04)]">
                <VerticalSplit
                  storageKey={flowOpen ? FLOW_PEEK_HEIGHT_KEY : PEEK_HEIGHT_KEY}
                  {...(flowOpen ? { defaultFraction: FLOW_PEEK_FRACTION, minTop: FLOW_MIN_TOP } : peekOpen ? peekSplitLimits(headHeight) : {})}
                  label="Resize the code peek"
                  top={
                    <div
                      ref={topRef}
                      data-scroll-container
                      className={cn("min-h-0 flex-1 overflow-auto", flowOpen && "flex flex-col")}
                      aria-live={flowOpen ? undefined : "polite"}
                    >
                      {detailBody}
                    </div>
                  }
                  bottom={
                    peekTarget !== undefined ? (
                      <div className="flex min-h-0 flex-1 flex-col">
                        {peekTarget.facts !== null && (
                          <div className="border-b bg-muted/50 px-4 py-1.5 font-mono text-xs [overflow-wrap:anywhere]" data-testid="edge-facts">
                            {peekTarget.facts}
                          </div>
                        )}
                        {flowData !== null && hasDifferences(peekTarget.differences) && (
                          <LinkDifferences diffs={peekTarget.differences} ide={flowData.ide} className="max-h-[40%] overflow-auto border-b bg-muted/30 px-4 py-2" />
                        )}
                        <CodePeek
                          className="flex-1"
                          panelId={PEEK_PANEL_ID}
                          id={peekTarget.id}
                          client={sourceClient}
                          expectedRun={dataRun ?? undefined}
                          stale={status.kind === "ok" && status.status.freshness.state === "stale"}
                          title={peekTarget.title}
                          step={peekTarget.step}
                          reload={reload}
                          onClose={closePeek}
                        />
                      </div>
                    ) : null
                  }
                />
              </div>
            }
          />
        </div>
        {gap !== null && (
          <GapSheet
            key={`${gap.transport}:${gap.category}:${params.root ?? ""}:${scope}`}
            gap={gap}
            repository={data.repository}
            scope={scope}
            summary={gapSummary}
            root={params.root}
            statuses={statuses}
            unlisted={unlistable}
            calls={callsMismatch ? { kind: "loading" } : calls.state}
            mismatch={callsMismatch ? mismatchText("list of calls", dataRun, callsRun) : null}
            ide={calls.state.kind === "ok" ? calls.state.data.ide : { available: true, error: null }}
            onRetry={callsMismatch ? reloadAll : calls.retry}
            onClose={() => {
              update({ gap: null });
            }}
            onSelectEndpoint={(id) => {
              setNotice(null);
              update({ sel: id, gap: null, peek: null });
            }}
          />
        )}
      </>
    );
  }

  return (
    <section aria-labelledby="api-title" className="flex min-h-0 flex-1 flex-col gap-3.5 compact:gap-3">
      <Toolbar
        transports={data?.transports ?? null}
        transport={params.transport}
        onTransport={(transport) => {
          update({ transport });
        }}
        tests={params.tests}
        onTests={(tests) => {
          update({ tests });
        }}
        root={params.root}
        onRoot={(root) => {
          update({ root });
        }}
        roots={rootOptions(shown)}
        finishedAt={data?.index?.finished_at ?? null}
      />
      <FreshnessBanner status={status} />
      {notice !== null && (
        <Banner tone="info" testId="notice">
          {notice}{" "}
          <button
            type="button"
            className="font-medium text-brand-text underline underline-offset-[3px]"
            onClick={() => {
              setNotice(null);
            }}
          >
            Dismiss
          </button>
        </Banner>
      )}
      {body}
    </section>
  );
}
