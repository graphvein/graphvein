// The repository badge (T022 B9): names the member a node belongs to where
// a view mixes the members of a project (cross-repository callers, impact
// paths, the Project view). Single-repository views never show it.

import { FolderGit2 } from "lucide-react";

import { cn } from "@/lib/utils";

/** RepoBadge names a project member. */
export function RepoBadge({ repository, className }: { repository: string; className?: string }) {
  return (
    <span
      className={cn("inline-flex h-[18px] max-w-[16rem] flex-none items-center gap-1 rounded-[5px] border border-brand/30 bg-brand-soft px-1.5 text-[11.5px] font-medium text-brand-text", className)}
      title={`repository ${repository}`}
      data-testid="repo-badge"
      data-repository={repository}
    >
      <FolderGit2 aria-hidden="true" className="size-3 flex-none" />
      <span className="truncate">{repository}</span>
    </span>
  );
}

/** StaleLinkChip marks a stale project link; its reasons are the tooltip
 * and the accessible name, never colour alone. */
export function StaleLinkChip({ reasons }: { reasons: string[] }) {
  const why = reasons.length > 0 ? reasons.join("; ") : "the link pass is older than the members' indexes";
  return (
    <span className="inline-flex h-[18px] flex-none items-center rounded-[5px] border border-dynamic-bar px-1.5 text-[11.5px] font-medium text-dynamic" title={`stale: ${why}`} aria-label={`stale link: ${why}`} data-testid="stale-link">
      stale link
    </span>
  );
}
