// The request differences of a link (T029 W4, rows SD-8..SD-12 of
// docs/contract/claims/shape-differences.md): the keys a linked frontend
// call sends that the handler does not read, and the keys the handler
// requires that the call does not send, each with both sides' provenance
// chips. Shown inside the existing link details (the Flow tab's line
// detail, the Explorer operation's links area), and only when the server
// compared the link and found a difference: a link that was not compared,
// or that agrees, shows nothing here.

import type { IdeStatus } from "@/api/endpoints";
import { hasShapeDifferences, type DifferenceSide, type ShapeDifference, type ShapeDifferences } from "@/api/shapeDifferences";
import { FactChip, TierChip } from "@/components/precision/ProvChip";
import { cn } from "@/lib/utils";
import { ChipIdeButton, ChipLinksProvider } from "@/sections/explorer/chipLinks";
import { shortFile } from "@/sections/explorer/chips";

/** hasDifferences says whether a comparison has anything to show. */
export const hasDifferences = hasShapeDifferences;

/** SIDE_TEXT names how each side is known, as the chip says it. */
const SIDE_TEXT: Record<string, { text: string; title: string }> = {
  literal: { text: "Literal", title: "The object literal written at the call (its keys are what the call sends)" },
  typed: { text: "Typed client", title: "The type of the generated client's operation (a value of the type may carry more keys)" },
  fact: { text: "Fact", title: "A declaration the framework enforces at runtime" },
};

/** SideChip is one side's provenance chip, with the editor button its
 * side's links have for file:line. Its kind is the shared provenance chip
 * (T041): a fact side reads "Fact · 1.0" like every fact chip (a shape
 * item read from code has confidence 1, claim row SM-11); a literal or a
 * typed client is neither fact nor inferred, so it takes the neutral look. */
function SideChip({ side, who }: { side: DifferenceSide; who: string }) {
  const s = SIDE_TEXT[side.kind] ?? { text: side.kind, title: side.kind };
  return (
    <span
      title={`${who}: ${s.title}\n${side.file}:${String(side.line)}\nrule ${side.rule}`}
      data-testid="difference-chip"
      data-side={who}
      data-kind={side.kind}
      className="inline-flex h-5 max-w-full flex-none items-center gap-1.5 rounded-[5px] border border-border bg-background pr-1.5 pl-1.5 align-middle text-[11.5px] leading-none whitespace-nowrap text-foreground"
    >
      <span className="font-medium">{who}</span>
      {side.kind === "fact" ? <FactChip size="sm" testId="difference-kind" /> : <TierChip tier="other" label={{ text: s.text, title: "" }} size="sm" testId="difference-kind" />}
      <span className="min-w-0 truncate font-mono text-[11px] text-foreground/80">
        {shortFile(side.file)}:{side.line}
      </span>
      <ChipIdeButton file={side.file} line={side.line} />
    </span>
  );
}

function DifferenceRow({ d, diffs, ide }: { d: ShapeDifference; diffs: ShapeDifferences; ide: IdeStatus }) {
  return (
    <li className="grid gap-1" data-testid="difference" data-direction={d.direction} data-row={d.row}>
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span className="font-mono text-[12px] font-medium [overflow-wrap:anywhere]">
          {d.section === "query" ? `?${d.field}` : d.field}
        </span>
        <span className="text-[12px] text-muted-foreground">{d.text}</span>
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <ChipLinksProvider value={{ status: ide, links: diffs.callLinks }}>
          <SideChip side={d.call} who="Call" />
        </ChipLinksProvider>
        <ChipLinksProvider value={{ status: ide, links: diffs.routeLinks }}>
          <SideChip side={d.route} who="Handler" />
        </ChipLinksProvider>
      </div>
    </li>
  );
}

/** LinkDifferences lists one link's differences; nothing when there are
 * none to show. */
export function LinkDifferences({ diffs, ide, className }: { diffs: ShapeDifferences | null | undefined; ide: IdeStatus; className?: string }) {
  if (!hasDifferences(diffs)) return null;
  return (
    <div className={cn("grid gap-1.5", className)} data-testid="link-differences">
      <div className="flex flex-wrap items-center gap-2 text-[12px] font-semibold">
        Request differences
        {diffs.crossRepository && (
          <span className="rounded-[5px] border border-brand/30 bg-brand-soft px-1.5 text-[11px] font-normal text-brand-text" title="The call and the route are in different repositories of the project" data-testid="cross-repository">
            cross-repository link
          </span>
        )}
      </div>
      <ul className="grid gap-1.5">
        {diffs.differences.map((d) => (
          <DifferenceRow key={`${d.section}:${d.field}:${d.direction}`} d={d} diffs={diffs} ide={ide} />
        ))}
      </ul>
    </div>
  );
}
