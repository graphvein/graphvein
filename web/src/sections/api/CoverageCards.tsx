// The coverage cards (M5 .covc), behind the strips' "Coverage and roots"
// disclosure (T065): per transport the
// endpoint count, linked percentage, stacked bar, one stat cell per
// category (the M5 five always, further categories when not zero) and the
// per-source-root breakdown. Non-linked cells with calls open the gap
// sheet; the linked cell is static.

import { ArrowRight } from "lucide-react";

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
  POSSIBLY_CALLED,
  rawLabel,
  type Category,
  type CoverageCell,
} from "@/api/categories";
import type { TransportSummary } from "@/api/endpoints";
import { cn } from "@/lib/utils";
import {
  barParts,
  noneFound,
  manifestTitle,
  rootLabel,
  rootCategoryCounts,
  type CoverageActions,
} from "@/sections/api/coverage";
import { BADGE, BAR_CLASS, CATEGORY_ICON, StackedBar, TransportBadge } from "@/sections/api/parts";
import { FamilyChip } from "@/sections/api/precision";
import { RootChip, RootsToggle, groupLabel, useRootsFold } from "@/sections/api/RootsFold";

const TOP: Record<string, string> = { http: "border-t-http-bar", tauri_ipc: "border-t-ipc-bar", server_action: "border-t-action-bar" };

function cellTitle(cell: CoverageCell): string {
  if (cell.kind === "never") return `Endpoints with no stored caller. ${NO_LINKED_CALLER_HINT}`;
  return cell.raws.length === 0
    ? `Status: ${cell.category}`
    : `Raw statuses: ${cell.raws.map((r) => `${rawLabel(r.status)}: ${String(r.count)}`).join(", ")}`;
}

function StatCell({ t, cell, scope, actions }: { t: TransportSummary; cell: CoverageCell; scope: string; actions: CoverageActions }) {
  const category = cell.kind === "never" ? "never" : cell.category;
  const tone = cell.kind === "never" ? "never" : CATEGORY_TONE[cell.category];
  const never = neverScoped(t);
  const label = cell.kind === "never" ? neverCalledLabel(scope, undefined, never.project) : categoryLabel(cell.category, t.calls.repository_by_status !== null);
  const clickable = category !== "linked" && cell.count > 0;
  const selected =
    actions.activeGap !== null && actions.activeGap.transport === t.transport && actions.activeGap.category === category;
  const Icon = CATEGORY_ICON[category];
  return (
    <button
      type="button"
      disabled={!clickable}
      title={cellTitle(cell)}
      data-cell={category}
      aria-label={`${String(cell.count)} ${label}${clickable ? ", show the list" : ""}`}
      className={cn(
        "grid min-w-0 grid-cols-[minmax(0,1fr)] content-between border-r px-2.5 py-1.5 text-left last:border-r-0",
        clickable ? "cursor-pointer hover:bg-accent" : "cursor-default",
        cell.count === 0 && "opacity-75",
        selected && "bg-brand-soft shadow-[inset_0_-2px_0_hsl(var(--brand))]",
      )}
      onClick={() => {
        if (clickable && cell.kind === "never") actions.openGap(t.transport, "never");
        else if (clickable && cell.kind === "category" && cell.category !== "linked") actions.openGap(t.transport, cell.category);
      }}
    >
      <span className={cn("flex items-center gap-1.5 text-lg font-semibold tracking-tight tabular-nums", cell.count === 0 && "text-muted-foreground")}>
        <i aria-hidden="true" className={cn("size-2 rounded-[2px]", BAR_CLASS[tone])} />
        {cell.count}
      </span>
      <span className="flex min-w-0 items-center justify-between gap-1 text-xs text-muted-foreground">
        {/* The label wraps instead of overflowing into the next cell: with
            seven cells "No linked caller in production" is wider than one. */}
        <span className="flex min-w-0 items-center gap-1">
          {cell.kind === "category" && cell.category !== "linked" && !["unmatched", "dynamic", "wrapper"].includes(cell.category) ? (
            <Icon aria-hidden="true" className="size-3 shrink-0" />
          ) : null}
          <span className="min-w-0">{label}</span>
        </span>
        {clickable ? <ArrowRight aria-hidden="true" className="size-3 shrink-0" /> : null}
      </span>
    </button>
  );
}

/** categoryLabel is a cell's label; in project scope "Unmatched" says it
 * is the project's (T022 MR-UI). */
function categoryLabel(category: Category, project: boolean): string {
  return project && category === "unmatched" ? "Unmatched in the project" : CATEGORY_LABEL[category];
}

/** RepositoryCounts is the secondary line of a card in project mode: the
 * repository's own linked percentage and status counts. */
function RepositoryCounts({ t, repository }: { t: TransportSummary; repository: string }) {
  const own = t.calls.repository_by_status;
  if (own === null) return null;
  const pct = linkedPercent(own.linked ?? 0, t.calls.total);
  const parts = coverageCells(t.transport, own, 0)
    .filter((c): c is Extract<CoverageCell, { kind: "category" }> => c.kind === "category" && c.count > 0)
    .map((c) => `${String(c.count)} ${CATEGORY_LABEL[c.category].toLowerCase()}`);
  return (
    <p className="m-0 text-[12.5px] text-muted-foreground" data-testid={`repository-counts-${t.transport}`}>
      Inside {repository} alone: {pct === null ? "-" : `${String(pct)}%`} linked{parts.length > 0 ? ` · ${parts.join(" · ")}` : ""}. The counts above are the project&apos;s: a call linked to a route of another repository counts as linked across repositories.{" "}
      <a href="#/project" className="font-medium text-brand-text underline decoration-brand-text/35 underline-offset-[3px]">
        Project view
      </a>
    </p>
  );
}

/** RootRows is today's per-root breakdown: one grid row per root with a
 * small bar, linked % and clickable counts. */
function RootRows({ t, actions }: { t: TransportSummary; actions: CoverageActions }) {
  return (
    <div className="grid grid-cols-[minmax(0,max-content)_minmax(40px,140px)_40px_minmax(0,1fr)] items-center gap-x-2.5 gap-y-0.5 text-xs">
      {t.roots.map((r) => {
        const pct = linkedPercent(linkedCount(r.by_status), r.total);
        const on = actions.root === r.root;
        return (
          <div
            key={r.root}
            data-root={r.root}
            className="contents"
          >
            <button
              type="button"
              title={`${manifestTitle(r)}. ${on ? "Show all roots" : "Filter by this root"}`}
              aria-pressed={on}
              className={cn("min-w-0 truncate rounded-md px-1.5 py-0.5 text-left font-mono text-[12px] hover:underline", on && "bg-brand-soft ring-1 ring-brand")}
              onClick={() => {
                actions.setRoot(on ? null : r.root);
              }}
            >
              {rootLabel(r.root)}
            </button>
            <StackedBar parts={barParts(t.transport, r.by_status)} className="h-1" />
            <span className="text-right tabular-nums">
              <b className="font-semibold">{pct === null ? "-" : `${String(pct)}%`}</b>
            </span>
            <span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
              <span className="text-muted-foreground tabular-nums">{r.total} total</span>
              {rootCategoryCounts(t.transport, r).map((c) => {
                const tone = CATEGORY_TONE[c.category];
                const click = c.category !== "linked";
                const name = categoryLabel(c.category, r.repository_by_status !== null).toLowerCase();
                const body = (
                  <>
                    <i aria-hidden="true" className={cn("size-[7px] rounded-[2px]", BAR_CLASS[tone])} />
                    <b className="font-semibold text-foreground tabular-nums">{c.count}</b>
                    {name}
                  </>
                );
                return click ? (
                  <button
                    key={c.category}
                    type="button"
                    title={`Show these calls (root ${r.root}; ${c.raws.map(rawLabel).join(", ")})`}
                    className="inline-flex items-center gap-1 rounded px-0.5 text-muted-foreground hover:bg-accent"
                    onClick={() => {
                      if (c.category !== "linked") actions.openGap(t.transport, c.category, r.root);
                    }}
                  >
                    {body}
                  </button>
                ) : (
                  <span key={c.category} className="inline-flex items-center gap-1 text-muted-foreground">
                    {body}
                  </span>
                );
              })}
            </span>
          </div>
        );
      })}
    </div>
  );
}

/** RootLines lists every source root of a transport (none hidden or
 * merged). When the strip's root chips would not fit on one line, the
 * list folds to one line of them (T016i D1, DD1 A): the largest roots,
 * the active root and "+N more roots", which unfolds today's rows. The
 * caption stays above in every state. */
export function RootLines({ t, actions }: { t: TransportSummary; actions: CoverageActions }) {
  const { view, lineRef, measuring, setExpanded } = useRootsFold(t, actions);
  if (t.roots.length === 0) return null;
  const id = `card-roots-list-${t.transport}`;
  const fits = view.kind === "fits";
  return (
    <div
      ref={lineRef}
      id={id}
      role="group"
      aria-label={groupLabel(t, view)}
      className={cn("relative", fits ? "grid gap-0.5" : "flex flex-wrap items-center gap-x-1.5 gap-y-1")}
      data-testid={`roots-${t.transport}`}
      data-fold={view.kind}
    >
      {measuring}
      <div className={cn("text-xs font-medium text-muted-foreground", !fits && "basis-full")}>By source root of the calling site</div>
      {view.kind === "folded" ? view.shown.map((r) => <RootChip key={r.root} t={t} r={r} actions={actions} />) : null}
      {view.kind !== "folded" && (fits ? <RootRows t={t} actions={actions} /> : <div className="basis-full"><RootRows t={t} actions={actions} /></div>)}
      <RootsToggle view={view} t={t} listId={id} setExpanded={setExpanded} />
    </div>
  );
}

function Card({ t, repository, scope, actions }: { t: TransportSummary; repository: string; scope: string; actions: CoverageActions }) {
  // Project mode: the project's counts (T022 MR-UI), linked across
  // repositories included; the repository's own are the secondary line.
  const project = t.calls.repository_by_status !== null;
  const linked = linkedCount(t.calls.by_status);
  const pct = linkedPercent(linked, t.calls.total);
  const cells = coverageCells(t.transport, t.calls.by_status, neverScoped(t).count, project);
  return (
    <section
      aria-label={`${TRANSPORT_LABEL[t.transport]} coverage`}
      data-testid={`card-${t.transport}`}
      className={cn("grid content-start gap-2.5 rounded-xl border border-t-[3px] bg-card px-3.5 pt-3 pb-3.5 shadow-[0_1px_2px_rgba(0,0,0,.04)]", TOP[t.transport])}
    >
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <TransportBadge transport={t.transport} />
        <span className={cn(BADGE, "bg-muted font-medium")}>
          {t.endpoint_count} {endpointNoun(t.transport, t.endpoint_count)}
        </span>
        <span className={cn(BADGE, "border-border font-medium")}>{repository}</span>
        {t.frameworks.map((f) => (
          <span key={f} className={cn(BADGE, "border-border font-medium")}>
            {f}
          </span>
        ))}
        <FamilyChip transport={t.transport} which="endpoints" label={t.precision} />
        <FamilyChip transport={t.transport} which="calls" label={t.callers_precision} />
        <span className="ml-auto text-[13px] whitespace-nowrap text-muted-foreground">
          <b className="text-[15px] text-foreground tabular-nums" data-testid={`pct-${t.transport}`}>
            {pct === null ? "-" : `${String(pct)}%`}
          </b>{" "}
          linked{project ? " in the project" : ""} · {t.calls.total} {callNoun(t.transport, t.calls.total)}
        </span>
      </div>
      <StackedBar parts={barParts(t.transport, t.calls.by_status)} />
      <div
        className="grid overflow-hidden rounded-lg border"
        style={{ gridTemplateColumns: `repeat(${String(cells.length)}, minmax(0, 1fr))` }}
      >
        {cells.map((cell) => (
          <StatCell key={cell.kind === "never" ? "never" : cell.category} t={t} cell={cell} scope={scope} actions={actions} />
        ))}
      </div>
      {t.never_called_in_project !== null && (
        <p className="m-0 text-[12.5px] text-muted-foreground" data-testid={`project-never-${t.transport}`}>
          {t.never_called} without a caller inside {repository}
          {t.never_called > t.never_called_in_project + (t.possibly_called_in_project ?? 0)
            ? ` (${String(t.never_called - t.never_called_in_project - (t.possibly_called_in_project ?? 0))} of them called from other repositories of the project)`
            : ""}
          .
        </p>
      )}
      {(t.possibly_called_in_project ?? 0) > 0 && (
        <p className="m-0 text-[12.5px] text-muted-foreground" data-testid={`project-possibly-${t.transport}`}>
          {t.possibly_called_in_project} {POSSIBLY_CALLED}: no caller or link, but calls of other repositories may reach {t.possibly_called_in_project === 1 ? "it" : "them"}; not counted as not called.
        </p>
      )}
      <RepositoryCounts t={t} repository={repository} />
      <RootLines t={t} actions={actions} />
    </section>
  );
}

/** CoverageCards renders one card per shown transport with endpoints or
 * calls, behind the strips' "Coverage and roots" disclosure (T065). A
 * transport with neither has no card: the toolbar says "none found". */
export function CoverageCards({
  transports,
  repository,
  scope,
  actions,
  collapsed = false,
  id,
}: {
  transports: TransportSummary[];
  repository: string;
  /** The scope the counts were read in (the endpoints answer's scope). */
  scope: string;
  actions: CoverageActions;
  /** Hidden while the code peek is open, so the callers keep room. */
  collapsed?: boolean;
  /** The id the disclosure button controls. */
  id?: string;
}) {
  const full = transports.filter((t) => !noneFound(t));
  if (full.length === 0) return null;
  return (
    <div id={id} className={cn("grid flex-none gap-2", collapsed && "hidden")} data-testid="coverage-cards">
      <div className={cn("grid gap-3.5", full.length > 1 ? "grid-cols-2 max-[1100px]:grid-cols-1" : "grid-cols-1")}>
        {full.map((t) => (
          <Card key={t.transport} t={t} repository={repository} scope={scope} actions={actions} />
        ))}
      </div>
    </div>
  );
}
