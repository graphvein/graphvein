// Shared parts of the precision chip and its Evidence popover (T020): the
// /precision loader context, the popover's focus and the two label icons.

import { createContext, type ReactNode } from "react";

import { httpPrecisionLoader, type PrecisionLoader } from "@/api/precision";
import { cn } from "@/lib/utils";

/** PrecisionLoaderContext supplies the /precision request; tests replace
 * it. */
export const PrecisionLoaderContext = createContext<PrecisionLoader>(httpPrecisionLoader);

/** Focus is what the popover's coverage table highlights: a column (a
 * rule row id or a default family) and a language row. */
export interface Focus {
  column?: string | null;
  language?: string | null;
}

/** PreciseIcon is the filled check of a Precise label. */
export function PreciseIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 12 12" aria-hidden="true" className={cn("size-3 flex-none", className)}>
      <circle cx="6" cy="6" r="6" fill="currentColor" />
      <path d="M3.4 6.2l1.7 1.7 3.5-3.6" fill="none" stroke="var(--color-brand-soft)" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

/** BestEffortIcon is the open ring of a Best effort label; dashed for a
 * label an external indexer reported. */
export function BestEffortIcon({ dashed = false, className }: { dashed?: boolean; className?: string }) {
  return (
    <svg viewBox="0 0 12 12" aria-hidden="true" className={cn("size-3 flex-none", className)}>
      <circle cx="6" cy="6" r="4.6" fill="none" stroke="currentColor" strokeWidth="1.4" {...(dashed ? { strokeDasharray: "2 1.6" } : {})} />
    </svg>
  );
}

/** LabelTag is a static (not interactive) label in the popover. */
export function LabelTag({ precise, children }: { precise: boolean; children: ReactNode }) {
  return (
    <span
      className={cn(
        "inline-flex h-[18px] items-center gap-1 rounded-[5px] border pr-1.5 pl-1 text-[11px] whitespace-nowrap",
        precise ? "border-brand/35 bg-brand-soft font-semibold text-brand-text" : "border-never-bar font-medium text-muted-foreground",
      )}
    >
      {precise ? <PreciseIcon className="size-[11px]" /> : <BestEffortIcon className="size-[11px]" />}
      {children}
    </span>
  );
}

