// The inspector's top block while a chain is drawn (T026 AG-C2, X+'s
// evidence chain): every step numbered as on the map, the stored edges
// into it with their wording and tier (link quality per step), its
// members with their source location, CH11 end lines under the member
// they end at, CH12 cap notes and CH10 hidden counts on the step, and the
// step's folded count. A member selected here or on the map is the same
// selection (the `at` hash parameter); its code peek opens below.
//
// The lists are never cut by the map budget: each step shows its first
// STEP_ROWS members with "Show all" for the rest (the answer's caps bound
// the total: 2,000 members per direction).

import { ChainEdgeLabel, LinkTierChip } from "@/components/precision/ProvChip";
import { LinkAssumptions, distinctAssumptions } from "@/components/LinkAssumptions";
import { ArrowDownUp, ExternalLink, Link2, Network, X } from "lucide-react";
import { useState } from "react";

import type { ChainDirection, ChainEdge, ChainMember } from "@/api/architecture";
import { CodePeek } from "@/components/CodePeek";
import { DepthSwitch } from "@/components/DepthSwitch";
import { useLabelStyle } from "@/hooks/useLabels";
import { formatCount } from "@/lib/count";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { flowHash, flowTarget, memberPath, memberTitle, type ChainPosition, type ChainView } from "@/sections/architecture/chain";

/** Members listed per step before "Show all". */
export const STEP_ROWS = 25;

export type ChainState = { kind: "loading" } | { kind: "failed"; error: string; retry: () => void } | { kind: "ok"; view: ChainView };

export interface ChainPanelProps {
  state: ChainState;
  /** A name for the start while the answer loads. */
  startName: string;
  dir: ChainDirection;
  /** The applied chain depth, null while it is not known yet (T042). */
  depth: number | null;
  /** Where the applied depth comes from, for the switch's tooltip. */
  depthTitle: string;
  selected: string | null;
  /** Show the start's code peek when nothing is selected (the inspector
   * has no other peek for it). */
  peekStart: boolean;
  onSelect: (identity: string) => void;
  onDirection: (d: ChainDirection) => void;
  onDepth: (depth: number) => void;
  onRestart: (identity: string) => void;
  onLinks: () => void;
  onClear: () => void;
  /** A database member's card name (members carry the raw card id). */
  databaseName?: ((id: string) => string | null) | undefined;
  /** The Architecture hash "Open in Flow tab" links back to (T026 AG-C3). */
  back?: string | null | undefined;
}

/** quality groups a step's incoming edges by wording, tier and row. */
function quality(edges: readonly ChainEdge[]): { wording: string; tier: string; row: string; dashed: boolean; n: number }[] {
  const out = new Map<string, { wording: string; tier: string; row: string; dashed: boolean; n: number }>();
  for (const e of edges) {
    const k = `${e.wording}|${e.tier}|${e.row}`;
    const q = out.get(k) ?? { wording: e.wording, tier: e.tier, row: e.row, dashed: e.dashed, n: 0 };
    q.n += Math.max(1, e.count);
    out.set(k, q);
  }
  return [...out.values()];
}

function where(m: ChainMember): string {
  if (m.file === null) return m.kind === "component" ? (m.id === "." ? "./" : `${m.id}/`) : "";
  return `${m.file}${m.line !== null ? `:${String(m.line)}` : ""}`;
}

function Badge({ n, on }: { n: number; on: boolean }) {
  return <span className={cn("grid size-[22px] flex-none place-items-center rounded-full text-[11px] font-semibold text-white", on ? "bg-foreground" : "bg-brand")}>{n}</span>;
}

function Step({ p, view, selected, onSelect, databaseName }: { p: ChainPosition; view: ChainView; selected: string | null; onSelect: (id: string) => void; databaseName?: ((id: string) => string | null) | undefined }) {
  const style = useLabelStyle();
  const [all, setAll] = useState(false);
  const kinds = [...new Set(p.members.map((m) => label(style, "nodeKind", m.kind).text))];
  const on = p.members.some((m) => m.id === selected);
  const list = all ? p.members : p.members.slice(0, STEP_ROWS);
  const ends = new Map<string, string[]>();
  for (const e of view.ends) {
    const t = label(style, "chainEnd", { text: e.text, row: e.row }).text;
    ends.set(e.member, [...(ends.get(e.member) ?? []), t]);
  }
  const pathStep = (m: ChainMember) => {
    if (m.tier === "fact" || m.tier_step === 0) return null;
    const side = p.side === "start" ? null : p.side;
    const at = side === null ? undefined : view.positions.find((x) => x.side === side && x.step === m.tier_step);
    return at?.number ?? null;
  };
  // The edges between this step and the one nearer the start: above a
  // down step, below an up step (reading order follows the numbers).
  const qualityLine =
    p.edges.length === 0 ? null : (
      <div className="ml-[10px] flex flex-wrap gap-x-2 gap-y-0.5 border-l-2 border-brand/30 py-0.5 pl-3 text-[12px] text-muted-foreground" data-testid="step-quality">
        {quality(p.edges).map((q) => {
          return <ChainEdgeLabel key={`${q.wording}|${q.tier}|${q.row}`} edge={{ wording: q.wording, row: q.row, tier: q.tier, dashed: q.dashed }} after={q.n > 1 ? <span>×{formatCount(q.n)}</span> : null} />;
        })}
        <LinkAssumptions list={distinctAssumptions(p.edges.map((e) => e.assumptions))} />
      </div>
    );
  return (
    <li className="grid gap-1" data-step={p.number} data-side={p.side}>
      {p.side === "down" && qualityLine}
      <div className={cn("grid gap-0.5 rounded-lg px-1.5 py-1", on && "bg-brand-soft/60 ring-1 ring-brand/40")}>
        <div className="flex min-w-0 items-center gap-2">
          <Badge n={p.number} on={on} />
          <span className="min-w-0 truncate text-[11.5px] font-semibold tracking-wide text-muted-foreground uppercase">
            {p.side === "start" ? "start · " : ""}
            {kinds.join(", ")}
          </span>
          <span className="ml-auto flex-none text-xs text-muted-foreground tabular-nums">{formatCount(p.members.length)}</span>
        </div>
        <ul className="grid gap-0.5 pl-[30px]">
          {list.map((m) => {
            const ps = pathStep(m);
            return (
              <li key={m.id}>
                <button
                  type="button"
                  aria-pressed={m.id === selected}
                  data-member={m.id}
                  className={cn("grid w-full min-w-0 grid-cols-[minmax(0,1fr)_auto] items-baseline gap-x-2 rounded-md px-1.5 py-0.5 text-left text-[12.5px] hover:bg-accent", m.id === selected && "bg-brand-soft ring-1 ring-brand")}
                  onClick={() => {
                    onSelect(m.id);
                  }}
                >
                  <span className="min-w-0 truncate font-mono" title={m.qualified_name}>
                    {m.kind === "route" && m.method !== null ? <span className="mr-1 text-[11px] font-semibold">{m.method}</span> : null}
                    {m.kind === "route" && memberPath(m) !== null ? memberPath(m) : m.kind === "database" ? (databaseName?.(m.id) ?? memberTitle(m)) : memberTitle(m)}
                    {!p.shown.has(m.id) && <span className="ml-1 font-sans text-[10.5px] text-muted-foreground">· folded</span>}
                  </span>
                  <span className="max-w-[150px] truncate font-mono text-[11px] text-muted-foreground" title={where(m)}>
                    {where(m).split("/").pop()}
                  </span>
                </button>
                {ps !== null && m.id === selected && (
                  <span className="block pl-1.5 text-[11px] text-muted-foreground">
                    path <LinkTierChip tier={m.tier} size="sm" /> at step {ps}
                  </span>
                )}
                {(ends.get(m.id) ?? []).map((t) => (
                  <span key={t} className="mt-0.5 ml-1.5 block rounded border border-dashed border-never-bar px-1.5 py-0.5 text-[11.5px] text-muted-foreground" data-testid="chain-end">
                    {t}
                  </span>
                ))}
              </li>
            );
          })}
        </ul>
        {p.members.length > STEP_ROWS && (
          <button
            type="button"
            className="ml-[30px] justify-self-start text-xs font-medium text-brand-text hover:underline"
            onClick={() => {
              setAll((v) => !v);
            }}
          >
            {all ? "Show fewer" : `Show all ${formatCount(p.members.length)}`}
          </button>
        )}
        <div className="grid gap-0.5 pl-[30px] text-[11.5px] text-muted-foreground">
          {p.folded > 0 && <span data-testid="step-folded">{label(style, "chainFold", { count: p.folded, number: p.number, hidden: 0 }).text} on the map</span>}
          {p.hiddenTest > 0 && <span title={label(style, "chainHidden", { count: p.hiddenTest }).title}>{label(style, "chainHidden", { count: p.hiddenTest }).text}</span>}
          {p.bounds.map((b) => {
            const l = label(style, "chainBound", { text: b.text, cap: b.cap });
            return (
              <span key={`${b.cap}:${String(b.step)}`} className="font-medium text-dynamic" title={l.title} data-testid="chain-bound">
                {l.text}
              </span>
            );
          })}
        </div>
      </div>
      {p.side === "up" && qualityLine}
    </li>
  );
}

/** ChainPanel renders the chain block. */
export function ChainPanel(p: ChainPanelProps) {
  const style = useLabelStyle();
  const view = p.state.kind === "ok" ? p.state.view : null;
  const target = view !== null ? flowTarget(view, p.selected) : null;
  const flowHref = target !== null ? flowHash(target, p.selected, p.back ?? null) : null;
  const sel = view !== null && p.selected !== null ? (view.member.get(p.selected) ?? null) : null;
  const startId = view?.response.start.id ?? null;
  // A member of another repository (CH13) has no code here and no chain
  // of its own in this repository's view.
  const remoteName = sel !== null && (sel.repository !== null || sel.kind === "repository") ? (sel.repository ?? sel.name) : null;
  const remote = remoteName !== null;
  const peek = sel !== null ? (sel.kind === "component" || sel.kind === "database" || remote ? null : sel.id) : p.peekStart && view !== null && !view.componentLevel ? startId : null;
  const summary = view !== null ? label(style, "chainSummary", { start: p.startName, steps: view.positions.length, members: view.members, folded: view.folded, direction: view.response.direction }) : null;
  const btn = "inline-flex h-7 items-center gap-1 rounded-md border px-2 text-[12.5px] font-medium hover:bg-accent [&_svg]:size-3.5";
  return (
    <section className="grid gap-2 border-t px-4 py-3" data-testid="chain-panel" aria-label="Chain">
      <div className="flex items-center gap-2">
        <h3 className="text-[11.5px] font-semibold tracking-wide text-muted-foreground uppercase">Evidence chain</h3>
        <button type="button" className="ml-auto grid size-6 place-items-center rounded-md hover:bg-accent" aria-label="Clear the chain" title="Clear the chain (Esc)" onClick={p.onClear}>
          <X aria-hidden="true" className="size-4" />
        </button>
      </div>
      {summary !== null && (
        <p className="text-[13px]" title={summary.title} data-testid="chain-summary">
          {summary.text}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        <div className="inline-flex rounded-md border p-0.5" role="group" aria-label="Direction">
          {(["up", "both", "down"] as const).map((d) => (
            <button
              key={d}
              type="button"
              aria-pressed={p.dir === d}
              className={cn("h-6 rounded px-2 text-[12px]", p.dir === d ? "bg-brand-soft font-medium text-brand-text" : "text-muted-foreground hover:text-foreground")}
              onClick={() => {
                p.onDirection(d);
              }}
            >
              {d === "up" ? "Upstream" : d === "down" ? "Downstream" : "Both"}
            </button>
          ))}
        </div>
        <DepthSwitch depth={p.depth} title={p.depthTitle} onDepth={p.onDepth} testId="chain-depth" />
        {flowHref !== null ? (
          <a href={flowHref} className={cn(btn, view?.response.map.open_in_flow === true && "border-brand bg-brand text-brand-foreground hover:bg-brand/90")} data-testid="open-in-flow" title="The Flow tab draws every member of this endpoint's neighbourhood">
            <ExternalLink aria-hidden="true" />
            Open in Flow tab
          </a>
        ) : (
          view !== null && (
            <span className={cn(btn, "cursor-default opacity-60")} title="The Flow tab shows endpoints; this chain reaches no route or IPC command">
              <ExternalLink aria-hidden="true" />
              Open in Flow tab
            </span>
          )
        )}
        {view !== null && (
          <button type="button" className={btn} onClick={p.onLinks} data-testid="show-in-links">
            <Link2 aria-hidden="true" />
            Show in Links tab
          </button>
        )}
      </div>
      {p.state.kind === "loading" && <p className="text-xs text-muted-foreground">Reading the chain…</p>}
      {p.state.kind === "failed" && (
        <p className="text-xs text-unmatched">
          The chain could not be loaded: {p.state.error}{" "}
          <button type="button" className="font-medium text-brand-text underline" onClick={p.state.retry}>
            Retry
          </button>
        </p>
      )}
      {view !== null && view.positions.length <= 1 && <p className="text-xs text-muted-foreground">No stored edge the chain follows starts here.</p>}
      {view !== null && (
        <ol className="grid gap-1" data-testid="chain-steps">
          {view.positions.map((pos) => (
            <Step key={pos.number} p={pos} view={view} selected={p.selected} onSelect={p.onSelect} databaseName={p.databaseName} />
          ))}
        </ol>
      )}
      {view !== null && Object.keys(view.notFollowed).length > 0 && (
        <p className="text-[11.5px] text-muted-foreground" data-testid="not-followed">
          {Object.entries(view.notFollowed)
            .map(([k, n]) => label(style, "chainNotFollowed", { kind: k, count: n }).text)
            .join(" · ")}
        </p>
      )}
      {remoteName !== null && (
        <p className="text-xs text-muted-foreground" data-testid="chain-remote">
          In {remoteName}, another member of the project: open that repository's view to follow it further.
        </p>
      )}
      {sel !== null && sel.id !== startId && !remote && (
        <button
          type="button"
          className={cn(btn, "justify-self-start")}
          onClick={() => {
            p.onRestart(sel.id);
          }}
        >
          <Network aria-hidden="true" />
          Chain from {memberTitle(sel)}
        </button>
      )}
      {peek !== null && (
        <div className="mt-1" data-testid="chain-peek">
          <CodePeek id={peek} className="max-h-[340px]" />
        </div>
      )}
      {view !== null && <p className="flex items-center gap-1 text-[11px] text-muted-foreground"><ArrowDownUp aria-hidden="true" className="size-3" />Arrow keys move along the chain; Esc clears it.</p>}
    </section>
  );
}
