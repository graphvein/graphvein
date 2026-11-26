// The Workspace group of the sidebar (T038 variant A, decisions of
// 2026-11-24): every project and repository of the workspace, projects
// first then by name (the server's order). A project is expandable (the
// viewed one starts expanded); its members are indented one step with a
// faint guide line from the project row to its last member, and the
// viewed project's link status line sits inside the guide. Top-level
// repositories align with the project rows, so the group visibly ends
// before the next top-level entry. From 9 entries a filter box appears;
// the list scrolls on its own with a thin thumb. The icon rail opens the
// same tree in a popover (WorkspaceRailButton).
//
// Claim rows: WS-41 (dots), WS-42 (folder gone), WS-43 (link status only
// for the viewed project), WS-44 (row progress), WS-55 ("not indexed yet"
// with Index it), WS-58 (names shared in one store), WS-69 (unknown
// words).

import { Boxes, ChevronRight, CircleAlert, FolderGit2, FolderX, Link2, LoaderCircle, Plus, Search } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { Popover } from "radix-ui";

import type { Freshness, WorkspaceEntry } from "@/api/workspace";
import { useLabel } from "@/hooks/useLabels";
import { cn } from "@/lib/utils";
import { useProject, type ProjectState } from "@/shell/project";
import type { WorkspaceState } from "@/shell/workspace/WorkspaceRoot";
import { conflictHint, dotState, entryDot, entryTitle, errorWords, freshnessText, jobProgress, jobRunning, needsIndex, type DotState } from "@/shell/workspace/words";

/** FILTER_FROM is the entry count from which the filter box appears. */
export const FILTER_FROM = 9;

// The list's filter, expansion choices and scroll position outlive the
// shell's remount on a switch, so the list does not jump under the
// pointer.
let savedFilter = "";
let savedScroll = 0;
const expandedChoice = new Map<string, boolean>();

/** resetListState clears the list's kept state (tests). */
export function resetListState(): void {
  savedFilter = "";
  savedScroll = 0;
  expandedChoice.clear();
}

export function Dot({ state, label }: { state: DotState; label: string }) {
  return (
    <span
      aria-hidden="true"
      title={label}
      data-testid="ws-dot"
      data-state={state}
      className={cn(
        // inline-block: an inline span ignores width and height, so a dot
        // outside a flex parent would draw 0x0 (T038 review).
        "inline-block size-2 flex-none rounded-full align-middle",
        state === "fresh" && "bg-linked",
        state === "warn" && "bg-dynamic-bar",
        state === "unchecked" && "border-[1.5px] border-muted-foreground",
        state === "gone" && "border-[1.5px] border-dashed border-unmatched",
        state === "none" && "border border-muted-foreground",
      )}
    />
  );
}

const row = "flex h-7 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-[13px] whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-ring";

/** RowEnd is the end of an entry row: the job's progress while it runs,
 * "opening" while the viewed index opens, else the freshness dot. */
function RowEnd({ entry, opening, dot, dotLabel }: { entry: WorkspaceEntry; opening: boolean; dot: DotState; dotLabel: string }) {
  if (entry.job !== null && jobRunning(entry.job)) {
    const p = jobProgress(entry.job);
    return (
      <span className="ml-auto flex items-center gap-1.5 text-[11.5px] text-muted-foreground tabular-nums" data-testid="ws-progress">
        {p.text}
        <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin text-brand" />
      </span>
    );
  }
  if (opening) {
    return (
      <span className="ml-auto flex items-center gap-1.5 text-[11.5px] text-muted-foreground" data-testid="ws-opening">
        opening
        <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin text-brand" />
      </span>
    );
  }
  const failed = entry.job !== null && entry.job.state === "failed";
  return (
    <span className="ml-auto flex items-center gap-1.5">
      {failed && (
        <span className="text-[11px] text-unmatched" title={entry.job?.error?.message ?? "the last re-index failed"} data-testid="ws-failed">
          {entry.job?.error?.code ?? "failed"}
        </span>
      )}
      <Dot state={dot} label={dotLabel} />
    </span>
  );
}

interface IndexIt {
  busy: boolean;
  error: { code: string; message: string } | null;
  start: () => void;
}

/** useIndexIt starts an entry's run from its row (WS-55; the server
 * creates the store); the run's progress then shows on the row. */
function useIndexIt(entry: WorkspaceEntry, state: WorkspaceState): IndexIt {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ code: string; message: string } | null>(null);
  const start = () => {
    setBusy(true);
    setError(null);
    state.client.run(entry.id).then(
      () => {
        setBusy(false);
        state.watch(entry.id);
      },
      (err: unknown) => {
        setBusy(false);
        setError(errorWords(err instanceof Error ? err : new Error(String(err))));
      },
    );
  };
  return { busy, error, start };
}

/** IndexItLine is the line under an entry whose check found no index
 * (WS-55): "not indexed yet · Index it" on one line that never wraps,
 * starting under the entry's icon. The sidebar's list is about 187 px
 * wide: on the entry's own row the 157 px of this line would leave the
 * name no width at all, so the row keeps the name (truncated when long)
 * and the neutral dot, and the action sits under it. */
function IndexItLine({ entry, run, indent }: { entry: WorkspaceEntry; run: IndexIt; indent: string }) {
  if (!needsIndex(entry)) return null;
  return (
    <div className={cn("flex min-h-6 min-w-0 items-center gap-1.5 overflow-hidden pr-2 pb-0.5 text-[11.5px] whitespace-nowrap", indent)} data-testid="ws-not-indexed">
      <span className="flex-none text-muted-foreground">not indexed yet</span>
      <span aria-hidden="true" className="flex-none text-muted-foreground">
        ·
      </span>
      <button
        type="button"
        onClick={run.start}
        disabled={run.busy}
        aria-label={`Index ${entry.name}`}
        className="flex-none rounded-sm font-medium text-brand-text outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
        data-testid="ws-index-it"
      >
        {run.busy ? "Starting…" : "Index it"}
      </button>
    </div>
  );
}

/** RunErrorLine is a refused "Index it" under the row: its code, with the
 * message as the tooltip. */
function RunErrorLine({ run, indent }: { run: IndexIt; indent: string }) {
  if (run.error === null) return null;
  return (
    <div className={cn("flex min-h-6 items-center pr-2 pb-0.5 text-[11.5px] whitespace-nowrap", indent)}>
      <span className="min-w-0 truncate text-unmatched" title={run.error.message} data-testid="ws-index-error">
        {run.error.code}: {run.error.message}
      </span>
    </div>
  );
}

/** ConflictLine is the line under an entry in a shared-store conflict
 * (WS-58): which entry holds the name and where to rename one; the
 * server's whole message is the tooltip and the screen-reader text. */
function ConflictLine({ entry, indent }: { entry: WorkspaceEntry; indent: string }) {
  const hint = conflictHint(entry);
  if (hint === null || entry.conflict === null) return null;
  return (
    <div className={cn("grid min-w-0 pr-2 pb-0.5 text-[11.5px] leading-snug whitespace-nowrap", indent)} title={entry.conflict.message} data-testid="ws-conflict">
      <span className="min-w-0 truncate text-unmatched" aria-hidden="true">
        {hint.holder}
      </span>
      <span className="min-w-0 truncate text-muted-foreground" aria-hidden="true">
        {hint.rename}
      </span>
      <span className="sr-only">{entry.conflict.message}</span>
    </div>
  );
}

/** entryWord is what an entry row says of its state for screen readers
 * and its dot. */
function entryWord(entry: WorkspaceEntry): string {
  if (entry.state === "folder_gone") return `folder not found at ${entry.path}`;
  if (entry.conflict !== null) return entry.conflict.message;
  return freshnessText(entry.freshness);
}

/** ProgressBar is the run-counted bar under a running entry (WS-44). */
function ProgressBar({ entry, indent }: { entry: WorkspaceEntry; indent: string }) {
  if (entry.job === null || !jobRunning(entry.job)) return null;
  const { fraction } = jobProgress(entry.job);
  if (fraction === null) return null;
  return (
    <div className={cn("-mt-0.5 mb-0.5 h-[3px] overflow-hidden rounded-full bg-accent", indent, "mr-2")} aria-hidden="true">
      <div className="h-full rounded-full bg-brand" style={{ width: `${String(Math.round(fraction * 100))}%` }} />
    </div>
  );
}

function LinkStatusLine({ project }: { project: ProjectState }) {
  const pass = project.project.link_pass;
  const l = useLabel("linkStatus", pass);
  const fresh = pass.state === "fresh";
  return (
    <a
      href="#/project"
      title={`${l.title}\nProject view: members, links, declarations`}
      data-testid="link-pass"
      data-state={pass.state}
      className={cn("flex min-h-7 items-start gap-2 rounded-md px-2 py-1 text-[12px] leading-snug hover:bg-accent", fresh ? "text-muted-foreground" : "font-medium text-dynamic")}
    >
      {fresh ? <Link2 aria-hidden="true" className="mt-px size-3.5 flex-none" /> : <CircleAlert aria-hidden="true" className="mt-px size-3.5 flex-none" />}
      <span className="min-w-0">{l.text}</span>
    </a>
  );
}

interface MemberItem {
  repository: string;
  freshness: Freshness | null;
  checkout: boolean;
}

/** viewedMembers are the viewed project's members as its own document
 * states them (fresher than the list's recorded checks), with the time
 * the server computed each state. A member whose state carries no time
 * keeps the list's recorded check (or none): the UI never stamps the
 * time of a render on a state (T038 review). */
function viewedMembers(p: ProjectState, listed: WorkspaceEntry["members"]): MemberItem[] {
  return p.project.members.map((m) => {
    const recorded = (listed ?? []).find((l) => l.repository === m.repository)?.freshness ?? null;
    const at = m.freshness.checked_at;
    return {
      repository: m.repository,
      checkout: m.checkout,
      freshness: !m.checkout ? null : at === null ? recorded : { state: m.freshness.state, checked_at: at, changed_count: m.freshness.changed_count, error: m.freshness.error },
    };
  });
}

function matches(text: string, filter: string): boolean {
  return filter === "" || text.toLowerCase().includes(filter.toLowerCase());
}

function Highlight({ text, filter }: { text: string; filter: string }) {
  const i = filter === "" ? -1 : text.toLowerCase().indexOf(filter.toLowerCase());
  if (i < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, i)}
      <mark className="rounded-[2px] bg-brand-soft text-brand-text">{text.slice(i, i + filter.length)}</mark>
      {text.slice(i + filter.length)}
    </>
  );
}

function ProjectGroup({ entry, state, viewed, opening, filter }: { entry: WorkspaceEntry; state: WorkspaceState; viewed: ProjectState | null; opening: boolean; filter: string }) {
  const isViewed = state.entry?.id === entry.id;
  const [expanded, setExpanded] = useState(() => expandedChoice.get(entry.id) ?? isViewed);
  const members: MemberItem[] = isViewed && viewed !== null ? viewedMembers(viewed, entry.members) : (entry.members ?? []).map((m) => ({ repository: m.repository, freshness: m.freshness, checkout: m.checkout }));
  const nameMatches = matches(entry.name, filter);
  const shownMembers = nameMatches ? members : members.filter((m) => matches(m.repository, filter));
  const open = expanded || (filter !== "" && !nameMatches);
  const gone = entry.state === "folder_gone";
  const indexIt = useIndexIt(entry, state);
  const toggle = () => {
    expandedChoice.set(entry.id, !open);
    setExpanded(!open);
  };
  return (
    <li data-testid="ws-project" data-id={entry.id}>
      <div className={cn("group flex h-7 items-center rounded-md", isViewed && !open ? "bg-brand-soft" : "hover:bg-accent")}>
        <button type="button" aria-label={`${open ? "Collapse" : "Expand"} ${entry.name}`} aria-expanded={open} onClick={toggle} className="grid h-7 w-4 flex-none place-items-center rounded-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <ChevronRight aria-hidden="true" className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        </button>
        <button
          type="button"
          title={entryTitle(entry)}
          aria-current={isViewed ? "true" : undefined}
          disabled={gone}
          data-testid="ws-entry"
          data-id={entry.id}
          onClick={() => {
            if (!isViewed) state.switchTo(entry.id);
            if (!open) toggle();
          }}
          className={cn(row, "pl-1", isViewed ? "font-semibold" : "", gone && "cursor-default text-muted-foreground")}
        >
          {gone ? <FolderX aria-hidden="true" className="size-4 flex-none text-muted-foreground" /> : <Boxes aria-hidden="true" className="size-4 flex-none text-muted-foreground" />}
          <span className={cn("min-w-0 truncate", gone && "line-through")}>
            <Highlight text={entry.name} filter={filter} />
          </span>
          {!open && entry.members !== null && <span className="ml-auto text-xs text-muted-foreground tabular-nums">{entry.members.length}</span>}
          <span className="sr-only">, project, {entryWord(entry)}</span>
          <span className={cn(open || entry.members === null ? "ml-auto" : "")}>
            <RowEnd entry={entry} opening={isViewed && opening} dot={entryDot(entry)} dotLabel={gone ? "folder not found" : entryWord(entry)} />
          </span>
        </button>
      </div>
      <ProgressBar entry={entry} indent="ml-10" />
      <IndexItLine entry={entry} run={indexIt} indent="pl-5" />
      <RunErrorLine run={indexIt} indent="pl-5" />
      <ConflictLine entry={entry} indent="pl-5" />
      {open && (
        // The guide: a faint line from under the project's icon to its
        // last member; members one step (16 px) further in.
        <div className="relative ml-[22px] border-l border-border pl-[9px]" data-testid="ws-guide">
          {entry.members_error !== null && !isViewed && (
            <p className="m-0 px-2 py-1 text-[11.5px] text-dynamic" title={entry.members_error}>
              manifest does not load
            </p>
          )}
          <ul role={isViewed ? "radiogroup" : undefined} aria-label={`Repositories of ${entry.name}`} className="grid grid-cols-[minmax(0,1fr)] gap-px">
            {shownMembers.map((m) => {
              const selected = isViewed && viewed !== null && viewed.repository === m.repository;
              const dot = m.checkout ? dotState(m.freshness) : "none";
              const word = m.checkout ? freshnessText(m.freshness) : "no local checkout here";
              return (
                <li key={m.repository}>
                  <button
                    type="button"
                    role={isViewed ? "radio" : undefined}
                    aria-checked={isViewed ? selected : undefined}
                    data-testid="member-button"
                    data-repository={m.repository}
                    title={`${m.repository}: ${word}`}
                    disabled={gone}
                    onClick={() => {
                      if (isViewed && viewed !== null) viewed.select(m.repository);
                      else state.switchTo(entry.id, m.repository);
                    }}
                    className={cn(row, selected ? "bg-brand-soft font-medium text-brand-text" : "hover:bg-accent")}
                  >
                    <FolderGit2 aria-hidden="true" className="size-4 flex-none text-muted-foreground" />
                    <span className="min-w-0 truncate">
                      <Highlight text={m.repository} filter={filter} />
                    </span>
                    <span className="ml-auto">
                      <Dot state={dot} label={word} />
                    </span>
                    <span className="sr-only">, {word}</span>
                  </button>
                  {selected && !viewed.explicit && <DefaultNote repository={m.repository} />}
                </li>
              );
            })}
          </ul>
          {isViewed && viewed !== null && <LinkStatusLine project={viewed} />}
        </div>
      )}
    </li>
  );
}

function DefaultNote({ repository }: { repository: string }) {
  const note = useLabel("defaultMember", repository);
  return (
    <p className="m-0 pr-2 pb-0.5 pl-8 text-[11.5px] leading-snug text-dynamic italic" title={note.title} data-testid="default-member">
      {note.text}
    </p>
  );
}

function RepositoryRow({ entry, state, opening, filter }: { entry: WorkspaceEntry; state: WorkspaceState; opening: boolean; filter: string }) {
  const isViewed = state.entry?.id === entry.id;
  const gone = entry.state === "folder_gone";
  const indexIt = useIndexIt(entry, state);
  return (
    <li data-testid="ws-repository" data-id={entry.id}>
      <button
        type="button"
        title={entryTitle(entry)}
        aria-current={isViewed ? "true" : undefined}
        disabled={gone}
        data-testid="ws-entry"
        data-id={entry.id}
        onClick={() => {
          state.switchTo(entry.id);
        }}
        // pl-6: the icon lines up with the project rows' icon (after their
        // chevron).
        className={cn(row, "pl-6", isViewed ? "bg-brand-soft font-medium text-brand-text" : "hover:bg-accent", gone && "cursor-default text-muted-foreground hover:bg-transparent")}
      >
        {gone ? <FolderX aria-hidden="true" className="size-4 flex-none text-muted-foreground" /> : <FolderGit2 aria-hidden="true" className="size-4 flex-none text-muted-foreground" />}
        <span className={cn("min-w-0 truncate", gone && "line-through")}>
          <Highlight text={entry.name} filter={filter} />
        </span>
        <span className="sr-only">, {entryWord(entry)}</span>
        <RowEnd entry={entry} opening={isViewed && opening} dot={entryDot(entry)} dotLabel={gone ? "folder not found" : entryWord(entry)} />
      </button>
      <ProgressBar entry={entry} indent="ml-12" />
      <IndexItLine entry={entry} run={indexIt} indent="pl-6" />
      <RunErrorLine run={indexIt} indent="pl-6" />
      <ConflictLine entry={entry} indent="pl-6" />
    </li>
  );
}

/** WorkspaceTree is the list itself (sidebar and rail popover). */
export function WorkspaceTree({ state, opening, scroll = true }: { state: WorkspaceState; opening: boolean; scroll?: boolean }) {
  const viewed = useProject();
  const [filter, setFilterState] = useState(savedFilter);
  const setFilter = (v: string) => {
    savedFilter = v;
    setFilterState(v);
  };
  const entries = state.workspace.entries;
  const showFilter = entries.length >= FILTER_FROM;
  const active = showFilter ? filter.trim() : "";
  const shown = entries.filter((e) => matches(e.name, active) || (e.members ?? []).some((m) => matches(m.repository, active)));
  const listRef = useRef<HTMLDivElement>(null);
  const scrollingTimer = useRef<number>(0);

  // Restore the list's scroll after a remount, then bring the viewed row
  // into view if it is not.
  useLayoutEffect(() => {
    const el = listRef.current;
    if (el === null || !scroll) return;
    el.scrollTop = savedScroll;
    const current = el.querySelector<HTMLElement>('[aria-current="true"]');
    if (current !== null && typeof current.scrollIntoView === "function") {
      const top = current.offsetTop - el.offsetTop;
      if (top < el.scrollTop || top + current.offsetHeight > el.scrollTop + el.clientHeight) current.scrollIntoView({ block: "nearest" });
    }
  }, [scroll]);
  useEffect(
    () => () => {
      window.clearTimeout(scrollingTimer.current);
    },
    [],
  );

  let body: ReactNode;
  if (entries.length === 0) {
    body = (
      <p className="m-1 rounded-md border border-dashed px-3 py-2.5 text-[12.5px] text-muted-foreground" data-testid="ws-empty">
        No folder indexed yet.
      </p>
    );
  } else if (shown.length === 0) {
    body = <p className="m-0 px-2 py-1.5 text-[12.5px] text-muted-foreground">No entry matches “{active}”.</p>;
  } else {
    body = (
      // minmax(0, 1fr): an implicit grid column grows to its rows' unwrapped
      // width, so a long name would push the row's end out of the sidebar
      // instead of truncating.
      <ul aria-label="Workspace entries" className="grid grid-cols-[minmax(0,1fr)] gap-px" data-testid="ws-list">
        {shown.map((e) =>
          e.kind === "project" ? (
            <ProjectGroup key={e.id} entry={e} state={state} viewed={state.entry?.id === e.id ? viewed : null} opening={opening} filter={active} />
          ) : (
            <RepositoryRow key={e.id} entry={e} state={state} opening={opening} filter={active} />
          ),
        )}
      </ul>
    );
  }
  return (
    <div className={cn("flex min-h-0 flex-col", scroll && "flex-1")} data-testid="workspace">
      <div className="flex h-[30px] flex-none items-center gap-1.5 px-2 text-xs font-medium text-muted-foreground">
        <span>Workspace</span>
        {showFilter && <span className="tabular-nums">{active === "" ? entries.length : `${String(shown.length)} of ${String(entries.length)}`}</span>}
        <button
          type="button"
          onClick={() => {
            state.openAdd();
          }}
          className="ml-auto flex h-6 items-center gap-1 rounded-md px-1.5 text-[12.5px] font-medium text-brand-text outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring"
          data-testid="ws-add"
        >
          <Plus aria-hidden="true" className="size-3.5" />
          Add
        </button>
      </div>
      {showFilter && (
        <label className="mx-1 mb-1 flex h-8 flex-none items-center gap-1.5 rounded-md border bg-background px-2 text-[13px] focus-within:ring-2 focus-within:ring-ring">
          <Search aria-hidden="true" className="size-3.5 flex-none text-muted-foreground" />
          <input
            type="search"
            value={filter}
            onChange={(ev) => {
              setFilter(ev.target.value);
            }}
            placeholder={`Filter ${String(entries.length)} entries`}
            aria-label="Filter workspace entries"
            className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
            data-testid="ws-filter"
          />
        </label>
      )}
      <div
        ref={listRef}
        className={cn("gv-thin-scroll min-h-0 overflow-y-auto overscroll-contain", scroll && "flex-1")}
        data-testid="ws-scroll"
        onScroll={(ev) => {
          const el = ev.currentTarget;
          if (scroll) savedScroll = el.scrollTop;
          el.setAttribute("data-scrolling", "");
          window.clearTimeout(scrollingTimer.current);
          scrollingTimer.current = window.setTimeout(() => {
            el.removeAttribute("data-scrolling");
          }, 900);
        }}
      >
        {body}
      </div>
    </div>
  );
}

/** WorkspaceRailButton is the icon rail's Workspace icon: a popover with
 * the same tree. */
export function WorkspaceRailButton({ state, opening }: { state: WorkspaceState; opening: boolean }) {
  const [open, setOpen] = useState(false);
  const name = state.entry === null ? "Workspace" : `Workspace: ${state.entry.name}`;
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <button
          type="button"
          aria-label={name}
          title={name}
          data-testid="workspace-rail"
          className={cn("relative grid size-9 place-items-center rounded-md outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring", open && "bg-accent ring-2 ring-ring/45")}
        >
          <FolderGit2 aria-hidden="true" className="size-4" />
          {state.workspace.entries.some((e) => jobRunning(e.job)) && <span aria-hidden="true" className="absolute top-1 right-1 size-2 animate-pulse rounded-full bg-brand" />}
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="right"
          align="start"
          sideOffset={10}
          collisionPadding={8}
          aria-label="Workspace"
          data-testid="workspace-popover"
          className="z-50 flex max-h-[min(560px,var(--radix-popover-content-available-height))] w-[264px] flex-col rounded-[calc(var(--radius)+2px)] border bg-popover p-1.5 text-popover-foreground shadow-[0_10px_30px_rgba(0,0,0,.16),0_2px_6px_rgba(0,0,0,.08)] outline-none"
        >
          <WorkspaceTree state={state} opening={opening} />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
