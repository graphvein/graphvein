// The right inspector of the architecture map (X+): the selected
// component, line, database, library of the band (T067) or expanded row,
// with every annotation cell's source and the evidence behind a line (its
// stored members, each with an IDE link where the component answer has
// one). With nothing selected it lists what the repository does not prove.
//
// Precision: values come from the answer only; a section whose facts are
// absent is left out (never "none"), unknowns keep their reason.
//
// AG-C2 hook: the chain of the selected node (steps, branches, folds and
// "Open in Flow tab") renders in the `chain` slot above the evidence.

import { LinkTierChip } from "@/components/precision/ProvChip";
import { LinkAssumptions } from "@/components/LinkAssumptions";
import { ArrowLeftRight, ArrowRight, ChevronDown, ChevronRight, Maximize2, Minimize2, X } from "lucide-react";
import { useState, type ReactNode } from "react";

import type { Accessor, ArchProject, ArchitectureResponse, Component, Link, LinkMember, NotProven } from "@/api/architecture";
import type { IdeLink, PresentedNode } from "@/api/endpoints";
import { CodePeek } from "@/components/CodePeek";
import { EditorLink } from "@/components/EditorLink";
import { IdeCaveat, IdeMark } from "@/components/IdeParts";
import { Skeleton } from "@/components/ui/skeleton";
import { useEditor } from "@/hooks/useEditor";
import { useLabelStyle } from "@/hooks/useLabels";
import { formatCount } from "@/lib/count";
import { presentIde } from "@/lib/ide";
import { shownRoutePath } from "@/lib/routePath";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { KindTextPlain, mainKind } from "@/sections/architecture/kindText";
import { CellLine, FAMILY_TEXT, Glyph, Strip } from "@/sections/architecture/Glyph";
import { databaseTitle, endpointName, type MapEdge, type MapModel, type MapNode, type ShowOptions } from "@/sections/architecture/model";
import { rowText, rowTitle } from "@/sections/architecture/Cards";
import { memberTitle } from "@/sections/architecture/chain";
import { arrangeNotProven, notProvenCount, type NotProvenItem } from "@/sections/architecture/notProven";
import type { DetailState } from "@/sections/architecture/useDetails";
import { AcrossLine } from "@/sections/architecture/Cards";

export interface InspectorProps {
  data: ArchitectureResponse;
  model: MapModel;
  node: string | null;
  edge: string | null;
  item: string | null;
  expanded: string | null;
  detail: (id: string) => DetailState;
  onSelectEdge: (id: string) => void;
  onSelectNode: (id: string) => void;
  onExpand: (component: string | null) => void;
  onClose?: (() => void) | undefined;
  /** AG-C2: the chain block of the selection (ChainPanel). */
  chain?: ReactNode;
  /** The chain's start name: with no other selection the inspector shows
   * the chain alone (a chain restarted from one of its members). */
  chainTitle?: string | null | undefined;
  /** The Show toggles (Not proven entries of hidden components). */
  show?: ShowOptions | undefined;
}

function Section({ title, children, testId }: { title: ReactNode; children: ReactNode; testId?: string }) {
  return (
    <section className="grid gap-1.5 border-t px-4 py-3" data-testid={testId}>
      <h3 className="text-[11.5px] font-semibold tracking-wide text-muted-foreground uppercase">{title}</h3>
      {children}
    </section>
  );
}

function Header({ kicker, title, extra, onClose }: { kicker: string; title: ReactNode; extra?: ReactNode; onClose?: (() => void) | undefined }) {
  return (
    <div className="grid gap-1.5 px-4 pt-3.5 pb-3">
      <div className="flex items-center text-[11.5px] font-semibold tracking-wide text-muted-foreground uppercase">
        {kicker}
        {onClose !== undefined && (
          <button type="button" className="ml-auto grid size-6 place-items-center rounded-md hover:bg-accent" aria-label="Close the inspector" onClick={onClose}>
            <X aria-hidden="true" className="size-4" />
          </button>
        )}
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-2 text-[16px] font-semibold">{title}</div>
      {extra}
    </div>
  );
}

/** IdeLinkButton opens a stored location through the server-built link. */
export function IdeLinkButton({ ide, place, compact = false }: { ide: IdeLink | null; place: string; compact?: boolean }) {
  const editor = useEditor();
  const view = presentIde(null, ide, editor);
  const base = "inline-flex h-6 flex-none items-center gap-1 whitespace-nowrap rounded-md px-1.5 text-xs font-medium [&_svg]:size-3.5";
  if (view.href === null) {
    return (
      <span title={view.title} aria-label={view.name} className={cn(base, "text-muted-foreground opacity-60")}>
        <IdeMark view={view} />
      </span>
    );
  }
  return (
    <EditorLink href={view.href} place={place} label={view.text} detail={view.title} aria-label={view.name} data-testid="arch-ide" className={cn(base, compact ? "hover:bg-accent" : "border bg-background hover:bg-accent")}>
      <IdeMark view={view} />
      {compact ? null : view.text}
      <IdeCaveat view={view} />
    </EditorLink>
  );
}

/** shortName reads a node identity's name ("kind:file#name"), decoded. */
export function shortName(identity: string): string {
  const hash = identity.indexOf("#");
  const raw = hash === -1 ? identity : identity.slice(hash + 1);
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

function identityFile(identity: string): string {
  const colon = identity.indexOf(":");
  const hash = identity.indexOf("#");
  return identity.slice(colon + 1, hash === -1 ? undefined : hash);
}

function Kv({ k, children }: { k: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[96px_1fr] gap-x-3 text-[13px]">
      <span className="text-muted-foreground">{k}</span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}

function FamilyTag({ family, row }: { family: string; row: string }) {
  const l = label(useLabelStyle(), "linkFamily", { family, row });
  return (
    <span className={cn("rounded-[4px] px-1.5 py-0.5 text-xs font-semibold", FAMILY_TEXT[family] ?? "bg-muted")} title={l.title}>
      {l.text}
    </span>
  );
}

function Rows({ rows }: { rows: readonly string[] }) {
  const l = label(useLabelStyle(), "contractRows", rows.join(" "));
  if (l.text === "") return null;
  return (
    <span className="font-mono text-[11px] text-muted-foreground" title={l.title}>
      {l.text}
    </span>
  );
}

/** lookup finds a presented node in the ends' component answers. */
function lookup(details: DetailState[], identity: string): PresentedNode | null {
  for (const d of details) {
    if (d.kind !== "ok") continue;
    for (const list of [d.data.calls, d.data.routes, d.data.commands, d.data.tables]) {
      const n = list.find((x) => x.identity === identity);
      if (n !== undefined) return n;
    }
  }
  return null;
}

const MEMBERS_SHOWN = 40;

/** fullPath is a route's first stored path template (the full path with
 * its prefixes), or null when the node stores none. */
export function fullPath(n: PresentedNode): string | null {
  // A path whose prefix the code does not fix keeps its "…" (T033 UP-33).
  return shownRoutePath(n.attributes);
}

function Evidence({ link, details }: { link: Link; details: DetailState[] }) {
  const style = useLabelStyle();
  if (link.members.length === 0) {
    return (
      <div className="grid gap-1 text-[13px]">
        {Object.entries(link.by_kind).map(([k, n]) => (
          <Kv key={k} k={label(style, "nodeKind", k).text}>
            {formatCount(n)} stored {n === 1 ? "edge" : "edges"}
          </Kv>
        ))}
        <p className="text-xs text-muted-foreground">This family lists no members; the counts are its stored edges. Their sites are in the chain view.</p>
      </div>
    );
  }
  const loading = details.some((d) => d.kind === "loading");
  return (
    <ul className="grid gap-1" data-testid="evidence">
      {link.members.slice(0, MEMBERS_SHOWN).map((m) => (
        <MemberRow key={`${m.source}>${m.target}`} m={m} source={lookup(details, m.source)} target={lookup(details, m.target)} loading={loading} />
      ))}
      {link.members.length > MEMBERS_SHOWN && <li className="text-xs text-muted-foreground">+ {link.members.length - MEMBERS_SHOWN} more · all in the Links tab</li>}
    </ul>
  );
}

function MemberRow({ m, source, target, loading }: { m: LinkMember; source: PresentedNode | null; target: PresentedNode | null; loading: boolean }) {
  const where = source !== null ? `${source.file.split("/").pop() ?? source.file}${source.span !== null ? `:${String(source.span.start_line)}` : ""}` : identityFile(m.source).split("/").pop();
  return (
    <li className="grid grid-cols-[1fr_auto] items-start gap-x-2 rounded-md bg-muted/50 px-2 py-1 text-[12.5px]">
      <span className="min-w-0">
        <span className="block truncate font-mono" title={shortName(m.source)}>
          {source?.qualified_name.replace(/\.(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS) .*$/, "") ?? shortName(m.source)}
        </span>
        <span className="block truncate text-xs text-muted-foreground">
          {m.source_repository !== null && `in ${m.source_repository}: `}
          {where} → {m.target_repository !== null && `${m.target_repository}: `}
          {m.method !== null ? `${m.method} ` : ""}
          {(target !== null ? fullPath(target) : null) ?? m.full_path ?? m.prefix_path ?? m.path ?? m.command ?? shortName(m.target)} ·{" "}
          <LinkTierChip tier={m.tier} size="sm" />
          {m.auth !== null && <> · {m.auth}</>}
        </span>
        <LinkAssumptions list={m.assumptions} className="block" />
      </span>
      {source !== null ? <IdeLinkButton ide={source.ide} place={source.identity} compact /> : loading ? <Skeleton className="h-5 w-6" /> : null}
    </li>
  );
}

function LinkSummary({ data, edge, details, chain }: { data: ArchitectureResponse; edge: MapEdge; details: DetailState[]; chain?: ReactNode }) {
  const family = edge.families.length === 1 ? (edge.families[0] ?? null) : null;
  return (
    <>
      <div className="px-4 pb-3">
        <Strip cells={edge.cells} family={family} />
      </div>
      <Section title="Cells and sources">
        <CellLine name="Protocol" cell={edge.cells.protocol} />
        <CellLine name="Auth" cell={edge.cells.auth} />
        <CellLine name="TLS" cell={edge.cells.tls} />
      </Section>
      {chain}
      {edge.links.map((l) => (
        <Section
          key={l.id}
          title={
            <span className="flex items-center gap-2 normal-case">
              <FamilyTag family={l.family} row={l.row} />
              <span className="tracking-normal">
                {endpointName(data, l.source)} → {endpointName(data, l.target)}
              </span>
              <Rows rows={[l.row]} />
            </span>
          }
          testId="link-section"
        >
          {edge.links.length > 1 && <Glyph cells={l.cells} family={l.family} />}
          <Kv k="Links">
            {formatCount(l.count)}
            {l.back_count > 0 && ` · ${formatCount(l.back_count)} back`}
          </Kv>
          <Kv k="Tiers">
            {Object.entries(l.tiers).map(([t, n]) => {
              return (
                <span key={t} className="mr-2 inline-block">
                  <LinkTierChip tier={t} size="sm" /> ×{formatCount(n)}
                </span>
              );
            })}
            {l.weakest !== null && <span className="text-xs text-muted-foreground">
                {" "}
                (weakest <LinkTierChip tier={l.weakest} size="sm" />)
              </span>}
          </Kv>
          {l.methods.length > 0 && <Kv k="Methods">{l.methods.join(" · ")}</Kv>}
          {l.repository !== null && (
            <Kv k="Across repositories">
              <span className="block">stored project links to {l.repository}, linked across repositories (never a fact of one repository)</span>
              {l.assumptions.map((a) => (
                <span key={a} className="block text-xs text-muted-foreground">
                  assumption: {a}
                </span>
              ))}
              {l.assumes.length > 0 && <span className="block text-xs text-muted-foreground">also assumes {l.assumes.join(", ")}</span>}
              {l.stale_reasons.length > 0 && <span className="block text-xs text-dynamic">stale: {l.stale_reasons.join("; ")}</span>}
            </Kv>
          )}
          {l.origins.length > 0 && (
            <Kv k="Origins">
              {l.origins.map((o) => (
                <span key={o.origin} className="block" title="documented origin; never this link's TLS (AT3)">
                  {o.text} (documented)
                </span>
              ))}
            </Kv>
          )}
          <h4 className="mt-1 text-xs font-semibold text-muted-foreground">Evidence</h4>
          <Evidence link={l} details={details} />
        </Section>
      ))}
    </>
  );
}

function ComponentView({ data, c, model, detail, expanded, onSelectEdge, onExpand }: { data: ArchitectureResponse; c: Component; model: MapModel; detail: DetailState; expanded: boolean; onSelectEdge: (id: string) => void; onExpand: (id: string | null) => void }) {
  const style = useLabelStyle();
  const nodeId = `c:${c.id}`;
  const lines = model.edges.filter((e) => e.type === "link" && (e.source === nodeId || e.target === nodeId));
  const stub = data.stubs.find((s) => s.component === c.id);
  const ghosts = data.ghosts.filter((g) => g.component === c.id);
  const across = data.project?.across.find((a) => a.component === c.id);
  const notProven = data.not_proven.filter((n) => n.component === c.id);
  const k = c.counts;
  return (
    <>
      <div className="flex flex-wrap gap-1.5 px-4 pb-3">
        <button
          type="button"
          className="inline-flex h-7 items-center gap-1 rounded-md border px-2 text-[12.5px] font-medium hover:bg-accent"
          onClick={() => {
            onExpand(expanded ? null : c.id);
          }}
        >
          {expanded ? <Minimize2 aria-hidden="true" className="size-3.5" /> : <Maximize2 aria-hidden="true" className="size-3.5" />}
          {expanded ? "Collapse" : "Expand in place"}
        </button>
      </div>
      <Section title="Detected by">
        {c.manifests.map((m) => (
          <Kv key={m.file} k={m.ecosystem ?? "manifest"}>
            <span className="font-mono text-xs">{m.file}</span>
            {m.name !== null && <span> · {m.name}</span>}
            {m.unread !== null && <span className="block text-xs text-dynamic">not read: {m.unread}</span>}
          </Kv>
        ))}
        {c.state === "unassigned" && <p className="text-xs text-muted-foreground">Files under no manifest (the root component).</p>}
        {c.reason !== null && <p className="text-xs text-muted-foreground">{c.reason}</p>}
        <Kv k="Kinds">
          {c.kinds.map((kind) => (
            <KindTextPlain key={kind} c={c} kind={kind} />
          ))}{" "}
          <Rows rows={c.rows} />
        </Kv>
        {c.markers.length > 0 && (
          <Kv k="Deploy">
            {c.markers.map((m) => {
              const bare = m.basis !== null && m.label.endsWith(`, ${m.basis}`) ? m.label.slice(0, -(m.basis.length + 2)) : m.label;
              const l = label(style, "marker", { label: bare, row: m.row });
              return (
                <span key={`${m.kind}:${m.label}`} className="block" title={l.title} data-testid="marker">
                  {l.text}
                  {m.basis !== null && <span className="block text-xs text-dynamic" data-testid="marker-basis">{m.basis}</span>}
                </span>
              );
            })}
            <span className="block text-xs text-muted-foreground">Markers never create links.</span>
          </Kv>
        )}
        {c.ingress.length > 0 && (
          <Kv k="Ingress">
            {c.ingress.map((g) => {
              const l = label(style, "ingress", { text: g.text, row: g.row, basis: g.basis });
              const written = [g.rule !== null ? `rule ${g.rule}` : null, g.entrypoints !== null ? `entrypoints ${g.entrypoints}` : null, g.certResolver !== null ? `certresolver ${g.certResolver}` : null].filter((x) => x !== null);
              return (
                <span key={`${g.file}:${g.router}`} className="block" title={l.title} data-testid="ingress">
                  {l.text}
                  {written.length > 0 && <span className="block font-mono text-[11px] text-muted-foreground">as written: {written.join(" · ")}</span>}
                  {g.basis !== null && <span className="block text-xs text-dynamic">{g.basis}</span>}
                </span>
              );
            })}
            <span className="block text-xs text-muted-foreground">The ingress only; a link's TLS stays unknown.</span>
          </Kv>
        )}
      </Section>
      <Section title="Counts">
        <Kv k="Files">
          {formatCount(k.files)}
          {k.test_files > 0 && ` (${formatCount(k.test_files)} test scope)`}
        </Kv>
        {k.routes > 0 && <Kv k="Routes">{formatCount(k.routes)}</Kv>}
        {k.client_calls > 0 && <Kv k="Client calls">{formatCount(k.client_calls)}</Kv>}
        {k.ipc_commands > 0 && <Kv k="IPC commands">{formatCount(k.ipc_commands)}</Kv>}
        {k.ipc_calls > 0 && <Kv k="IPC calls">{formatCount(k.ipc_calls)}</Kv>}
        {k.wrapper_calls > 0 && (
          <Kv k="Wrapper forwarding calls">
            <span title="calls inside a recognized wrapper that forward its parameter: their requests are the wrapper's call sites, counted above; neither linked nor in Calls not linked">{formatCount(k.wrapper_calls)}</span>
          </Kv>
        )}
        {k.tables > 0 && <Kv k="Tables">{formatCount(k.tables)}</Kv>}
      </Section>
      {lines.length > 0 && (
        <Section title="Links · focus dims the rest">
          <ul className="grid gap-1">
            {lines.map((e) => {
              const out = e.source === nodeId;
              const other = model.nodes.find((n) => n.id === (out ? e.target : e.source));
              return (
                <li key={e.id}>
                  <button type="button" className="flex w-full min-w-0 items-center gap-2 rounded-md bg-muted/50 px-2 py-1 text-left text-[12.5px] hover:bg-accent" onClick={() => { onSelectEdge(e.id); }}>
                    <span className="w-6 flex-none text-xs text-muted-foreground">{e.both ? "⇄" : out ? "out" : "in"}</span>
                    <span className="min-w-0 truncate font-medium">{other !== undefined ? nodeTitle(data, other) : "?"}</span>
                    {e.families.map((f) => (
                      <FamilyTag key={f} family={f} row="" />
                    ))}
                    <span className="tabular-nums">{formatCount(e.count)}</span>
                    <Glyph cells={e.cells} family={e.families.length === 1 ? (e.families[0] ?? null) : null} className="ml-auto" />
                  </button>
                </li>
              );
            })}
          </ul>
        </Section>
      )}
      {across !== undefined && (
        <Section title="Linked across repositories">
          <AcrossLine across={across} />
          <p className="text-xs text-muted-foreground">
            The project's stored link pass connects these calls to {across.repositories.join(", ")} under the cross-repository assumption; the lines to the other repository's card hold them. Counted here, not under Calls not linked.
          </p>
        </Section>
      )}
      {ghosts.length > 0 && (
        <Section title="Calls not linked">
          {ghosts.map((g) => {
            const l = label(style, "ghost", { total: g.total, row: g.row, by_status: g.by_status });
            return (
              <Kv key={g.family} k={label(style, "linkFamily", { family: g.family, row: g.row }).text}>
                {l.text}
                <span className="block text-xs text-muted-foreground">{l.title}; never a link</span>
              </Kv>
            );
          })}
        </Section>
      )}
      {stub !== undefined && (
        <Section title="SDK imports">
          <StubPackages text={label(style, "stub", { text: stub.text, row: stub.row })} detail={detail} />
        </Section>
      )}
      {notProven.length > 0 && (
        <Section title={`Not proven for this component · ${String(notProven.length)}`}>
          <NotProvenList data={data} items={notProven} />
        </Section>
      )}
    </>
  );
}

function StubPackages({ text, detail }: { text: { text: string; title: string }; detail: DetailState }) {
  return (
    <>
      <p className="text-[13px]" title={text.title}>
        {text.text} <span className="text-xs text-muted-foreground">· imports only, never a protocol</span>
      </p>
      {detail.kind === "loading" && <Skeleton className="h-4 w-1/2" />}
      {detail.kind === "failed" && <p className="text-xs text-unmatched">The package list could not be loaded: {detail.error}</p>}
      {detail.kind === "ok" && (
        <ul className="flex flex-wrap gap-1">
          {detail.data.external_packages.map((p) => (
            <li key={p.identity} className="rounded border px-1.5 py-0.5 font-mono text-[11.5px]" title={`${p.ecosystem}: ${String(p.imports)} imports`}>
              {p.name}
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

/** NotProvenList lists Not proven entries in the applied label style:
 * compose services mapped to no component in one collapsed group, an
 * entry of a hidden component with where it comes from (notProven.ts). */
export function NotProvenList({ data, items, show }: { data: ArchitectureResponse; items: readonly NotProven[]; show?: ShowOptions | undefined }) {
  const style = useLabelStyle();
  const arranged = arrangeNotProven(data, items, show);
  return (
    <ul className="grid gap-1.5" data-testid="not-proven">
      {arranged.map((it, i) => {
        if (it.type === "deploy") return <DeployGroup key={`deploy:${String(i)}`} item={it} />;
        const n = it.entry;
        const l = n.kind === "may_also_use" && it.card !== null && n.database !== null ? label(style, "mayAlsoUse", { database: n.database, card: it.card, component: n.component !== null ? (data.components.find((c) => c.id === n.component)?.name ?? n.component) : null, count: n.count ?? 0, row: n.row }) : label(style, "notProven", { text: n.text, row: n.row });
        const src = it.hidden !== null ? label(style, "hiddenSource", it.hidden) : null;
        return (
          <li key={`${n.kind}:${n.component ?? ""}:${String(i)}`} className="rounded-md border border-dashed px-2.5 py-1.5 text-[12.5px]" title={l.title} data-kind={n.kind}>
            {l.text}
            {src !== null && (
              <span className="block text-xs text-muted-foreground" title={src.title} data-testid="hidden-source">
                {src.text}
              </span>
            )}
          </li>
        );
      })}
    </ul>
  );
}

/** DbAccess is a database accessor's counts in the applied label style
 * (row 56). */
function DbAccess({ a }: { a: Accessor }) {
  const l = label(useLabelStyle(), "dbAccess", { reads: a.reads, builds: a.builds, writes: a.writes, mayAlsoUse: a.may_also_use });
  return (
    <span className="ml-auto text-xs whitespace-nowrap text-muted-foreground" title={l.title}>
      {l.text}
    </span>
  );
}

function DeployTied({ tied }: { tied: Extract<NotProvenItem, { type: "deploy" }>["tied"] }) {
  // A same-name entry without an image of its own follows the
  // tied service; only entries with their own image are "on the card".
  const name = (t: (typeof tied)[number]) => `${t.entry.service ?? "?"} (${t.entry.file ?? "?"})`;
  const own = tied.filter((t) => t.entry.database !== null);
  const l = label(useLabelStyle(), "deployTied", { services: own.map(name), followers: tied.filter((t) => t.entry.database === null).map(name), cards: [...new Set(tied.map((t) => t.card))] });
  return (
    <p className="border-t px-2.5 py-1 text-xs text-muted-foreground" data-testid="deploy-tied" title={l.title}>
      {l.text}
    </p>
  );
}

function DeployGroup({ item }: { item: Extract<NotProvenItem, { type: "deploy" }> }) {
  const style = useLabelStyle();
  const [open, setOpen] = useState(false);
  const head = label(style, "deployGroup", { count: item.entries.length });
  return (
    <li className="rounded-md border border-dashed text-[12.5px]" data-testid="deploy-group">
      <button type="button" aria-expanded={open} className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left" title={head.title} onClick={() => { setOpen((o) => !o); }}>
        {open ? <ChevronDown aria-hidden="true" className="size-3.5 flex-none" /> : <ChevronRight aria-hidden="true" className="size-3.5 flex-none" />}
        <span className="min-w-0">{head.text}</span>
      </button>
      {open && (
        <ul className="grid gap-1 border-t px-2.5 py-1.5" data-testid="deploy-group-entries">
          {item.entries.map((n, i) => {
            const l = label(style, "notProven", { text: n.text, row: n.row });
            return (
              <li key={`${n.file ?? ""}:${n.service ?? ""}:${String(i)}`} title={l.title}>
                {n.service !== null && n.reason !== null && style === "plain" ? (
                  <>
                    <span className="font-medium">{n.service}</span> <span className="font-mono text-[11.5px] text-muted-foreground">{n.file}</span>
                    <span className="block text-xs text-muted-foreground">{n.reason}</span>
                  </>
                ) : (
                  l.text
                )}
              </li>
            );
          })}
        </ul>
      )}
      {item.tied.length > 0 && (
        <DeployTied tied={item.tied} />
      )}
    </li>
  );
}

function nodeTitle(data: ArchitectureResponse, n: MapNode): string {
  switch (n.type) {
    case "component":
      return n.component.name;
    case "library":
      return n.component.name;
    case "band":
      return "Libraries";
    case "more":
      return `${String(n.count)} more libraries`;
    case "database":
      return databaseTitle(n.database);
    case "repository":
      return n.member.name;
    case "container":
      return nodeTitle(data, n.card);
    case "member":
      return memberTitle(n.member);
    case "fold":
      return `step ${String(n.number)}`;
  }
}

function ItemView({ node, links }: { node: PresentedNode; links: readonly Link[] }) {
  const style = useLabelStyle();
  const kind = label(style, "nodeKind", node.kind);
  const level = label(style, "levelTag", 4);
  const callers = links.flatMap((l) => l.members.filter((m) => m.target === node.identity));
  const targets = links.flatMap((l) => l.members.filter((m) => m.source === node.identity));
  return (
    <>
      <Section title="Location">
        <Kv k="Kind">
          <span title={kind.title}>{kind.text}</span>{" "}
          <span className="rounded bg-muted px-1 font-mono text-[10px] text-muted-foreground" title={level.title}>
            {level.text}
          </span>
        </Kv>
        <Kv k="File">
          <span className="flex min-w-0 items-center gap-1">
            <span className="min-w-0 truncate font-mono text-xs" title={node.file}>
              {node.file}
              {node.span !== null && `:${String(node.span.start_line)}`}
            </span>
            <IdeLinkButton ide={node.ide} place={node.identity} compact />
          </span>
        </Kv>
        {fullPath(node) !== null && <Kv k="Full path">{fullPath(node)}</Kv>}
      </Section>
      {callers.length > 0 && (
        <Section title={`Callers · ${String(callers.length)}`}>
          <ul className="grid gap-1">
            {callers.map((m) => (
              <li key={m.source} className="truncate font-mono text-[12px]" title={m.source}>
                {shortName(m.source)} <LinkTierChip tier={m.tier} size="sm" className="font-sans" />
              </li>
            ))}
          </ul>
        </Section>
      )}
      {targets.length > 0 && (
        <Section title="Linked to">
          <ul className="grid gap-1">
            {targets.map((m) => (
              <li key={m.target} className="truncate font-mono text-[12px]" title={m.target}>
                {m.method ?? ""} {m.full_path ?? m.prefix_path ?? m.path ?? shortName(m.target)} <LinkTierChip tier={m.tier} size="sm" className="font-sans" />
                {m.auth !== null && <span className="block font-sans text-xs text-muted-foreground">{m.auth}</span>}
                <LinkAssumptions list={m.assumptions} className="block font-sans" />
              </li>
            ))}
          </ul>
        </Section>
      )}
      <div className="border-t p-3">
        <CodePeek id={node.identity} className="max-h-[360px]" />
      </div>
    </>
  );
}

/** itemTitle names a row's node: a route's method and full path, else its
 * name. */
export function itemTitle(n: PresentedNode): string {
  const path = fullPath(n);
  const method = typeof n.attributes.method === "string" ? n.attributes.method : null;
  if (n.kind === "route" && path !== null) return `${method !== null ? `${method} ` : ""}${path}`;
  return n.name;
}

/** Inspector renders the panel for the current selection. */
export function Inspector(p: InspectorProps) {
  const { data, model } = p;
  // A row of the expanded component.
  if (p.item !== null && p.expanded !== null) {
    const d = p.detail(p.expanded);
    const node = d.kind === "ok" ? lookup([d], p.item) : null;
    return (
      <div data-testid="inspector" data-kind="item">
        <Header kicker={node !== null ? `${label("plain", "nodeKind", node.kind).text} · selected` : "Selected"} title={<span className="font-mono text-[14px] break-all">{node !== null ? itemTitle(node) : shortName(p.item)}</span>} onClose={p.onClose} />
        {p.chain}
        {node !== null ? <ItemView node={node} links={data.links} /> : d.kind === "loading" ? <Skeleton className="m-4 h-20" /> : <p className="px-4 text-sm text-muted-foreground">This row is not in the component's answer.</p>}
      </div>
    );
  }
  if (p.edge !== null) {
    const e = model.edges.find((x) => x.id === p.edge);
    if (e !== undefined && e.type === "link") {
      const ends = [...new Set(e.links.flatMap((l) => [l.source, l.target]))].filter((id) => data.components.some((c) => c.id === id));
      const details = ends.map((id) => p.detail(id));
      const s = model.nodes.find((n) => n.id === e.source);
      const t = model.nodes.find((n) => n.id === e.target);
      return (
        <div data-testid="inspector" data-kind="link">
          <Header
            kicker={e.links.length > 1 ? `Links · ${String(e.links.length)} bundled · selected` : "Link · selected"}
            title={
              <>
                <span className="min-w-0 truncate">{s !== undefined ? nodeTitle(data, s) : e.source}</span>
                {e.both ? <ArrowLeftRight aria-hidden="true" className="size-4 flex-none" /> : <ArrowRight aria-hidden="true" className="size-4 flex-none" />}
                <span className="min-w-0 truncate">{t !== undefined ? nodeTitle(data, t) : e.target}</span>
                {e.families.map((f) => (
                  <FamilyTag key={f} family={f} row={e.links.find((l) => l.family === f)?.row ?? ""} />
                ))}
              </>
            }
            onClose={p.onClose}
          />
          <LinkSummary data={data} edge={e} details={details} chain={p.chain} />
        </div>
      );
    }
  }
  if (p.node !== null) {
    const found = model.nodes.find((x) => x.id === p.node);
    // A card the chain opened reads as the card itself.
    const n = found?.type === "container" ? found.card : found;
    if (n !== undefined && (n.type === "component" || n.type === "database" || n.type === "library" || n.type === "repository")) return <NodeInspector {...p} n={n} />;
  }
  if (p.chain !== undefined && p.chain !== null && p.chainTitle !== undefined && p.chainTitle !== null) {
    return (
      <div data-testid="inspector" data-kind="chain">
        <Header kicker="Chain · selected" title={<span className="font-mono text-[14px] break-all">{p.chainTitle}</span>} onClose={p.onClose} />
        {p.chain}
      </div>
    );
  }
  return <Overview data={data} model={model} show={p.show} onClose={p.onClose} />;
}

function NodeInspector(p: InspectorProps & { n: Extract<MapNode, { type: "component" | "database" | "library" | "repository" }> }) {
  const { data, model, n } = p;
  const style = useLabelStyle();
  if (n.type === "repository") {
    const m = n.member;
    const lines = model.edges.filter((e) => e.type === "link" && (e.source === n.id || e.target === n.id));
    return (
      <div data-testid="inspector" data-kind="repository">
        <Header kicker="Other repository · selected" title={m.name} extra={<span className="text-xs text-muted-foreground">member of project {data.project?.project ?? ""}</span>} onClose={p.onClose} />
        <Section title="Linked across repositories">
          <Kv k="Calls to it">{formatCount(m.calls_out)}</Kv>
          <Kv k="Calls from it">{formatCount(m.calls_in)}</Kv>
          <Kv k="Operations documenting it">{formatCount(m.operations_out)}</Kv>
          <Kv k="Operations from it">{formatCount(m.operations_in)}</Kv>
          {lines.map((e) => (
            <button key={e.id} type="button" className="text-left text-[12.5px] text-brand-text hover:underline" onClick={() => { p.onSelectEdge(e.id); }}>
              {e.families.map((f) => label(style, "linkFamily", { family: f, row: "" }).text).join(" + ")} {formatCount(e.count)} with{" "}
              {nodeTitle(data, model.nodes.find((x) => x.id === (e.source === n.id ? e.target : e.source)) ?? n)}
            </button>
          ))}
          <p className="text-xs text-muted-foreground">Every link here is a stored project link of the last link pass ({data.project?.linkPass.state ?? "none"}). Its components are in that repository's own overview.</p>
        </Section>
        <Section title="Assumption">
          {n.assumptions.map((a) => (
            <p key={a} className="text-[13px]">
              {a}
            </p>
          ))}
        </Section>
      </div>
    );
  }
  if (n.type === "component") {
    const c = n.component;
    return (
      <div data-testid="inspector" data-kind="component">
        <Header
          kicker={`Component · selected`}
          title={
            <>
              <span className="min-w-0 truncate">{c.name}</span>
              <span className="rounded-md border px-1.5 py-0.5 text-xs font-medium">
                <KindTextPlain c={c} kind={mainKind(c)} className="" />
              </span>
            </>
          }
          extra={<span className="font-mono text-xs text-muted-foreground">{c.directory === "." ? "./" : `${c.directory}/`}</span>}
          onClose={p.onClose}
        />
        {p.chain}
        <ComponentView data={data} c={c} model={model} detail={p.detail(c.id)} expanded={p.expanded === c.id} onSelectEdge={p.onSelectEdge} onExpand={p.onExpand} />
      </div>
    );
  }
  if (n.type === "database") {
    const d = n.database;
    return (
      <div data-testid="inspector" data-kind="database">
        <Header kicker="Database · selected" title={databaseTitle(d)} extra={<span className="font-mono text-xs text-muted-foreground">schema set {d.set}</span>} onClose={p.onClose} />
        <Section title="Engine">
          <CellLine name="Engine" cell={d.engine} />
          {d.evidence.map((ev) => (
            <p key={ev} className="text-xs text-muted-foreground">
              {ev} <span>(evidence, never the engine)</span>
            </p>
          ))}
        </Section>
        <Section title="Schema">
          <Kv k="Tool">{d.tool}</Kv>
          <Kv k="Tables">{formatCount(d.tables)}</Kv>
        </Section>
        {d.accessors.length > 0 && (
          <Section title="Accessed by">
            {d.accessors.map((a) => (
              <div key={a.component} className="flex items-center gap-2 text-[13px]">
                {a.link !== null && model.edges.some((e) => e.links.some((l) => l.id === a.link)) ? (
                  <button
                    type="button"
                    className="min-w-0 truncate font-medium text-brand-text underline-offset-2 hover:underline"
                    onClick={() => {
                      const e = model.edges.find((x) => x.links.some((l) => l.id === a.link));
                      if (e !== undefined) p.onSelectEdge(e.id);
                    }}
                  >
                    {endpointName(data, a.component)}
                  </button>
                ) : (
                  <span className="min-w-0 truncate font-medium">{endpointName(data, a.component)}</span>
                )}
                <DbAccess a={a} />
              </div>
            ))}
          </Section>
        )}
      </div>
    );
  }
  return <LibraryView p={p} n={n} />;
}

/** LibraryView is a library of the band selected (T062 A2): the
 * components using it and the libraries it uses, each with the summed
 * count of the stored links between them, and what its band row means.
 * A library no stored link touches says so, never "unused". */
function LibraryView({ p, n }: { p: InspectorProps; n: Extract<MapNode, { type: "library" }> }) {
  const { data, model } = p;
  const c = n.component;
  const dir = c.directory === "." ? "./" : `${c.directory}/`;
  const usedBy = new Map<string, number>();
  const uses = new Map<string, number>();
  for (const l of data.links) {
    if (l.source === l.target) continue;
    if (l.target === c.id) usedBy.set(l.source, (usedBy.get(l.source) ?? 0) + l.count + l.back_count);
    if (l.source === c.id) uses.set(l.target, (uses.get(l.target) ?? 0) + l.count + l.back_count);
  }
  const sorted = (m: Map<string, number>) => [...m].sort((x, y) => y[1] - x[1] || endpointName(data, x[0]).localeCompare(endpointName(data, y[0])));
  const nodeOf = (id: string) => model.nodes.find((x) => (x.type === "component" || x.type === "library") && x.component.id === id)?.id ?? null;
  // A library reads as on its chip (the shared prefix dropped).
  const nameOf = (id: string) => {
    const x = model.nodes.find((y) => y.type === "library" && y.component.id === id);
    return x?.type === "library" ? x.display : endpointName(data, id);
  };
  const row = model.nodes.find((x) => x.type === "band");
  const bandRow = row?.type === "band" ? row.band.rows[n.row] : undefined;
  const Entry = ({ id, count }: { id: string; count: number }) => {
    const target = nodeOf(id);
    const isCard = target?.startsWith("c:") === true;
    const name = (
      <>
        <span className="min-w-0 truncate font-mono text-[12.5px]" title={endpointName(data, id)}>
          {nameOf(id)}
        </span>
        {isCard && <span className="flex-none text-xs text-muted-foreground">card</span>}
        <span className="ml-auto flex-none tabular-nums">{formatCount(count)}</span>
      </>
    );
    return (
      <li>
        {target !== null ? (
          <button type="button" className="flex w-full min-w-0 items-center gap-1.5 rounded-md px-1 py-0.5 text-left hover:bg-accent" onClick={() => { p.onSelectNode(target); }}>
            {name}
          </button>
        ) : (
          <span className="flex min-w-0 items-center gap-1.5 px-1 py-0.5" title="not drawn: hidden by a Show toggle">
            {name}
          </span>
        )}
      </li>
    );
  };
  return (
    <div data-testid="inspector" data-kind="library">
      <Header kicker="Library · selected" title={c.name} extra={<span className="font-mono text-xs text-muted-foreground">{[dir, ...c.manifests.map((m) => m.file.split("/").pop() ?? m.file), `${formatCount(c.counts.files)} files`].join(" · ")}</span>} onClose={p.onClose} />
      {usedBy.size + uses.size === 0 ? (
        <Section title="Links">
          <p className="text-[13px]">No stored link starts or ends at this library in this answer.</p>
          <p className="text-xs text-muted-foreground">Not read as unused: a use the indexer did not resolve is not stored.</p>
        </Section>
      ) : (
        <>
          <Section title={`Used by ↑ · ${String(usedBy.size)}`} testId="library-used-by">
            {usedBy.size > 0 ? (
              <ul className="grid gap-0.5">
                {sorted(usedBy).map(([id, k]) => (
                  <Entry key={id} id={id} count={k} />
                ))}
              </ul>
            ) : (
              <p className="text-xs text-muted-foreground">No stored link ends at this library.</p>
            )}
          </Section>
          {uses.size > 0 && (
            <Section title={`Uses ↓ · ${String(uses.size)}`} testId="library-uses">
              <ul className="grid grid-cols-2 gap-x-4 gap-y-0.5">
                {sorted(uses).map(([id, k]) => (
                  <Entry key={id} id={id} count={k} />
                ))}
              </ul>
            </Section>
          )}
        </>
      )}
      {bandRow !== undefined && (
        <Section title="Band row">
          <p className="text-[13px]">{[rowText(bandRow).label ?? "Used by libraries", rowText(bandRow).sub].filter((x) => x !== null).join(", ")}</p>
          <p className="text-xs text-muted-foreground">{rowTitle(bandRow)}. The numbers are the stored links' counts, as the band's chips show them while this library is selected (↑ uses it, ↓ it uses); otherwise a chip's number is the components using it.</p>
        </Section>
      )}
      <Section title="In-process">
        <p className="text-xs text-muted-foreground">Calls and imports between components; protocol, auth and TLS do not apply. Each line's cells and evidence: select the line.</p>
      </Section>
    </div>
  );
}

/** ProjectOverview is the overview's project block (serve --project,
 * CE8): the calls linked across repositories each way, the assumption
 * and what this repository alone counts. */
function ProjectOverview({ p }: { p: ArchProject }) {
  const heading = label(useLabelStyle(), "archProjectHeading", { project: p.project });
  const out = p.across.reduce((n, a) => n + a.linked_across, 0);
  const ops = p.across.reduce((n, a) => n + a.operations_across, 0);
  const noRoute = p.across.reduce((n, a) => n + a.repository_no_route, 0);
  const alone = p.across.reduce((n, a) => n + a.repository_not_linked, 0);
  const into = p.members.filter((m) => m.calls_in > 0);
  const documentedBy = p.members.filter((m) => m.operations_in > 0);
  const to = p.members.filter((m) => m.calls_out > 0);
  return (
    <Section
      title={
        <span title={heading.title}>{heading.text}</span>
      }
    >
      <div className="grid gap-0.5 text-[13px]" data-testid="overview-across">
        <p className="text-xs text-muted-foreground">Project {p.project}</p>
        {out > 0 && (
          <p>
            {formatCount(out)} of its calls linked across repositories to {to.map((m) => m.repository).join(", ")}
          </p>
        )}
        {ops > 0 && (
          <p>
            {formatCount(ops)} of its OpenAPI operations document routes of {p.across.flatMap((a) => (a.operations_across > 0 ? a.repositories : [])).filter((r, i, l) => l.indexOf(r) === i).join(", ")}
          </p>
        )}
        {into.map((m) => (
          <p key={m.id}>
            {formatCount(m.calls_in)} calls of {m.repository} linked across repositories to its routes
          </p>
        ))}
        {documentedBy.map((m) => (
          <p key={`${m.id}-ops`}>
            {formatCount(m.operations_in)} OpenAPI operations of {m.repository} document its routes
          </p>
        ))}
        {out === 0 && ops === 0 && into.length === 0 && documentedBy.length === 0 && <p>No stored project link starts or ends in this repository.</p>}
      </div>
      <p className="text-xs text-muted-foreground">
        Stored project links (link pass {p.linkPass.state}), each with its assumption: {p.assumptions.join("; ") || "none stated"}.
        {out > 0 && ` This repository alone counts ${formatCount(alone)} calls of those components as not linked.`}
        {ops > 0 && ` Alone it lists ${formatCount(noRoute)} operations as documented, no route.`}
      </p>
    </Section>
  );
}

function Overview({ data, model, show, onClose }: { data: ArchitectureResponse; model: MapModel; show?: ShowOptions | undefined; onClose?: (() => void) | undefined }) {
  const style = useLabelStyle();
  const bandNode = model.nodes.find((n) => n.type === "band");
  const band = bandNode?.type === "band" ? bandNode.band : undefined;
  const count = notProvenCount(arrangeNotProven(data, data.not_proven, show));
  const notDrawn = Object.entries(data.not_drawn);
  const linksLine = label(style, "archLinksSummary", { links: data.links.length, components: data.components.length });
  return (
    <div data-testid="inspector" data-kind="overview">
      <Header kicker="Architecture" title={data.repository} extra={<p className="text-[13px] font-normal text-muted-foreground">Select a card, a library or a line chip to see its cells, their sources and the evidence.</p>} onClose={onClose} />
      {data.notice !== null && (
        <Section title="Manifests">
          <p className="text-[13px] text-dynamic">{data.notice}</p>
        </Section>
      )}
      {data.project !== null && <ProjectOverview p={data.project} />}
      {band !== undefined && (
        <Section title="Libraries" testId="overview-libraries">
          <dl className="grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5 text-[13px]">
            {(
              [
                ["Workspace packages (kind package only)", band.libraries],
                ["Card ⇄ library pairs, drawn", band.cardLines],
                ["Links between libraries", band.between],
                ["With no stored link", band.unlinked],
              ] as const
            ).map(([k, v]) => (
              <div key={k} className="contents">
                <dt>{k}</dt>
                <dd className="text-right tabular-nums">{formatCount(v)}</dd>
              </div>
            ))}
          </dl>
          <p className="text-xs text-muted-foreground">Select a library or a card to mark its neighbours on their chips: ↑ uses the selection, ↓ used by it, with the stored counts.</p>
        </Section>
      )}
      <Section title={`Not proven in this repository · ${String(count)}`}>
        {data.not_proven.length > 0 ? <NotProvenList data={data} items={data.not_proven} show={show} /> : <p className="text-xs text-muted-foreground">The answer lists no entry.</p>}
      </Section>
      {notDrawn.length > 0 && (
        <Section title="Counted, not drawn">
          {notDrawn.map(([k, n]) => (
            <Kv key={k} k={label(style, "nodeKind", k).text}>
              {formatCount(n)}
            </Kv>
          ))}
          <p className="text-xs text-muted-foreground">Edges inside one component and kinds that are not component links.</p>
        </Section>
      )}
      <Section title="Links">
        <p className="text-[13px]">
          <span title={linksLine.title}>{linksLine.text}</span> · <span className="text-muted-foreground">every link with its cells in the Links tab</span>
        </p>
      </Section>
    </div>
  );
}
