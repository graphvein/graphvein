// The legend of the Flow tab. T016f D6 (UQ4 A) made the whole legend an
// always visible strip; T073 (T071 question 8, Proposed revision of ADR
// 0046) folds it: the strip below the canvas shows only the kinds drawn
// on this screen (they stay filter chips), "+ n kinds with no line here",
// the precision labels the route header does not already show, and a
// Legend button. The button opens the full legend over the bottom of the
// canvas: every tier the filter can dim as a toggle chip with a sample
// line and the number of drawn lines; "defined in", the other-target box,
// the wrapper pill and the changed dot explained without a chip; Dynamic
// and Unmatched, never attached to an endpoint, disabled at 0 with that
// reason and a link to the gap sheets; the notes and every precision
// label. The choice is not remembered: it is closed on every visit.
//
// A chip counts drawn lines, not callers: one caller can be drawn with
// several lines (a call with two stored targets), and the call tier counts
// calls the handler makes. The unit is in each chip's accessible name and
// tooltip ("N lines") and in the strip's note; the numbers are unchanged.

import { ChevronRight } from "lucide-react";
import { useId, useRef, useState, type KeyboardEvent } from "react";

import type { ScopeLabel, Transport } from "@/api/endpoints";
import { cn } from "@/lib/utils";
import { PrecisionChip } from "@/components/precision/PrecisionChip";
import { familyName, scopeChip } from "@/components/precision/words";
import { column, FamilyChip } from "@/sections/api/precision";
import { TIER_STYLE } from "@/sections/api/flow/FlowEdge";
import { FILTER_TIERS, type FilterTier, type Tier } from "@/sections/api/flow/tiers";
import { useLabelStyle } from "@/hooks/useLabels";
import type { LabelStyle } from "@/lib/labelChoice";
import { label, RANK_NOTE, type WordedTier } from "@/lib/labels";

/** UNWORDED is the legend text of the tiers T025 did not reword; the
 * fact, exact and suffix chips come from the label table (legendText). */
const UNWORDED: Record<Exclude<Tier, WordedTier>, { label: string; detail: string }> = {
  call: { label: "call", detail: "in-code" },
  access: { label: "reads / writes", detail: "stored table access; \"may\" when inferred" },
  contains: { label: "defined in", detail: "stored containment, not a call" },
  candidate: { label: "near miss", detail: "recorded candidate, no stored edge" },
  other: { label: "other", detail: "raw kind and confidence" },
};

/** legendText is a tier's chip label and explanation in a label style. */
function legendText(style: LabelStyle, t: Tier): { label: string; detail: string } {
  if (t === "fact" || t === "exact" || t === "suffix") {
    const l = label(style, "legendTier", t);
    return { label: l.text, detail: l.title };
  }
  return UNWORDED[t];
}

const CHIP_NAME: Record<FilterTier, string> = {
  fact: "Fact",
  exact: "Exact",
  suffix: "Suffix",
  call: "Call",
  access: "Reads / writes",
  candidate: "Near miss",
  other: "Other",
};

/** sentence ends a chip explanation with one full stop: a plain worded
 * tier's explanation already ends in the rank sentence (T041). */
function sentence(detail: string): string {
  return detail.endsWith(".") ? detail : `${detail}.`;
}

/** lines words a chip's count with its unit. */
export function lines(n: number): string {
  return `${String(n)} ${n === 1 ? "line" : "lines"}`;
}

/** Sample draws a short line in a tier's style. */
export function Sample({ tier }: { tier: Tier }) {
  const st = TIER_STYLE[tier];
  const c = `hsl(var(--${st.color}))`;
  return (
    <svg aria-hidden="true" width="40" height="14" className="flex-none">
      <line x1="2" y1="7" x2={st.arrow === "dot" ? 31 : 32} y2="7" stroke={c} strokeWidth={st.width} strokeDasharray={st.dash ?? undefined} />
      {st.arrow === "filled" && <path d="M31 3.5 L38 7 L31 10.5 Z" fill={c} />}
      {st.arrow === "open" && <path d="M31 3.5 L38 7 L31 10.5" fill="none" stroke={c} strokeWidth="1.5" />}
      {st.arrow === "dot" && <circle cx="35" cy="7" r="3" fill="none" stroke={c} strokeWidth="1.2" />}
    </svg>
  );
}

export interface LegendProps {
  /** T020: the route or command family's and the calls' labels from
   * /flow; absent or null: no chip. */
  transport?: Transport;
  precision?: ScopeLabel | null;
  callersPrecision?: ScopeLabel | null;
  /** T020 O-S1-5: the label of the code `calls` edges (the handler's call
   * chain), which the cards' file labels never cover. */
  codeCallsPrecision?: ScopeLabel | null;
  counts: Map<Tier, number>;
  tiers: ReadonlySet<FilterTier>;
  onToggle: (tier: FilterTier) => void;
  onGap: (category: "dynamic" | "unmatched") => void;
  /** Opens the full legend on the first render (tests). */
  defaultOpen?: boolean;
}

const CHIP = "inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-ring";

/** listedTiers are the filter tiers the full legend lists: every tier,
 * except "other" and "reads / writes" while nothing of them is drawn. */
function listedTiers(counts: Map<Tier, number>): FilterTier[] {
  return FILTER_TIERS.filter((t) => !((t === "other" || t === "access") && (counts.get(t) ?? 0) === 0));
}

/** hiddenKinds counts the kinds the strip leaves to the full legend: the
 * listed tiers with no line drawn, plus Dynamic and Unmatched, which are
 * never attached to an endpoint (always 0 here). */
export function hiddenKinds(counts: Map<Tier, number>): number {
  return listedTiers(counts).filter((t) => (counts.get(t) ?? 0) === 0).length + 2;
}

function TierChip({ t, n, on, onToggle, testId }: { t: FilterTier; n: number; on: boolean; onToggle: (tier: FilterTier) => void; testId: string }) {
  const style = useLabelStyle();
  const text = legendText(style, t);
  return (
    <button
      type="button"
      aria-pressed={on}
      title={`${text.label}: ${sentence(text.detail)} ${lines(n)} drawn. ${on ? "Click to dim these lines" : "Click to show these lines"}`}
      aria-label={`${CHIP_NAME[t]}: ${text.label}, ${lines(n)}`}
      data-testid={testId}
      data-count={n}
      className={cn(CHIP, on ? "bg-card" : "bg-muted text-muted-foreground line-through decoration-muted-foreground/60")}
      onClick={() => {
        onToggle(t);
      }}
    >
      <Sample tier={t} />
      <b className="font-medium text-foreground">{text.label}</b>
      <span className="rounded-[4px] bg-muted px-1 font-mono text-foreground">{n}</span>
    </button>
  );
}

function hasChip(...labels: (ScopeLabel | null)[]): boolean {
  return labels.some((l) => scopeChip(l).kind !== "none");
}

/** Legend renders the strip below the canvas and, behind its Legend
 * button, the full legend. */
export function Legend({ counts, tiers, onToggle, onGap, transport = "http", precision = null, callersPrecision = null, codeCallsPrecision = null, defaultOpen = false }: LegendProps) {
  const style = useLabelStyle();
  const textOf = (t: Tier) => legendText(style, t);
  const [open, setOpen] = useState(defaultOpen);
  const button = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  const listed = listedTiers(counts);
  const drawn = listed.filter((t) => (counts.get(t) ?? 0) > 0);
  const hidden = label(style, "legendHidden", hiddenKinds(counts));
  const close = (e: KeyboardEvent) => {
    if (e.key !== "Escape" || !open) return;
    e.preventDefault();
    e.stopPropagation();
    setOpen(false);
    button.current?.focus();
  };
  // Sticky: if the detail pane ever scrolls (a very short split), the
  // strip stays at its bottom edge; the full legend opens above it, over
  // the canvas, so opening it never lays the graph out again.
  return (
    <div className="@container/legend sticky bottom-0 z-[6] border-t bg-card text-xs" data-testid="flow-legend" data-open={open} onKeyDown={close}>
      {open && (
        <div id={panelId} role="region" aria-label="Flow legend" className="absolute inset-x-0 bottom-full grid max-h-[min(60vh,420px)] gap-1.5 overflow-auto border-t bg-card px-4 py-2 shadow-[0_-4px_12px_rgba(0,0,0,.08)]" data-testid="flow-legend-full">
          <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1">
            <span className="mr-1 text-[13px] font-semibold">Every line kind</span>
            <span className="contents" role="group" aria-label="Confidence filter, every kind">
              {listed.map((t) => (
                <TierChip key={t} t={t} n={counts.get(t) ?? 0} on={tiers.has(t)} onToggle={onToggle} testId={`legend-chip-${t}`} />
              ))}
            </span>
            {(["dynamic", "unmatched"] as const).map((cat) => (
              <span key={cat} className={cn(CHIP, "border-dashed text-muted-foreground")} data-testid={`chip-${cat}`}>
                <button type="button" disabled className="cursor-not-allowed" title="Never attached to an endpoint">
                  {cat === "dynamic" ? "Dynamic" : "Unmatched"} <span className="font-mono">0</span>
                </button>
                <span>never attached to an endpoint ·</span>
                <button
                  type="button"
                  className="font-medium text-brand-text underline underline-offset-[3px]"
                  onClick={() => {
                    onGap(cat);
                  }}
                >
                  gap sheet
                </button>
              </span>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-muted-foreground">
            <span className="inline-flex items-center gap-1.5" title={textOf("contains").detail}>
              <Sample tier="contains" />
              <b className="font-medium text-foreground">{textOf("contains").label}</b> not a call
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span aria-hidden="true" className="h-3.5 w-6 rounded-[3px] border border-dashed border-muted-foreground" />
              other target
            </span>
            <span className="inline-flex items-center gap-1.5" title="The call goes through a wrapper function">
              <span aria-hidden="true" className="h-3.5 w-6 rounded-full border border-dashed border-wrapper/70 bg-wrapper-soft" />
              via wrapper()
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span aria-hidden="true" className="size-[9px] rounded-full bg-stale" />
              file changed
            </span>
          </div>
          <div className="text-muted-foreground">
            Counts are lines, not callers. Dimmed lines are filtered, not removed.{" "}
            <span data-testid="flow-legend-rank" title={RANK_NOTE}>
              A number is a fixed rank per match rule, not a probability.
            </span>
          </div>
          {/* An external run's labels show no chip (O6 a): then no heading
              either, as the summary strip does. */}
          {hasChip(precision, callersPrecision, codeCallsPrecision) && (
            <span className="flex flex-wrap items-center gap-1.5" data-testid="flow-legend-precision-all">
              <b className="text-[13px] font-semibold">Precision</b>
              <FamilyChip transport={transport} which="endpoints" label={precision} testId="flow-precision" />
              <FamilyChip transport={transport} which="calls" label={callersPrecision} testId="legend-callers-precision" />
              <PrecisionChip state={scopeChip(codeCallsPrecision)} scope={familyName("code_calls")} focus={{ column: column(codeCallsPrecision, "code_calls") }} testId="legend-code-calls-precision" />
              <span className="text-muted-foreground">other cards: their file&apos;s label, in the code peek</span>
            </span>
          )}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1 px-4 py-1.5" data-testid="flow-legend-strip">
        <span className="mr-1 text-[13px] font-semibold">Confidence</span>
        <span className="contents" role="group" aria-label="Confidence filter">
          {drawn.map((t) => (
            <TierChip key={t} t={t} n={counts.get(t) ?? 0} on={tiers.has(t)} onToggle={onToggle} testId={`chip-${t}`} />
          ))}
        </span>
        <span className="ml-1 whitespace-nowrap text-muted-foreground" title={hidden.title} data-testid="flow-legend-hidden">
          {hidden.text}
        </span>
        {/* The route's own label is in the header above; the calls' labels
            stay here while the strip is at least 1000 px wide, else they
            are in the full legend only. */}
        {hasChip(callersPrecision, codeCallsPrecision) && (
          <span className="ml-3 inline-flex flex-wrap items-center gap-1.5 @max-[1000px]/legend:hidden" data-testid="flow-legend-precision">
            <b className="text-[13px] font-semibold">Precision</b>
            <FamilyChip transport={transport} which="calls" label={callersPrecision} testId="flow-callers-precision" />
            <PrecisionChip state={scopeChip(codeCallsPrecision)} scope={familyName("code_calls")} focus={{ column: column(codeCallsPrecision, "code_calls") }} testId="flow-code-calls-precision" />
          </span>
        )}
        <button
          ref={button}
          type="button"
          aria-expanded={open}
          aria-controls={open ? panelId : undefined}
          title="Every line kind with its sample and count, the boxes and marks, the gap sheets and the precision labels"
          className="ml-auto inline-flex h-7 items-center gap-0.5 rounded-md border px-2 text-[12.5px] font-medium whitespace-nowrap outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
          data-testid="flow-legend-toggle"
          onClick={() => {
            setOpen((o) => !o);
          }}
        >
          Legend <ChevronRight aria-hidden="true" className={cn("size-3.5 transition-transform", open && "-rotate-90")} />
        </button>
      </div>
    </div>
  );
}
