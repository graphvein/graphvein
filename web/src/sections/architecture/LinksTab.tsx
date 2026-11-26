// The Links tab (X+, AG1): one row per link of the answer with every cell's
// value and source, the members behind it (expand a row), the SDK stubs
// ("imports only, no call proven"), the documents counts (CE5, no map
// line) and the Not proven list. Every link is listed, also those whose
// cards the map's toggles hide, so the table is the complete ledger.
// AG-C2: "Show in Links tab" from a chain filters the ledger to the links
// the chain crosses (chain.ts chainLinkIds); CSV downloads the rows shown,
// every cell with its state and source.

import { LinkTierChip } from "@/components/precision/ProvChip";
import { LinkAssumptions } from "@/components/LinkAssumptions";
import { ChevronDown, ChevronRight, Download, Network, Search, X } from "lucide-react";
import { Fragment, useState, type ReactNode } from "react";

import type { ArchitectureResponse, Cell, Link, LinkMember } from "@/api/architecture";
import { useLabelStyle } from "@/hooks/useLabels";
import { formatCount } from "@/lib/count";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { FAMILY_TEXT, Glyph } from "@/sections/architecture/Glyph";
import { NotProvenList, shortName } from "@/sections/architecture/Inspector";
import { endpointName } from "@/sections/architecture/model";
import { arrangeNotProven, notProvenCount } from "@/sections/architecture/notProven";

/** ChainFilter limits the ledger to the links a chain crosses. */
export interface ChainFilter {
  ids: ReadonlySet<string>;
  name: string;
  onClear: () => void;
}

function csvField(v: string | number): string {
  const t = String(v);
  return /[",\n]/.test(t) ? `"${t.replace(/"/g, '""')}"` : t;
}

/** memberPath is an HTTP member's path as served (the route's stored
 * full path, as the chain shows it), else the pattern as written. */
export function memberPath(m: LinkMember): string | null {
  return m.full_path ?? m.prefix_path ?? m.path;
}

/** memberText is a member in the CSV: method, served path and the
 * pattern as written when it differs. */
function memberText(m: LinkMember): string {
  const p = memberPath(m) ?? m.command ?? m.target;
  const shown = m.full_path ?? m.prefix_path;
  const written = shown !== null && m.path !== null && m.path !== shown ? ` (as written ${m.path})` : "";
  return `${m.method !== null ? `${m.method} ` : ""}${p}${written}`;
}

/** linksCsv writes links as CSV: endpoints, family and row, counts, and
 * each cell's value, state and source (the ledger's columns). */
export function linksCsv(data: ArchitectureResponse, links: readonly Link[]): string {
  const head = ["from", "to", "family", "row", "count", "back_count", "tiers", "protocol", "protocol_state", "protocol_source", "auth", "auth_state", "auth_source", "tls", "tls_state", "tls_source", "members"];
  const rows = links.map((l) => [
    endpointName(data, l.source),
    endpointName(data, l.target),
    l.family,
    l.row,
    l.count,
    l.back_count,
    Object.entries(l.tiers)
      .map(([t, n]) => `${t} x${String(n)}`)
      .join("; "),
    ...[l.cells.protocol, l.cells.auth, l.cells.tls].flatMap((c) => [c.value, c.state, c.source]),
    l.members.map(memberText).join("; "),
  ]);
  return [head, ...rows].map((r) => r.map(csvField).join(",")).join("\n") + "\n";
}

const FAMILIES = ["http", "server_action", "tauri_ipc", "in_process", "sql"];

function CellCell({ cell, lead }: { cell: Cell; lead?: ReactNode }) {
  const style = useLabelStyle();
  const state = label(style, "cellState", cell.state);
  const source = label(style, "cellSource", { source: cell.source, state: cell.state, rows: cell.rows });
  return (
    <td className="px-3 py-2 align-top">
      {lead}
      <div className="flex flex-wrap items-baseline gap-x-1.5">
        <span className={cn("font-medium", cell.state === "unknown" && "rounded border border-dashed border-never-bar px-1 text-muted-foreground")}>{cell.value}</span>
        <span className="text-xs text-muted-foreground" title={state.title}>
          {state.text}
        </span>
      </div>
      <div className="text-xs break-words text-muted-foreground" title={source.title}>
        {source.text}
      </div>
      {cell.counts !== null && (
        <div className="mt-0.5 text-xs">
          {Object.entries(cell.counts).map(([v, n]) => (
            <span key={v} className="mr-2 inline-block">
              {v} <b className="font-medium">×{formatCount(n)}</b>
            </span>
          ))}
        </div>
      )}
    </td>
  );
}

function hasUnknown(l: Link): boolean {
  return [l.cells.protocol, l.cells.auth, l.cells.tls].some((c) => c.state === "unknown" || c.state === "mixed");
}

function matches(data: ArchitectureResponse, l: Link, q: string): boolean {
  if (q === "") return true;
  const hay = [endpointName(data, l.source), endpointName(data, l.target), l.source, l.target, l.family, ...l.methods, ...l.members.map((m) => `${m.full_path ?? ""} ${m.path ?? ""} ${m.command ?? ""} ${m.source}`)].join(" ").toLowerCase();
  return hay.includes(q);
}

function LinkRow({ data, l, open, onToggle }: { data: ArchitectureResponse; l: Link; open: boolean; onToggle: () => void }) {
  const style = useLabelStyle();
  const fam = label(style, "linkFamily", { family: l.family, row: l.row });
  const canOpen = l.members.length > 0;
  return (
    <>
      <tr className={cn("border-t", open && "bg-brand-soft/40")} data-testid="link-row" data-link={l.id}>
        <td className="px-3 py-2 align-top">
          <button
            type="button"
            disabled={!canOpen}
            aria-expanded={canOpen ? open : undefined}
            className="flex items-start gap-1 text-left font-medium disabled:cursor-default"
            onClick={onToggle}
            title={canOpen ? "Show the stored members" : "This family lists no members"}
          >
            {canOpen ? open ? <ChevronDown aria-hidden="true" className="mt-0.5 size-3.5 flex-none" /> : <ChevronRight aria-hidden="true" className="mt-0.5 size-3.5 flex-none" /> : <span className="w-3.5 flex-none" />}
            <span>
              {endpointName(data, l.source)} {l.family === "in_process" ? "⇄" : "→"} {endpointName(data, l.target)}
              {l.repository !== null && (
                <span className="block text-xs font-normal text-muted-foreground" title={l.assumptions.join("\n")} data-testid="link-across">
                  linked across repositories · {l.assumptions[0] ?? "assumption not stated"}
                </span>
              )}
            </span>
          </button>
        </td>
        <CellCell
          cell={l.cells.protocol}
          lead={
            <div className="mb-1 flex items-center gap-1.5">
              <span className={cn("rounded-[4px] px-1.5 py-0.5 text-xs font-semibold", FAMILY_TEXT[l.family] ?? "bg-muted")} title={fam.title}>
                {fam.text}
              </span>
              <Glyph cells={l.cells} family={l.family} />
            </div>
          }
        />
        <td className="px-3 py-2 align-top whitespace-nowrap">
          <div className="font-medium tabular-nums">
            {formatCount(l.count)}
            {l.back_count > 0 && <span className="font-normal text-muted-foreground"> · {formatCount(l.back_count)} back</span>}
          </div>
          <div className="text-xs text-muted-foreground">
            {Object.entries(l.tiers).map(([t, n]) => {
              return (
                <span key={t} className="mr-1.5 inline-block">
                  <LinkTierChip tier={t} size="sm" /> ×{formatCount(n)}
                </span>
              );
            })}
          </div>
          <div className="text-xs text-muted-foreground">
            {Object.entries(l.by_kind)
              .map(([k, n]) => `${label(style, "nodeKind", k).text} ${formatCount(n)}`)
              .join(" · ")}
          </div>
        </td>
        <CellCell cell={l.cells.auth} />
        <CellCell cell={l.cells.tls} />
      </tr>
      {open && (
        <tr className="bg-brand-soft/20">
          <td colSpan={5} className="px-3 pb-3">
            <ul className="grid max-h-[320px] gap-0.5 overflow-auto rounded-md border bg-card p-1.5 text-[12.5px]" data-testid="link-members">
              {l.members.map((m) => (
                <li key={`${m.source}>${m.target}`} className="grid grid-cols-[minmax(0,2fr)_minmax(0,2fr)_auto_minmax(0,2fr)] gap-x-3 px-1.5 py-0.5">
                  <span className="truncate font-mono" title={m.source}>
                    {m.source_repository !== null && <span className="font-sans text-muted-foreground">{m.source_repository}: </span>}
                    {shortName(m.source)}
                  </span>
                  <span className="truncate font-mono" title={m.target}>
                    {m.target_repository !== null && <span className="font-sans text-muted-foreground">{m.target_repository}: </span>}
                    {m.method !== null ? `${m.method} ` : ""}
                    {memberPath(m) ?? m.command ?? shortName(m.target)}
                    {(m.full_path ?? m.prefix_path) !== null && m.path !== null && m.path !== (m.full_path ?? m.prefix_path) && (
                      <span className="ml-1.5 font-sans text-muted-foreground">as written {m.path}</span>
                    )}
                  </span>
                  <span>
                    <LinkTierChip tier={m.tier} size="sm" />
                  </span>
                  <span className="truncate text-muted-foreground" title={m.auth ?? ""}>
                    {m.auth ?? ""}
                  </span>
                  {m.assumptions.length > 0 && <LinkAssumptions list={m.assumptions} className="col-span-4" />}
                </li>
              ))}
            </ul>
          </td>
        </tr>
      )}
    </>
  );
}

/** LinksTab renders the ledger of every link. */
export function LinksTab({ data, chain = null }: { data: ArchitectureResponse; chain?: ChainFilter | null }) {
  const style = useLabelStyle();
  const [q, setQ] = useState("");
  const [family, setFamily] = useState("all");
  const [unknownOnly, setUnknownOnly] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const s = q.trim().toLowerCase();
  const keep = (l: Link) => (chain === null || chain.ids.has(l.id)) && (family === "all" || l.family === family) && (!unknownOnly || hasUnknown(l)) && matches(data, l, s);
  const between = data.links.filter((l) => l.family !== "sql" && keep(l));
  const sql = data.links.filter((l) => l.family === "sql" && keep(l));
  const stubs = family === "all" && !unknownOnly && chain === null ? data.stubs.filter((st) => s === "" || endpointName(data, st.component).toLowerCase().includes(s)) : [];
  const docs = family === "all" && !unknownOnly && chain === null ? data.documents : [];
  const csv = `data:text/csv;charset=utf-8,${encodeURIComponent(linksCsv(data, [...between, ...sql]))}`;
  const notProven = arrangeNotProven(data, data.not_proven);
  const families = [...new Set([...FAMILIES.filter((f) => data.links.some((l) => l.family === f)), ...data.links.map((l) => l.family)])];
  const groups: { title: string; links: Link[] }[] = [
    { title: "Between components", links: between },
    { title: "To databases", links: sql },
  ];
  const th = "px-3 py-2 text-left text-xs font-medium text-muted-foreground";
  return (
    <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_320px] overflow-hidden max-[1100px]:grid-cols-1 max-[1100px]:overflow-auto" data-testid="links-tab">
      <div className="flex min-h-0 min-w-0 flex-col max-[1100px]:min-h-fit">
        <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2.5">
          <span className="text-sm font-semibold">Links</span>
          <span className="rounded-md bg-muted px-1.5 py-0.5 text-xs">{data.links.length} links</span>
          <label className="flex h-8 min-w-[180px] flex-1 items-center gap-1.5 rounded-md border bg-background px-2 text-[13px] sm:max-w-[320px]">
            <Search aria-hidden="true" className="size-3.5 text-muted-foreground" />
            <input
              className="min-w-0 flex-1 bg-transparent outline-none"
              placeholder="Filter links, components, paths…"
              aria-label="Filter links"
              value={q}
              onChange={(e) => {
                setQ(e.target.value);
              }}
            />
          </label>
          <select
            className="h-8 rounded-md border bg-background px-2 text-[13px]"
            aria-label="Protocol"
            value={family}
            onChange={(e) => {
              setFamily(e.target.value);
            }}
          >
            <option value="all">Protocol: all</option>
            {families.map((f) => (
              <option key={f} value={f}>
                {label(style, "linkFamily", { family: f, row: "" }).text}
              </option>
            ))}
          </select>
          {chain !== null && (
            <span className="inline-flex h-8 items-center gap-1.5 rounded-md border border-brand bg-brand-soft px-2 text-[13px] text-brand-text" data-testid="links-chain-filter">
              <Network aria-hidden="true" className="size-3.5" />
              Chain of <span className="max-w-[220px] truncate font-mono text-[12px]">{chain.name}</span>
              <button type="button" className="grid size-5 place-items-center rounded hover:bg-card" aria-label="Show every link" title="Show every link" onClick={chain.onClear}>
                <X aria-hidden="true" className="size-3.5" />
              </button>
            </span>
          )}
          <button
            type="button"
            aria-pressed={unknownOnly}
            className={cn("h-8 rounded-md border px-2.5 text-[13px]", unknownOnly ? "border-brand bg-brand-soft text-brand-text" : "bg-card hover:bg-accent")}
            onClick={() => {
              setUnknownOnly((v) => !v);
            }}
          >
            Has unknown
          </button>
          <a href={csv} download={`${data.repository}-links.csv`} className="inline-flex h-8 items-center gap-1 rounded-md border bg-card px-2.5 text-[13px] hover:bg-accent" data-testid="links-csv" title="The rows shown, every cell with its state and source">
            <Download aria-hidden="true" className="size-3.5" />
            CSV
          </a>
        </div>
        <div className="min-h-0 flex-1 overflow-auto max-[1100px]:overflow-x-auto max-[1100px]:overflow-y-visible">
          <table className="w-full min-w-[760px] border-collapse text-[13px]">
            <thead className="sticky top-0 z-[1] bg-card">
              <tr className="border-b">
                <th className={cn(th, "w-[20%]")}>From → To</th>
                <th className={cn(th, "w-[22%]")}>Protocol</th>
                <th className={cn(th, "w-[14%]")}>Links</th>
                <th className={th}>Auth</th>
                <th className={th}>TLS</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) =>
                g.links.length === 0 ? null : (
                  <Fragment key={g.title}>
                    <tr className="bg-muted/60">
                      <td colSpan={5} className="px-3 py-1.5 text-[11.5px] font-semibold tracking-wide text-muted-foreground uppercase">
                        {g.title} · {g.links.length}
                      </td>
                    </tr>
                    {g.links.map((l) => (
                      <LinkRow key={l.id} data={data} l={l} open={open === l.id} onToggle={() => { setOpen((o) => (o === l.id ? null : l.id)); }} />
                    ))}
                  </Fragment>
                ),
              )}
              {between.length === 0 && sql.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-3 py-6 text-center text-muted-foreground">
                    No link matches.
                  </td>
                </tr>
              )}
              {stubs.length > 0 && (
                <>
                  <tr className="bg-muted/60">
                    <td colSpan={5} className="px-3 py-1.5 text-[11.5px] font-semibold tracking-wide text-muted-foreground uppercase">
                      SDK imports · no call proven · {stubs.length}
                    </td>
                  </tr>
                  {stubs.map((st) => {
                    const l = label(style, "stub", { text: st.text, row: st.row });
                    return (
                      <tr key={st.component} className="border-t" data-testid="stub-row">
                        <td className="px-3 py-2 font-medium">{endpointName(data, st.component)} → external packages</td>
                        <CellCell cell={st.protocol} />
                        <td className="px-3 py-2" title={l.title}>
                          {l.text}
                        </td>
                        <td className="px-3 py-2 text-xs text-muted-foreground">-</td>
                        <td className="px-3 py-2 text-xs text-muted-foreground">-</td>
                      </tr>
                    );
                  })}
                </>
              )}
              {docs.length > 0 && (
                <>
                  <tr className="bg-muted/60">
                    <td colSpan={5} className="px-3 py-1.5 text-[11.5px] font-semibold tracking-wide text-muted-foreground uppercase">
                      Documents · no map line · {docs.length}
                    </td>
                  </tr>
                  {docs.map((d) => (
                    <tr key={`${d.source}>${d.target}`} className="border-t">
                      <td className="px-3 py-2 font-medium">
                        {endpointName(data, d.source)} → {endpointName(data, d.target)}
                      </td>
                      <td className="px-3 py-2 text-xs text-muted-foreground">documents</td>
                      <td colSpan={3} className="px-3 py-2 text-xs">
                        {Object.entries(d.by_kind)
                          .map(([k, n]) => `${label(style, "nodeKind", k).text} ${formatCount(n)}`)
                          .join(" · ")}
                        <span className="text-muted-foreground"> · feeds the documented auth of the HTTP links</span>
                      </td>
                    </tr>
                  ))}
                </>
              )}
            </tbody>
          </table>
        </div>
      </div>
      <aside className="min-h-0 overflow-auto border-l bg-card px-4 py-3 max-[1100px]:border-t max-[1100px]:border-l-0" aria-label="Not proven">
        <h3 className="mb-2 text-[11.5px] font-semibold tracking-wide text-muted-foreground uppercase">Not proven · {notProvenCount(notProven)}</h3>
        {data.not_proven.length > 0 ? <NotProvenList data={data} items={data.not_proven} /> : <p className="text-xs text-muted-foreground">The answer lists no entry.</p>}
        {Object.keys(data.not_drawn).length > 0 && (
          <>
            <h3 className="mt-4 mb-1.5 text-[11.5px] font-semibold tracking-wide text-muted-foreground uppercase">Counted, not drawn</h3>
            <ul className="grid gap-0.5 text-[12.5px]">
              {Object.entries(data.not_drawn).map(([k, n]) => (
                <li key={k} className="flex justify-between">
                  <span title={label(style, "nodeKind", k).title}>{label(style, "nodeKind", k).text}</span>
                  <span className="tabular-nums">{formatCount(n)}</span>
                </li>
              ))}
            </ul>
          </>
        )}
      </aside>
    </div>
  );
}
