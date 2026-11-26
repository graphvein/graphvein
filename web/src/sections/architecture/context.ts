// What the map's cards and lines share: the selection, focus dimming, the
// expanded component's rows and the actions behind every control. Passed
// by context so React Flow's node and edge data stay plain values.

import { createContext, useContext } from "react";

import type { ArchitectureResponse } from "@/api/architecture";
import type { ChainOverlay } from "@/sections/architecture/chain";
import type { Row } from "@/sections/architecture/rows";

/** ExpandedRows is the expanded component's content: loading, its rows,
 * or the failure. */
export type ExpandedRows = { kind: "loading" } | { kind: "ok"; rows: Row[] } | { kind: "failed"; error: string };

export interface MapActions {
  data: ArchitectureResponse;
  selectedNode: string | null;
  selectedEdge: string | null;
  selectedItem: string | null;
  /** Ids left undimmed while something is selected (null: none dimmed). */
  focus: { nodes: Set<string>; edges: Set<string> } | null;
  expandedRows: ExpandedRows;
  /** The chain drawn in place (AG-C2), or null. */
  chain: ChainOverlay | null;
  /** Selects a chain member (map and inspector stay in sync). */
  selectMember: (identity: string) => void;
  selectNode: (id: string) => void;
  selectEdge: (id: string) => void;
  selectItem: (identity: string) => void;
  expand: (component: string | null) => void;
  /** Clears the selection (a click on the empty canvas, Escape). */
  clear: () => void;
  retryRows: () => void;
}

export const MapContext = createContext<MapActions | null>(null);

/** useMapActions reads the context; cards render only inside it. */
export function useMapActions(): MapActions {
  const v = useContext(MapContext);
  if (v === null) throw new Error("architecture cards must be rendered inside MapContext");
  return v;
}
