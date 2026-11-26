// The top bar: breadcrumb, compare chip, freshness badge, "Check" (re-requests
// the status; it does not re-index), the Re-index slot (T016j variant D) and
// the extractor badge. Check stays enabled during a run (decision D7). The
// compare chip (T019 Revision 2) sits after the flexible gap, so its width
// changes move nothing else.
//
// The layout follows the top bar's own width (a container query on
// `top`), not the window's, because the sidebar's width changes with the
// view (T019b part 2, measured):
//   - 1300 px and wider: one row with every label;
//   - 980 to 1299 px: one row; the badge's time and Check's label move into
//     the tooltip and the accessible name, so the chip has room for its base;
//   - below 980 px (a side-by-side window): two rows of fixed height, the
//     breadcrumb and the status controls above, the chip and the extractor
//     badge below.
// Every part reserves its widest state, so no state change moves the bar.

import { RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import type { StatusResult } from "@/hooks/useStatus";
import type { Route } from "@/router";
import { CompareChip } from "@/shell/CompareChip";
import { useOptionalCompare } from "@/shell/compare";
import { ExtractorBadge } from "@/shell/ExtractorBadge";
import { FreshnessBadge } from "@/shell/FreshnessBadge";
import { ReindexControl } from "@/shell/ReindexControl";
import { useProject } from "@/shell/project";
import { CopyLinkButton } from "@/shell/workspace/CopyLink";
import { useWorkspace } from "@/shell/workspace/WorkspaceRoot";
import type { ReindexModel } from "@/shell/reindexModel";

/** Reindex is what the top bar needs for the Re-index slot. */
export interface ReindexProps {
  model: ReindexModel;
  start: () => void;
  dismiss: () => void;
}

/** TopBar renders the top bar for the current route and status result. */
export function TopBar({ route, result, reindex }: { route: Route; result: StatusResult; reindex?: ReindexProps }) {
  const { view, checking, repository, check } = result;
  const page = route.kind === "section" ? route.section.label : "Page not found";
  const compare = useOptionalCompare();
  const project = useProject();
  const workspace = useWorkspace();
  // The breadcrumb names the project and its member, or the repository
  // (from the workspace list while the index's first answer is awaited).
  const crumbs = project !== null ? [project.project.project, project.repository] : repository !== undefined ? [repository] : workspace !== null && workspace.entry !== null ? [workspace.entry.name] : [];
  return (
    <header className="@container/top flex-none border-b" data-testid="top-bar">
      <div className="flex h-[51px] items-center gap-2.5 px-5 max-[1400px]:px-4 @max-[979px]/top:h-[91px] @max-[979px]/top:flex-wrap @max-[979px]/top:content-center @max-[979px]/top:gap-y-2">
        <div className="flex min-w-0 shrink-[4] items-center gap-2 text-sm whitespace-nowrap text-muted-foreground @max-[979px]/top:flex-1" data-testid="top-crumb">
          {crumbs.map((c) => (
            <span key={c} className="contents">
              <span className="min-w-0 truncate" title={c}>
                {c}
              </span>
              <span aria-hidden="true">/</span>
            </span>
          ))}
          <span className="flex-none font-medium text-foreground">{page}</span>
          {route.kind === "section" && <CopyLinkButton entry={workspace?.entry?.name ?? project?.project.project ?? repository ?? null} section={page} />}
        </div>
        <div className="min-w-0 flex-1 @max-[979px]/top:hidden" />
        {compare !== null && (
          <div className="order-none flex min-w-[112px] shrink items-center @max-[979px]/top:order-2 @max-[979px]/top:min-w-0 @max-[979px]/top:flex-1" data-testid="top-chip-slot">
            <CompareChip />
          </div>
        )}
        <FreshnessBadge view={view} override={reindex?.model.badge ?? null} />
        <Button variant="outline" size="sm" onClick={check} aria-busy={checking} title="Check freshness now (does not re-index)" className="@max-[1299px]/top:px-2">
          <RefreshCw aria-hidden="true" className={checking ? "animate-spin" : undefined} />
          <span className="@max-[1299px]/top:sr-only">Check</span>
        </Button>
        {reindex !== undefined && (
          <ReindexControl model={reindex.model} status={view} onStart={reindex.start} onDismiss={reindex.dismiss} />
        )}
        {/* The row break of the two-row layout. */}
        <div aria-hidden="true" className="hidden @max-[979px]/top:order-1 @max-[979px]/top:block @max-[979px]/top:h-0 @max-[979px]/top:basis-full" />
        {view.kind === "ok" && (
          <>
            <Separator orientation="vertical" className="data-[orientation=vertical]:h-[18px] @max-[979px]/top:hidden" />
            <span className="flex-none @max-[979px]/top:order-3">
              <ExtractorBadge extractor={view.status.extractor} hasRun={view.status.index.last_run !== null} />
            </span>
          </>
        )}
      </div>
    </header>
  );
}
