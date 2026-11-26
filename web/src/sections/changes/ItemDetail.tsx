// The item pane of the Changes section (V3 mockup): the item's change,
// cause and row, then the Change, Impact and Raw tabs. Change shows the
// base and head locations (the head's editor link only when the server
// built one: the head is the current index and the file is unchanged,
// ADR 0050; the base location is text), the changed fields, the proven
// reason, the stored fields compared and the inferred move hints.

import { useRef, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";

import type { ChangeItem, ItemSide } from "@/api/changes";
import { useApiRequest } from "@/hooks/useApiRequest";
import { useEditor } from "@/hooks/useEditor";
import { useLabelStyle } from "@/hooks/useLabels";
import { label } from "@/lib/labels";
import { presentIde } from "@/lib/ide";
import { cn } from "@/lib/utils";
import { EditorLink } from "@/components/EditorLink";
import { IdeCaveat, IdeMark } from "@/components/IdeParts";
import { CauseChip, ChangeChip, LabelChip, LabelRowTag, ProvChip, RouteName, RowTag, Tag } from "@/sections/changes/chips";
import { ImpactTab } from "@/sections/changes/ImpactTab";
import type { ItemTab } from "@/sections/changes/params";
import { fileLine, groupLabel, itemChangeLabel, itemRouteTitle, itemTitle, reasonText, shortName, sideMissingText, valueText } from "@/sections/changes/words";
import { FieldPrecisionChip, ItemPrecisionChips } from "@/sections/changes/precision";
import { Banner } from "@/sections/schema/parts";
import { useCheckedAnswer, useCompare } from "@/shell/compare";

const TABS: readonly ItemTab[] = ["change", "impact", "raw"];
const TAB_LABEL: Record<ItemTab, string> = { change: "Change", impact: "Impact", raw: "Raw" };

/** lookupQuery is the items `q` that finds an item by id: edges and gaps
 * match on their identity, which their id prefixes. */
export function lookupQuery(id: string): string {
  if (id.startsWith("edge:")) return id.slice(5);
  if (id.startsWith("gap:")) return id.slice(4);
  return id;
}

function HeadLink({ side }: { side: ItemSide }) {
  const editor = useEditor();
  if (side.ide === null) return null;
  const view = presentIde(null, side.ide, editor);
  if (view.href === null) return null;
  return (
    // The change endpoints take no editor= (docs/cli/serve.md): the link is
    // the repository default's whatever this browser chose, so it never asks.
    <EditorLink
      href={view.href}
      place={view.href}
      label={view.text}
      detail={view.title}
      follows={false}
      aria-label={view.name}
      data-testid="ide-button"
      className="inline-flex h-6 items-center gap-1 rounded-md border border-input bg-background px-[7px] text-xs font-medium hover:bg-accent [&_svg]:size-3.5"
    >
      <IdeMark view={view} />
      {view.text}
      <IdeCaveat view={view} />
    </EditorLink>
  );
}

function Location({ which, item }: { which: "base" | "head"; item: ChangeItem }) {
  const side = which === "base" ? item.before : item.after;
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[12.5px]" data-testid={`loc-${which}`}>
      <span className="w-9 flex-none text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">{which}</span>
      {side === null ? (
        <span className={cn(item.change === "unknown_now" || item.change === "known_now" || item.change === "link_unknown_now" ? "text-dynamic italic" : "text-muted-foreground")}>{sideMissingText(which, item)}</span>
      ) : side.file === null ? (
        <span className="text-muted-foreground">no file</span>
      ) : (
        <>
          <code className="font-mono [overflow-wrap:anywhere]">{fileLine(side.file, side.span)}</code>
          {which === "head" && <HeadLink side={side} />}
          {which === "head" && side.ide === null && side.span !== null && (
            <span className="text-[11.5px] text-muted-foreground">no editor link: the head is not the current index, or the file changed since it was indexed</span>
          )}
          {which === "base" && <span className="text-[11.5px] text-muted-foreground">baseline location (no editor link)</span>}
        </>
      )}
    </div>
  );
}

function FieldValue({ v, tone }: { v: unknown; tone: "old" | "new" | "unknown" }) {
  return (
    <code
      className={cn(
        "font-mono text-[12px] [overflow-wrap:anywhere]",
        tone === "old" && "text-unmatched line-through decoration-unmatched/40",
        tone === "new" && "text-linked",
        tone === "unknown" && "text-dynamic italic",
      )}
    >
      {valueText(v)}
    </code>
  );
}

function ChangedFields({ item }: { item: ChangeItem }) {
  if (item.fields.length === 0) return null;
  return (
    <div className="grid gap-1">
      <b className="text-[12.5px]">Changed fields</b>
      <table className="w-full table-fixed border-collapse text-[12.5px]" data-testid="changed-fields">
        <thead>
          <tr className="text-left text-[11px] text-muted-foreground">
            <th className="w-[28%] py-1 font-medium">field</th>
            <th className="py-1 font-medium">base</th>
            <th className="py-1 font-medium">head</th>
          </tr>
        </thead>
        <tbody>
          {item.fields.map((f) => (
            <tr key={f.name} className="border-t align-top" data-field-change={f.change}>
              <td className="py-1 pr-2 font-mono text-[12px] [overflow-wrap:anywhere]">
                {f.name}
                <FieldPrecisionChip item={item} field={f.name} />
              </td>
              <td className="py-1 pr-2">
                <FieldValue v={f.before} tone={f.change === "known_now" ? "unknown" : "old"} />
              </td>
              <td className="py-1">
                <FieldValue v={f.after} tone={f.change === "unknown_now" ? "unknown" : "new"} />
                {f.change !== "changed" && <ChangeChip change={f.change} className="ml-1.5 align-middle" />}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ComparedFields({ item }: { item: ChangeItem }) {
  const keys = [...new Set([...Object.keys(item.before?.fields ?? {}), ...Object.keys(item.after?.fields ?? {})])].sort();
  if (keys.length === 0) return null;
  const changed = new Set(item.fields.map((f) => f.name));
  return (
    <details open={item.fields.length === 0} className="text-[12.5px]">
      <summary className="cursor-pointer font-semibold">Stored fields the row compares</summary>
      <table className="mt-1 w-full table-fixed border-collapse" data-testid="compared-fields">
        <thead>
          <tr className="text-left text-[11px] text-muted-foreground">
            <th className="w-[28%] py-1 font-medium">field</th>
            <th className="py-1 font-medium">base</th>
            <th className="py-1 font-medium">head</th>
          </tr>
        </thead>
        <tbody>
          {keys.map((k) => (
            <tr key={k} className={cn("border-t align-top", changed.has(k) && "bg-brand-soft/50")}>
              <td className="py-1 pr-2 font-mono text-[12px] [overflow-wrap:anywhere]">{k}</td>
              <td className="py-1 pr-2 font-mono text-[12px] [overflow-wrap:anywhere]">{item.before === null ? <span className="text-muted-foreground">—</span> : valueText(item.before.fields[k])}</td>
              <td className="py-1 font-mono text-[12px] [overflow-wrap:anywhere]">{item.after === null ? <span className="text-muted-foreground">—</span> : valueText(item.after.fields[k])}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}

function ChangeTab({ item, onOpen }: { item: ChangeItem; onOpen: (id: string) => void }) {
  return (
    <div className="grid grid-cols-[minmax(0,1fr)] gap-3">
      <div className="grid gap-1.5">
        <Location which="base" item={item} />
        <Location which="head" item={item} />
      </div>
      {item.change === "unknown_now" && (
        <Banner tone="warn" testId="unknown-now">
          <b>Unknown now.</b> The head side could not read {item.after?.file ?? item.before?.file ?? "the file"} (a knowledge gap): the fact may still exist. It is not removed.
        </Banner>
      )}
      {item.change === "known_now" && (
        <Banner tone="info" testId="known-now">
          <b>Known now.</b> The base side could not read this file; the fact may have existed before. It is not added.
        </Banner>
      )}
      {item.reason !== null && (
        <Banner tone={item.reason === "gap" ? "warn" : "info"} testId="reason">
          <b>{item.change.startsWith("link_") ? "Reason proven" : "Reason"}:</b> {reasonText(item.reason)} <RowTag row={item.row} />
        </Banner>
      )}
      {item.provenance !== null && (
        <div className="flex items-center gap-2 text-[12.5px]">
          <span className="text-muted-foreground">edge</span>
          <ProvChip provenance={item.provenance} confidence={item.confidence} />
        </div>
      )}
      <ChangedFields item={item} />
      <ComparedFields item={item} />
      {item.hints.map((h) => (
        <Banner key={`${h.other}\u0000${h.basis}`} tone="info" testId="hint">
          <b>Possible move</b> <ProvChip provenance={h.provenance} confidence={null} /> <RowTag row={h.row} /> {item.change === "removed" ? "added as" : "was"}{" "}
          <button type="button" className="font-mono text-brand-text underline-offset-2 hover:underline" onClick={() => { onOpen(h.other); }}>
            {shortName(h.other)}
          </button>{" "}
          ({h.basis === "content_hash" ? "same kind and same content" : h.basis === "qualified_name" ? "same kind and same qualified name in another file" : h.basis}). Both items stay {item.change === "removed" ? "removed" : "added"}; nothing follows the hint.
        </Banner>
      ))}
      {item.group === "gaps" && Array.isArray(item.after?.fields.codes) && (
        <div className="text-[12.5px]">
          <span className="text-muted-foreground">diagnostics in the head run: </span>
          <code className="font-mono">{valueText(item.after.fields.codes)}</code>
        </div>
      )}
    </div>
  );
}

function RawTab({ item }: { item: ChangeItem }) {
  const c = useCompare();
  const key = c.choice.on && c.comparison !== null ? `raw#${c.comparison}#${item.id}` : null;
  const res = useApiRequest(key, (signal) => c.client.items({ base: c.choice.base, group: item.group, q: lookupQuery(item.id), tests: true, raw: true, limit: 50 }, { signal }));
  const s = useCheckedAnswer(c, res.state);
  const found = s.kind === "ok" ? s.data.items.find((i) => i.id === item.id) : undefined;
  const doc = s.kind === "ok" ? (found ?? item) : item;
  return (
    <div className="grid gap-2">
      {s.kind === "loading" && <div aria-busy="true" className="h-10 animate-pulse rounded bg-muted" />}
      {s.kind === "failed" && <p className="m-0 text-[12.5px] text-unmatched">Could not load the stored payloads: {s.error.message}</p>}
      {s.kind === "ok" && found === undefined && <p className="m-0 text-[12.5px] text-muted-foreground">The stored payloads were not found on this page of the comparison; the item as listed:</p>}
      <pre className="m-0 max-h-[60vh] overflow-auto rounded-md bg-code-bg p-2.5 font-mono text-[11.5px] leading-snug" data-testid="raw-json">
        {JSON.stringify(doc, null, 1)}
      </pre>
    </div>
  );
}

function Tabs({ tab, onTab, impact }: { tab: ItemTab; onTab: (t: ItemTab) => void; impact: number | null }) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const onKey = (e: ReactKeyboardEvent, i: number) => {
    let next = -1;
    if (e.key === "ArrowRight") next = (i + 1) % TABS.length;
    else if (e.key === "ArrowLeft") next = (i + TABS.length - 1) % TABS.length;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = TABS.length - 1;
    if (next === -1) return;
    e.preventDefault();
    const t = TABS[next];
    if (t !== undefined) onTab(t);
    refs.current[next]?.focus();
  };
  return (
    <div role="tablist" aria-label="Item views" className="flex flex-none gap-0.5 border-b px-3">
      {TABS.map((t, i) => (
        <button
          key={t}
          ref={(el) => {
            refs.current[i] = el;
          }}
          type="button"
          role="tab"
          id={`change-tab-${t}`}
          aria-selected={tab === t}
          aria-controls="change-tab-panel"
          tabIndex={tab === t ? 0 : -1}
          onKeyDown={(e) => {
            onKey(e, i);
          }}
          onClick={() => {
            onTab(t);
          }}
          className={cn("-mb-px h-[34px] border-b-2 px-2.5 text-[12.5px] whitespace-nowrap", tab === t ? "border-brand font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground")}
        >
          {TAB_LABEL[t]}
          {t === "impact" && impact !== null && <span className="ml-1 text-[11px] text-muted-foreground tabular-nums">{impact}</span>}
        </button>
      ))}
    </div>
  );
}

export interface ItemDetailProps {
  item: ChangeItem;
  tab: ItemTab;
  /** The hash's depth, null when the link names none (T042). */
  depth: number | null;
  onTab: (t: ItemTab) => void;
  onDepth: (d: number) => void;
  onOpen: (id: string) => void;
  /** The narrow layout's back step (below 600 px). */
  back: ReactNode;
}

/** ItemDetail is the item pane's content. */
export function ItemDetail({ item, tab, depth, onTab, onDepth, onOpen, back }: ItemDetailProps) {
  const route = itemRouteTitle(item);
  const style = useLabelStyle();
  const row = label(style, "contractRow", item.row);
  const kind = label(style, "nodeKind", item.kind);
  return (
    <>
      <div className="flex flex-none flex-col gap-1.5 border-b px-3 pt-2.5 pb-2">
        {back}
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <ChangeChip change={item.change} label={itemChangeLabel(item)} />
          <h2 className="m-0 min-w-0 font-mono text-[15px] font-semibold [overflow-wrap:anywhere]" title={route !== null ? `${route.title}\n${item.identity}` : item.identity}>
            {route !== null ? <RouteName route={route} /> : itemTitle(item)}
          </h2>
          <CauseChip cause={item.cause} />
          <LabelRowTag tag={row} />
          {item.test && <Tag title="a test-scope item">test</Tag>}
        </div>
        <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
          <span title={row.title} data-row={item.row}>
            {kind.text} · {groupLabel(item.group)}
          </span>
          {/* T020: the scope and file labels come first, then the item's own fact labels; never merged. */}
          <ItemPrecisionChips item={item} />
          {item.labels.map((l) => (
            <LabelChip key={l} label={l} />
          ))}
        </div>
      </div>
      <Tabs tab={tab} onTab={onTab} impact={item.impact_count} />
      <div id="change-tab-panel" role="tabpanel" aria-labelledby={`change-tab-${tab}`} className="min-h-0 min-w-0 flex-1 overflow-auto px-3.5 py-2.5 max-[599px]:overflow-visible" data-testid="item-panel">
        {tab === "change" && <ChangeTab item={item} onOpen={onOpen} />}
        {tab === "impact" && <ImpactTab item={item} linkDepth={depth} onDepth={onDepth} />}
        {tab === "raw" && <RawTab item={item} />}
      </div>
    </>
  );
}
