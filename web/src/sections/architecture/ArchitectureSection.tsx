// The Architecture section (T026, combination X+, ADR 0079):
// the component map with its line chips, the Show toggles and Hidden
// note, the inspector, and the Links tab. The map's canvas is a lazy chunk
// (MapView: React Flow and the ELK worker); the header, the inspector and
// the Links tab are not, so they show while the canvas loads.
//
// AG-C2's chain drawn in place: selecting a card, a row or a chain member's
// "Chain from" requests /architecture/chain (hash `chain`, `dir`, `at`);
// chain.ts turns the answer into the map's opened cards, numbered members
// and folds, ChainPanel lists it in the inspector, and the Links tab can
// filter to it. AG-C3's Tables column belongs to the API section's Flow
// tab, not here.

import "@/sections/architecture/architecture.css";

import { CircleHelp, ExternalLink, Layers, X } from "lucide-react";
import { Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { httpArchitectureClient, type ArchitectureClient, type ArchitectureResponse, type ChainDirection } from "@/api/architecture";
import { ParseFailedNotice } from "@/components/ParseFailedNotice";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiRequest } from "@/hooks/useApiRequest";
import { depthSourceText, useDepth } from "@/hooks/useDepth";
import { useLabelStyle } from "@/hooks/useLabels";
import type { StatusView } from "@/hooks/useStatus";
import { BUILT_IN_DEPTH } from "@/lib/depthChoice";
import { label } from "@/lib/labels";
import { cn } from "@/lib/utils";
import { replaceHash, useHash } from "@/router";
import { compareRuns } from "@/sections/api/ApiSection";
import { buildChainMap, chainLinkIds, chainNumbersOnCards, chainOrder, chainView, componentChainFocus, flowHash, flowTarget, memberTitle, type ChainOverlay } from "@/sections/architecture/chain";
import { ChainPanel, type ChainState } from "@/sections/architecture/ChainPanel";
import type { ExpandedRows, MapActions } from "@/sections/architecture/context";
import { Inspector, itemTitle, shortName } from "@/sections/architecture/Inspector";
import { LinksTab } from "@/sections/architecture/LinksTab";
import type { LayoutRunner } from "@/sections/architecture/MapView";
import { bandHoverNodes, buildMap, componentNodeId, databaseTitle, focusOf, selectionMarks, type Mark, type ShowOptions } from "@/sections/architecture/model";
import { archHash, parseArchParams, type ArchParams } from "@/sections/architecture/params";
import { buildRows } from "@/sections/architecture/rows";
import { useDetails } from "@/sections/architecture/useDetails";

const MapView = lazy(() => import("@/sections/architecture/MapView"));

/** Below this section width the inspector floats over the canvas. */
const NARROW = 1080;

function useWidth(ref: React.RefObject<HTMLElement | null>): number | null {
  const [w, setW] = useState<number | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (el === null || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver((entries) => {
      const e = entries[0];
      if (e !== undefined) setW(e.contentRect.width);
    });
    ro.observe(el);
    return () => {
      ro.disconnect();
    };
  }, [ref]);
  return w;
}

function Toggle({ on, onChange, children, count, title }: { on: boolean; onChange: (on: boolean) => void; children: string; count?: number; title: string }) {
  return (
    <button
      type="button"
      aria-pressed={on}
      title={title}
      className={cn("inline-flex h-7 items-center gap-1 rounded-md border px-2 text-[13px] whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-ring", on ? "border-brand/50 bg-brand-soft text-brand-text" : "bg-card hover:bg-accent")}
      onClick={() => {
        onChange(!on);
      }}
    >
      {children}
      {count !== undefined && count > 0 && <span className="text-xs opacity-75">{count}</span>}
    </button>
  );
}

function Crumbs({ data, params, itemName }: { data: ArchitectureResponse | null; params: ArchParams; itemName: string | null }) {
  const style = useLabelStyle();
  const tag = (n: number) => {
    const l = label(style, "levelTag", n);
    return (
      <span className="rounded bg-brand-soft px-1 font-mono text-[11px] text-brand-text" title={l.title}>
        {l.text}
      </span>
    );
  };
  const comp = params.expanded !== null ? (data?.components.find((c) => c.id === params.expanded) ?? null) : null;
  const item = params.item;
  const file = item !== null ? item.slice(item.indexOf(":") + 1, item.includes("#") ? item.indexOf("#") : undefined) : null;
  return (
    <nav aria-label="Level" className="flex min-w-0 items-center gap-1.5 text-[15px] whitespace-nowrap" data-testid="arch-crumbs">
      {tag(1)}
      <h1 className="flex-none text-[19px] font-semibold tracking-tight">Architecture</h1>
      {comp !== null && (
        <>
          <span className="text-muted-foreground">/</span>
          {tag(2)}
          <span className="min-w-0 truncate font-medium">{comp.name}</span>
        </>
      )}
      {comp !== null && file !== null && (
        <>
          <span className="text-muted-foreground">/</span>
          {tag(3)}
          <span className="min-w-0 truncate font-mono text-[13px]" title={file}>
            {file.split("/").pop()}
          </span>
          <span className="text-muted-foreground">/</span>
          {tag(4)}
          <span className="min-w-0 truncate font-mono text-[13px]" title={item ?? ""}>
            {itemName ?? (item !== null ? shortName(item) : "")}
          </span>
        </>
      )}
    </nav>
  );
}

export interface ArchitectureSectionProps {
  status: StatusView;
  client?: ArchitectureClient | undefined;
  /** Replaces the page's ELK worker in tests. */
  elk?: LayoutRunner | undefined;
}

/** ArchitectureSection is the #/architecture page. */
export function ArchitectureSection({ status, client = httpArchitectureClient, elk }: ArchitectureSectionProps) {
  const hash = useHash();
  const params = useMemo(() => parseArchParams(hash), [hash]);
  const update = useCallback((patch: Partial<ArchParams>) => {
    replaceHash(archHash({ ...parseArchParams(window.location.hash), ...patch }));
  }, []);
  const style = useLabelStyle();
  const [reload, setReload] = useState(0);
  const [reloadedFor, setReloadedFor] = useState<string | null>(null);
  const scope = params.show.tests ? "all" : "production";
  const overview = useApiRequest(JSON.stringify(["architecture", scope, reload]), (signal) => client.overview(scope, { signal }));
  // The answer of the other scope stays on screen while a toggle loads.
  const [last, setLast] = useState<ArchitectureResponse | null>(null);
  const fresh = overview.state.kind === "ok" ? overview.state.data : null;
  if (fresh !== null && fresh !== last) setLast(fresh);
  const data = fresh ?? last;

  // A newer index run than the answer's: request everything again, once
  // per run (as the API section, D9).
  const dataRun = data === null ? null : (data.index?.finished_at ?? "none");
  const statusRun = status.kind === "ok" ? (status.status.index.last_run?.finished_at ?? "none") : null;
  if (dataRun !== null && statusRun !== null && compareRuns(statusRun, dataRun) > 0 && reloadedFor !== statusRun) {
    setReloadedFor(statusRun);
    setReload((r) => r + 1);
  }

  // The band's "+ n more libraries" chip opens the rest (not in the hash).
  const [bandAll, setBandAll] = useState(false);
  const baseModel = useMemo(() => (data === null ? null : buildMap(data, params.show, params.expanded, { bandAll })), [data, params.show, params.expanded, bandAll]);
  const bandDirs = baseModel?.nodes.some((n) => n.type === "band" && n.band.dirs) ?? false;

  // The band's lines light for the card or chip under the pointer, else
  // the one holding keyboard focus; a selected card or library marks its
  // neighbours' chips with the stored counts (T072).
  const [pointer, setPointer] = useState<string | null>(null);
  const [focused, setFocused] = useState<string | null>(null);
  const hover = pointer ?? focused;
  const hoverNodes = useMemo(() => (baseModel === null ? new Set<string>() : bandHoverNodes(baseModel, hover)), [baseModel, hover]);
  const marks = useMemo(() => (baseModel === null ? new Map<string, Mark>() : selectionMarks(baseModel, params.node)), [baseModel, params.node]);
  const setHover = useCallback((id: string | null, by: "pointer" | "focus") => {
    if (by === "pointer") setPointer(id);
    else setFocused(id);
  }, []);

  // The chain of the selection (AG16): requested once the overview is in.
  // Its depth (T042): the link's, else this browser's choice, else the
  // repository's ui.chain_depth, else 6; unknown while GET /api/v0/ui is
  // pending, so the chain is requested once.
  const chainDepth = useDepth("chain", params.depth);
  const depth = chainDepth.depth;
  const chainKey = params.chain !== null && data !== null && depth !== null ? JSON.stringify(["chain", params.chain, params.dir, depth, scope, reload]) : null;
  const chainReq = useApiRequest(chainKey, (signal) => client.chain({ id: params.chain ?? "", direction: params.dir, scope, depth: depth ?? BUILT_IN_DEPTH.chain }, { signal }));
  const chainData = chainReq.state.kind === "ok" && chainKey !== null ? chainReq.state.data : null;
  const view = useMemo(() => (chainData === null ? null : chainView(chainData)), [chainData]);
  const chainState: ChainState | null =
    params.chain === null ? null : view !== null ? { kind: "ok", view } : chainReq.state.kind === "failed" ? { kind: "failed", error: chainReq.state.error.message, retry: chainReq.retry } : { kind: "loading" };
  const chained = useMemo(() => {
    if (baseModel === null || view === null) return null;
    if (view.componentLevel) return { model: baseModel, focus: componentChainFocus(baseModel, view), numbers: chainNumbersOnCards(baseModel, view) };
    return { ...buildChainMap(baseModel, view), numbers: new Map<string, number[]>() };
  }, [baseModel, view]);
  const model = chained?.model ?? baseModel;
  const overlay: ChainOverlay | null = useMemo(() => (view !== null && chained !== null ? { view, numbers: chained.numbers, selected: params.at } : null), [view, chained, params.at]);

  // Component answers: the expanded card, the selected card, the ends of
  // the selected line.
  const wanted = useMemo(() => {
    const ids = new Set<string>();
    if (params.expanded !== null) ids.add(params.expanded);
    if (model !== null && params.node?.startsWith("c:") === true) ids.add(params.node.slice(2));
    if (model !== null && params.edge !== null) {
      const e = model.edges.find((x) => x.id === params.edge);
      for (const l of e?.links ?? []) for (const id of [l.source, l.target]) if (data?.components.some((c) => c.id === id) === true) ids.add(id);
    }
    return [...ids];
  }, [params.expanded, params.node, params.edge, model, data]);
  const details = useDetails(client, wanted, reload);

  const expandedRows: ExpandedRows = useMemo(() => {
    if (params.expanded === null || data === null) return { kind: "loading" };
    const d = details.get(params.expanded);
    if (d.kind === "ok") return { kind: "ok", rows: buildRows(d.data, data.links) };
    if (d.kind === "failed") return { kind: "failed", error: d.error };
    return { kind: "loading" };
  }, [params.expanded, data, details]);

  const focus = useMemo(() => {
    if (model === null) return null;
    if (chained !== null) return chained.focus;
    if (params.item !== null && params.expanded !== null) return focusOf(model, { node: componentNodeId(params.expanded), edge: null });
    return focusOf(model, { node: params.node, edge: params.edge });
  }, [model, chained, params.node, params.edge, params.item, params.expanded]);

  const actions: MapActions | null = useMemo(
    () =>
      data === null
        ? null
        : {
            data,
            selectedNode: params.node,
            selectedEdge: params.edge,
            selectedItem: params.item,
            focus,
            expandedRows,
            chain: overlay,
            selectMember: (identity) => {
              update({ at: identity });
            },
            // A component card draws its chain (CH9); other cards have none.
            selectNode: (id) => {
              update({ node: id, edge: null, item: null, chain: id.startsWith("c:") ? id.slice(2) : null, at: null });
            },
            selectEdge: (id) => {
              update({ node: null, edge: id, item: null, chain: null, at: null });
            },
            selectItem: (identity) => {
              update({ node: null, edge: null, item: identity, chain: identity, at: null });
            },
            expand: (component) => {
              update({ expanded: component, item: null, node: component !== null ? componentNodeId(component) : null, edge: null, chain: component, at: null });
            },
            clear: () => {
              update({ node: null, edge: null, item: null, chain: null, at: null });
            },
            retryRows: details.retry,
            toggleBand: () => {
              setBandAll((v) => !v);
            },
            bandDirs,
            hover,
            hoverNodes,
            setHover,
            marks,
          },
    [data, params.node, params.edge, params.item, focus, expandedRows, overlay, update, details.retry, bandDirs, hover, hoverNodes, setHover, marks],
  );

  const boxRef = useRef<HTMLDivElement>(null);
  const width = useWidth(boxRef);
  const narrow = width !== null && width < NARROW;
  const [layoutMs, setLayoutMs] = useState<number | null>(null);
  const selected = params.node !== null || params.edge !== null || params.item !== null || params.chain !== null;

  // Arrow keys walk the chain's members, Escape clears it (X+-2).
  const order = useMemo(() => (view !== null ? chainOrder(view) : []), [view]);
  useEffect(() => {
    if (params.chain === null || params.tab !== "map") return;
    const onKey = (e: KeyboardEvent) => {
      const t = e.target;
      if (t instanceof HTMLElement && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable)) return;
      if (e.key === "Escape") {
        update({ node: null, edge: null, item: null, chain: null, at: null });
        return;
      }
      if ((e.key === "ArrowDown" || e.key === "ArrowUp") && order.length > 0) {
        e.preventDefault();
        const cur = params.at !== null ? order.indexOf(params.at) : -1;
        const next = e.key === "ArrowDown" ? Math.min(order.length - 1, cur + 1) : Math.max(0, cur === -1 ? 0 : cur - 1);
        update({ at: order[next] ?? null });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [params.chain, params.tab, params.at, order, update]);

  const setShow = (patch: Partial<ShowOptions>) => {
    update({ show: { ...params.show, ...patch } });
  };

  if (overview.state.kind === "failed" && data === null) {
    return (
      <div className="rounded-lg border bg-card p-5 text-sm" role="alert">
        <p className="font-medium">The architecture could not be loaded.</p>
        <p className="mt-1 text-muted-foreground">{overview.state.error.message}</p>
        <button type="button" className="mt-2 font-medium text-brand-text underline" onClick={overview.retry}>
          Retry
        </button>
      </div>
    );
  }

  const counts = {
    libraries: data?.components.filter((c) => c.visibility === "folded").length ?? 0,
    tests: data?.components.filter((c) => c.visibility === "hidden" && c.hidden_by === "tests").length ?? 0,
    docs: data?.components.filter((c) => c.visibility === "hidden" && c.hidden_by === "docs").length ?? 0,
    configured: data?.components.filter((c) => c.visibility === "hidden" && c.hidden_by !== "tests" && c.hidden_by !== "docs").length ?? 0,
  };
  const anyHidden = model !== null && Object.values(model.hidden).some((n) => n > 0);
  const hiddenLabel = model !== null && anyHidden ? label(style, "hiddenNote", model.hidden) : null;
  const expDetail = params.expanded !== null ? details.get(params.expanded) : null;
  const itemNode =
    params.item !== null && expDetail?.kind === "ok"
      ? ([...expDetail.data.routes, ...expDetail.data.calls, ...expDetail.data.commands, ...expDetail.data.tables].find((n) => n.identity === params.item) ?? null)
      : null;
  const itemName = itemNode !== null ? itemTitle(itemNode) : null;
  const expandedName = params.expanded !== null ? (data?.components.find((c) => c.id === params.expanded)?.name ?? params.expanded) : null;
  const startMember = view?.member.get(view.response.start.id);
  const chainStartName =
    params.chain === null
      ? null
      : startMember !== undefined
        ? memberTitle(startMember)
        : (data?.components.find((c) => c.id === params.chain)?.name ?? (params.chain === params.item && itemName !== null ? itemName : shortName(params.chain)));
  const chainOnly = params.chain !== null && params.node === null && params.edge === null && (params.item === null || params.expanded === null);
  const chainPanel =
    chainState !== null && chainStartName !== null ? (
      <ChainPanel
        state={chainState}
        startName={chainStartName}
        dir={params.dir}
        depth={depth}
        depthTitle={depthSourceText("chain", chainDepth)}
        selected={params.at}
        back={archHash(params)}
        peekStart={chainOnly}
        databaseName={(id) => {
          const d = data?.databases.find((x) => x.id === id);
          return d !== undefined ? databaseTitle(d) : null;
        }}
        onSelect={(id) => {
          update({ at: id });
        }}
        onDirection={(d: ChainDirection) => {
          update({ dir: d, at: null });
        }}
        onDepth={(d) => {
          chainDepth.choose(d);
          update({ depth: d, at: null });
        }}
        onRestart={(id) => {
          update({ chain: id, at: null, node: null, item: null, edge: null });
        }}
        onLinks={() => {
          update({ tab: "links" });
        }}
        onClear={() => {
          update({ node: null, edge: null, item: null, chain: null, at: null });
        }}
      />
    ) : undefined;
  const flowTo = view !== null && view.response.map.open_in_flow ? flowTarget(view, params.at) : null;

  const inspector =
    data !== null && model !== null ? (
      <Inspector
        data={data}
        model={model}
        node={params.node}
        edge={params.edge}
        item={params.item}
        expanded={params.expanded}
        detail={details.get}
        chain={chainPanel}
        chainTitle={chainOnly ? chainStartName : null}
        show={params.show}
        onSelectEdge={(id) => {
          update({ node: null, edge: id, item: null, chain: null, at: null });
        }}
        onSelectNode={(id) => {
          update({ node: id, edge: null, item: null, chain: id.startsWith("c:") ? id.slice(2) : null, at: null });
        }}
        onExpand={(c) => {
          update({ expanded: c, item: null, node: c !== null ? componentNodeId(c) : params.node, edge: null });
        }}
        onClose={
          narrow
            ? () => {
                update({ node: null, edge: null, item: null, chain: null, at: null });
              }
            : undefined
        }
      />
    ) : null;

  return (
    <section ref={boxRef} className="-mx-5 -my-4 flex min-h-[560px] min-w-0 flex-1 flex-col overflow-hidden bg-card max-[1400px]:-mx-4 max-[1400px]:-my-3" aria-label="Architecture" data-testid="arch-section" data-layout-ms={layoutMs !== null ? Math.round(layoutMs) : undefined}>
      <header className="flex flex-none flex-wrap items-center gap-x-4 gap-y-2 border-b px-5 py-2.5">
        <Crumbs data={data} params={params} itemName={itemName} />
        <div className="inline-flex rounded-lg bg-muted p-0.5" role="tablist" aria-label="Architecture view">
          {(["map", "links"] as const).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              aria-selected={params.tab === t}
              className={cn("h-7 rounded-md px-3 text-[13.5px] font-medium", params.tab === t ? "bg-card shadow-xs" : "text-muted-foreground hover:text-foreground")}
              onClick={() => {
                update({ tab: t });
              }}
            >
              {t === "map" ? "Map" : "Links"}
              {t === "links" && data !== null && <span className="ml-1.5 text-xs text-muted-foreground tabular-nums">{data.links.length}</span>}
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Show">
          <span className="mr-0.5 text-[13px] text-muted-foreground">Show</span>
          <Toggle on={params.show.libraries} count={counts.libraries} onChange={(on) => { setShow({ libraries: on }); }} title="Workspace libraries in the band below the cards (AG4)">
            Libraries
          </Toggle>
          <Toggle on={params.show.tests} count={counts.tests} onChange={(on) => { setShow({ tests: on }); }} title="Test components and test-scope links (AG10)">
            Tests
          </Toggle>
          <Toggle on={params.show.docs} count={counts.docs} onChange={(on) => { setShow({ docs: on }); }} title="Docs-site components (O1)">
            Docs
          </Toggle>
          {counts.configured > 0 && (
            <Toggle on={params.show.configured} count={counts.configured} onChange={(on) => { setShow({ configured: on }); }} title="Components hidden by architecture.hide in graphvein.yaml">
              Configured
            </Toggle>
          )}
          <Toggle on={params.show.sdk} onChange={(on) => { setShow({ sdk: on }); }} title="SDK import chips on the cards, one per component (AG5)">
            SDK imports
          </Toggle>
        </div>
        {expandedName !== null && params.tab === "map" && (
          <button
            type="button"
            className="ml-auto inline-flex h-7 items-center rounded-md border px-2.5 text-[13px] hover:bg-accent"
            onClick={() => {
              update({ expanded: null, item: null });
            }}
          >
            Collapse {expandedName}
          </button>
        )}
      </header>
      {data !== null && data.notice !== null && <div className="border-b bg-dynamic-soft px-5 py-1.5 text-[13px] text-dynamic">{data.notice}</div>}
      {data !== null && <ParseFailedNotice count={data.parse_failed} className="border-b bg-dynamic-soft px-5 py-1.5" />}
      {data === null || model === null || actions === null ? (
        <div className="grid flex-1 place-items-center" aria-label="Loading the architecture">
          <Skeleton className="h-48 w-96" />
        </div>
      ) : params.tab === "links" ? (
        <LinksTab
          data={data}
          chain={
            view !== null && chainStartName !== null
              ? {
                  ids: chainLinkIds(data, view),
                  name: chainStartName,
                  onClear: () => {
                    update({ chain: null, at: null, node: null, item: null });
                  },
                }
              : null
          }
        />
      ) : (
        <div className={cn("grid min-h-0 flex-1", narrow ? "grid-cols-1" : "grid-cols-[minmax(0,1fr)_380px] max-[1400px]:grid-cols-[minmax(0,1fr)_350px]")}>
          <div className="relative min-h-0 min-w-0">
            {chainState !== null && chainStartName !== null && (
              <ChainBar
                name={chainStartName}
                state={chainState}
                flowHref={flowTo !== null ? flowHash(flowTo, params.at, archHash(params)) : null}
                onClear={() => {
                  update({ node: null, edge: null, item: null, chain: null, at: null });
                }}
              />
            )}
            {hiddenLabel !== null && (
              <div
                className={cn("absolute left-3 z-[5] flex max-w-[calc(100%-24px)] items-center gap-2 rounded-lg border bg-card/95 px-2.5 py-1 text-[12.5px] shadow-xs", chainState !== null ? "top-[58px]" : "top-3")}
                data-testid="hidden-note"
                title={hiddenLabel.title}
              >
                <CircleHelp aria-hidden="true" className="size-3.5 flex-none text-muted-foreground" />
                <span className="min-w-0 truncate">{hiddenLabel.text}</span>
                {overview.state.kind === "loading" && <span className="text-xs text-muted-foreground">· updating</span>}
              </div>
            )}
            <Suspense fallback={<Skeleton className="m-6 h-48 w-96" />}>
              <MapView model={model} actions={actions} expandedRows={expandedRows.kind === "ok" ? expandedRows.rows.length : null} elk={elk} onLayout={setLayoutMs} />
            </Suspense>
            {narrow && selected && (
              <div className="absolute top-3 right-3 bottom-3 z-[8] w-[min(380px,calc(100%-24px))] overflow-auto rounded-lg border bg-card shadow-lg" data-testid="inspector-overlay">
                {inspector}
              </div>
            )}
            {narrow && !selected && (
              <div className="absolute top-3 right-3 z-[6] rounded-lg border bg-card/95 px-2.5 py-1 text-xs text-muted-foreground shadow-xs">
                <Layers aria-hidden="true" className="mr-1 inline size-3.5" />
                Select a card or chip for details
              </div>
            )}
          </div>
          {!narrow && <aside className="min-h-0 overflow-auto border-l bg-card" aria-label="Inspector">{inspector}</aside>}
        </div>
      )}
    </section>
  );
}

/** ChainBar is the map's chain line (X+): what is drawn, "Open in Flow
 * tab" when the map folds members, and Clear. */
function ChainBar({ name, state, flowHref, onClear }: { name: string; state: ChainState; flowHref: string | null; onClear: () => void }) {
  const style = useLabelStyle();
  const view = state.kind === "ok" ? state.view : null;
  const l = view !== null ? label(style, "chainSummary", { start: name, steps: view.positions.length, members: view.members, folded: view.folded, direction: view.response.direction }) : null;
  return (
    <div className="absolute top-3 left-3 z-[6] flex max-w-[calc(100%-24px)] items-center gap-2 rounded-lg border border-brand/40 bg-card/95 px-2.5 py-1 text-[13px] shadow-sm" data-testid="chain-bar" title={l?.title}>
      <span className="min-w-0 truncate">{l !== null ? l.text : state.kind === "failed" ? `The chain of ${name} could not be loaded` : `Reading the chain of ${name}…`}</span>
      {flowHref !== null && (
        <a href={flowHref} className="inline-flex h-6 flex-none items-center gap-1 rounded-md bg-brand px-2 text-[12px] font-medium text-brand-foreground hover:bg-brand/90" data-testid="chain-bar-flow">
          <ExternalLink aria-hidden="true" className="size-3.5" />
          Open in Flow tab
        </a>
      )}
      <button type="button" className="inline-flex h-6 flex-none items-center gap-1 rounded-md border px-1.5 text-[12px] hover:bg-accent" onClick={onClear} aria-label="Clear the chain">
        Clear <X aria-hidden="true" className="size-3" />
      </button>
    </div>
  );
}
