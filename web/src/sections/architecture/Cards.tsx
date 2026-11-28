// The map's cards (combination X+): component and database cards, a
// component expanded in place, and the libraries band with its chips
// (T067, T062 variant A). Cards show stored
// facts only: kinds from the server's CK rows, deploy markers as text
// (never a line), calls without a link as a dashed chip (never a line), an
// engine only when the answer proves one.

import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import {
  ArrowUpRight,
  BookOpen,
  Braces,
  Container,
  Cpu,
  Database,
  FileQuestion,
  FileText,
  FolderGit2,
  Globe,
  Maximize2,
  Minimize2,
  Monitor,
  Package,
  Search,
  Server,
  Table2,
  Terminal,
  type LucideIcon,
} from "lucide-react";
import { useState, type KeyboardEvent, type ReactNode } from "react";

import type { Across, Component } from "@/api/architecture";
import { Skeleton } from "@/components/ui/skeleton";
import { useLabelStyle } from "@/hooks/useLabels";
import { formatCount } from "@/lib/count";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { useMapActions } from "@/sections/architecture/context";
import { KindTextPlain, mainKind } from "@/sections/architecture/kindText";
import { FAMILY_TEXT, Glyph } from "@/sections/architecture/Glyph";
import { BAND_FOOT_H, EXP_HEADER, FAN_H, FILTER_H, FOOTER_H, LIST_ROWS_H } from "@/sections/architecture/layout";
import { databaseTitle, type BandRow, type MapNode } from "@/sections/architecture/model";
import { CONTAINER_HEAD } from "@/sections/architecture/layout";
import { memberPath, memberTitle } from "@/sections/architecture/chain";
import { filterRows, isList, type Row } from "@/sections/architecture/rows";

/** bandRows: the band's row tops (the band node only). */
export type CardNode = Node<{ node: MapNode; width: number; height: number; bandRows?: number[] | null }, "card">;

const KIND_ICON: Record<string, LucideIcon> = {
  server: Server,
  client: Monitor,
  tauri_core: Cpu,
  schema_source: Database,
  binary: Terminal,
  docs_site: BookOpen,
  package: Package,
  unknown: FileQuestion,
};

/** KindText is a component kind in the applied label style. */
function KindText({ c, kind, className }: { c: Component; kind: string; className?: string }) {
  return <KindTextPlain c={c} kind={kind} className={className ?? ""} />;
}

const chip = "inline-flex h-[22px] max-w-full items-center gap-1 truncate rounded-[5px] px-1.5 text-xs whitespace-nowrap";

function Chip({ children, dashed = false, title }: { children: ReactNode; dashed?: boolean; title?: string }) {
  return (
    <span title={title} className={cn(chip, dashed ? "border border-dashed border-never-bar text-muted-foreground" : "bg-muted text-foreground")}>
      {children}
    </span>
  );
}

function plural(n: number, one: string, many: string): string {
  return `${formatCount(n)} ${n === 1 ? one : many}`;
}

function countChips(c: Component): string[] {
  const k = c.counts;
  const out: string[] = [];
  if (k.routes > 0) out.push(plural(k.routes, "route", "routes"));
  if (k.ipc_commands > 0) out.push(plural(k.ipc_commands, "IPC command", "IPC commands"));
  if (k.client_calls > 0) out.push(plural(k.client_calls, "client call", "client calls"));
  if (k.ipc_calls > 0) out.push(plural(k.ipc_calls, "IPC call", "IPC calls"));
  if (k.tables > 0) out.push(plural(k.tables, "table", "tables"));
  out.push(plural(k.files, "file", "files"));
  return out;
}

/** cardKey turns Enter and Space on a card into a click. */
function cardKey(e: KeyboardEvent<HTMLElement>, act: () => void) {
  if (e.key === "Enter" || e.key === " ") {
    e.preventDefault();
    act();
  }
}

function Frame({
  id,
  name,
  dim,
  selected,
  muted = false,
  dashed = false,
  children,
  onSelect,
}: {
  id: string;
  name: string;
  dim: boolean;
  selected: boolean;
  muted?: boolean;
  dashed?: boolean;
  children: ReactNode;
  onSelect: () => void;
}) {
  return (
    <div
      role="button"
      tabIndex={0}
      aria-pressed={selected}
      aria-label={name}
      data-arch-card={id}
      data-dim={dim ? "true" : undefined}
      onClick={(e) => {
        e.stopPropagation();
        onSelect();
      }}
      onKeyDown={(e) => {
        cardKey(e, onSelect);
      }}
      className={cn(
        "relative flex h-full w-full cursor-pointer flex-col gap-1.5 overflow-hidden rounded-[10px] border bg-card px-3 py-2.5 text-left shadow-[0_1px_2px_hsl(240_10%_4%/.05)] outline-none transition-opacity focus-visible:ring-2 focus-visible:ring-ring",
        dashed && "border-dashed border-never-bar",
        muted && "bg-canvas text-muted-foreground shadow-none",
        selected && "border-brand ring-2 ring-brand/35",
        dim && "opacity-35",
      )}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} />
      {children}
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </div>
  );
}

function Head({ icon: Icon, name, right, tone, extra }: { icon: LucideIcon; name: string; right: ReactNode; tone?: string; extra?: ReactNode }) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span className={cn("grid size-6 flex-none place-items-center rounded-md", tone ?? "bg-brand-soft text-brand-text")}>
        <Icon aria-hidden="true" className="size-3.5" />
      </span>
      <span className="min-w-0 truncate text-[14.5px] font-semibold" title={name}>
        {name}
      </span>
      <span className="ml-auto min-w-0 flex-none truncate text-xs text-muted-foreground">{right}</span>
      {extra}
    </div>
  );
}

function ComponentCard({ node, dim, selected }: { node: Extract<MapNode, { type: "component" }>; dim: boolean; selected: boolean }) {
  const a = useMapActions();
  const style = useLabelStyle();
  const c = node.component;
  const kind = mainKind(c);
  const Icon = KIND_ICON[kind] ?? Package;
  const manifests = c.manifests.map((m) => m.file.split("/").pop() ?? m.file);
  const dir = c.directory === "." ? "./" : `${c.directory}/`;
  const stub = node.stub;
  const stubText = stub !== null ? label(style, "stub", { text: stub.text, row: stub.row }) : null;
  return (
    <Frame id={node.id} name={`Component ${c.name}`} dim={dim} selected={selected} onSelect={() => { a.selectNode(node.id); }}>
      <Head
        icon={Icon}
        name={c.name}
        right={node.narrow ? null : <KindText c={c} kind={kind} />}
        extra={
          <button
            type="button"
            className="nodrag -my-1 -mr-1 grid size-6 flex-none place-items-center rounded-md text-muted-foreground hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
            aria-label={`Expand ${c.name} in place`}
            title={`Expand ${c.name} in place`}
            onClick={(e) => {
              e.stopPropagation();
              a.expand(c.id);
            }}
          >
            <Maximize2 aria-hidden="true" className="size-3.5" />
          </button>
        }
      />
      {node.narrow ? (
        <div className="truncate text-xs text-muted-foreground" title={[dir, ...manifests].join(" · ")}>
          <KindText c={c} kind={kind} className="text-foreground/75" /> · <span className="font-mono">{dir}</span>
        </div>
      ) : (
        <div className="truncate font-mono text-xs text-muted-foreground" title={[dir, ...manifests].join(" · ")}>
          {[dir, ...manifests].join(" · ")}
        </div>
      )}
      <div className="flex min-w-0 flex-wrap gap-1 overflow-hidden" style={{ maxHeight: 22 }}>
        {c.state === "unread" && (
          <Chip dashed title={c.reason ?? "manifest not read"}>
            manifest unread
          </Chip>
        )}
        {countChips(c)
          .slice(0, -1)
          .map((t) => (
            <Chip key={t}>{t}</Chip>
          ))}
        {node.ghosts.map((g) => {
          const l = label(style, "ghost", { total: g.total, row: g.row, by_status: g.by_status });
          return (
            <Chip key={g.family} dashed title={`${l.title}; never a link`}>
              {l.text}
            </Chip>
          );
        })}
        <Chip>{countChips(c).at(-1)}</Chip>
        {stub !== null && stubText !== null && (
          <Chip dashed title={`SDK imports: ${stubText.text}. ${stubText.title}; protocol ${stub.protocol.value} (${stub.protocol.source}); never a line`}>
            SDK {formatCount(stub.packages)}
          </Chip>
        )}
      </div>
      {(c.markers.length > 0 || c.ingress.length > 0) && <CardMarkers c={c} />}
      {node.across !== null && <AcrossLine across={node.across} />}
    </Frame>
  );
}

/** AcrossLine is a project member's calls the stored link pass links into
 * another member (CE8): counted as linked across repositories, with the
 * repository's own count of calls without a link as secondary text. */
export function AcrossLine({ across }: { across: Across }) {
  const repos = across.repositories.join(", ");
  const parts = [...(across.linked_across > 0 ? [plural(across.linked_across, "call", "calls")] : []), ...(across.operations_across > 0 ? [plural(across.operations_across, "operation", "operations")] : [])];
  const alone = [...(across.linked_across > 0 ? [`${formatCount(across.repository_not_linked)} not linked`] : []), ...(across.operations_across > 0 ? [`${formatCount(across.repository_no_route)} documented, no route`] : [])];
  const title = `${parts.join(" and ")} without a link or route in this repository that the project's stored links connect to ${repos} (under the cross-repository assumption; see the line's inspector and the Links tab). This repository alone: ${alone.join(", ")}.`;
  return (
    <div className="grid min-w-0 gap-1 text-xs text-muted-foreground" data-testid="card-across" title={title}>
      <div className="flex min-w-0 items-center gap-1">
        <ArrowUpRight aria-hidden="true" className="size-3 flex-none" />
        <span className="min-w-0 truncate rounded-[4px] bg-http-soft px-1 text-[11px] font-medium text-http">{parts.join(" · ")} linked across repositories</span>
      </div>
      <span className="truncate pl-4">repository alone: {alone.join(" · ")}</span>
    </div>
  );
}

/** RepositoryCard is another project member's muted card (CE8): the
 * stored project links end on it; its own components are not drawn. */
function RepositoryCard({ node, dim, selected }: { node: Extract<MapNode, { type: "repository" }>; dim: boolean; selected: boolean }) {
  const a = useMapActions();
  const m = node.member;
  return (
    <Frame id={node.id} name={`Other repository ${m.repository}`} dim={dim} selected={selected} muted dashed onSelect={() => { a.selectNode(node.id); }}>
      <Head icon={FolderGit2} name={m.repository} right="other repository" tone="bg-muted text-muted-foreground" />
      <div className="flex min-w-0 flex-wrap gap-1 overflow-hidden" style={{ maxHeight: 22 }}>
        {m.calls_out > 0 && <Chip title="calls of this repository the stored project links connect to its routes">{plural(m.calls_out, "call", "calls")} to it</Chip>}
        {m.calls_in > 0 && <Chip title="its calls the stored project links connect to this repository's routes">{plural(m.calls_in, "call", "calls")} from it</Chip>}
        {m.operations_out > 0 && <Chip title="OpenAPI operations of this repository the stored project links connect to its routes (documents)">{plural(m.operations_out, "operation", "operations")} documenting it</Chip>}
        {m.operations_in > 0 && <Chip title="its OpenAPI operations the stored project links connect to this repository's routes (documents)">{plural(m.operations_in, "operation", "operations")} from it</Chip>}
      </div>
      <div className="truncate text-xs" title={node.assumptions.join("\n")}>
        linked across repositories · {node.assumptions[0] ?? "assumption not stated"}
      </div>
    </Frame>
  );
}

/** CardMarkers is a card's deploy line: AT2 ingress first (TLS as
 * configured, never a link's TLS), then the deploy markers with their
 * basis. Text only, never a line. */
function CardMarkers({ c }: { c: Component }) {
  const style = useLabelStyle();
  const ingress = c.ingress.map((g) => label(style, "ingress", { text: g.text, row: g.row, basis: g.basis }));
  const markers = c.markers.map((m) => label(style, "marker", { label: m.label, row: m.row }));
  // A CD4 basis stays on the chip and the short line ("assumed"
  // when the files are assumed combined, "merged" by Compose's default).
  const basisWord = (b: string | null) => (b === null ? null : b.startsWith("assumption") ? "assumed" : "merged");
  const short = [...c.ingress.map((g) => `ingress TLS as configured (${[g.router, basisWord(g.basis)].filter((x) => x !== null).join(", ")})`), ...markers.map((m) => m.text)];
  const chipBasis = [...new Set(c.ingress.map((g) => basisWord(g.basis)).filter((x) => x !== null))];
  const title = [...ingress.map((l) => `${l.text}\n  ${l.title}`), ...markers.map((m) => m.text), ...c.markers.filter((m) => m.basis !== null).map((m) => `basis: ${m.basis ?? ""}`)].join("\n");
  return (
    <div className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground" data-testid="card-markers">
      <Container aria-hidden="true" className="size-3 flex-none" />
      {c.ingress.length > 0 && (
        <span className="flex-none rounded-[4px] bg-wrapper-soft px-1 text-[11px] font-medium text-wrapper" data-testid="card-ingress" title={ingress.map((l) => l.title).join("\n")}>
          TLS as configured{chipBasis.length > 0 ? `, ${chipBasis.join(" / ")}` : ""}
        </span>
      )}
      <span className="truncate" title={title}>
        {short.join(" · ")}
      </span>
    </div>
  );
}

function DatabaseCard({ node, dim, selected }: { node: Extract<MapNode, { type: "database" }>; dim: boolean; selected: boolean }) {
  const a = useMapActions();
  const d = node.database;
  const may = d.accessors.reduce((s, x) => s + x.may_also_use, 0);
  return (
    <Frame id={node.id} name={`Database ${databaseTitle(d)}`} dim={dim} selected={selected} onSelect={() => { a.selectNode(node.id); }}>
      <Head icon={Database} name={databaseTitle(d)} right="Database" />
      <div className="truncate font-mono text-xs text-muted-foreground" title={d.set}>
        {d.set}
      </div>
      <div className="flex min-w-0 flex-wrap gap-1 overflow-hidden" style={{ maxHeight: 22 }}>
        <Chip>{plural(d.tables, "table", "tables")}</Chip>
        <Chip dashed={d.engine.state !== "proven"} title={d.engine.source}>
          {d.engine.value}
        </Chip>
        {may > 0 && (
          <Chip dashed title="table-access sites bound to no table; never a link">
            may also use {formatCount(may)}
          </Chip>
        )}
      </div>
    </Frame>
  );
}

const METHOD_TONE: Record<string, string> = { GET: "text-m-get", POST: "text-m-post", PUT: "text-m-put", PATCH: "text-m-patch", DELETE: "text-m-delete" };

function RowView({ row, selected, onSelect }: { row: Row; selected: boolean; onSelect: (id: string) => void }) {
  const style = useLabelStyle();
  if (row.type === "group") {
    return <div className="flex h-[26px] items-center px-2 pt-1 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">{row.text}</div>;
  }
  if (row.type === "file") {
    const lvl = label(style, "levelTag", 3);
    return (
      <div className="flex h-[26px] min-w-0 items-center gap-1.5 px-2 text-[12.5px]" data-row="file">
        <span className="rounded bg-muted px-1 font-mono text-[10px] text-muted-foreground" title={lvl.title}>
          {lvl.text}
        </span>
        <FileText aria-hidden="true" className="size-3.5 flex-none text-muted-foreground" />
        <span className="min-w-0 truncate font-mono font-medium" title={row.file}>
          {row.display}
        </span>
        <span className="ml-auto flex-none text-xs text-muted-foreground">{row.text}</span>
      </div>
    );
  }
  const lvl = label(style, "levelTag", 4);
  return (
    <button
      type="button"
      aria-pressed={selected}
      data-row="node"
      data-identity={row.node.identity}
      onClick={(e) => {
        e.stopPropagation();
        onSelect(row.node.identity);
      }}
      className={cn(
        "flex h-[26px] w-full min-w-0 items-center gap-2 rounded-md pr-2 pl-6 text-left font-mono text-[12px] outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring",
        selected && "bg-brand-soft ring-1 ring-brand",
      )}
    >
      <span className="sr-only">{lvl.text}</span>
      {row.method !== null && <span className={cn("w-[52px] flex-none text-[11px] font-semibold", METHOD_TONE[row.method] ?? "text-foreground")}>{row.method}</span>}
      <span className="min-w-0 truncate" title={`${row.text} (${row.node.file}${row.node.span !== null ? `:${String(row.node.span.start_line)}` : ""})`}>
        {row.text}
      </span>
      {row.note !== null && <span className="ml-auto flex-none font-sans text-xs text-muted-foreground">{row.note}</span>}
    </button>
  );
}

function ExpandedCard({ node, dim, selected }: { node: Extract<MapNode, { type: "component" }>; dim: boolean; selected: boolean }) {
  const a = useMapActions();
  const style = useLabelStyle();
  const c = node.component;
  const kind = mainKind(c);
  const Icon = KIND_ICON[kind] ?? Package;
  const [q, setQ] = useState("");
  const st = a.expandedRows;
  const rows = st.kind === "ok" ? st.rows : [];
  const list = isList(rows);
  const shown = list ? filterRows(rows, q) : rows;
  const lvl = label(style, "levelTag", 2);
  const nodes = rows.filter((r) => r.type === "node").length;
  return (
    <div
      className={cn("flex h-full w-full flex-col overflow-hidden rounded-[12px] border-2 border-brand/60 bg-card shadow-[0_2px_6px_hsl(245_60%_30%/.10)] transition-opacity", dim && "opacity-35")}
      data-arch-card={node.id}
      data-expanded="true"
      data-dim={dim ? "true" : undefined}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} />
      <div
        role="button"
        tabIndex={0}
        aria-pressed={selected}
        aria-label={`Component ${c.name}, expanded`}
        className={cn("flex flex-none cursor-pointer flex-col gap-1 border-b px-3 py-2 outline-none focus-visible:ring-2 focus-visible:ring-ring", selected && "bg-brand-soft/60")}
        style={{ height: EXP_HEADER }}
        onClick={(e) => {
          e.stopPropagation();
          a.selectNode(node.id);
        }}
        onKeyDown={(e) => {
          cardKey(e, () => { a.selectNode(node.id); });
        }}
      >
        <div className="flex min-w-0 items-center gap-2">
          <span className="grid size-6 flex-none place-items-center rounded-md bg-brand-soft text-brand-text">
            <Icon aria-hidden="true" className="size-3.5" />
          </span>
          <span className="rounded bg-muted px-1 font-mono text-[10px] text-muted-foreground" title={lvl.title}>
            {lvl.text}
          </span>
          <span className="min-w-0 truncate text-[14.5px] font-semibold">{c.name}</span>
          <KindText c={c} kind={kind} className="flex-none truncate text-xs text-muted-foreground" />
          <button
            type="button"
            className="nodrag ml-auto grid size-6 flex-none place-items-center rounded-md border bg-card text-muted-foreground hover:bg-accent"
            aria-label={`Collapse ${c.name}`}
            title={`Collapse ${c.name}`}
            onClick={(e) => {
              e.stopPropagation();
              a.expand(null);
            }}
          >
            <Minimize2 aria-hidden="true" className="size-3.5" />
          </button>
        </div>
        <div className="truncate text-xs text-muted-foreground">
          expanded · {c.directory === "." ? "./" : `${c.directory}/`} · {st.kind === "ok" ? `${String(nodes)} ${nodes === 1 ? "row" : "rows"}` : "loading"}
        </div>
      </div>
      {st.kind === "loading" && (
        <div className="grid gap-1.5 p-3" aria-label="Loading the component">
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-4 w-1/2" />
        </div>
      )}
      {st.kind === "failed" && (
        <div className="p-3 text-xs text-unmatched">
          Could not load this component: {st.error}{" "}
          <button type="button" className="nodrag font-medium text-brand-text underline" onClick={a.retryRows}>
            Retry
          </button>
        </div>
      )}
      {st.kind === "ok" && rows.length === 0 && <div className="p-3 text-xs text-muted-foreground">No routes, calls, commands or tables are stored for this component.</div>}
      {st.kind === "ok" && list && (
        <label className="nodrag flex flex-none items-center gap-1.5 border-b px-3 text-xs" style={{ height: FILTER_H }}>
          <Search aria-hidden="true" className="size-3.5 text-muted-foreground" />
          <input
            className="h-7 min-w-0 flex-1 rounded-md border bg-background px-2 text-[12.5px] outline-none focus-visible:ring-2 focus-visible:ring-ring"
            placeholder="Filter routes, paths, files…"
            aria-label={`Filter the rows of ${c.name}`}
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
            }}
            onClick={(e) => {
              e.stopPropagation();
            }}
          />
        </label>
      )}
      {st.kind === "ok" && rows.length > 0 && (
        <div className={cn("nodrag nowheel min-h-0 px-1 py-1", list ? "overflow-y-auto" : "overflow-hidden")} style={list ? { height: LIST_ROWS_H } : undefined} data-testid="expanded-rows">
          {shown.map((r) => (
            <RowView key={r.key} row={r} selected={a.selectedItem === r.key} onSelect={a.selectItem} />
          ))}
          {list && shown.length === 0 && <div className="p-2 text-xs text-muted-foreground">No row matches “{q}”.</div>}
        </div>
      )}
      {st.kind === "ok" && list && (
        <div className="flex flex-none items-center border-t px-3 text-[11.5px] text-muted-foreground" style={{ height: FOOTER_H }}>
          {shown.filter((r) => r.type === "node").length} of {nodes} rows · scroll for more
        </div>
      )}
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </div>
  );
}

const MEMBER_ICON: Record<string, LucideIcon> = { table: Table2, route: Globe, client_call: Globe, ipc_command: Terminal, ipc_call: Terminal, component: Package, database: Database, file: FileText };

/** StepBadges are a chain's step numbers on a card or member (AG16). */
function StepBadges({ numbers, selected = false }: { numbers: readonly number[]; selected?: boolean }) {
  return (
    <span className="pointer-events-none absolute -top-2.5 -left-2.5 z-[2] flex gap-0.5" data-testid="step-badges">
      {numbers.map((n) => (
        <span key={n} className={cn("grid size-[22px] place-items-center rounded-full text-[11px] font-semibold text-white shadow-sm ring-2 ring-card", selected ? "bg-foreground" : "bg-brand")}>
          {n}
        </span>
      ))}
    </span>
  );
}

/** ContainerCard is a card opened to the chain's members only; the
 * members are their own nodes drawn on top of it. */
function ContainerCard({ node }: { node: Extract<MapNode, { type: "container" }> }) {
  const style = useLabelStyle();
  const card = node.card;
  const lvl = label(style, "levelTag", 2);
  const name = card.type === "component" || card.type === "library" ? card.component.name : card.type === "database" ? databaseTitle(card.database) : card.member.repository;
  const Icon = card.type === "component" ? (KIND_ICON[mainKind(card.component)] ?? Package) : card.type === "database" ? Database : card.type === "repository" ? FolderGit2 : Package;
  return (
    <div className="h-full w-full rounded-[14px] border-[1.5px] border-brand/45 bg-brand-soft/25" data-arch-card={node.id} data-container="true">
      <Handle type="target" position={Position.Top} isConnectable={false} />
      <div className="flex min-w-0 items-center gap-2 px-3" style={{ height: CONTAINER_HEAD }}>
        <span className="grid size-6 flex-none place-items-center rounded-md bg-brand-soft text-brand-text">
          <Icon aria-hidden="true" className="size-3.5" />
        </span>
        <span className="min-w-0 truncate text-[14.5px] font-semibold">{name}</span>
        <span className="rounded bg-muted px-1 font-mono text-[10px] text-muted-foreground" title={lvl.title}>
          {lvl.text}
        </span>
        <span className="truncate text-xs text-muted-foreground">
          {card.type === "component" && <KindText c={card.component} kind={mainKind(card.component)} />}
          {card.type === "database" && <span title={card.database.engine.source}>{card.database.engine.value}</span>}
          {card.type === "repository" && <span title={card.assumptions.join("\n")}>linked across repositories</span>}
          {card.type === "library" && "Library"}
          {" · chain only"}
        </span>
      </div>
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </div>
  );
}

const MEMBER_TONE: Record<string, string> = { GET: "text-m-get", POST: "text-m-post", PUT: "text-m-put", PATCH: "text-m-patch", DELETE: "text-m-delete" };

/** MemberCard is one drawn chain member. */
function MemberCard({ node }: { node: Extract<MapNode, { type: "member" }> }) {
  const a = useMapActions();
  const style = useLabelStyle();
  const m = node.member;
  const selected = a.chain?.selected === m.id;
  const start = a.chain?.view.response.start.id === m.id;
  const end = a.chain?.view.ends.find((e) => e.member === m.id);
  const ended = end !== undefined;
  const crosses = end?.row === "CH13";
  const Icon = MEMBER_ICON[m.kind] ?? Braces;
  const kind = label(style, "nodeKind", m.kind);
  const lvl = label(style, "levelTag", 4);
  const where = m.file !== null ? `${m.file.split("/").pop() ?? m.file}${m.line !== null ? `:${String(m.line)}` : ""}` : "";
  const title = memberTitle(m);
  const select = () => {
    a.selectMember(m.id);
  };
  return (
    <div className="relative h-full w-full">
      <StepBadges numbers={[node.number]} selected={selected} />
      <div
        role="button"
        tabIndex={0}
        aria-pressed={selected}
        aria-label={`Step ${String(node.number)}: ${kind.text} ${title}`}
        data-arch-member={m.id}
        onClick={(e) => {
          e.stopPropagation();
          select();
        }}
        onKeyDown={(e) => {
          cardKey(e, select);
        }}
        className={cn(
          "flex h-full w-full cursor-pointer flex-col justify-center gap-0.5 overflow-hidden rounded-[9px] border bg-card px-2.5 py-1.5 text-left shadow-[0_1px_2px_hsl(240_10%_4%/.06)] outline-none focus-visible:ring-2 focus-visible:ring-ring",
          start && "border-brand",
          selected && "border-brand ring-2 ring-brand/40",
        )}
      >
        <Handle type="target" position={Position.Top} isConnectable={false} />
        <div className="flex min-w-0 items-center gap-1.5">
          <Icon aria-hidden="true" className="size-3.5 flex-none text-brand-text" />
          {m.kind === "route" && m.method !== null && <span className={cn("flex-none font-mono text-[11px] font-semibold", MEMBER_TONE[m.method] ?? "")}>{m.method}</span>}
          <span className="min-w-0 truncate font-mono text-[12.5px] font-semibold" title={m.qualified_name}>
            {m.kind === "route" && memberPath(m) !== null ? memberPath(m) : title}
          </span>
          <span className="ml-auto flex-none rounded bg-muted px-1 font-mono text-[10px] text-muted-foreground" title={lvl.title}>
            {lvl.text}
          </span>
        </div>
        <div className="flex min-w-0 items-center gap-1.5 text-[11.5px] text-muted-foreground">
          <span className="flex-none" title={kind.title}>
            {kind.text}
          </span>
          <span className="min-w-0 truncate font-mono" title={m.file ?? ""}>
            {where}
          </span>
          {start && <span className="ml-auto flex-none rounded bg-brand-soft px-1 text-[10.5px] text-brand-text">start</span>}
          {ended && !start && !crosses && (
            <span className="ml-auto flex-none rounded border border-dashed border-never-bar px-1 text-[10.5px]" title="the chain ends here (CH11); see the inspector">
              ? not modelled
            </span>
          )}
          {crosses && !start && (
            <span className="ml-auto flex-none rounded bg-http-soft px-1 text-[10.5px] text-http" title={end.text}>
              continues in {m.repository ?? "another repository"}
            </span>
          )}
        </div>
        <Handle type="source" position={Position.Bottom} isConnectable={false} />
      </div>
    </div>
  );
}

/** FoldCard is a step's members over the map budget, counted. */
function FoldCard({ node }: { node: Extract<MapNode, { type: "fold" }> }) {
  const a = useMapActions();
  const l = label(useLabelStyle(), "chainFold", { count: node.count, number: node.number, hidden: node.hidden });
  const first = a.chain?.view.positions.find((p) => p.number === node.number)?.members.find((m) => !(a.chain?.view.positions.find((p) => p.number === node.number)?.shown.has(m.id) ?? false));
  return (
    <div className="relative h-full w-full">
      <StepBadges numbers={[node.number]} />
      <button
        type="button"
        className="flex h-full w-full items-center rounded-full border border-dashed border-brand/50 bg-card px-3 pl-4 text-left text-[12px] text-muted-foreground hover:bg-accent"
        title={l.title}
        data-arch-fold={node.id}
        onClick={(e) => {
          e.stopPropagation();
          if (first !== undefined) a.selectMember(first.id);
        }}
      >
        <Handle type="target" position={Position.Top} isConnectable={false} />
        <span className="truncate">{l.text}</span>
        <Handle type="source" position={Position.Bottom} isConnectable={false} />
      </button>
    </div>
  );
}

/** MapCard renders any card; React Flow's node type "card". */
export function MapCard({ data }: NodeProps<CardNode>) {
  const a = useMapActions();
  const n = data.node;
  const dim = a.focus !== null && !a.focus.nodes.has(n.id) && n.type !== "band" && n.type !== "more";
  const selected = a.selectedNode === n.id;
  const numbers = a.chain?.numbers.get(n.id);
  let body: ReactNode;
  switch (n.type) {
    case "container":
      body = <ContainerCard node={n} />;
      break;
    case "member":
      body = <MemberCard node={n} />;
      break;
    case "fold":
      body = <FoldCard node={n} />;
      break;
    case "component": {
      const card = n.expanded ? <ExpandedCard node={n} dim={dim} selected={selected} /> : <ComponentCard node={n} dim={dim} selected={selected} />;
      body =
        n.fan === null ? (
          card
        ) : (
          <>
            <div style={{ height: data.height - FAN_H }}>{card}</div>
            <FanChip node={n} fan={n.fan} dim={dim} on={(a.focus !== null && !dim) || a.hoverNodes.has(n.id)} />
          </>
        );
      break;
    }
    case "band":
      body = <BandCard node={n} rows={data.bandRows ?? []} />;
      break;
    case "library":
      body = <LibraryChip node={n} dim={dim} on={(a.focus !== null && !dim) || a.hoverNodes.has(n.id)} selected={selected} dirs={a.bandDirs} />;
      break;
    case "more":
      body = <MoreChip node={n} on={(a.focus !== null && a.focus.nodes.has(n.id)) || a.hoverNodes.has(n.id)} />;
      break;
    case "database":
      body = <DatabaseCard node={n} dim={dim} selected={selected} />;
      break;
    case "repository":
      body = <RepositoryCard node={n} dim={dim} selected={selected} />;
      break;
  }
  // The band lines of the card or chip under the pointer, or holding the
  // keyboard focus, light (T072).
  const hoverable = (n.type === "component" && n.fan !== null) || n.type === "library" || n.type === "more";
  return (
    <div
      className="group relative"
      style={{ width: data.width, height: data.height }}
      onMouseEnter={hoverable ? () => { a.setHover(n.id, "pointer"); } : undefined}
      onMouseLeave={hoverable ? () => { a.setHover(null, "pointer"); } : undefined}
      onFocus={hoverable ? () => { a.setHover(n.id, "focus"); } : undefined}
      onBlur={hoverable ? () => { a.setHover(null, "focus"); } : undefined}
    >
      {numbers !== undefined && numbers.length > 0 && <StepBadges numbers={numbers} />}
      {body}
    </div>
  );
}


/** FanChip is a card's line chip into the band (T062 A): the families,
 * the summed count of its card-library links, the libraries they reach and
 * the glyph; one line per library leaves from it. Selecting it selects
 * the card, which lights those libraries. */
function FanChip({ node, fan, dim, on }: { node: Extract<MapNode, { type: "component" }>; fan: NonNullable<Extract<MapNode, { type: "component" }>["fan"]>; dim: boolean; on: boolean }) {
  const a = useMapActions();
  const style = useLabelStyle();
  const fams = fan.families.map((f) => ({ f, l: label(style, "linkFamily", { family: f, row: "" }) }));
  const libs = `${formatCount(fan.libraries)} ${fan.libraries === 1 ? "library" : "libraries"}`;
  const name = `${fams.map((x) => x.l.text).join(" + ")} ${formatCount(fan.count)} to ${libs}`;
  return (
    <div className={cn("relative flex flex-col items-center transition-opacity", dim && "opacity-35")} style={{ height: FAN_H }}>
      <span className="h-3 w-px bg-edge" aria-hidden="true" />
      <button
        type="button"
        className={cn("nodrag nopan flex h-6 items-center gap-1 rounded-full border bg-card px-1 text-[11px] leading-none font-medium whitespace-nowrap shadow-[0_1px_2px_hsl(240_10%_4%/.06)] outline-none focus-visible:ring-2 focus-visible:ring-ring", on && "border-brand text-brand-text")}
        aria-label={`Line chip: ${name}`}
        title={`${name}: one line per library below. Select to light them; each line's cells in the card's inspector`}
        data-arch-fan={node.id}
        onClick={(e) => {
          e.stopPropagation();
          a.selectNode(node.id);
        }}
      >
        {fams.map(({ f, l }) => (
          <span key={f} className={cn("rounded-[4px] px-0.5 py-[3px] text-[10.5px] font-semibold", FAMILY_TEXT[f] ?? "bg-muted")} title={l.title}>
            {l.text}
          </span>
        ))}
        <span className="tabular-nums">
          {fan.both ? "⇄ " : ""}
          {formatCount(fan.count)} · {libs}
        </span>
        <Glyph cells={fan.cells} family={fan.families.length === 1 ? (fan.families[0] ?? null) : null} />
      </button>
    </div>
  );
}

/** rowText is a band row's label: plain words and the number (ADR 0078). */
export function rowText(row: BandRow): { label: string | null; sub: string | null } {
  switch (row.kind) {
    case "card":
      return { label: "Used by a card", sub: null };
    case "level":
      return { label: row.level === 1 ? "Used by libraries" : null, sub: `level ${String(row.level)}` };
    case "nouser":
      return { label: "No stored user", sub: null };
    case "none":
      return { label: "No stored link", sub: null };
  }
}

/** rowTitle explains a band row (the tooltip of its label). */
export function rowTitle(row: BandRow): string {
  switch (row.kind) {
    case "card":
      return "Libraries a stored link from a card reaches: on the top line under the cards with one or few libraries, where a straight line reaches them; the libraries of cards with many on the lines below, fed by a bus between the lines";
    case "level":
      return `Libraries used by libraries only, ${String(row.level)} below the libraries a card uses (the longest chain of stored library links; a cycle stops at its first repeat)`;
    case "nouser":
      return "Libraries with stored links of their own that no stored link ends at";
    case "none":
      return "Libraries no stored link starts or ends at in this answer; never read as unused (a link the indexer did not resolve is not stored)";
  }
}

/** BandCard is the band's background: row labels and the footer. */
function BandCard({ node, rows }: { node: Extract<MapNode, { type: "band" }>; rows: readonly number[] }) {
  const a = useMapActions();
  const b = node.band;
  // While a card or library is selected its neighbours' chips show the
  // stored counts with the selection instead of their users (T072).
  const selected = a.selectedNode !== null && a.marks.size > 0 ? (a.selectedNode.startsWith("lib:") ? "library" : a.selectedNode.startsWith("c:") ? "card" : null) : null;
  let users = 0;
  let used = 0;
  for (const mk of a.marks.values()) {
    if (mk.usedBy > 0) users++;
    if (mk.uses > 0) used++;
  }
  const marked = selected === "library" ? `selected: ↑ ${formatCount(users)} use it · ↓ it uses ${formatCount(used)} · stored counts` : selected === "card" ? `selected card uses ↓ ${formatCount(used)} · stored counts` : null;
  const footer =
    b.between > 0
      ? `· ${plural(b.between, "link", "links")} between them${marked !== null ? ` · ${marked}` : ", marked on the chips when one is selected · n = components using it"}`
      : b.cardLines > 0
        ? `· no stored link between them · ${marked ?? "n = components using it"}`
        : "· no stored link to a card or between them";
  return (
    <div className="relative h-full w-full rounded-[14px] border bg-muted/45" data-arch-band="true" aria-label="Libraries band">
      {b.rows.map((row, i) => {
        const t = rowText(row);
        return (
          <div key={`${row.kind}:${String(row.level)}`} className="absolute left-3.5 w-[104px] text-[12px] leading-[1.3] text-muted-foreground" style={{ top: (rows[i] ?? 0) + 6 }} title={rowTitle(row)} data-band-row={row.kind}>
            {t.label !== null && (
              <>
                <b className="font-medium text-foreground">{t.label}</b>
                <br />
              </>
            )}
            {t.sub !== null && `${t.sub} · `}
            {formatCount(row.total)}
          </div>
        );
      })}
      <div className="absolute inset-x-3.5 bottom-0 flex min-w-0 items-center gap-2 text-[12.5px] whitespace-nowrap" style={{ height: BAND_FOOT_H }} data-testid="band-footer">
        <b className="text-[14px] font-semibold">Libraries</b>
        <span>{plural(b.libraries, "workspace package", "workspace packages")}</span>
        <span className="min-w-0 truncate text-muted-foreground">
          {footer}
        </span>
        {b.prefix !== null && (
          <span className="ml-auto flex-none text-xs text-muted-foreground">
            names start with <span className="font-mono">{b.prefix}</span>
          </span>
        )}
      </div>
    </div>
  );
}

/** LibraryChip is one library in the band: its name without the shared
 * prefix, the number of components using it, the directory on a second
 * line when the band shows directories (else in the tooltip). */
function LibraryChip({ node, dim, on, selected, dirs }: { node: Extract<MapNode, { type: "library" }>; dim: boolean; on: boolean; selected: boolean; dirs: boolean }) {
  const a = useMapActions();
  const c = node.component;
  const dir = c.directory === "." ? "./" : `${c.directory}/`;
  const zero = node.users === 0;
  // While a card or library is selected, the chip's number is the stored
  // count of its links with the selection, with the direction (T072).
  const mark = a.marks.get(node.id) ?? null;
  return (
    <button
      type="button"
      aria-pressed={selected}
      aria-label={`Library ${c.name}`}
      title={`${c.name} · ${dir} · used by ${plural(node.users, "component", "components")} (stored links)${mark !== null ? ` · ${mark.uses > 0 ? `the selection uses it: ${formatCount(mark.uses)}` : ""}${mark.uses > 0 && mark.usedBy > 0 ? "; " : ""}${mark.usedBy > 0 ? `it uses the selection: ${formatCount(mark.usedBy)}` : ""} (stored links' counts)` : ""}`}
      data-arch-card={node.id}
      data-dim={dim ? "true" : undefined}
      data-mark={mark !== null ? (mark.usedBy > 0 && mark.uses > 0 ? "both" : mark.usedBy > 0 ? "user" : "used") : undefined}
      className={cn(
        "relative grid h-full w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-1.5 rounded-[7px] border bg-card px-2.5 text-left outline-none transition-opacity focus-visible:ring-2 focus-visible:ring-ring",
        dirs ? "content-center" : "",
        zero && "border-dashed text-muted-foreground",
        on && "border-brand bg-brand-soft/60 text-brand-text",
        mark !== null && mark.usedBy > 0 && mark.uses === 0 && "border-foreground/70 bg-card text-foreground",
        mark !== null && "gap-x-1 px-1.5",
        selected && "ring-2 ring-brand/40",
        selected && "border-brand bg-brand text-brand-foreground",
        dim && "opacity-25",
      )}
      onClick={(e) => {
        e.stopPropagation();
        a.selectNode(node.id);
      }}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} />
      <span className={cn("min-w-0 truncate", dirs ? "text-[13px]" : "font-mono text-[12.5px]")}>{node.display}</span>
      {mark === null ? (
        <span className={cn("text-xs tabular-nums", selected ? "text-brand-foreground/80" : "text-muted-foreground", dirs && "row-span-2")}>{formatCount(node.users)}</span>
      ) : (
        <span className={cn("text-[11px] font-medium tabular-nums", dirs && "row-span-2")}>
          {mark.usedBy > 0 && `↑${formatCount(mark.usedBy)}`}
          {mark.usedBy > 0 && mark.uses > 0 && " "}
          {mark.uses > 0 && `↓${formatCount(mark.uses)}`}
        </span>
      )}
      {dirs && <span className="min-w-0 truncate font-mono text-[11.5px] text-muted-foreground">{dir}</span>}
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </button>
  );
}

/** MoreChip folds the libraries past the band cap (T062 question 4) and
 * opens them in the band; open, it folds them again. */
function MoreChip({ node, on }: { node: Extract<MapNode, { type: "more" }>; on: boolean }) {
  const a = useMapActions();
  // A card's line into the folded libraries ends here (T072): the chip
  // lights with it and carries the selection's mark.
  const mark = a.marks.get(node.id) ?? null;
  return (
    <button
      type="button"
      className={cn("relative h-full w-full rounded-[7px] border border-dashed bg-card px-2.5 text-left text-[12.5px] font-medium text-brand-text outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring", on && "border-brand bg-brand-soft/60")}
      data-arch-card={node.id}
      data-mark={mark !== null ? (mark.usedBy > 0 && mark.uses > 0 ? "both" : mark.usedBy > 0 ? "user" : "used") : undefined}
      onClick={(e) => {
        e.stopPropagation();
        a.toggleBand();
      }}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} />
      {node.expanded ? "Fewer libraries" : `+ ${formatCount(node.count)} more ${node.count === 1 ? "library" : "libraries"}`}
      {mark !== null && <span className="ml-1 text-[11px] tabular-nums">{[mark.usedBy > 0 ? `↑${formatCount(mark.usedBy)}` : "", mark.uses > 0 ? `↓${formatCount(mark.uses)}` : ""].filter((x) => x !== "").join(" ")}</span>}
    </button>
  );
}
