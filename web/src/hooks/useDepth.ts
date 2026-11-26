// The depth a walk starts with (T042), in the precedence of the label
// style (ADR 0078): (1) this browser's choice; (2) the repository default,
// graphvein.yaml's ui.impact_depth / ui.chain_depth (GET /api/v0/ui);
// (3) the built-in 3 (impact) or 6 (chain). A depth in the hash (a shared
// link) is the view itself and wins for that page without being stored.

import { useCallback, useEffect, useState } from "react";

import { useRepoDepths } from "@/hooks/useLabels";
import { BUILT_IN_DEPTH, readDepth, subscribeDepths, writeDepth, type DepthKind, type StoredDepth } from "@/lib/depthChoice";

/** Where the applied depth comes from. */
export type DepthSource = "link" | "browser" | "repository" | "default";

/** DepthState is the applied depth of one walk. */
export interface DepthState {
  /** The applied depth; null while the repository default is unknown and
   * nothing else decides it (the walk waits rather than load twice). */
  depth: number | null;
  source: DepthSource;
  /** False when browser storage is blocked: a choice lasts for this page. */
  saved: boolean;
  /** Chooses a depth in this browser. */
  choose: (depth: number) => void;
}

/** useDepth resolves the depth of one walk; linkDepth is the hash's
 * depth parameter (null when absent or invalid). */
export function useDepth(kind: DepthKind, linkDepth: number | null): DepthState {
  const repo = useRepoDepths();
  const [stored, setStored] = useStoredDepth(kind);
  const choose = useCallback(
    (depth: number) => {
      const saved = writeDepth(kind, depth);
      setStored({ depth, saved });
    },
    [kind, setStored],
  );
  // A choice made here also goes into the hash; it reads as this
  // browser's choice, not as a link's.
  if (linkDepth !== null) return { depth: linkDepth, source: linkDepth === stored.depth ? "browser" : "link", saved: stored.saved, choose };
  if (stored.depth !== null) return { depth: stored.depth, source: "browser", saved: stored.saved, choose };
  if (!repo.loaded) return { depth: null, source: "default", saved: stored.saved, choose };
  const fromRepo = repo[kind];
  if (fromRepo !== null) return { depth: fromRepo, source: "repository", saved: stored.saved, choose };
  return { depth: BUILT_IN_DEPTH[kind], source: "default", saved: stored.saved, choose };
}

/** depthSourceText says where an applied depth comes from, for the depth
 * switch's tooltip. */
export function depthSourceText(kind: DepthKind, s: DepthState): string {
  const what = kind === "impact" ? "change impact" : "chain";
  const from =
    s.source === "link"
      ? "set by this link"
      : s.source === "browser"
        ? s.saved
          ? "your last choice in this browser"
          : "your choice for this page (browser storage is blocked)"
        : s.source === "repository"
          ? `the repository default (graphvein.yaml ui.${kind}_depth)`
          : `the built-in default (${String(BUILT_IN_DEPTH[kind])})`;
  return `Steps the ${what} follows, at most 10; this depth is ${from}. A choice here is remembered in this browser.`;
}

/** useStoredDepth is this browser's stored choice of one walk, kept in
 * step with every write of the page (Settings' depth rows and the depth
 * switches) and of other tabs. */
export function useStoredDepth(kind: DepthKind): [StoredDepth, (s: StoredDepth) => void] {
  const [stored, setStored] = useState<StoredDepth>(() => readDepth(kind));
  useEffect(
    () =>
      subscribeDepths(() => {
        setStored(readDepth(kind));
      }),
    [kind],
  );
  return [stored, setStored];
}
