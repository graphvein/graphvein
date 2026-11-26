// The precision chip (T020, Variant A, ADR 0066): "<scope> · Precise" or
// "<scope> · Best effort" wherever the API serves a scope or file label.
// The chip is a button. Hover or focus shows a short tooltip; click or
// Enter opens the Evidence popover (a Radix Popover): the rule row's
// evidence or the reason for Best effort, the run's eligibility and the
// coverage table of this index from GET /api/v0/precision. Esc closes the
// popover and returns focus to the chip.
//
// The chip never decides a label: state comes from words.ts, which reads
// only what the server sent. No label from the server, no chip.

import { Popover } from "radix-ui";
import { useState, type ReactNode } from "react";

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { EvidencePanel } from "@/components/precision/EvidencePanel";
import { BestEffortIcon, PreciseIcon, type Focus } from "@/components/precision/parts";
import { bestEffortLead, PRECISE_TEXT, REPORTED_TEXT, reasonShort, reasonText, type ChipState } from "@/components/precision/words";

/** labelWord is the chip's label text. */
export function labelWord(state: ChipState, indexer: string | null): string {
  if (state.kind === "precise") return "Precise";
  if (state.kind === "best_effort") return "Best effort";
  if (state.kind === "reported") return `reported by ${indexer ?? "the external indexer"}: ${state.value}`;
  return "";
}

const CHIP = "inline-flex h-5 flex-none cursor-pointer items-center gap-[5px] rounded-[5px] border pr-[7px] pl-[5px] align-middle font-sans text-[11.5px] leading-none whitespace-nowrap outline-none hover:brightness-[.97] focus-visible:ring-2 focus-visible:ring-ring";

const TONE: Record<"precise" | "best_effort" | "reported", string> = {
  precise: "border-brand/35 bg-brand-soft text-brand-text",
  best_effort: "border-never-bar bg-transparent text-muted-foreground",
  reported: "border-dashed border-other-bar bg-transparent text-other",
};

/** ChipTip is the tooltip text: the label, its meaning, and how to get to
 * the evidence. */
function ChipTip({ state, scope, indexer }: { state: Exclude<ChipState, { kind: "none" }>; scope: string; indexer: string | null }) {
  const foot = (tail: string) => (
    <span className="mt-1.5 block border-t border-background/25 pt-1 opacity-90">
      Click or <kbd className="rounded-[3px] border border-background/40 px-[3px] font-mono text-[10.5px]">Enter</kbd> for evidence · {tail}
    </span>
  );
  if (state.kind === "reported") {
    return (
      <span className="block" data-testid="precision-tip">
        <b className="font-semibold">
          {scope}: {labelWord(state, indexer)}
        </b>
        <span className="mt-0.5 block">{REPORTED_TEXT}</span>
        {foot("not checked by Graphvein")}
      </span>
    );
  }
  if (state.kind === "precise") {
    return (
      <span className="block" data-testid="precision-tip">
        <b className="font-semibold">{scope} · Precise</b>
        <span className="mt-0.5 block">{PRECISE_TEXT}</span>
        {foot(`row ${state.row ?? "unknown"}`)}
      </span>
    );
  }
  const refused = state.reason !== null && state.reason !== "default_branch";
  return (
    <span className="block" data-testid="precision-tip">
      <b className="font-semibold">{scope} · Best effort</b>
      <span className="mt-0.5 block">{bestEffortLead(state.reason)}</span>
      {refused && <span className="mt-0.5 block">{reasonText(state.reason, state.side)}</span>}
      {foot(reasonShort(state.reason, state.side))}
    </span>
  );
}

export interface PrecisionChipProps {
  state: ChipState;
  /** The scope's name on the chip and in the popover ("Routes", "python"). */
  scope: string;
  /** What the popover's coverage table highlights. */
  focus?: Focus;
  /** The external indexer's name for a reported label. */
  indexer?: string | null;
  /** Show the label only (the scope is named next to the chip). */
  bare?: boolean;
  className?: string;
  testId?: string;
}

/** PrecisionChip renders one chip with its tooltip and Evidence popover,
 * or nothing when the server sent no label. */
export function PrecisionChip({ state, scope, focus = {}, indexer = null, bare = false, className, testId = "precision-chip" }: PrecisionChipProps) {
  const [open, setOpen] = useState(false);
  const [tip, setTip] = useState(false);
  if (state.kind === "none") return null;
  const word = labelWord(state, indexer);
  const icon: ReactNode = state.kind === "precise" ? <PreciseIcon /> : <BestEffortIcon dashed={state.kind === "reported"} />;
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      {/* Its own provider, so a chip also renders outside the shell. */}
      <TooltipProvider delayDuration={200}>
      <Tooltip open={tip && !open} onOpenChange={setTip}>
        <TooltipTrigger asChild>
          <Popover.Trigger asChild>
            <button
              type="button"
              aria-label={`${scope}: ${word}. Show ${state.kind === "precise" ? "evidence" : state.kind === "reported" ? "details" : "why"}`}
              className={cn(CHIP, TONE[state.kind], open && "ring-2 ring-brand/35", className)}
              data-testid={testId}
              data-label={state.kind}
              data-row={state.kind === "precise" || state.kind === "best_effort" ? (state.row ?? "") : ""}
              onClick={(e) => {
                // The chip sits in rows and panels that react to clicks.
                e.stopPropagation();
              }}
            >
              {icon}
              {!bare && (
                <>
                  <span className="font-normal">{scope}</span>
                  <span aria-hidden="true" className="opacity-55">
                    ·
                  </span>
                </>
              )}
              <span className={state.kind === "precise" ? "font-semibold" : "font-medium"}>{word}</span>
            </button>
          </Popover.Trigger>
        </TooltipTrigger>
        <TooltipContent side="bottom" align="start" sideOffset={6} collisionPadding={8} className="max-w-[340px] text-left text-xs leading-[1.45]">
          <ChipTip state={state} scope={scope} indexer={indexer} />
        </TooltipContent>
      </Tooltip>
      </TooltipProvider>
      <Popover.Portal>
        <Popover.Content
          side="bottom"
          align="start"
          sideOffset={6}
          collisionPadding={8}
          aria-label={`Precision evidence: ${scope}`}
          className="z-50 max-h-(--radix-popover-content-available-height) w-[min(840px,calc(100vw-32px))] overflow-auto rounded-xl border bg-popover text-[12.5px] text-popover-foreground shadow-[0_16px_40px_rgb(0_0_0/.22)] outline-none"
          data-testid="precision-popover"
          // React events bubble through the portal to the chip's
          // ancestors (a code peek closes on Escape, rows select on
          // click); the popover's own events stay inside it.
          onKeyDown={(e) => {
            e.stopPropagation();
          }}
          onClick={(e) => {
            e.stopPropagation();
          }}
        >
          <EvidencePanel state={state} scope={scope} focus={focus} indexer={indexer} onClose={() => { setOpen(false); }} />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
