// The toolbar of the selected card (T016f D7): every keyboard action on a
// card has a button here. Peek source (Enter), the IDE button with the
// configured label (o), and the connected cards through each drawn line
// ("Incoming", "Outgoing"; not "Callers"/"Callees", since a "defined in"
// line is not a call), each with a button that selects the line itself.

import { NodeToolbar, Position } from "@xyflow/react";
import { ArrowLeft, ArrowRight, CodeXml, Spline } from "lucide-react";
import { useState } from "react";

import { EditorLink } from "@/components/EditorLink";
import { IdeCaveat, IdeMark } from "@/components/IdeParts";
import { useEditor } from "@/hooks/useEditor";
import { presentIde } from "@/lib/ide";
import { cn } from "@/lib/utils";
import { useFlowActions, type Neighbour } from "@/sections/api/flow/context";
import type { GraphNode } from "@/sections/api/flow/graph";
import { TIER_LABEL } from "@/sections/api/flow/tiers";

const BTN =
  "inline-flex h-7 items-center gap-1 whitespace-nowrap rounded-md px-2 text-xs font-medium outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring [&_svg]:size-3.5";

function Menu({ label, icon, items, testId }: { label: string; icon: "in" | "out"; items: Neighbour[]; testId: string }) {
  const a = useFlowActions();
  const [open, setOpen] = useState(false);
  const Icon = icon === "in" ? ArrowLeft : ArrowRight;
  return (
    <div className="relative">
      <button
        type="button"
        className={cn(BTN, open && "bg-accent")}
        aria-expanded={open}
        aria-keyshortcuts={icon === "in" ? "ArrowLeft" : "ArrowRight"}
        title={`${label} cards (${icon === "in" ? "Left" : "Right"} arrow moves to the first)`}
        disabled={items.length === 0}
        data-testid={testId}
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        <Icon aria-hidden="true" />
        {label} {items.length}
      </button>
      {open && items.length > 0 && (
        <ul className="absolute top-8 left-0 z-20 grid max-h-64 min-w-64 gap-px overflow-auto rounded-md border bg-popover p-1 shadow-md">
          {items.map((n) => (
            <li key={n.edge.id} className="flex items-center gap-1">
              <button
                type="button"
                className={cn(BTN, "flex-1 justify-start font-mono")}
                onClick={() => {
                  setOpen(false);
                  a.selectNode(n.node.id);
                }}
              >
                {n.node.title}
              </button>
              <button
                type="button"
                className={BTN}
                title={`Select the ${n.edge.label ?? TIER_LABEL[n.edge.tier].toLowerCase()} line`}
                aria-label={`Select the line to ${n.node.title}`}
                onClick={() => {
                  setOpen(false);
                  a.selectEdge(n.edge.id);
                }}
              >
                <Spline aria-hidden="true" />
                {n.edge.label ?? TIER_LABEL[n.edge.tier].toLowerCase()}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** FlowNodeToolbar shows the actions of a selected card above it. */
export function FlowNodeToolbar({ card, visible }: { card: GraphNode; visible: boolean }) {
  const a = useFlowActions();
  const view = presentIde(a.ide, card.node?.ide ?? null, useEditor());
  return (
    <NodeToolbar isVisible={visible} position={Position.Top} offset={6} align="start">
      <div className="flex items-center gap-0.5 rounded-lg border bg-popover p-0.5 shadow-md" data-testid="node-toolbar" role="toolbar" aria-label={`Actions for ${card.title}`}>
        <button
          type="button"
          className={cn(BTN, a.peekOpen && "bg-brand text-brand-foreground hover:bg-brand/90")}
          aria-pressed={a.peekOpen}
          aria-keyshortcuts="Enter"
          title={a.peekOpen ? "Close the code peek (Enter on the card opens it)" : "Peek source (Enter)"}
          data-testid="toolbar-peek"
          onClick={a.togglePeek}
        >
          <CodeXml aria-hidden="true" />
          Peek source
        </button>
        {view.href !== null ? (
          <EditorLink
            href={view.href}
            place={card.id}
            label={view.text}
            detail={`${view.title}\nShortcut: o`}
            className={BTN}
            aria-keyshortcuts="o"
            aria-label={`${view.name} (o)`}
            data-testid="toolbar-ide"
          >
            <IdeMark view={view} />
            {view.text}
            <IdeCaveat view={view} />
          </EditorLink>
        ) : (
          <button type="button" disabled className={cn(BTN, "cursor-not-allowed opacity-60")} title={view.title} aria-label={view.name} data-testid="toolbar-ide">
            <IdeMark view={view} />
            {view.text}
          </button>
        )}
        <Menu label="Incoming" icon="in" items={a.incoming(card.id)} testId="toolbar-incoming" />
        <Menu label="Outgoing" icon="out" items={a.outgoing(card.id)} testId="toolbar-outgoing" />
      </div>
    </NodeToolbar>
  );
}
