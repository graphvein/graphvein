// The transport strips (T061 D3 variant A, T065): one line per transport
// that has endpoints or calls, at every window size: transport, endpoint
// count, frameworks, the status bar, the linked share of the calls, one
// chip per category and the endpoints with no linked caller. The
// "Coverage and roots" disclosure (remembered per browser, closed by
// default) opens today's coverage cards with the per-root rows below the
// strips. Transports with neither endpoints nor calls get no strip; the
// toolbar shows them as dashed "none found" tabs (Toolbar.tsx). Below
// 1300 px the chips drop their words (the words stay in the tooltip and
// for screen readers) and the frameworks (the list's group header names
// them), so the strip stays one line at 1280 px.

import { ChevronDown } from "lucide-react";
import { useState } from "react";

import {
  CATEGORY_LABEL,
  CATEGORY_TONE,
  callNoun,
  coverageCells,
  endpointNoun,
  linkedCount,
  linkedPercent,
  neverCalledLabel,
  NO_LINKED_CALLER_HINT,
  neverScoped,
  rawLabel,
} from "@/api/categories";
import type { TransportSummary } from "@/api/endpoints";
import { cn } from "@/lib/utils";
import { barParts, noneFound, type CoverageActions } from "@/sections/api/coverage";
import { BAR_CLASS, StackedBar, TransportBadge } from "@/sections/api/parts";

const ACCENT: Record<string, string> = { http: "bg-http-bar", tauri_ipc: "bg-ipc-bar", server_action: "bg-action-bar" };
const CHIP =
  "inline-flex h-[26px] items-center gap-1.5 whitespace-nowrap rounded-md border px-2 text-[12.5px] text-muted-foreground max-[1299px]:gap-1 max-[1299px]:px-1.5";
/** WORDS hides a chip's words below 1300 px (T061 open question 9): the
 * number and its coloured mark stay, the words stay readable to screen
 * readers and in the tooltip. */
const WORDS = "max-[1299px]:sr-only";

/** COVERAGE_OPEN_KEY is the localStorage key of the "Coverage and roots"
 * disclosure: exactly "open" opens it; anything else (or no storage) is
 * the default, closed (T061 open question 10). */
export const COVERAGE_OPEN_KEY = "graphvein.api.coverage";

function readOpen(): boolean {
  try {
    return window.localStorage.getItem(COVERAGE_OPEN_KEY) === "open";
  } catch {
    return false;
  }
}

function writeOpen(open: boolean): void {
  try {
    if (open) window.localStorage.setItem(COVERAGE_OPEN_KEY, "open");
    else window.localStorage.removeItem(COVERAGE_OPEN_KEY);
  } catch {
    // Storage blocked: the choice lasts for this page only.
  }
}

/** useCoverageOpen is the disclosure's state, read once from storage and
 * written back on every change. */
export function useCoverageOpen(): [boolean, (open: boolean) => void] {
  const [open, setOpen] = useState(readOpen);
  return [
    open,
    (next: boolean) => {
      writeOpen(next);
      setOpen(next);
    },
  ];
}

/** COVERAGE_PANEL_ID is the id of the cards the disclosure controls. */
export const COVERAGE_PANEL_ID = "api-coverage-panel";

function Row({ t, scope, actions }: { t: TransportSummary; scope: string; actions: CoverageActions }) {
  // Project mode: the project's counts (T022 MR-UI); the repository's own
  // linked percentage is in the tooltip.
  const project = t.calls.repository_by_status !== null;
  const pct = linkedPercent(linkedCount(t.calls.by_status), t.calls.total);
  const ownPct = project ? linkedPercent(t.calls.repository_by_status?.linked ?? 0, t.calls.total) : null;
  return (
    <div className="flex min-h-10 min-w-0 flex-wrap items-center gap-x-3 gap-y-1 py-[5px] pr-2.5 pl-3 max-[1299px]:gap-x-2" data-testid={`strip-${t.transport}`}>
      <span className="flex flex-none items-center gap-2 text-[12.5px] whitespace-nowrap">
        <TransportBadge transport={t.transport} />
        <span data-testid={`strip-count-${t.transport}`}>
          <b className="text-[13px] font-semibold tabular-nums">{t.endpoint_count}</b>{" "}
          <span className="text-muted-foreground">{endpointNoun(t.transport, t.endpoint_count)}</span>
        </span>
        {t.frameworks.length > 0 && (
          // Below 1300 px the list group header still names them.
          <span className="max-w-[180px] truncate text-muted-foreground max-[1299px]:hidden">{t.frameworks.join(", ")}</span>
        )}
      </span>
      <StackedBar parts={barParts(t.transport, t.calls.by_status)} className="w-[96px] flex-none max-[1299px]:w-[72px]" />
      <span className="flex-none text-[12.5px] whitespace-nowrap">
        <span
          data-testid={`strip-pct-${t.transport}`}
          title={project ? `linked in the project, linked across repositories included; inside this repository alone ${ownPct === null ? "-" : `${String(ownPct)}%`}` : undefined}
        >
          <b className="text-sm tabular-nums">{pct === null ? "-" : `${String(pct)}%`}</b> <span className="text-muted-foreground">linked</span>
        </span>
        <span className="text-muted-foreground" data-testid={`strip-total-${t.transport}`}>
          {" "}
          of {t.calls.total} {callNoun(t.transport, t.calls.total)}
        </span>
      </span>
      {coverageCells(t.transport, t.calls.by_status, neverScoped(t).count, project).map((cell) => {
        const category = cell.kind === "never" ? "never" : cell.category;
        const tone = cell.kind === "never" ? "never" : CATEGORY_TONE[cell.category];
        const label =
          cell.kind === "never"
            ? neverCalledLabel(scope, undefined, neverScoped(t).project).toLowerCase()
            : project && cell.category === "unmatched"
              ? "unmatched in the project"
              : CATEGORY_LABEL[cell.category].toLowerCase();
        const clickable = category !== "linked" && cell.count > 0;
        const selected = actions.activeGap?.transport === t.transport && actions.activeGap.category === category;
        const detail = cell.kind === "category" ? cell.raws.map((r) => `${rawLabel(r.status)}: ${String(r.count)}`).join(", ") : NO_LINKED_CALLER_HINT;
        return (
          <button
            key={category}
            type="button"
            disabled={!clickable}
            data-cell={category}
            title={`${String(cell.count)} ${label}${detail === "" ? "" : `. ${detail}`}`}
            className={cn(
              CHIP,
              clickable ? "cursor-pointer hover:bg-accent" : "cursor-default",
              cell.count === 0 && "opacity-60",
              selected && "border-brand bg-brand-soft",
            )}
            onClick={() => {
              if (clickable && cell.kind === "never") actions.openGap(t.transport, "never");
              else if (clickable && cell.kind === "category" && cell.category !== "linked") actions.openGap(t.transport, cell.category);
            }}
          >
            <i aria-hidden="true" className={cn("size-[7px] rounded-[2px]", BAR_CLASS[tone])} />
            <b className="font-semibold text-foreground tabular-nums">{cell.count}</b>
            <span className={WORDS}>{label}</span>
            {clickable && <span aria-hidden="true">›</span>}
          </button>
        );
      })}
    </div>
  );
}

/** SummaryStrip renders the strips of the shown transports and the
 * "Coverage and roots" disclosure button. scope is the scope the counts
 * were read in (the endpoints answer's scope). Nothing is rendered when
 * no shown transport has endpoints or calls. */
export function SummaryStrip({
  transports,
  scope,
  actions,
  open,
  onOpen,
}: {
  transports: TransportSummary[];
  scope: string;
  actions: CoverageActions;
  /** Whether the coverage cards are open below the strips. */
  open: boolean;
  onOpen: (open: boolean) => void;
}) {
  const full = transports.filter((t) => !noneFound(t));
  if (full.length === 0) return null;
  return (
    <div className="relative flex flex-none items-start rounded-xl border bg-card shadow-[0_1px_2px_rgba(0,0,0,.04)]" data-testid="summary-strip">
      <div className="min-w-0 flex-1 divide-y">
        {full.map((t) => (
          <div key={t.transport} className="relative">
            <span aria-hidden="true" className={cn("absolute inset-y-1.5 left-0 w-[3px] rounded-r-[2px]", ACCENT[t.transport])} />
            <Row t={t} scope={scope} actions={actions} />
          </div>
        ))}
      </div>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={COVERAGE_PANEL_ID}
        title={open ? "Hide the coverage cards and source roots" : "Show the coverage cards, precision and source roots"}
        className="m-[7px] inline-flex h-[26px] flex-none items-center gap-1 rounded-md px-2 text-[12.5px] font-medium whitespace-nowrap hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
        data-testid="coverage-toggle"
        onClick={() => {
          onOpen(!open);
        }}
      >
        Coverage and roots
        <ChevronDown aria-hidden="true" className={cn("size-3.5 transition-transform", open && "rotate-180")} />
      </button>
    </div>
  );
}
