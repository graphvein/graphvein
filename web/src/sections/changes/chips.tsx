// The chips every change view shares (V3 mockup, shared.css): proven
// changes are solid chips with a sign (+ added, - no longer in the index,
// ~ changed); knowledge changes are dashed outline chips (? unknown now,
// ! known now); not extracted is a dotted grey chip and never a zero;
// "edited (history rewritten)" is red with a border; causes are code
// (solid), relink (outline), extractor or relink (dashed).

import type { ReactNode } from "react";

import { cn } from "@/lib/utils";
import type { Label } from "@/lib/labels";
import { CAUSE_LABEL, CAUSE_TITLE, changeLabel, itemLabel, type RouteTitle } from "@/sections/changes/words";

const CHIP = "inline-flex h-5 flex-none items-center gap-1 whitespace-nowrap rounded-[5px] border border-transparent px-1.5 text-[11.5px] font-medium";

/** CHANGE_STYLE maps a change kind to its chip classes and sign. */
const CHANGE_STYLE: Record<string, { cls: string; sign: string }> = {
  added: { cls: "bg-linked-soft text-linked", sign: "+" },
  removed: { cls: "bg-unmatched-soft text-unmatched", sign: "−" },
  changed: { cls: "bg-brand-soft text-brand-text", sign: "~" },
  body_changed: { cls: "bg-brand-soft text-brand-text", sign: "~" },
  unknown_now: { cls: "border-dashed border-dynamic-bar bg-transparent text-dynamic", sign: "?" },
  known_now: { cls: "border-dashed border-linked-bar bg-transparent text-linked", sign: "!" },
  link_lost: { cls: "bg-unmatched-soft text-unmatched", sign: "≠" },
  link_gained: { cls: "bg-linked-soft text-linked", sign: "=" },
  link_unknown_now: { cls: "border-dashed border-dynamic-bar bg-transparent text-dynamic", sign: "?" },
  became_partial: { cls: "bg-dynamic-soft text-dynamic", sign: "!" },
  recovered: { cls: "bg-linked-soft text-linked", sign: "✓" },
  unclassified: { cls: "bg-other-soft text-other", sign: "·" },
};

/** changeStyle is exported for tests: an unknown kind gets the neutral
 * style and no sign, never a known kind's. */
export function changeStyle(change: string): { cls: string; sign: string } {
  return CHANGE_STYLE[change] ?? { cls: "border-border bg-muted text-foreground", sign: "" };
}

/** ChangeChip shows a change kind; label overrides the text (counts,
 * "deleted"). */
export function ChangeChip({ change, label, className }: { change: string; label?: string; className?: string }) {
  const st = changeStyle(change);
  return (
    <span className={cn(CHIP, st.cls, className)} data-change={change} data-testid="change-chip">
      {st.sign !== "" && (
        <span aria-hidden="true" className="font-mono text-[11px] font-bold">
          {st.sign}
        </span>
      )}
      {label ?? changeLabel(change)}
    </span>
  );
}

/** NotExtractedChip is the dotted chip of a group or relation the index
 * does not extract. */
export function NotExtractedChip({ children = "not extracted", wrap = false }: { children?: ReactNode; wrap?: boolean }) {
  return (
    <span className={cn(CHIP, "border-dotted border-never-bar bg-transparent text-never italic", wrap && "h-auto min-h-5 min-w-0 shrink whitespace-normal")} data-testid="not-extracted">
      <span aria-hidden="true" className="font-mono text-[11px] not-italic">
        {"∅"}
      </span>
      {children}
    </span>
  );
}

/** LabelChip shows an item label; "edited" is red with a border. */
export function LabelChip({ label }: { label: string }) {
  return (
    <span
      className={cn(CHIP, label === "edited" ? "border-unmatched/40 bg-unmatched-soft text-unmatched" : "border-border bg-muted text-muted-foreground")}
      data-testid="label-chip"
      data-label={label}
    >
      {itemLabel(label)}
    </span>
  );
}

export function CauseChip({ cause }: { cause: string | null }) {
  if (cause === null) return null;
  return (
    <span
      title={CAUSE_TITLE[cause] ?? `cause: ${cause}`}
      data-testid="cause-chip"
      data-cause={cause}
      className={cn(
        "inline-flex h-5 flex-none items-center whitespace-nowrap rounded-full border px-2 text-[11px]",
        cause === "code" && "border-transparent bg-muted text-foreground",
        cause === "relink" && "border-http/50 text-http",
        cause === "extractor_or_relink" && "border-dashed border-other-bar text-other",
        !(cause in CAUSE_LABEL) && "border-border text-muted-foreground",
      )}
    >
      {CAUSE_LABEL[cause] ?? cause}
    </span>
  );
}

/** RowTag names a contract row. */
export function RowTag({ row, title = "contract row" }: { row: string; title?: string }) {
  return (
    <span title={title} className="flex-none rounded-[3px] bg-muted px-1 font-mono text-[10px] font-semibold whitespace-nowrap text-muted-foreground">
      {row}
    </span>
  );
}

/** LabelRowTag is a row or rule tag from the label table (T025 rows
 * 11-13): shown in the technical style only; the plain style keeps the id
 * in the tooltip of the element it belongs to. */
export function LabelRowTag({ tag }: { tag: Label }) {
  return tag.text === "" ? null : <RowTag row={tag.text} title={tag.title} />;
}

/** ProvChip is the shared provenance chip (components/precision/ProvChip,
 * T041): the Changes tag became the look of every provenance label. */
export { ProvChip } from "@/components/precision/ProvChip";

/** Tag is a small neutral tag (dirty, test, partial). */
export function Tag({ children, title, tone = "muted" }: { children: ReactNode; title?: string; tone?: "muted" | "warn" }) {
  return (
    <span
      title={title}
      className={cn(
        "inline-flex h-[18px] flex-none items-center rounded border px-1.5 text-[11px] whitespace-nowrap",
        tone === "warn" ? "border-dynamic-bar/60 bg-dynamic-soft text-dynamic" : "border-border bg-muted text-muted-foreground",
      )}
    >
      {children}
    </span>
  );
}

/** RouteName shows a route's full path, or the path as written with a
 * marker of what is not known (T025). */
export function RouteName({ route }: { route: RouteTitle }) {
  return (
    <>
      <span data-testid="route-path">{route.text}</span>
      {route.marker !== null && (
        <span className="ml-1.5 font-sans text-xs font-normal whitespace-nowrap text-muted-foreground" data-testid="route-marker">
          {route.marker}
        </span>
      )}
      {route.more !== null && <span className="ml-1.5 font-sans text-xs font-normal whitespace-nowrap text-muted-foreground">{route.more}</span>}
    </>
  );
}
