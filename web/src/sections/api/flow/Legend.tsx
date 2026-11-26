// The legend strip of the Flow tab, always visible (T016f D6, UQ4 A). The
// tiers the filter can dim are toggle chips with a sample line and the
// number of drawn lines; "defined in", the other-target box, the wrapper
// pill and the changed dot are explained without a chip. Dynamic and
// unmatched calls are never attached to an endpoint, so their chips are
// disabled at 0 with that reason and a link to the gap sheets.
//
// A chip counts drawn lines, not callers: one caller can be drawn with
// several lines (a call with two stored targets), and the call tier counts
// calls the handler makes. The unit is in each chip's accessible name and
// tooltip ("N lines") and in the strip's note; the numbers are unchanged.

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
}

/** Legend renders the strip below the canvas: the filter chips (label,
 * sample, count; the meaning in the tooltip and in words for the
 * structural entries) in as few rows as the width allows. */
export function Legend({ counts, tiers, onToggle, onGap, transport = "http", precision = null, callersPrecision = null, codeCallsPrecision = null }: LegendProps) {
  const style = useLabelStyle();
  const textOf = (t: Tier) => legendText(style, t);
  const chip = "inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-ring";
  // Sticky: if the detail pane ever scrolls (a very short split), the
  // legend stays at its bottom edge.
  return (
    <div className="sticky bottom-0 z-[6] flex flex-wrap items-center gap-x-1.5 gap-y-1 border-t bg-card px-4 py-1.5 text-xs" data-testid="flow-legend">
      <span className="mr-1 text-[13px] font-semibold">Confidence</span>
      <span className="contents" role="group" aria-label="Confidence filter">
        {FILTER_TIERS.map((t) => {
          const n = counts.get(t) ?? 0;
          if ((t === "other" || t === "access") && n === 0) return null;
          const on = tiers.has(t);
          return (
            <button
              key={t}
              type="button"
              aria-pressed={on}
              title={`${textOf(t).label}: ${sentence(textOf(t).detail)} ${lines(n)} drawn. ${on ? "Click to dim these lines" : "Click to show these lines"}`}
              aria-label={`${CHIP_NAME[t]}: ${textOf(t).label}, ${lines(n)}`}
              data-testid={`chip-${t}`}
              data-count={n}
              className={cn(chip, on ? "bg-card" : "bg-muted text-muted-foreground line-through decoration-muted-foreground/60")}
              onClick={() => {
                onToggle(t);
              }}
            >
              <Sample tier={t} />
              <b className="font-medium text-foreground">{textOf(t).label}</b>
              <span className="rounded-[4px] bg-muted px-1 font-mono text-foreground">{n}</span>
            </button>
          );
        })}
      </span>
      {(["dynamic", "unmatched"] as const).map((cat) => (
        <span key={cat} className={cn(chip, "border-dashed text-muted-foreground")} data-testid={`chip-${cat}`}>
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
      <span className="ml-2 inline-flex items-center gap-1.5 text-muted-foreground" title={textOf("contains").detail}>
        <Sample tier="contains" />
        <b className="font-medium text-foreground">{textOf("contains").label}</b> not a call
      </span>
      <span className="inline-flex items-center gap-1.5 text-muted-foreground">
        <span aria-hidden="true" className="h-3.5 w-6 rounded-[3px] border border-dashed border-muted-foreground" />
        other target
      </span>
      <span className="inline-flex items-center gap-1.5 text-muted-foreground" title="The call goes through a wrapper function">
        <span aria-hidden="true" className="h-3.5 w-6 rounded-full border border-dashed border-wrapper/70 bg-wrapper-soft" />
        via wrapper()
      </span>
      <span className="inline-flex items-center gap-1.5 text-muted-foreground">
        <span aria-hidden="true" className="size-[9px] rounded-full bg-stale" />
        file changed
      </span>
      <span className="text-muted-foreground">· counts are lines, not callers · dimmed lines are filtered, not removed</span>
      <span className="text-muted-foreground" data-testid="flow-legend-rank" title={RANK_NOTE}>
        · a number is a fixed rank per match rule, not a probability
      </span>
      {/* An external run's labels show no chip (O6 a): then no heading
          either, as the summary strip does. */}
      {(scopeChip(precision).kind !== "none" || scopeChip(callersPrecision).kind !== "none" || scopeChip(codeCallsPrecision).kind !== "none") && (
        <span className="inline-flex flex-wrap items-center gap-1.5" data-testid="flow-legend-precision">
          <b className="ml-1 text-[13px] font-semibold">Precision</b>
          <FamilyChip transport={transport} which="endpoints" label={precision} testId="flow-precision" />
          <FamilyChip transport={transport} which="calls" label={callersPrecision} testId="flow-callers-precision" />
          <PrecisionChip state={scopeChip(codeCallsPrecision)} scope={familyName("code_calls")} focus={{ column: column(codeCallsPrecision, "code_calls") }} testId="flow-code-calls-precision" />
          <span className="text-muted-foreground">other cards: their file&apos;s label, in the code peek</span>
        </span>
      )}
    </div>
  );
}
