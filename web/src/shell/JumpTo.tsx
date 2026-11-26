// Jump to (T040): the Ctrl+K / Cmd+K palette and the "Jump to…" box at the
// top of the sidebar. One search over the stored names and paths (GET
// /api/v0/search), grouped by kind in the server's order, plus the
// workspace's other entries ("Switch to <name>", GET /api/v0/workspace).
// Files and symbols open in the editor (the server-built link); routes,
// tables and Explorer operations open their section. In project mode
// every member is searched, the current member first, and opening another
// member's result switches the member. No result is never worded as proof
// of absence (row JS-14).
//
// An editor link opens through the app's one first-click chooser
// (components/EditorLink.tsx, developer decision 2026-11-25): while
// neither this browser nor graphvein.yaml chose an editor, opening a file
// or symbol shows the chooser anchored to its row instead. The pick is
// stored like any other; the link for the picked editor is then asked of
// the server (the same search with editor=, the same item found again,
// within PENDING_MS) and opened; picking the editor the links already use
// opens the row's link at once.

import { Dialog, Popover } from "radix-ui";
import { ArrowLeftRight, BookOpenText, Boxes, Braces, FileText, Route, Search, Table2, type LucideIcon } from "lucide-react";
import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import { useApiRequest } from "@/hooks/useApiRequest";

import { fetchSearch, type SearchAnswer, type SearchFetcher, type SearchItem, type SearchKind } from "@/api/search";
import { EditorChooserContent, PENDING_MS, editorNav } from "@/components/EditorLink";
import { RepoBadge } from "@/components/RepoBadge";
import { useEditor, withEditor } from "@/hooks/useEditor";
import { chooserNeeded } from "@/lib/ide";
import { formatCount } from "@/lib/count";
import { cn } from "@/lib/utils";
import { Method } from "@/sections/api/parts";
import {
  KIND_LABEL,
  KIND_SECTION,
  addRecent,
  buildGroups,
  chipOrder,
  flatRows,
  highlight,
  itemKey,
  matchWorkspace,
  move,
  nextGroup,
  noResultText,
  readRecent,
  recentForIndex,
  recentKey,
  searchedText,
  servedIndex,
  targetOf,
  type JumpGroup,
  type JumpRow,
  type KindFilter,
  writeRecent,
} from "@/shell/jumpModel";
import type { WorkspaceEntry } from "@/api/workspace";
import { useProject } from "@/shell/project";
import { useWorkspace } from "@/shell/workspace/WorkspaceRoot";

/** goToHash opens a section's route (outside the component, so the
 * React compiler does not see a global written during render). */
function goToHash(hash: string): void {
  window.location.hash = hash;
}

/** isMac says whether the shortcut is Cmd+K rather than Ctrl+K. */
function isMac(): boolean {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
}

/** SHORTCUT_TEXT is the key the box and the footer name. */
export function shortcutText(): string {
  return isMac() ? "⌘ K" : "Ctrl K";
}

/** isJumpShortcut says whether a key event is Ctrl+K or Cmd+K. */
export function isJumpShortcut(e: { key: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }): boolean {
  return (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k";
}

/** useJumpShortcut opens the palette on Ctrl+K / Cmd+K anywhere on the
 * page (and closes it when open). */
export function useJumpShortcut(toggle: () => void): void {
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (!isJumpShortcut(e)) return;
      e.preventDefault();
      toggle();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [toggle]);
}

function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="inline-flex h-[18px] items-center rounded-[4px] border bg-background px-1 font-mono text-[11px] text-muted-foreground">{children}</kbd>;
}

/** JumpToButton is the "Jump to…" box of the sidebar; the rail shows its
 * icon. */
export function JumpToButton({ onOpen, rail = false }: { onOpen: () => void; rail?: boolean }) {
  const key = shortcutText();
  if (rail) {
    return (
      <button type="button" onClick={onOpen} aria-label={`Jump to (${key})`} title={`Jump to (${key})`} className="grid size-9 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground" data-testid="jump-to-button">
        <Search aria-hidden="true" className="size-4" />
      </button>
    );
  }
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-keyshortcuts={isMac() ? "Meta+K" : "Control+K"}
      className="mb-2 flex h-8 w-full items-center gap-2 rounded-md border bg-background px-2 text-left text-sm text-muted-foreground shadow-xs hover:border-ring/50 hover:text-foreground"
      data-testid="jump-to-button"
    >
      <Search aria-hidden="true" className="size-4 flex-none" />
      <span className="min-w-0 flex-1 truncate">Jump to…</span>
      <Kbd>{key}</Kbd>
    </button>
  );
}

const KIND_ICON: Record<SearchKind | "workspace", LucideIcon> = {
  route: Route,
  operation: BookOpenText,
  table: Table2,
  symbol: Braces,
  file: FileText,
  workspace: ArrowLeftRight,
};

/** The palette list keeps one height in every state (no layout shift). */
const LIST_HEIGHT = "h-[min(420px,calc(100vh-260px))]";

/** SEARCH_DELAY_MS waits for typing to pause before a request. */
export const SEARCH_DELAY_MS = 120;

/** Items per kind: a few for every kind, many for one chosen kind. */
const LIMIT_ALL = 8;
/** NO_ENTRIES is the workspace list of a server without one. */
const NO_ENTRIES: readonly WorkspaceEntry[] = [];
const LIMIT_ONE = 50;

function Highlighted({ text, query, className }: { text: string; query: string; className?: string }) {
  return (
    <span className={cn("min-w-0 truncate", className)}>
      {highlight(text, query).map((s, i) =>
        s.match ? (
          <mark key={i} className="rounded-[2px] bg-brand-soft px-0 text-brand-text">
            {s.text}
          </mark>
        ) : (
          <span key={i}>{s.text}</span>
        ),
      )}
    </span>
  );
}

function Where({ file, line }: { file: string; line: number | null }) {
  // Right to left, so a long path loses its start, never its file name.
  return (
    <span className="ml-auto max-w-[45%] min-w-0 flex-none truncate text-left font-mono text-[12px] text-muted-foreground [direction:rtl]">
      <bdi>
        {file}
        {line !== null ? `:${String(line)}` : ""}
      </bdi>
    </span>
  );
}

function RowView({ row, index, query, project, selected, id, onPick, onHover, chooser }: { row: JumpRow; index: number; query: string; project: boolean; selected: boolean; id: string; onPick: (editor: boolean) => void; onHover: () => void; chooser?: ReactNode }) {
  const Icon = KIND_ICON[row.type === "workspace" ? "workspace" : row.item.kind];
  const base = cn("flex h-9 cursor-pointer items-center gap-2.5 border-l-2 px-4 text-[13.5px]", selected ? "border-brand bg-brand-soft/70" : "border-transparent");
  if (row.type === "workspace") {
    const e = row.entry;
    return (
      <li id={id} role="option" aria-selected={selected} className={base} onMouseMove={onHover} onClick={() => { onPick(false); }} data-testid="jump-row" data-kind="workspace" data-index={index}>
        {e.kind === "project" ? <Boxes aria-hidden="true" className="size-4 flex-none text-muted-foreground" /> : <Icon aria-hidden="true" className="size-4 flex-none text-muted-foreground" />}
        <span className="min-w-0 truncate">
          Switch to <Highlighted text={e.name} query={query} className="font-medium" />
        </span>
        <span className="text-[12px] text-muted-foreground">{e.kind === "project" ? "project" : "repository"}</span>
        <Where file={e.path} line={null} />
      </li>
    );
  }
  const it = row.item;
  const t = targetOf(row);
  const mono = it.kind === "route" || it.kind === "operation" || it.kind === "file";
  const li = (
    <li
      id={id}
      role="option"
      aria-selected={selected}
      aria-disabled={t.kind === "none" ? true : undefined}
      className={base}
      onMouseMove={onHover}
      onClick={(e) => {
        onPick(e.ctrlKey || e.metaKey);
      }}
      data-testid="jump-row"
      data-kind={it.kind}
      data-repository={it.repository}
      data-index={index}
    >
      <Icon aria-hidden="true" className="size-4 flex-none text-muted-foreground" />
      {(it.kind === "route" || it.kind === "operation") && <Method method={it.method} className="w-[52px] flex-none text-[12px]" />}
      <Highlighted text={it.label} query={query} className={cn(mono && "font-mono text-[13px]")} />
      {it.kind === "symbol" && it.node_kind !== null && <span className="flex-none text-[12px] text-muted-foreground">{it.node_kind.replace(/_/g, " ")}</span>}
      {it.kind === "table" && it.source !== null && <span className="flex-none text-[12px] text-muted-foreground">{it.source}</span>}
      {it.test === true && <span className="flex-none rounded-[4px] border px-1 text-[11px] text-muted-foreground">test</span>}
      {project && <RepoBadge repository={it.repository} />}
      {/* A file's label is its path already. */}
      {it.kind === "file" ? <span className="ml-auto" /> : <Where file={it.file} line={it.line} />}
      <span className={cn("w-[64px] flex-none text-right text-[12px] text-muted-foreground", !selected && "invisible")} aria-hidden="true">
        {t.kind === "editor" ? "→ editor" : t.kind === "none" ? "no link" : "→ open"}
      </span>
    </li>
  );
  if (chooser === undefined) return li;
  // The editor chooser of this row, anchored to it (the row stays as is).
  return (
    <Popover.Root
      open
      onOpenChange={(o) => {
        if (!o) onPick(false);
      }}
    >
      <Popover.Anchor asChild>{li}</Popover.Anchor>
      {chooser}
    </Popover.Root>
  );
}

function GroupHeading({ g }: { g: JumpGroup }) {
  const shown = g.rows.length;
  const notes: string[] = [];
  if (g.kind === "recent") notes.push("in this browser");
  else if (g.kind !== "workspace") notes.push(KIND_SECTION[g.kind]);
  if (g.split.length > 1) notes.push(g.split.map((s) => `${s.repository} ${formatCount(s.total)}`).join(" · "));
  if (g.truncated) notes.push(`${formatCount(shown)} shown, choose the chip to see more`);
  return (
    <li role="presentation" className="flex items-center gap-2 px-4 pt-3 pb-1 text-[11.5px] font-medium tracking-[.04em] text-muted-foreground uppercase">
      <span>{g.label}</span>
      <span className="tabular-nums">{formatCount(g.total)}</span>
      {notes.length > 0 && <span className="normal-case tracking-normal">· {notes.join(" · ")}</span>}
      <span className="h-px flex-1 bg-border" />
    </li>
  );
}

/** JumpToDialog is the palette. served is the repository the status
 * answer names (it keys the recent jumps outside project mode);
 * searchFetcher replaces the request in tests. The workspace entries come
 * from the workspace root (T038), which also switches without a reload. */
export function JumpToDialog({
  open,
  onOpenChange,
  served,
  searchFetcher = fetchSearch,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  served?: string | undefined;
  searchFetcher?: SearchFetcher;
}) {
  const [query, setQuery] = useState("");
  // The query searched: the typed one once typing pauses.
  const [debounced, setDebounced] = useState("");
  const timer = useRef<number | undefined>(undefined);
  const [filter, setFilter] = useState<KindFilter>("all");
  const [member, setMember] = useState<string>("all");
  const [last, setLast] = useState<SearchAnswer | null>(null);
  const [sel, setSel] = useState<{ key: string; index: number }>({ key: "", index: -1 });
  const [note, setNote] = useState<string | null>(null);
  // The row whose editor chooser is open (its key), and the request for
  // the picked editor's link while it runs.
  const [choosing, setChoosing] = useState<string | null>(null);
  const opening = useRef<AbortController | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const project = useProject();
  const editor = useEditor();
  const listId = useId();
  const listRef = useRef<HTMLUListElement>(null);

  // The workspace list the sidebar reads (T038); a server without one
  // lists none.
  const ws = useWorkspace();
  const wsEntries = ws?.workspace.entries ?? NO_ENTRIES;

  const q = debounced;
  const requestEditor = editor.requestEditor;
  const current = project?.repository ?? null;
  const scope = project === null || current === null ? undefined : member === "all" ? { member: "all" as const, first: current } : { member };
  const limit = filter === "all" ? LIMIT_ALL : LIMIT_ONE;
  const key = open && q !== "" ? JSON.stringify(["search", q, limit, requestEditor ?? null, scope ?? null]) : null;
  const search = useApiRequest(
    key,
    (signal) =>
      searchFetcher({ q, limit, ...withEditor(requestEditor), ...(scope !== undefined ? { project: scope } : {}) }, { signal }).then((a) => {
        setLast(a);
        return a;
      }),
    requestEditor === undefined,
  );
  const state = search.state;

  const viewed = ws?.entry?.id ?? null;
  // Recent jumps, per browser and per index viewed (the served index and
  // the workspace entry); read on each opening.
  const storeKey = recentKey(servedIndex(project?.project.project ?? null, served), viewed);
  const [stored, setStored] = useState(0);
  // eslint-disable-next-line react-hooks/exhaustive-deps -- stored only marks a write, so the list is read again
  const recent = useMemo(() => (open ? readRecent(storeKey) : []), [open, storeKey, stored]);
  const members = project?.project.members;
  const servedRepositories = useMemo(() => (members !== undefined ? members.map((m) => m.repository) : served !== undefined ? [served] : []), [members, served]);
  const recentShown = useMemo(() => recentForIndex(recent, servedRepositories), [recent, servedRepositories]);
  const remember = (item: SearchItem) => {
    const next = addRecent(recentForIndex(readRecent(storeKey), servedRepositories), item);
    writeRecent(storeKey, next);
    setStored((n) => n + 1);
  };
  // While a new search runs the last answer stays (no flicker, no shift).
  const shownAnswer = q === "" || query.trim() === "" ? null : state.kind === "ok" ? state.data : state.kind === "loading" ? last : null;
  const wsMatches = useMemo(() => matchWorkspace(wsEntries, viewed, query), [wsEntries, viewed, query]);
  const emptyQuery = query.trim() === "";
  const groups = useMemo(() => buildGroups(shownAnswer, filter, wsMatches, emptyQuery ? recentShown : []), [shownAnswer, filter, wsMatches, emptyQuery, recentShown]);
  const rows = useMemo(() => flatRows(groups), [groups]);
  // A new result list selects its first row.
  const rowsKey = rows.map((r) => r.key).join("\n");
  const selected = sel.key === rowsKey ? sel.index : rows.length > 0 ? 0 : -1;
  const setSelected = (f: (i: number) => number) => {
    setSel({ key: rowsKey, index: f(selected) });
  };

  useEffect(() => {
    if (selected < 0) return;
    const el = listRef.current?.querySelector(`[data-index="${String(selected)}"]`);
    if (el instanceof HTMLElement && typeof el.scrollIntoView === "function") el.scrollIntoView({ block: "nearest" });
  }, [selected]);

  const type = (value: string) => {
    setQuery(value);
    setNote(null);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      setDebounced(value.trim());
    }, SEARCH_DELAY_MS);
  };

  // Closing, however it happens, clears the query and the chips: the next
  // opening starts empty, with the recent jumps.
  const reset = (o: boolean) => {
    if (!o) {
      window.clearTimeout(timer.current);
      setQuery("");
      setDebounced("");
      setFilter("all");
      setMember("all");
      setNote(null);
      setChoosing(null);
      opening.current?.abort();
      opening.current = null;
    }
    onOpenChange(o);
  };

  const close = () => {
    reset(false);
  };

  const pick = (row: JumpRow, toEditor: boolean) => {
    const t = targetOf(row, toEditor);
    switch (t.kind) {
      case "none":
        setNote(t.reason);
        return;
      case "editor":
        if (chooserNeeded(editor)) {
          // Nothing chose an editor: the shared chooser asks first.
          setNote(null);
          setChoosing(row.key);
          return;
        }
        if (row.type === "item") remember(row.item);
        close();
        editorNav.open(t.link);
        return;
      case "workspace":
        close();
        // The workspace root's switch, as the sidebar's: no reload, the
        // target's remembered view restored (T038).
        ws?.switchTo(t.id);
        return;
      case "hash":
        if (row.type === "item") remember(row.item);
        close();
        goToHash(t.hash);
        if (project !== null && t.repository !== project.repository) project.select(t.repository);
        return;
    }
  };

  // The chooser's pick: stored as everywhere, then the row's link for that
  // editor (asked of the server unless the links already use it) opens.
  const pickEditor = (row: JumpRow, link: string, picked: string, keep: boolean) => {
    const before = editor.info.kind === "ok" ? editor.info.repository.editor : null;
    editor.setChoice(picked, keep);
    setChoosing(null);
    if (row.type !== "item") return;
    if (picked === before) {
      remember(row.item);
      close();
      editorNav.open(link);
      return;
    }
    const it = row.item;
    const want = itemKey(it);
    opening.current?.abort();
    const ctl = new AbortController();
    opening.current = ctl;
    const timeout = window.setTimeout(() => {
      ctl.abort();
    }, PENDING_MS);
    setNote(`Opening in ${picked}…`);
    const scope = project === null ? undefined : { member: it.repository };
    searchFetcher({ q: it.label, limit: LIMIT_ALL, editor: picked, ...(scope !== undefined ? { project: scope } : {}) }, { signal: ctl.signal })
      .then((a) => {
        if (ctl.signal.aborted) return;
        const found = a.groups.flatMap((g) => g.items).find((x) => itemKey(x) === want);
        if (found?.ide == null) {
          setNote(`No ${picked} link for this result; open it again`);
          return;
        }
        remember(found);
        close();
        editorNav.open(found.ide.link);
      })
      .catch(() => {
        if (opening.current === ctl) setNote(`The ${picked} link could not be built in time; open it again`);
      })
      .finally(() => {
        window.clearTimeout(timeout);
        if (opening.current === ctl) opening.current = null;
      });
  };

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setNote(null);
      setSelected((i) => move(i, e.key === "ArrowDown" ? 1 : -1, rows.length));
    } else if (e.key === "Tab" && rows.length > 0) {
      e.preventDefault();
      setNote(null);
      setSelected((i) => nextGroup(groups, i, e.shiftKey ? -1 : 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const row = rows[selected];
      if (row !== undefined) pick(row, e.ctrlKey || e.metaKey);
    }
  };


  const chips = chipOrder(shownAnswer?.kinds ?? ["route", "operation", "table", "symbol", "file"]);
  const total = (k: KindFilter): number | null => {
    if (shownAnswer === null) return null;
    if (k === "all") return Object.values(shownAnswer.totals).reduce((n, v) => n + v, 0);
    return shownAnswer.totals[k] ?? 0;
  };
  const anyItems = groups.some((g) => g.kind !== "workspace" && g.rows.length > 0);
  const activeId = selected >= 0 ? `${listId}-${String(selected)}` : undefined;
  // The flat index of each group's first row.
  const offsets = groups.reduce<number[]>((acc, g, i) => [...acc, i === 0 ? 0 : (acc[i - 1] ?? 0) + (groups[i - 1]?.rows.length ?? 0)], []);

  return (
    <Dialog.Root open={open} onOpenChange={reset}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-overlay/40" />
        <Dialog.Content
          aria-describedby={undefined}
          className="fixed top-[9vh] left-1/2 z-50 flex w-[min(1060px,calc(100vw-32px))] -translate-x-1/2 flex-col overflow-hidden rounded-xl border bg-popover text-popover-foreground shadow-[0_24px_60px_rgba(0,0,0,.22),0_4px_12px_rgba(0,0,0,.08)] outline-none"
          data-testid="jump-to"
        >
          <Dialog.Title className="sr-only">Jump to</Dialog.Title>
          <div className="flex items-center gap-3 border-b px-4 py-3">
            <Search aria-hidden="true" className="size-5 flex-none text-muted-foreground" />
            <input
              ref={inputRef}
              autoFocus
              value={query}
              onChange={(e) => {
                type(e.target.value);
              }}
              onKeyDown={onKey}
              role="combobox"
              aria-expanded="true"
              aria-controls={listId}
              aria-activedescendant={activeId}
              aria-autocomplete="list"
              aria-label="Jump to a route, operation, table, symbol or file"
              placeholder="Route, operation, table, symbol or file"
              spellCheck={false}
              className="min-w-0 flex-1 bg-transparent text-[17px] outline-none placeholder:text-muted-foreground/70"
              data-testid="jump-input"
            />
            <Kbd>Esc</Kbd>
          </div>
          <div className="flex flex-wrap items-center gap-1.5 border-b px-4 py-2" role="group" aria-label="Kinds">
            {chips.map((k) => {
              const n = total(k);
              return (
                <button
                  key={k}
                  type="button"
                  aria-pressed={filter === k}
                  onClick={() => {
                    setFilter(k);
                  }}
                  className={cn("inline-flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-[13px]", filter === k ? "border-brand/50 bg-brand-soft font-medium text-brand-text" : "hover:bg-accent")}
                  data-testid="jump-chip"
                  data-kind={k}
                >
                  {k === "all" ? "All" : KIND_LABEL[k]}
                  <span className={cn("tabular-nums", n === null && "invisible")}>{n === null ? "0" : formatCount(n)}</span>
                </button>
              );
            })}
          </div>
          {project !== null && (
            <div className="flex flex-wrap items-center gap-1.5 border-b px-4 py-2 text-[13px]" role="group" aria-label="Members" data-testid="jump-members">
              <span className="text-muted-foreground">Project {project.project.project}:</span>
              {["all", ...project.project.members.map((m) => m.repository)].map((m) => {
                const n = shownAnswer === null ? null : m === "all" ? Object.values(shownAnswer.totals).reduce((a, b) => a + b, 0) : Object.values(shownAnswer.members.find((x) => x.repository === m)?.totals ?? {}).reduce((a, b) => a + b, 0);
                return (
                  <button
                    key={m}
                    type="button"
                    aria-pressed={member === m}
                    onClick={() => {
                      setMember(m);
                    }}
                    className={cn("inline-flex h-7 items-center gap-1.5 rounded-full border px-2.5", member === m ? "border-brand/50 bg-brand-soft font-medium text-brand-text" : "hover:bg-accent")}
                    data-testid="jump-member"
                  >
                    {m === "all" ? "All members" : m}
                    {n !== null && (member === "all" || member === m) && <span className="tabular-nums">{formatCount(n)}</span>}
                  </button>
                );
              })}
              <span className="text-[12.5px] text-muted-foreground">{project.project.members.length > 1 ? `${project.repository} first; opening another member's result switches to it` : ""}</span>
            </div>
          )}
          <ul ref={listRef} id={listId} role="listbox" aria-label="Results" className={cn("overflow-y-auto py-1", LIST_HEIGHT)} data-testid="jump-list">
            {q !== "" && state.kind === "failed" && (
              <li role="presentation" className="px-4 pt-3 text-[13px] text-destructive" data-testid="jump-failed">
                The search failed: {state.error.message}
              </li>
            )}
            {q !== "" && shownAnswer !== null && !anyItems && state.kind === "ok" && (
              <li role="presentation" className="grid gap-1 px-4 pt-3 pb-2 text-[13px]" data-testid="jump-no-result">
                <span className="font-medium">
                  No {filter === "all" ? "stored name or path" : KIND_LABEL[filter].toLowerCase()} matches &ldquo;{q}&rdquo;.
                </span>
                <span className="text-muted-foreground">{noResultText(shownAnswer)}</span>
              </li>
            )}
            {groups.map((g, gi) => (
              <li key={g.key} role="presentation">
                <ul role="group" aria-label={g.label} className="grid">
                  <GroupHeading g={g} />
                  {g.unavailable.map((u) => (
                    <li key={u.repository} role="presentation" className="px-4 pb-1 text-[12.5px] text-dynamic">
                      Not searched in {u.repository}: {u.reason}
                    </li>
                  ))}
                  {g.rows.map((row, ri) => {
                    const i = (offsets[gi] ?? 0) + ri;
                    return (
                        <RowView
                          key={row.key}
                          row={row}
                          index={i}
                          query={q}
                          project={project !== null}
                          selected={i === selected}
                          id={`${listId}-${String(i)}`}
                          onPick={(toEditor) => {
                            if (choosing === row.key) {
                              // The chooser closed without a pick.
                              setChoosing(null);
                              return;
                            }
                            pick(row, toEditor);
                          }}
                          chooser={
                            choosing === row.key && row.type === "item" && row.item.ide !== null ? (
                              <EditorChooserContent
                                state={editor}
                                onPick={(picked, keep) => {
                                  pickEditor(row, row.item.ide?.link ?? "", picked, keep);
                                }}
                                onClose={() => {
                                  setChoosing(null);
                                }}
                                returnFocus={() => {
                                  inputRef.current?.focus();
                                }}
                              />
                            ) : undefined
                          }
                          onHover={() => {
                            if (selected !== i) setSelected(() => i);
                          }}
                        />
                    );
                  })}
                </ul>
              </li>
            ))}
            {query.trim() === "" && (
              <li role="presentation" className="px-4 pt-3 pb-2 text-[13px] text-muted-foreground" data-testid="jump-empty">
                Type part of a name or path: routes, Explorer operations, tables, symbols (functions, classes, types and more) and files. Only stored names and paths are searched, never file contents.
              </li>
            )}
          </ul>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t px-4 py-2 text-[12.5px] text-muted-foreground" data-testid="jump-footer">
            <span className="inline-flex items-center gap-1">
              <Kbd>↑</Kbd>
              <Kbd>↓</Kbd> move
            </span>
            <span className="inline-flex items-center gap-1">
              <Kbd>Enter</Kbd> open
            </span>
            <span className="inline-flex items-center gap-1">
              <Kbd>{isMac() ? "⌘" : "Ctrl"}</Kbd>
              <Kbd>Enter</Kbd> editor
            </span>
            <span className="inline-flex items-center gap-1">
              <Kbd>Tab</Kbd> next kind
            </span>
            <span className="inline-flex items-center gap-1">
              <Kbd>Esc</Kbd> close
            </span>
            <span role="status" className="ml-auto min-w-0 truncate" data-testid="jump-status">
              {note ?? (state.kind === "loading" ? "Searching…" : shownAnswer !== null ? searchedText(shownAnswer) : "")}
            </span>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
