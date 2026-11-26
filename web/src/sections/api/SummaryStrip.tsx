// The compact coverage summary (M5 .strip/.srow), shown below 1400 px wide
// or 820 px high instead of the cards: one row per transport with the bar,
// linked percentage and a chip per category, then the per-root chips
// (every root is listed; a list that would wrap folds to one line, T016i
// D1, RootsFold.tsx). While the code peek is open the strip is shown at
// every size without the per-root chips.

import {
  CATEGORY_LABEL,
  CATEGORY_TONE,
  TRANSPORT_LABEL,
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
import { barParts, isEmptyTransport, type CoverageActions } from "@/sections/api/coverage";
import { BAR_CLASS, StackedBar, TransportBadge } from "@/sections/api/parts";
import { scopeChip } from "@/components/precision/words";
import { FamilyChip } from "@/sections/api/precision";
import { RootChip, RootsToggle, groupLabel, useRootsFold } from "@/sections/api/RootsFold";

const ACCENT: Record<string, string> = { http: "bg-http-bar", tauri_ipc: "bg-ipc-bar", server_action: "bg-action-bar" };
const CHIP =
  "inline-flex h-[26px] items-center gap-1.5 whitespace-nowrap rounded-md border px-2 text-[12.5px] text-muted-foreground";

/** StripRoots is a transport's Roots row: one chip per root, folded to one
 * line when the chips would wrap (T016i D1). */
function StripRoots({ t, actions }: { t: TransportSummary; actions: CoverageActions }) {
  const { view, lineRef, labelRef, measuring, setExpanded } = useRootsFold(t, actions);
  const id = `strip-roots-list-${t.transport}`;
  const roots = view.kind === "folded" ? view.shown : t.roots;
  return (
    <div
      ref={lineRef}
      id={id}
      role="group"
      aria-label={groupLabel(t, view)}
      className="relative flex flex-wrap items-center gap-1.5 pb-2 pl-[15px] pr-2.5"
      data-testid={`strip-roots-${t.transport}`}
      data-fold={view.kind}
    >
      {measuring}
      <span ref={labelRef} className="text-xs text-muted-foreground">
        Roots
      </span>
      {roots.map((r) => (
        <RootChip key={r.root} t={t} r={r} actions={actions} />
      ))}
      <RootsToggle view={view} t={t} listId={id} setExpanded={setExpanded} />
    </div>
  );
}

function Row({ t, scope, actions, roots }: { t: TransportSummary; scope: string; actions: CoverageActions; roots: boolean }) {
  if (isEmptyTransport(t)) {
    return (
      <div className="flex min-h-10 items-center gap-3 border-b pr-2.5 last:border-b-0">
        <span aria-hidden="true" className={cn("w-[3px] self-stretch rounded-r-[2px]", ACCENT[t.transport])} />
        <TransportBadge transport={t.transport} />
        <span className="text-[12.5px] text-muted-foreground">
          No {TRANSPORT_LABEL[t.transport]} {endpointNoun(t.transport, 2)} or {callNoun(t.transport, 2)} in this repository
        </span>
      </div>
    );
  }
  // Project mode: the project's counts (T022 MR-UI); the repository's own
  // linked percentage is in the tooltip.
  const project = t.calls.repository_by_status !== null;
  const pct = linkedPercent(linkedCount(t.calls.by_status), t.calls.total);
  const ownPct = project ? linkedPercent(t.calls.repository_by_status?.linked ?? 0, t.calls.total) : null;
  return (
    <div className="border-b last:border-b-0" data-testid={`strip-${t.transport}`}>
      <div className="flex min-h-10 items-center gap-3 pr-2.5">
        <span aria-hidden="true" className={cn("w-[3px] self-stretch rounded-r-[2px]", ACCENT[t.transport])} />
        <span className="flex w-[168px] flex-none items-center gap-2 text-[12.5px] whitespace-nowrap">
          <TransportBadge transport={t.transport} />
          <span className="text-muted-foreground">
            {t.endpoint_count} {endpointNoun(t.transport, t.endpoint_count)}
          </span>
        </span>
        <StackedBar parts={barParts(t.transport, t.calls.by_status)} className="w-[120px] flex-none" />
        <span
          className="w-[84px] flex-none text-[12.5px] whitespace-nowrap"
          title={project ? `linked in the project, linked across repositories included; inside this repository alone ${ownPct === null ? "-" : `${String(ownPct)}%`}` : undefined}
          data-testid={`strip-pct-${t.transport}`}
        >
          <b className="text-sm tabular-nums">{pct === null ? "-" : `${String(pct)}%`}</b>{" "}
          <span className="text-muted-foreground">linked</span>
        </span>
        <div className="flex min-w-0 flex-wrap gap-1.5 py-1.5">
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
            const selected =
              actions.activeGap?.transport === t.transport && actions.activeGap.category === category;
            return (
              <button
                key={category}
                type="button"
                disabled={!clickable}
                data-cell={category}
                title={cell.kind === "category" ? cell.raws.map((r) => `${rawLabel(r.status)}: ${String(r.count)}`).join(", ") : NO_LINKED_CALLER_HINT}
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
                {label}
                {clickable ? " ›" : ""}
              </button>
            );
          })}
        </div>
        <span className="ml-auto text-xs whitespace-nowrap text-muted-foreground max-[1300px]:hidden">
          {t.calls.total} {callNoun(t.transport, t.calls.total)}
        </span>
      </div>
      {(scopeChip(t.precision).kind !== "none" || scopeChip(t.callers_precision).kind !== "none") && (
        // Their own line, so the category cells keep the row's width.
        <div className="flex flex-wrap items-center justify-end gap-1.5 pr-2.5 pb-1.5 pl-[15px]" data-testid={`strip-precision-${t.transport}`}>
          <FamilyChip transport={t.transport} which="endpoints" label={t.precision} />
          <FamilyChip transport={t.transport} which="calls" label={t.callers_precision} />
        </div>
      )}
      {roots && t.roots.length > 0 && <StripRoots t={t} actions={actions} />}
    </div>
  );
}

/** SummaryStrip is the compact coverage view. scope is the scope the
 * counts were read in (the endpoints answer's scope). */
export function SummaryStrip({
  transports,
  scope,
  actions,
  shown = false,
}: {
  transports: TransportSummary[];
  scope: string;
  actions: CoverageActions;
  /** Shown at any size (the code peek is open and the cards collapse).
   * The per-root rows are then left out, one line per transport, so the
   * callers list keeps room (T016e Q1); the toolbar's Source root select
   * still filters by root. */
  shown?: boolean;
}) {
  return (
    <div className={cn("hidden rounded-xl border bg-card shadow-[0_1px_2px_rgba(0,0,0,.04)] compact:block", shown && "block")} data-testid="summary-strip">
      {transports.map((t) => (
        <Row key={t.transport} t={t} scope={scope} actions={actions} roots={!shown} />
      ))}
    </div>
  );
}
