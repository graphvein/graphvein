// The extractor version badge. A mismatch between the binary's extractors
// and the ones the index was built with does not change the freshness state
// (that stays content-based, like graphvein status); it is shown here as a
// warning with "re-index recommended" (task T016c, Decisions Q1).

import { TriangleAlert } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { Extractor } from "@/api/status";
import { cn } from "@/lib/utils";

/**
 * extractorNote is the tooltip text for an extractor comparison. hasRun is
 * false when the store has no index run: matches is null then too, but
 * because there is nothing to compare with, not because another indexer
 * built the index. Without a run, matches true or false contradicts the
 * data (there is no indexed version to compare with), so no comparison is
 * claimed: hasRun is checked first.
 */
export function extractorNote(extractor: Extractor, hasRun: boolean): string {
  if (!hasRun) return "Extractor version of this binary. No index run to compare with.";
  if (extractor.matches === false) {
    return `Indexed with ${extractor.indexed ?? "another version"}; this binary has ${extractor.current}. Run graphvein index.`;
  }
  if (extractor.matches === true) return "Extractor version of this binary; the index was built with the same version.";
  return "Extractor version of this binary. The index was not built by these extractors, so versions are not compared.";
}

interface ExtractorBadgeProps {
  extractor: Extractor;
  /** Whether the store has an index run (status index.last_run not null). */
  hasRun: boolean;
}

/**
 * ExtractorBadge shows this binary's extractor version in the top bar and,
 * when the index was built with another version, "re-index recommended".
 * It is focusable so keyboard users can open the comparison tooltip.
 */
export function ExtractorBadge({ extractor, hasRun }: ExtractorBadgeProps) {
  // Without a run there is nothing to re-index against (see extractorNote).
  const mismatch = hasRun && extractor.matches === false;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge
          variant="outline"
          tabIndex={0}
          className={cn(
            "h-[22px] rounded-md px-[7px] font-mono text-[11.5px] font-medium",
            mismatch && "border-stale bg-stale/15 text-foreground",
          )}
        >
          {mismatch && <TriangleAlert aria-hidden="true" />}
          {extractor.current}
          {mismatch && <span className="font-sans">re-index recommended</span>}
        </Badge>
      </TooltipTrigger>
      <TooltipContent side="bottom" align="end" sideOffset={6} className="max-w-xs">
        {extractorNote(extractor, hasRun)}
      </TooltipContent>
    </Tooltip>
  );
}
