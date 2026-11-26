// The list pane of the Changes section (V3 mockup with V1's density): the
// group's name, rows and counts, a chip per change kind (the summary's
// counts; one selects the server's `change` filter), a name filter, the
// test-scope toggle, one row per item and the paging footer with what was
// counted but not listed (moves, other changes, context).

import { useEffect, useEffectEvent, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";

import type { ChangeItem, ChangesItems, GroupCount } from "@/api/changes";
import { cn } from "@/lib/utils";
import { useLabel, useLabelStyle } from "@/hooks/useLabels";
import { label as labelFor } from "@/lib/labels";
import { ChangeChip, LabelChip, LabelRowTag, NotExtractedChip, RouteName } from "@/sections/changes/chips";
import type { Paged } from "@/sections/changes/usePaged";
import { changeLabel, fileLine, GROUP_ROWS, groupLabel, itemChangeLabel, itemRouteTitle, itemTitle, plural, type GroupEntry } from "@/sections/changes/words";
import { OUTLINE_BUTTON } from "@/sections/schema/parts";

function Row({ item, selected, onSelect, onKey, tabbable }: { item: ChangeItem; selected: boolean; onSelect: () => void; onKey: (e: ReactKeyboardEvent) => void; tabbable: boolean }) {
  const side = item.after ?? item.before;
  const where = side?.file === null || side === null ? null : fileLine(side.file, side.span);
  const route = itemRouteTitle(item);
  const kind = useLabel("nodeKind", item.kind);
  return (
    <li role="presentation">
      <button
        type="button"
        role="option"
        aria-selected={selected}
        tabIndex={tabbable ? 0 : -1}
        data-id={item.id}
        data-testid="change-row"
        onClick={onSelect}
        onKeyDown={onKey}
        className={cn(
          "grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-2 gap-y-0.5 border-b px-3 py-[7px] text-left hover:bg-accent focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring",
          selected && "bg-brand-soft shadow-[inset_3px_0_0_hsl(var(--brand))]",
        )}
      >
        <ChangeChip change={item.change} label={itemChangeLabel(item)} />
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="min-w-0 truncate font-mono text-[12.5px]" title={route !== null ? `${route.title}\n${item.identity}` : item.identity}>
            {route !== null ? <RouteName route={route} /> : itemTitle(item)}
          </span>
          {item.labels.includes("edited") && <LabelChip label="edited" />}
        </span>
        <span className="text-[11px] whitespace-nowrap text-muted-foreground tabular-nums">{item.impact_count !== null ? `→ ${String(item.impact_count)}` : ""}</span>
        <span className="col-span-2 col-start-2 truncate text-[11.5px] text-muted-foreground">
          {kind.text}
          {where !== null ? ` · ${where}` : ""}
          {item.test ? " · test" : ""}
        </span>
      </button>
    </li>
  );
}

export interface ItemListProps {
  groupId: string;
  group: GroupCount | null;
  notExtracted: boolean;
  entries: GroupEntry[];
  change: string | null;
  q: string;
  tests: boolean;
  sel: string | null;
  paged: Paged<ChangesItems>;
  onGroup: (g: string) => void;
  onChange: (c: string | null) => void;
  onQuery: (q: string) => void;
  onTests: (t: boolean) => void;
  onSelect: (id: string) => void;
}

export function ItemList(p: ItemListProps) {
  const { group, paged } = p;
  const [draft, setDraft] = useState(p.q);
  const listRef = useRef<HTMLUListElement>(null);
  // The name filter is sent 250 ms after the last key.
  const send = useEffectEvent((q: string) => {
    p.onQuery(q);
  });
  useEffect(() => {
    if (draft === p.q) return;
    const t = setTimeout(() => {
      send(draft);
    }, 250);
    return () => {
      clearTimeout(t);
    };
  }, [draft, p.q]);

  const s = paged.first.state;
  const items = s.kind === "ok" ? [...s.data.items, ...paged.more.flatMap((m) => m.items)] : [];
  const total = s.kind === "ok" ? s.data.total : null;
  const hidden = s.kind === "ok" ? s.data.hidden_tests : (group?.hidden_tests ?? 0);
  const cursor = paged.more.at(-1)?.next_cursor ?? (s.kind === "ok" ? s.data.next_cursor : null);
  const selIndex = items.findIndex((i) => i.id === p.sel);

  const move = (e: ReactKeyboardEvent, i: number) => {
    let next = -1;
    if (e.key === "ArrowDown") next = Math.min(items.length - 1, i + 1);
    else if (e.key === "ArrowUp") next = Math.max(0, i - 1);
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = items.length - 1;
    if (next === -1 || next === i) return;
    e.preventDefault();
    const it = items[next];
    if (it === undefined) return;
    p.onSelect(it.id);
    listRef.current?.querySelectorAll<HTMLButtonElement>("[data-testid=change-row]")[next]?.focus();
  };

  const label = groupLabel(p.groupId);
  const groupRows = GROUP_ROWS[p.groupId];
  const style = useLabelStyle();
  const rows = groupRows !== undefined ? labelFor(style, "contractRows", groupRows) : undefined;
  const filtered = p.change !== null || p.q !== "";
  return (
    <>
      <div className="flex flex-none flex-col gap-1.5 border-b px-3 pt-2.5 pb-2">
        <select
          aria-label="Group"
          className="h-8 rounded-md border border-input bg-background px-2 text-[13px] min-[600px]:hidden"
          value={p.groupId}
          onChange={(e) => {
            p.onGroup(e.target.value);
          }}
        >
          {p.entries.map((g) => (
            <option key={g.id} value={g.id}>
              {g.label} ({g.text})
            </option>
          ))}
        </select>
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="m-0 text-[15px] font-semibold" title={rows?.title} data-rows={GROUP_ROWS[p.groupId]}>
            {label}
          </h2>
          {rows !== undefined && <LabelRowTag tag={rows} />}
          <span className="text-xs text-muted-foreground tabular-nums">{p.notExtracted ? "" : group === null ? "" : String(group.items)}</span>
        </div>
        {group !== null && !p.notExtracted && Object.keys(group.changes).length > 0 && (
          <div className="flex flex-wrap gap-1" role="group" aria-label="Filter by change">
            {[null, ...Object.keys(group.changes)].map((k) => (
              <button
                key={k ?? "all"}
                type="button"
                aria-pressed={p.change === k}
                className={cn("h-[22px] rounded-full border px-2 text-[11.5px] whitespace-nowrap", p.change === k ? "border-transparent bg-foreground text-background" : "hover:bg-accent")}
                onClick={() => {
                  p.onChange(k);
                }}
              >
                {k === null ? "all" : `${changeLabel(k)} ${String(group.changes[k] ?? 0)}`}
              </button>
            ))}
          </div>
        )}
        {!p.notExtracted && (
          <div className="flex items-center gap-2">
            <input
              type="search"
              aria-label="Filter by name or identity"
              placeholder="Filter by name or identity…"
              className="h-7 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-[12.5px]"
              value={draft}
              onChange={(e) => {
                setDraft(e.target.value);
              }}
            />
            {(hidden > 0 || p.tests) && (
              <label className="flex items-center gap-1 text-[12px] whitespace-nowrap">
                <input
                  type="checkbox"
                  checked={p.tests}
                  onChange={(e) => {
                    p.onTests(e.target.checked);
                  }}
                />
                Include tests{!p.tests && hidden > 0 ? ` (${String(hidden)})` : ""}
              </label>
            )}
          </div>
        )}
      </div>
      <div className="min-h-0 flex-1 overflow-auto max-[599px]:overflow-visible" data-testid="change-list">
        {p.notExtracted ? (
          <div className="grid justify-items-start gap-2 px-3 py-4 text-[12.5px]" data-testid="group-not-extracted">
            <NotExtractedChip />
            <p className="m-0">Graphvein does not extract {label.toLowerCase()} yet, so this group is not compared. This is not “no change”.</p>
          </div>
        ) : s.kind === "loading" ? (
          <div aria-busy="true" className="grid gap-1 p-3">
            <span className="sr-only">Loading the items</span>
            {[0, 1, 2, 3].map((i) => (
              <div key={i} className="h-9 animate-pulse rounded bg-muted" />
            ))}
          </div>
        ) : s.kind === "failed" ? (
          <div role="alert" className="grid justify-items-start gap-2 p-3 text-[12.5px]">
            <span>
              <b>Could not load the items.</b> {s.error.message}
            </span>
            <button type="button" className={OUTLINE_BUTTON} onClick={paged.first.retry}>
              Retry
            </button>
          </div>
        ) : items.length === 0 ? (
          <p className="m-0 px-3 py-4 text-[12.5px] text-muted-foreground" data-testid="group-empty">
            {filtered ? "No item of this group matches the filter." : `No ${label} item changed: the group was compared on both sides.`}
            {hidden > 0 && !p.tests ? ` ${plural(hidden, "test-scope item")} hidden.` : ""}
          </p>
        ) : (
          <ul ref={listRef} role="listbox" aria-label={`${label} changes`} className="m-0 grid list-none p-0">
            {items.map((it, i) => (
              <Row
                key={it.id}
                item={it}
                selected={it.id === p.sel}
                tabbable={selIndex === -1 ? i === 0 : i === selIndex}
                onSelect={() => {
                  p.onSelect(it.id);
                }}
                onKey={(e) => {
                  move(e, i);
                }}
              />
            ))}
          </ul>
        )}
      </div>
      <div className="flex flex-none flex-wrap items-center gap-2 border-t px-3 py-1.5 text-[11.5px] text-muted-foreground" data-testid="list-footer">
        {total !== null && !p.notExtracted && (
          <span className="tabular-nums">{items.length === 0 ? `0 of ${String(total)}` : `1–${String(items.length)} of ${String(total)}`}</span>
        )}
        {cursor !== null && (
          <button type="button" className={cn(OUTLINE_BUTTON, "h-6")} disabled={paged.loadingMore} onClick={paged.loadMore}>
            Next {Math.min(100, (total ?? 0) - items.length)}
          </button>
        )}
        {paged.moreError !== null && <span className="text-unmatched">{paged.moreError.message}</span>}
        <span className="ml-auto">
          {group !== null && !p.notExtracted
            ? [group.moves > 0 ? plural(group.moves, "move") : null, group.other_changes > 0 ? `${String(group.other_changes)} other` : null, group.context > 0 ? `${String(group.context)} context` : null]
                .filter((x) => x !== null)
                .join(" · ") || "stable order"
            : ""}
          {group !== null && !p.notExtracted && (group.moves > 0 || group.other_changes > 0 || group.context > 0) ? " counted, not listed" : ""}
        </span>
      </div>
    </>
  );
}
