// The assumptions a stored calls_route link rests on, shown wherever a
// link is drawn or listed (T033): an origin nothing proves to be
// the server (row LF-1) and the unknown-prefix assumptions UP-V1 (the
// route is served below a prefix the code does not fix) and UP-V2 (the
// call's unknown base is that prefix). A link with assumptions is never
// shown as a plain link.

import { cn } from "@/lib/utils";

/** LinkAssumptionItem is one assumption: "origin" or a row id, and its
 * text as the server states it. */
export interface LinkAssumptionItem {
  id: string;
  text: string;
}

/** distinctAssumptions merges lists, each assumption once, in order. */
export function distinctAssumptions(lists: readonly (readonly LinkAssumptionItem[])[]): LinkAssumptionItem[] {
  const out: LinkAssumptionItem[] = [];
  const seen = new Set<string>();
  for (const list of lists) {
    for (const a of list) {
      const key = `${a.id}\u0000${a.text}`;
      if (!seen.has(key)) {
        seen.add(key);
        out.push(a);
      }
    }
  }
  return out;
}

/** assumptionWord is the short word an assumption is listed under. */
export function assumptionWord(a: LinkAssumptionItem): string {
  return a.id === "origin" ? "origin assumed" : a.id;
}

/** LinkAssumptions lists a link's assumptions ("assumes UP-V1 · UP-V2"),
 * each with its full text as the tooltip and for screen readers; nothing
 * when the list is empty. */
export function LinkAssumptions({ list, className }: { list: readonly LinkAssumptionItem[]; className?: string }) {
  if (list.length === 0) return null;
  return (
    <span className={cn("text-[11.5px] text-muted-foreground", className)} data-testid="link-assumptions">
      assumes{" "}
      {list.map((a, i) => (
        <span key={`${a.id}-${String(i)}`}>
          {i > 0 && " · "}
          <abbr title={a.text} aria-label={`${assumptionWord(a)}: ${a.text}`} className="font-medium text-dynamic no-underline">
            {assumptionWord(a)}
          </abbr>
        </span>
      ))}
    </span>
  );
}
