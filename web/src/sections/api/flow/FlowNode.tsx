// The cards of the Flow tab: one per drawn node (a call with its folded
// owner, an endpoint, a function), a state box for a handler that is not
// stored, and the column captions (a row pinned below the canvas's
// toolbar). A card is a button: click (or Enter)
// selects it and opens its code peek; arrows move to connected cards. Text
// sizes match measure.ts (13 px titles, 12 px everything else). A card is
// at most its layout's cap wide (cardCap, 240-360 px by the canvas width):
// long texts are cut in the middle (measure.ts),
// and a card with a cut text shows the full texts in a tooltip on hover
// and keyboard focus; its accessible name is built from the full texts.

import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import type { ReactElement } from "react";

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useFlowActions } from "@/sections/api/flow/context";
import type { Badge, GraphNode } from "@/sections/api/flow/graph";
import type { CaptionPlace } from "@/sections/api/flow/layout";
import { cardLayout, chips } from "@/sections/api/flow/measure";
import { FlowNodeToolbar } from "@/sections/api/flow/NodeToolbar";
import { itemsFor, VIEW_GROUPS } from "@/sections/changes/markers";
import { ChangeMarker } from "@/shell/ChangeMarker";
import { useChangeMarkers } from "@/shell/markers";

/** CardNode is a card in React Flow; `cap` is the card width cap its
 * layout was made at, so the drawn texts are cut as ELK's box was sized. */
export type CardNode = Node<{ card: GraphNode; related: boolean; cap: number }, "card">;

const TONE: Record<Badge["tone"], string> = {
  linked: "bg-linked-soft text-linked",
  unmatched: "bg-unmatched-soft text-unmatched",
  dynamic: "bg-dynamic-soft text-dynamic",
  wrapper: "bg-wrapper-soft text-wrapper",
  other: "bg-other-soft text-other",
  never: "bg-never-soft text-never",
  http: "bg-http-soft text-http",
  ipc: "bg-ipc-soft text-ipc",
};

const METHOD: Record<string, string> = {
  GET: "text-m-get",
  POST: "text-m-post",
  PUT: "text-m-put",
  PATCH: "text-m-patch",
  DELETE: "text-m-delete",
};

/** cardLabel is a card's accessible name: what it is and where, from the
 * full texts (never the cut ones). */
export function cardLabel(c: GraphNode): string {
  const what = c.method !== null ? `${c.method} ${c.title}` : c.title;
  return `${what}, ${c.step}, ${c.sub}`;
}

/** CARD_HINT is a card's shortcut hint (its title, or the tooltip's last
 * line when the card has cut texts). */
function cardHint(c: GraphNode): string {
  return `${c.step}: click or Enter to peek the source; arrows move to connected cards`;
}

/** FullTexts is a cut card's tooltip: every text in full. */
function FullTexts({ c }: { c: GraphNode }) {
  return (
    <span className="grid gap-0.5 [overflow-wrap:anywhere]" data-testid="card-tooltip">
      <span className="font-mono font-medium">{c.method !== null ? `${c.method} ${c.title}` : c.title}</span>
      <span className="font-mono">{c.sub}</span>
      {c.notes.map((n) => (
        <span key={n}>{n}</span>
      ))}
      <span className="opacity-75">{cardHint(c)}</span>
    </span>
  );
}

function WithTooltip({ c, cut, children }: { c: GraphNode; cut: boolean; children: ReactElement }) {
  if (!cut) return children;
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side="bottom" align="start" sideOffset={6} className="max-w-[560px]">
        <FullTexts c={c} />
      </TooltipContent>
    </Tooltip>
  );
}

function Handles() {
  return (
    <>
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <Handle type="source" position={Position.Right} isConnectable={false} />
    </>
  );
}

/** CardMarker is a card's "changed since" marker: the items of every
 * identity the card stands for and the links that leave them. It sits on
 * the card's top border, so it changes no card size ELK laid out. */
function CardMarker({ c }: { c: GraphNode }) {
  const m = useChangeMarkers(VIEW_GROUPS.flow);
  if (m.kind !== "ok") return null;
  const items = c.members.flatMap((id) => itemsFor(m.index, id, true));
  if (items.length === 0) return null;
  return <ChangeMarker items={items} baseText={m.baseText} className="absolute -top-[9px] right-2 z-10 shadow-[0_0_0_2px_hsl(var(--canvas))]" />;
}

/** FlowCard renders one card. */
export function FlowCard({ data, width, height }: NodeProps<CardNode>) {
  const a = useFlowActions();
  const c = data.card;
  const selected = a.selectedNode === c.id;
  const ipc = c.node?.kind === "ipc_command";
  const action = c.node?.kind === "server_action";
  const endpoint = c.node?.kind === "route" || ipc || action;
  const chipList = chips(c);
  const shown = cardLayout(c, data.cap);
  if (c.type === "state") {
    return (
      <div
        style={{ width, height }}
        className="grid grid-cols-[minmax(0,1fr)] content-center gap-0.5 rounded-lg border border-dashed border-muted-foreground/70 px-3 py-[7px] text-xs text-muted-foreground"
        data-flow-state={c.role}
      >
        <Handles />
        <span className="truncate text-[13px] leading-5 font-medium text-foreground" title={shown.truncated ? c.title : undefined}>
          {shown.title}
        </span>
        <span className="truncate font-mono" title={shown.truncated ? c.sub : undefined}>
          {shown.sub}
        </span>
        {shown.notes.map((n, i) => (
          <span key={c.notes[i] ?? n} className="truncate" title={n !== c.notes[i] ? c.notes[i] : undefined} data-flow-note>
            {n}
          </span>
        ))}
      </div>
    );
  }
  return (
    <div style={{ width, height }} className={cn("relative", c.dim && "opacity-30")} data-flow-dim={c.dim ? "true" : undefined}>
      <Handles />
      <CardMarker c={c} />
      <FlowNodeToolbar card={c} visible={selected} />
      <WithTooltip c={c} cut={shown.truncated}>
      <button
        type="button"
        data-flow-card={c.id}
        data-roles={c.roles.join(" ")}
        aria-pressed={selected}
        aria-label={cardLabel(c)}
        title={shown.truncated ? undefined : cardHint(c)}
        data-flow-cut={shown.truncated ? "true" : undefined}
        className={cn(
          "grid h-full w-full grid-cols-[minmax(0,1fr)] content-center gap-0.5 rounded-lg border bg-card px-3 py-[7px] text-left shadow-[0_1px_2px_rgba(0,0,0,.06)] outline-none",
          "focus-visible:ring-[3px] focus-visible:ring-ring/50 hover:border-muted-foreground/60",
          endpoint && (ipc ? "border-l-[3px] border-l-ipc-bar" : action ? "border-l-[3px] border-l-action-bar" : "border-l-[3px] border-l-http-bar"),
          c.focus && "border-brand shadow-[0_0_0_3px_hsl(var(--brand)/.25)]",
          c.muted && "border-dashed border-muted-foreground/70 bg-transparent shadow-none",
          (c.role === "callee" || c.role === "caller" || c.role === "enclosing") && !c.focus && "bg-muted/55",
          selected && "border-brand ring-2 ring-brand",
          data.related && !selected && "border-brand/70",
        )}
        onClick={(e) => {
          e.stopPropagation();
          a.selectNode(c.id);
        }}
        onKeyDown={(e) => {
          a.onCardKey(e, c.id);
        }}
      >
        <span className={cn("flex min-w-0 items-center gap-[7px] font-mono text-[13px] leading-5 font-medium whitespace-nowrap", c.muted && "text-muted-foreground")}>
          {c.method !== null && <span className={cn("flex-none text-xs font-semibold", METHOD[c.method] ?? "text-foreground")}>{c.method}</span>}
          <span className="min-w-0 overflow-hidden text-ellipsis" data-flow-text="title">
            {shown.title}
          </span>
          {c.badges.map((b) => (
            <span key={b.text} title={b.title} className={cn("flex-none rounded-[4px] px-[6px] font-sans text-xs leading-4 font-semibold", TONE[b.tone])}>
              {b.text}
            </span>
          ))}
          {c.changed && (
            <span
              title="File changed since the index"
              aria-label="file changed since the index"
              className="ml-auto size-[9px] flex-none rounded-full bg-stale shadow-[0_0_0_2px_hsl(var(--card)),0_0_0_3.5px_hsl(var(--stale)/.45)]"
            />
          )}
        </span>
        <span className="flex min-w-0 items-center gap-[7px] font-mono text-xs leading-[18px] whitespace-nowrap text-muted-foreground">
          {c.wrapper !== null && (
            <span
              title="The stored edge leaves this call site; the wrapper function performs the request"
              className="flex-none rounded-full border border-dashed border-wrapper/70 bg-wrapper-soft px-[6px] font-sans leading-4 font-semibold text-wrapper"
            >
              {c.wrapper}
            </span>
          )}
          <span className="min-w-0 overflow-hidden text-ellipsis" data-flow-text="sub">
            {shown.sub}
          </span>
        </span>
        {shown.notes.map((n, i) => (
          <span key={c.notes[i] ?? n} className="min-w-0 overflow-hidden text-xs leading-[18px] text-ellipsis whitespace-nowrap text-muted-foreground" data-flow-note>
            {n}
          </span>
        ))}
        {chipList.length > 0 && (
          <span className="flex gap-[7px] pt-1">
            {chipList.map((t) => (
              <span
                key={t}
                title="Cut by the fan-out cap of 25; not expandable in this version"
                className="rounded-[4px] border px-[6px] text-xs leading-4 font-semibold text-muted-foreground"
                data-flow-omitted
              >
                {t}
              </span>
            ))}
          </span>
        )}
      </button>
      </WithTooltip>
    </div>
  );
}

/** FlowCaption is a column caption in the pinned caption row (FlowTab's
 * overlay, T016i2 D1): screen space, so it stays in view while its
 * column's left edge is panned off (T016i D2.6) and when the view is
 * panned down. It never extends beyond its own column on screen
 * (placeCaption): 12 px at zoom >= 1, scaled with the zoom below 1, cut
 * with an ellipsis to the column's width (the full text in its title) and
 * not drawn at all over a column too narrow to read. Captions are not read
 * out: the cards carry their step. */
export function FlowCaption({ text, place }: { text: string; place: CaptionPlace }) {
  if (place.hidden) return null;
  return (
    <div
      className={cn(
        "absolute top-0 left-0 origin-[left_center] overflow-hidden text-xs leading-[22px] font-semibold tracking-[.05em] text-ellipsis whitespace-nowrap text-muted-foreground uppercase",
        // A cut caption takes the pointer so its title (the full text) shows.
        place.clipped && "pointer-events-auto",
      )}
      style={{ transform: `translateX(${String(place.x)}px) scale(${String(place.scale)})`, maxWidth: place.maxWidth / place.scale }}
      title={place.clipped ? text : undefined}
      data-flow-caption={text}
      data-flow-caption-cut={place.clipped ? "true" : undefined}
    >
      {text}
    </div>
  );
}
