// A component kind in the applied label style (label table row 31), shared
// by the map's cards (lazy chunk) and the inspector (entry chunk).

import type { Component } from "@/api/architecture";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";

/** mainKind is the kind a card names first: the first kind that is not
 * "package" (a server that is also a schema source reads "server"). */
export function mainKind(c: Component): string {
  return c.kinds.find((k) => k !== "package") ?? c.kinds[0] ?? "unknown";
}

const CK_ROWS = ["server", "tauri_core", "client", "schema_source", "binary", "docs_site", "unassigned_files"];

/** kindRow is the CK row of a kind (CK7 for the unassigned root's
 * unassigned_files, CKD for package), null for others. */
export function kindRow(kind: string): string | null {
  const i = CK_ROWS.indexOf(kind);
  if (i >= 0) return `CK${String(i + 1)}`;
  return kind === "package" ? "CKD" : null;
}

/** KindTextPlain renders one kind of c; the framework is named for a
 * server. */
export function KindTextPlain({ c, kind, className }: { c: Component; kind: string; className?: string }) {
  const l = label(useLabelStyle(), "archKind", { kind, framework: kind === "server" ? (c.frameworks[0] ?? null) : null, row: kindRow(kind) });
  return (
    <span className={className ?? "mr-1.5 inline-block"} title={l.title}>
      {l.text}
    </span>
  );
}
