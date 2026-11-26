// What the Flow tab's cards, lines and node toolbar share: the selection,
// the peek state and the actions behind every visible control. Passed by
// context so React Flow's node and edge data stay plain values.

import { createContext, useContext, type KeyboardEvent } from "react";

import type { IdeStatus } from "@/api/endpoints";
import type { GraphEdge, GraphNode } from "@/sections/api/flow/graph";

/** A neighbour of a card through one drawn line. */
export interface Neighbour {
  node: GraphNode;
  edge: GraphEdge;
}

export interface FlowActions {
  selectedNode: string | null;
  selectedEdge: string | null;
  /** The code peek of the selection is open. */
  peekOpen: boolean;
  ide: IdeStatus;
  selectNode: (id: string) => void;
  selectEdge: (id: string) => void;
  togglePeek: () => void;
  incoming: (id: string) => Neighbour[];
  outgoing: (id: string) => Neighbour[];
  /** Arrow keys on a card: move the focus to a connected card. */
  onCardKey: (event: KeyboardEvent<HTMLElement>, id: string) => void;
}

export const FlowContext = createContext<FlowActions | null>(null);

/** useFlowActions reads the context; the cards render only inside it. */
export function useFlowActions(): FlowActions {
  const v = useContext(FlowContext);
  if (v === null) throw new Error("Flow cards must be rendered inside FlowContext");
  return v;
}
