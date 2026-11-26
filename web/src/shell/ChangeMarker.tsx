// A "changed since <base>" marker (T019 Part b item 6): a small chip naming
// what changed, that opens the item in Changes. Proven changes are solid
// (changed, added, no longer in the index, edited); knowledge changes
// ("unknown now", "known now") are a dashed outline and never read as a
// change. The base is in the tooltip and the accessible name; the compare
// chip in the top bar shows it too.

import type { MouseEvent } from "react";

import type { ChangeItem } from "@/api/changes";
import { cn } from "@/lib/utils";
import { itemsFor, markerTitle, markerWord, type MarkerTone } from "@/sections/changes/markers";
import { changesHash } from "@/sections/changes/params";
import { useChangeMarkers } from "@/shell/markers";

const TONE: Record<MarkerTone, string> = {
  changed: "border-brand/30 bg-brand-soft text-brand-text",
  added: "border-transparent bg-linked-soft text-linked",
  removed: "border-transparent bg-unmatched-soft text-unmatched",
  edited: "border-unmatched-bar bg-unmatched-soft text-unmatched",
  knowledge: "border-dashed border-dynamic-bar bg-transparent text-dynamic",
};

/** itemHash is the Changes deep link of one item, its group selected; a
 * test-scope item opens with "Include tests" on, so the list holds it. */
export function itemHash(item: Pick<ChangeItem, "id" | "group" | "test">): string {
  return changesHash({ group: item.group, change: null, q: "", tests: item.test, sel: item.id, tab: "change", depth: null, pane: "item" });
}

/**
 * ChangeMarker draws the marker of a view's node. as="link" is a link
 * (rows that are not themselves controls, cards, detail headers);
 * as="decoration" is a span inside a listbox option, which may not hold a
 * control: a click still opens the item, keyboard users reach it from the
 * detail header's link, and the option's name carries the words.
 */
export function ChangeMarker({
  items,
  baseText,
  as = "link",
  className,
  shrinkInCell = false,
}: {
  items: readonly ChangeItem[];
  baseText: string;
  as?: "link" | "decoration";
  className?: string;
  /** Inside a container named "cell" narrower than 200 px the marker
   * becomes a dot (its words stay in the tooltip and accessible name), so
   * it never takes the room of the text it follows (R5-6). */
  shrinkInCell?: boolean;
}) {
  const first = items[0];
  if (first === undefined) return null;
  const w = markerWord(first);
  const title = markerTitle(items, baseText);
  const cls = cn(
    "inline-flex h-[18px] max-w-[180px] flex-none items-center gap-1 rounded-full border px-[7px] font-sans text-[11px] leading-none font-medium whitespace-nowrap no-underline",
    TONE[w.tone],
    className,
    shrinkInCell && "@max-[199px]/cell:min-w-0 @max-[199px]/cell:px-1",
  );
  const body = (
    <>
      <span className={cn("min-w-0 truncate", shrinkInCell && "@max-[199px]/cell:hidden")}>{w.text}</span>
      {items.length > 1 && <span className={cn("flex-none opacity-80", shrinkInCell && "@max-[199px]/cell:hidden")}>+{items.length - 1}</span>}
      {shrinkInCell && (
        <span aria-hidden="true" className="hidden size-1.5 flex-none rounded-full bg-current @max-[199px]/cell:inline-block" data-testid="change-marker-dot" />
      )}
    </>
  );
  const data = { "data-testid": "change-marker", "data-count": String(items.length), "data-tone": w.tone, "data-item": first.id };
  if (as === "decoration") {
    const open = (e: MouseEvent) => {
      e.stopPropagation();
      e.preventDefault();
      window.location.hash = itemHash(first);
    };
    return (
      <span className={cn(cls, "cursor-pointer")} title={title} onClick={open} onMouseDown={(e) => { e.stopPropagation(); }} {...data}>
        {body}
      </span>
    );
  }
  return (
    <a href={itemHash(first)} className={cn(cls, "hover:brightness-95 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none")} title={title} aria-label={title} {...data}>
      {body}
    </a>
  );
}

/** NodeMarker is the marker of one shown node: it reads the view's groups
 * for the compare chip's base and draws nothing while markers are off,
 * loading, failed, or when the node has no item. */
export function NodeMarker({ id, groups, as = "link", className }: { id: string; groups: readonly string[]; as?: "link" | "decoration"; className?: string }) {
  const m = useChangeMarkers(groups);
  if (m.kind !== "ok") return null;
  return <ChangeMarker items={itemsFor(m.index, id)} baseText={m.baseText} as={as} {...(className !== undefined ? { className } : {})} />;
}
