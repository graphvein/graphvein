// The Impact tab of a change item (V3 mockup, T019 Revision 4): the paths
// from the item to what depends on it, grouped by relation and indented by
// hop, with edge kinds, wording, fact versus inferred and confidence; on
// I8 steps the owner entry's verification and assumptions; the table users
// as a lower bound ("uses found", "may also be used by"); the unknowns
// (blind spots, near misses, gap files, untraversed kinds, not extracted
// relations) and the caps. Fact paths read "depends on", inferred paths
// "may depend on"; never "breaks".

import { LinkAssumptions, distinctAssumptions } from "@/components/LinkAssumptions";
import { useState, type ReactNode } from "react";

import type { ChangeItem, ChangesImpact, ImpactPath, Step } from "@/api/changes";
import { DepthSwitch } from "@/components/DepthSwitch";
import { RepoBadge } from "@/components/RepoBadge";
import { depthSourceText, useDepth } from "@/hooks/useDepth";
import { BUILT_IN_DEPTH, MAX_DEPTH } from "@/lib/depthChoice";
import { cn } from "@/lib/utils";
import { AssumptionText, ImpactCatalogue } from "@/sections/changes/assumptions";
import { useLabel, useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { LabelRowTag, NotExtractedChip, ProvChip, RouteName } from "@/sections/changes/chips";
import { ProvenanceChip } from "@/sections/explorer/chips";
import { foldPaths, groupPaths, pathLocation, type PathGroup } from "@/sections/changes/impact";
import { usePaged } from "@/sections/changes/usePaged";
import { capText, fileLine, isSeed, notExtractedRelationText, notSeedText, itemRouteTitle, plural, routeTitle, shortName, verificationTitle } from "@/sections/changes/words";
import { OUTLINE_BUTTON } from "@/sections/schema/parts";
import { useCheckedAnswer, useCompare } from "@/shell/compare";

function Head({ children, note }: { children: ReactNode; note?: ReactNode }) {
  return (
    <div className="mt-2 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 border-b pb-1 text-[12.5px] font-semibold">
      {children}
      {note !== undefined && <small className="text-[11.5px] font-normal text-muted-foreground">{note}</small>}
    </div>
  );
}

function StepAccess({ step }: { step: Step }) {
  const a = step.access;
  if (a === null) return null;
  return (
    <div className="flex basis-full flex-wrap items-center gap-1.5 pl-1 text-[11.5px]" data-testid="step-access">
      <span
        className={cn("inline-flex h-[18px] items-center rounded border px-1.5", a.verification === "oracle" ? "border-linked-bar/50 text-linked" : "border-dashed border-never-bar text-muted-foreground")}
        title={verificationTitle(a.verification)}
        data-testid="verification"
      >
        {a.verification_wording}
      </span>
      <span className="text-muted-foreground">
        {a.tool} · {a.role}
        {a.operations.length > 0 ? ` · ${a.operations.join(", ")}` : ""}
        {a.rows.length > 0 ? ` · ${a.rows.join(" ")}` : ""}
      </span>
      {a.assumes.map((id) => (
        <AssumptionText key={id} id={id} />
      ))}
      {a.column_use === "not_extracted" && <NotExtractedChip wrap>column use not extracted (edges are table-level)</NotExtractedChip>}
    </div>
  );
}

/** modelRole says a model use's section as the I16 wording does (row
 * MT-15): request body, response (with its status), or a parameter. */
export function modelRole(section: string, status: string | null): string {
  if (section === "body") return "request body";
  if (section === "responses") return status !== null ? `response ${status}` : "response";
  return `${section} parameter`;
}

/** StepModel lists how the route of an I16 step uses the changed model
 * (T029 W2, rows MT-14, MT-15, MT-19): one provenance chip per use, the
 * Explorer's chip, so fact, intended, returned and mapped uses read the
 * same as in the route's shape. */
function StepModel({ step }: { step: Step }) {
  const m = step.model;
  if (m === null || m.uses.length === 0) return null;
  return (
    <div className="flex basis-full flex-wrap items-center gap-1.5 pl-1 text-[11.5px]" data-testid="step-model">
      <span className="text-muted-foreground">
        model <span className="font-mono">{m.name}</span>:
      </span>
      {m.uses.map((u, i) => (
        <span key={i} className="inline-flex items-center gap-1" data-testid="model-use">
          <span className="text-muted-foreground">{modelRole(u.section, u.status)}</span>
          <ProvenanceChip p={{ kind: u.kind, rule: u.rule, file: u.file, line: u.line, chain: [], confidence: u.confidence }} />
        </span>
      ))}
    </div>
  );
}

/** crossings lists the project links a path goes through, as (from, to)
 * repository pairs in path order (T022 B9). */
export function crossings(p: ImpactPath): { from: string; to: string; assumption: string | null }[] {
  const out: { from: string; to: string; assumption: string | null }[] = [];
  for (const s of p.steps) {
    if (s.source_repository !== null && s.target_repository !== null && s.source_repository !== s.target_repository) {
      // A link step points from the caller to the route; the impact walks
      // from the route's repository into the caller's.
      out.push({ from: s.target_repository, to: s.source_repository, assumption: s.assumption });
    }
  }
  return out;
}

function PathRow({ p, home }: { p: ImpactPath; home: string | null }) {
  const loc = pathLocation(p);
  const route = p.kind === "route" ? routeTitle(shortName(p.node), p.route_paths ?? undefined, p.prefix_unknown_paths, p.route_shadowed, p.route_withheld) : null;
  // A step through a calls_route link carries the link's assumptions (an
  // origin nothing proves, UP-V1, UP-V2; T033): never a plain link.
  const linkAssumed = distinctAssumptions(p.steps.map((s) => s.link_assumptions));
  const access = p.steps.filter((s) => s.access !== null);
  // Only the path's own last step: a longer path through a model step
  // repeats it on the row of the route it reached.
  const last = p.steps[p.steps.length - 1];
  const models = last !== undefined && last.model !== null ? [last] : [];
  const style = useLabelStyle();
  const kind = label(style, "nodeKind", p.kind);
  const rule = label(style, "impactRule", p.relation);
  const kinds = label(style, "edgeKinds", p.steps);
  const crossed = crossings(p);
  return (
    <li className="flex flex-wrap items-center gap-x-1.5 gap-y-1 border-b py-1.5 text-[12.5px]" style={{ paddingLeft: `${String(Math.min(p.distance - 1, 5) * 16)}px` }} data-testid="impact-path">
      <span className="font-mono text-[11px] text-muted-foreground">hop {p.distance}</span>
      {p.repository !== null && p.repository !== home && <RepoBadge repository={p.repository} />}
      <span className="min-w-0 font-mono [overflow-wrap:anywhere]" title={route !== null ? `${route.title}\n${p.node}` : p.node}>
        {route !== null ? <RouteName route={route} /> : shortName(p.node)}
      </span>
      <span className="text-[11px] text-muted-foreground" title={kind.title}>
        {kind.text}
      </span>
      <span className="text-muted-foreground" title={rule.title} data-relation={p.relation}>
        {p.wording}
      </span>
      <span className={cn("text-[11.5px]", p.claim === "depends on" ? "font-medium" : "italic")}>{p.claim}</span>
      <span className={cn("rounded bg-muted px-1 text-[11px]", style === "technical" && "font-mono")} title={kinds.title}>
        {kinds.text}
      </span>
      <LabelRowTag tag={rule} />
      <ProvChip provenance={p.provenance} confidence={p.confidence} />
      <span className="flex-1" />
      {loc !== null && (
        <span className="font-mono text-[11.5px] text-muted-foreground [overflow-wrap:anywhere]" title="where the last step was read">
          {loc}
        </span>
      )}
      {access.map((s, i) => (
        <StepAccess key={i} step={s} />
      ))}
      {models.map((s, i) => (
        <StepModel key={i} step={s} />
      ))}
      {crossed.map((c, i) => (
        <CrossingLine key={i} from={c.from} to={c.to} assumption={c.assumption} />
      ))}
      {linkAssumed.length > 0 && <LinkAssumptions list={linkAssumed} className="basis-full pl-1" />}
    </li>
  );
}

/** CrossingLine labels a path that crosses repositories through a project
 * link, with the link's assumption (T022 B9). */
function CrossingLine({ from, to, assumption }: { from: string; to: string; assumption: string | null }) {
  const l = useLabel("crossRepository", { from, to });
  return (
    <div className="flex basis-full flex-wrap items-center gap-1.5 pl-1 text-[11.5px]" data-testid="impact-crossing">
      <span className="inline-flex h-[18px] items-center rounded border border-dashed border-brand/50 px-1.5 font-medium text-brand-text" title={l.title}>
        {l.text}
      </span>
      {assumption !== null && <span className="text-muted-foreground italic">assumption: {assumption}</span>}
    </div>
  );
}

function GroupView({ g, home }: { g: PathGroup; home: string | null }) {
  const [open, setOpen] = useState<Set<string>>(new Set());
  const { shown, folds } = foldPaths(g.paths);
  const rule = useLabel("impactRule", g.relation);
  return (
    <section aria-label={g.wording} data-testid="impact-group">
      <Head note={plural(g.paths.length, "path")}>
        <span title={rule.title} data-relation={g.relation}>
          {g.wording}
        </span>
        <LabelRowTag tag={rule} />
      </Head>
      <ul className="grid grid-cols-[minmax(0,1fr)]">
        {shown.map((p) => (
          <PathRow key={`${p.seed}\u0000${p.node}`} p={p} home={home} />
        ))}
        {folds.map((f) => (
          <li key={f.dir} className="border-b" data-testid="impact-fold">
            <button
              type="button"
              aria-expanded={open.has(f.dir)}
              className="flex w-full items-center gap-2 py-1.5 text-left text-[12.5px] hover:bg-accent"
              onClick={() => {
                setOpen((s) => {
                  const n = new Set(s);
                  if (n.has(f.dir)) n.delete(f.dir);
                  else n.add(f.dir);
                  return n;
                });
              }}
            >
              <span aria-hidden="true">{open.has(f.dir) ? "▾" : "▸"}</span>
              <span className="font-mono">{f.dir}</span>
              <span className="text-muted-foreground">{plural(f.paths.length, "path")} folded</span>
            </button>
            {open.has(f.dir) && (
              <ul className="grid grid-cols-[minmax(0,1fr)]">
                {f.paths.map((p) => (
                  <PathRow key={`${p.seed}\u0000${p.node}`} p={p} home={home} />
                ))}
              </ul>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

function Unknown({ tag, children }: { tag: ReactNode; children: ReactNode }) {
  return (
    <li className="flex flex-wrap items-start gap-x-2 gap-y-0.5 border-b py-1.5 text-[12.5px]">
      {tag}
      <span className="min-w-0 [overflow-wrap:anywhere]">{children}</span>
    </li>
  );
}

function UnknownTag({ children }: { children: string }) {
  return <span className="inline-flex h-5 flex-none items-center rounded-[5px] border border-dashed border-dynamic-bar px-1.5 text-[11.5px] text-dynamic">{children}</span>;
}

/** ImpactBody renders a loaded traversal (exported for tests). */
export function ImpactBody({ data, paths, more, item }: { data: ChangesImpact; paths: ImpactPath[]; more: ReactNode; item?: ChangeItem }) {
  const groups = groupPaths(paths);
  const footnote = useLabel("impactFootnote", null);
  const style = useLabelStyle();
  // A near miss names a seed or reached route: its full path from the seed
  // item or a loaded path's route_paths, else the path as written, marked.
  const nearRoute = (id: string) => {
    if (item !== undefined && item.identity === id) return itemRouteTitle(item) ?? routeTitle(shortName(id), undefined);
    const p = paths.find((x) => x.node === id && x.route_paths !== null);
    return routeTitle(shortName(id), p?.route_paths ?? undefined, p?.prefix_unknown_paths ?? [], p?.route_shadowed ?? false, p?.route_withheld ?? false);
  };
  const u = data.unknowns;
  const untraversed = Object.entries(u.untraversed).filter(([, n]) => n > 0);
  const noUnknowns = u.blind_spots.length === 0 && u.near_misses.length === 0 && u.gap_nodes.length === 0 && untraversed.length === 0 && u.not_extracted.length === 0;
  return (
    <div className="grid grid-cols-[minmax(0,1fr)] gap-1" data-testid="impact-body">
      <div className="text-[12.5px] text-muted-foreground" data-testid="impact-total">
        {data.total === 0 ? "No dependent reached by the relations followed (see unknowns: they are not impact)." : `${plural(data.total, "dependent")} reached at depth ${String(data.depth)}`}
        {paths.length < data.total ? ` · ${String(paths.length)} shown` : ""}
      </div>
      {groups.map((g) => (
        <GroupView key={g.key} g={g} home={data.repository} />
      ))}
      {more}
      {data.table_uses.length > 0 && (
        <section aria-label="Table users" data-testid="table-uses">
          <Head note="a lower bound, never “no users”">Table users</Head>
          <ul className="grid grid-cols-[minmax(0,1fr)]">
            {data.table_uses.map((t) => (
              <li key={t.table} className="grid gap-1 border-b py-1.5 text-[12.5px]">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="font-mono font-medium">{shortName(t.table)}</span>
                  <span className="text-muted-foreground" data-testid="uses-found">
                    {t.wording} (a lower bound): {t.found.length === 0 ? "none found so far" : t.found.map(shortName).join(", ")}
                  </span>
                </div>
                {t.may_also_be_used_by.length > 0 && (
                  <div className="grid gap-0.5 pl-3" data-testid="may-also">
                    <span className="text-muted-foreground">{t.may_also_wording}:</span>
                    {t.may_also_be_used_by.map((m) => (
                      <div key={`${m.owner}\u0000${String(m.line)}`} className="flex flex-wrap items-center gap-1.5">
                        <span className="font-mono">{shortName(m.owner)}</span>
                        <ProvChip provenance={m.provenance} confidence={null} />
                        <span className="text-[11.5px] text-muted-foreground">
                          {m.tool} · {m.row}
                          {m.line !== null ? ` · line ${String(m.line)}` : ""}
                          {m.partial ? " · partial site" : ""} · {m.reason}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
      <section aria-label="Unknowns" data-testid="impact-unknowns">
        <Head note="never counted as impact">Unknowns</Head>
        {noUnknowns ? (
          <p className="m-0 py-1.5 text-[12.5px] text-muted-foreground">None reported by the traversal.</p>
        ) : (
          <ul className="grid grid-cols-[minmax(0,1fr)]">
            {u.blind_spots.map((b, i) => (
              <Unknown key={`b${String(i)}`} tag={<UnknownTag>blind spot</UnknownTag>}>
                Unresolved call <code className="font-mono">{b.name ?? "?"}(…)</code> at <span className="font-mono">{fileLine(b.file, b.span)}</span>
                {b.caller !== null ? ` in ${shortName(b.caller)}` : ""} matches {b.matches.map(shortName).join(", ") || "a reached function"} by name (ADR 0009).
              </Unknown>
            ))}
            {u.near_misses.map((m, i) => (
              <Unknown key={`n${String(i)}`} tag={<UnknownTag>near miss</UnknownTag>}>
                <span className="font-mono">{shortName(m.call)}</span> lists{" "}
                <span className="font-mono" title={nearRoute(m.route).title} data-testid="near-miss-route">
                  <RouteName route={nearRoute(m.route)} />
                </span>{" "}
                as a candidate without an edge ({m.status}, ADR 0042).
              </Unknown>
            ))}
            {u.gap_nodes.map((g) => (
              <Unknown key={`g${g}`} tag={<UnknownTag>gap file</UnknownTag>}>
                <span className="font-mono">{shortName(g)}</span> is in a file with a knowledge gap: what depends on it may be missing.
              </Unknown>
            ))}
            {untraversed.length > 0 && (
              <Unknown tag={<UnknownTag>not followed</UnknownTag>}>
                Edge kinds no relation follows:{" "}
                {untraversed.map(([k, n], i) => (
                  <span key={k}>
                    {i > 0 ? ", " : ""}
                    <code className={cn(style === "technical" && "font-mono")} title={label(style, "untraversedKind", k).title}>
                      {label(style, "untraversedKind", k).text}
                    </code>{" "}
                    {n}
                  </span>
                ))}
              </Unknown>
            )}
            {u.not_extracted.map((n) => (
              <Unknown key={`x${n.relation}`} tag={<NotExtractedChip />}>
                <span title={notExtractedRelationText(n.relation, n.nodes, style).title} data-relation={n.relation}>
                  {notExtractedRelationText(n.relation, n.nodes, style).text}
                </span>
              </Unknown>
            ))}
          </ul>
        )}
      </section>
      <section aria-label="Caps" data-testid="impact-caps">
        <Head>Caps</Head>
        <p className="m-0 py-1 text-[12.5px] text-muted-foreground">{data.caps.length === 0 ? "No cap reached." : data.caps.map((c) => capText(c, data.depth)).join(" ")}</p>
      </section>
      <p className="m-0 mt-1 text-[11.5px] text-muted-foreground">{footnote.text}</p>
    </div>
  );
}

/** ImpactTab requests and shows the traversal of one item. linkDepth is
 * the hash's depth (null: this browser's choice, the repository default
 * or 3 applies, T042); onDepth writes a chosen depth to the hash. */
export function ImpactTab({ item, linkDepth, onDepth }: { item: ChangeItem; linkDepth: number | null; onDepth: (d: number) => void }) {
  const c = useCompare();
  const seed = isSeed(item);
  const pref = useDepth("impact", linkDepth);
  const depth = pref.depth;
  const key = seed && c.choice.on && c.comparison !== null && depth !== null ? `impact#${c.comparison}#${item.id}#${String(depth)}` : null;
  const raw = usePaged(key, (cursor, signal) => c.client.impact({ base: c.choice.base, id: item.id, depth: depth ?? BUILT_IN_DEPTH.impact, cursor }, { signal }));
  // An answer from another index run than the summary shows as loading
  // while the summary reloads (R5-2).
  const firstState = useCheckedAnswer(c, raw.first.state, raw.more);
  const paged = { ...raw, first: { ...raw.first, state: firstState }, more: firstState.kind === "ok" ? raw.more : [] };
  if (!seed) {
    return (
      <p className="m-0 text-[12.5px] text-muted-foreground" data-testid="not-seed">
        {notSeedText(item)}
      </p>
    );
  }
  const s = paged.first.state;
  const paths = s.kind === "ok" ? [...s.data.paths, ...paged.more.flatMap((p) => p.paths)] : [];
  const lastCursor = paged.more.at(-1)?.next_cursor ?? (s.kind === "ok" ? s.data.next_cursor : null);
  const assumes = paths.some((p) => p.steps.some((st) => st.access !== null && st.access.assumes.length > 0));
  return (
    <ImpactCatalogue needed={assumes}>
    <div className="grid grid-cols-[minmax(0,1fr)] gap-2">
      <div className="flex flex-wrap items-center gap-2 text-[12px]">
        <DepthSwitch
          depth={depth}
          title={depthSourceText("impact", pref)}
          testId="impact-depth"
          onDepth={(d) => {
            pref.choose(d);
            onDepth(d);
          }}
        />
        <span className="text-muted-foreground">of {MAX_DEPTH} · grouped by relation, indented by hop</span>
      </div>
      {s.kind === "loading" && (
        <div aria-busy="true" className="h-24 animate-pulse rounded-md bg-muted">
          <span className="sr-only">Loading the impact</span>
        </div>
      )}
      {s.kind === "failed" && (
        <div role="alert" className="text-[12.5px]">
          <b>Could not load the impact.</b> {s.error.message}{" "}
          <button type="button" className={OUTLINE_BUTTON} onClick={paged.first.retry}>
            Retry
          </button>
        </div>
      )}
      {s.kind === "ok" && (
        <ImpactBody
          item={item}
          data={s.data}
          paths={paths}
          more={
            lastCursor !== null && (
              <div className="flex items-center gap-2 py-1">
                <button type="button" className={OUTLINE_BUTTON} disabled={paged.loadingMore} onClick={paged.loadMore}>
                  Next {Math.min(100, s.data.total - paths.length)} paths
                </button>
                {paged.moreError !== null && <span className="text-[12px] text-unmatched">{paged.moreError.message}</span>}
              </div>
            )
          }
        />
      )}
    </div>
    </ImpactCatalogue>
  );
}
