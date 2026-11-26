// The Not proven list arranged for reading (T026 AG-C2 fixes): compose
// services mapped to no component are grouped into one collapsed item
// with each reason inside; a service whose database image AE2 ties to a
// database card is not listed as "not mapped" (its image is on that
// card), nor are same-name entries of its directory that write no image or
// build of their own; an entry of a component the Show toggles hide says
// where it comes from; "may also use" names the database card.
//
// Precision: nothing is dropped silently. Tied services are counted in
// the group's footnote and stay in the API answer; every other entry keeps
// the server's text.

import type { ArchitectureResponse, NotProven } from "@/api/architecture";
import { databaseTitle, placement, type ShowOptions } from "@/sections/architecture/model";

/** NO_OWN_IMAGE starts the server's CD3/CD4 reason for a service that
 * writes neither build nor image (docs/cli/serve.md, `service_not_mapped`). */
export const NO_OWN_IMAGE = "no build or image of its own";

export type NotProvenItem =
  | { type: "entry"; entry: NotProven; hidden: { hiddenBy: string; name: string } | null; card: string | null }
  | { type: "deploy"; entries: NotProven[]; tied: { entry: NotProven; card: string }[] };

function directory(file: string | null): string {
  if (file === null) return "";
  const i = file.lastIndexOf("/");
  return i === -1 ? "." : file.slice(0, i);
}

/** arrangeNotProven orders the list: entries in the answer's order, the
 * deploy group where its first entry was. show (when given) decides
 * whether a component counts as hidden; without it, a component hidden
 * by default does. */
export function arrangeNotProven(data: ArchitectureResponse, items: readonly NotProven[], show?: ShowOptions): NotProvenItem[] {
  const tiedCard = new Map<string, string>();
  for (const n of items) {
    if (n.kind === "service_not_mapped" && n.database !== null && n.service !== null) tiedCard.set(`${directory(n.file)}|${n.service}`, n.database);
  }
  // A card's name; its schema set too when another card has the same name.
  const cardName = (id: string) => {
    const d = data.databases.find((x) => x.id === id);
    if (d === undefined) return id;
    const title = databaseTitle(d);
    return data.databases.some((x) => x.id !== d.id && databaseTitle(x) === title) ? `${title} (${d.set})` : title;
  };
  const out: NotProvenItem[] = [];
  let group: Extract<NotProvenItem, { type: "deploy" }> | null = null;
  for (const n of items) {
    if (n.kind === "service_not_mapped" || n.kind === "ingress_not_mapped") {
      if (group === null) {
        group = { type: "deploy", entries: [], tied: [] };
        out.push(group);
      }
      const key = `${directory(n.file)}|${n.service ?? ""}`;
      const tied = n.kind !== "service_not_mapped" ? null : (n.database ?? (n.reason?.startsWith(NO_OWN_IMAGE) === true ? (tiedCard.get(key) ?? null) : null));
      if (tied !== null) group.tied.push({ entry: n, card: cardName(tied) });
      else group.entries.push(n);
      continue;
    }
    const c = n.component !== null ? data.components.find((x) => x.id === n.component) : undefined;
    const hiddenNow = c !== undefined && (show !== undefined ? placement(c, show) === "hidden" && c.visibility === "hidden" : c.visibility === "hidden");
    out.push({
      type: "entry",
      entry: n,
      hidden: hiddenNow ? { hiddenBy: c.hidden_by ?? "configured", name: c.name } : null,
      card: n.kind === "may_also_use" && n.database !== null ? cardName(n.database) : null,
    });
  }
  return out;
}

/** notProvenCount is the number of items a reader sees (the group counts
 * as one entry per untied service). */
export function notProvenCount(items: readonly NotProvenItem[]): number {
  return items.reduce((a, i) => a + (i.type === "deploy" ? i.entries.length : 1), 0);
}
