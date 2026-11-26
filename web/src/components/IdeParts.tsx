// The editor mark and caveat icon that start and end every IDE button
// (IdeButton, the code peek's button, the Flow toolbar and the provenance
// chips' editor buttons), so all of them show the same editor the same way
// (T016k, T029).

import { ExternalLink, Info, TriangleAlert } from "lucide-react";

import { EditorMark } from "@/components/EditorMark";
import type { IdeView } from "@/lib/ide";

/** IdeMark is the start of an IDE button: the editor's mark (or the
 * generic external-link icon when the editor is not known). */
export function IdeMark({ view, size = "sm" }: { view: IdeView; size?: "xs" | "sm" }) {
  return view.mark !== null ? <EditorMark mark={view.mark} size={size} /> : <ExternalLink aria-hidden="true" />;
}

/** IdeCaveat is the visible caveat after a button's label: a warning for
 * a partly verified preset, a custom template or an ignored choice, an
 * info icon for a limited preset (rule 14: on the link itself, not only
 * in the picker). */
export function IdeCaveat({ view }: { view: IdeView }) {
  if (view.caveat === null) return null;
  const Icon = view.caveat === "warn" ? TriangleAlert : Info;
  return <Icon aria-hidden="true" data-testid="ide-caveat" data-caveat={view.caveat} className={view.caveat === "warn" ? "text-dynamic" : "text-muted-foreground"} />;
}

