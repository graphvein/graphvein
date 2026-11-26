// The gap sheet (U4 A, D8): the calls behind a non-linked coverage count,
// or the endpoints never called, in a side sheet with its own search and
// grouping. It is a native <dialog> opened with showModal() (top layer,
// inert background, Esc closes it), not Radix Dialog, whose scroll lock
// injects <style> elements the page's CSP blocks (D11, A4). Search and
// grouping run in the browser: the lists are bounded by the repository's
// call count (D4).

import { Check, Copy, Search, X } from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";

import { CATEGORY_LABEL, CATEGORY_TONE, TRANSPORT_LABEL, callNoun, endpointNoun, isNeverRow, neverCalledLabel, neverScoped, rawLabel } from "@/api/categories";
import type { CallsResponse, EndpointRow, IdeStatus, IdeLink, Scope, TransportSummary } from "@/api/endpoints";
import type { CallProject } from "@/api/project";
import { Skeleton } from "@/components/ui/skeleton";
import type { RequestState } from "@/hooks/useApiRequest";
import { cn } from "@/lib/utils";
import { endpointLabel } from "@/sections/api/EndpointList";
import type { Gap } from "@/sections/api/params";
import {
  BADGE,
  CATEGORY_ICON,
  FileStateChip,
  Highlight,
  IdeButton,
  Method,
  TONE_CLASS,
  TransportBadge,
  WrappedPath,
  callExpression,
  callMethod,
  callerName,
  statusOf,
} from "@/sections/api/parts";

/** One row of the sheet, from a call or a never-called endpoint. */
export interface SheetItem {
  key: string;
  title: string;
  file: string;
  line: number | null;
  method: string | null;
  root: string | null;
  expression: string | null;
  status: string;
  reasonCode: string;
  reason: string;
  /** Every reason text, for search. */
  reasonsText: string;
  ide: IdeLink | null;
  /** The node's file_state (D9); null when the row has none. */
  fileState: string | null;
  targets: string[];
  endpointId: string | null;
  /** T022 B9 (serve --project): what the project's link pass says about
   * the row (a call linked or left ambiguous across repositories, a route
   * called from another repository); null otherwise. */
  project: string | null;
}

/** callProjectNote words a call's project links and outcomes for the
 * sheet; null when the pass says nothing beyond the repository's own
 * result. */
export function callProjectNote(p: CallProject | null): string | null {
  if (p === null) return null;
  if (p.declaration_conflict !== null) {
    const c = p.declaration_conflict;
    return `Linked inside the repository; the declaration names another scope (${c.consumer} declares ${c.declared.join("; ")})`;
  }
  if (p.links.length > 0) {
    const to = p.links.map((l) => `${l.target.repository} ${l.target.name !== "" ? l.target.name : l.target.identity}${l.stale ? " (stale link)" : ""}`).join(", ");
    return `Linked across repositories to ${to}; assumption: ${p.links[0]?.assumption ?? ""}`;
  }
  const ambiguous = p.outcomes.filter((o) => o.status === "ambiguous");
  if (ambiguous.length > 0) {
    const repos = [...new Set(ambiguous.flatMap((o) => o.candidates.map((c) => c.repository)))].sort();
    return `Across the project: several possible routes (${repos.join(", ")}), so no link`;
  }
  return null;
}

export type Grouping = "reason" | "file" | "root" | "none";

const EXPLANATION: Record<Gap["category"], string> = {
  unmatched: "The URL or command name is known, but no route or command matches it.",
  dynamic: "The URL, method or command name is computed at runtime, so these calls are listed but not linked to any endpoint.",
  wrapper: "Inner call sites of wrapper functions; the wrapper's call sites are listed as calls instead.",
  ambiguous: "Several endpoints match equally, so none is linked.",
  linked_across:
    "Not linked inside this repository; a stored project link connects each to a route of another repository of the project (inferred, with the assumption it rests on).",
  ambiguous_across:
    "Not linked inside this repository, and routes of several other repositories of the project match, so no link is made. Declare what this repository consumes in the project manifest to choose.",
  test_only: "Only test-scope endpoints match, so these calls are not linked.",
  unproven: "A command registry or permission is not proven, so these invokes are not linked.",
  plugin: "Plugin commands are not linked.",
  global: "Calls through the window.__TAURI__ globals are not linked.",
  other: "Statuses this Graphvein version does not know. They are never treated as linked.",
  never: "No stored client call is linked to these. Unresolved calls may still reach them; they may also be used by clients that are not indexed, or be dead code.",
};

/** In production scope test calls are not callers (docs/cli/serve.md). */
const NEVER_PRODUCTION =
  "No production client call is linked to these (test calls are excluded; turn on Include tests to see them). Unresolved calls may still reach them; they may also be used by clients that are not indexed, or be dead code.";

/** NEVER_PROJECT explains the never-called list under serve --project. */
const NEVER_PROJECT =
  "No client call of this repository and no project link from another repository is linked to these. Unresolved calls may still reach them; they may also be used by clients outside the project, or be dead code.";

function explanation(category: Gap["category"], scope: Scope, project = false): string {
  if (category === "never" && project) return NEVER_PROJECT;
  return category === "never" && scope === "production" ? NEVER_PRODUCTION : EXPLANATION[category];
}

/** itemsFromCalls turns a D4 response into sheet rows. */
export function itemsFromCalls(res: CallsResponse): SheetItem[] {
  return res.calls.map((c) => {
    const primary = c.reasons[0];
    const status = statusOf(c.node);
    return {
      key: c.node.identity,
      title: callerName(c.owner, c.node),
      file: c.node.file,
      line: c.node.span?.start_line ?? null,
      method: callMethod(c.node),
      root: c.node.source_root,
      expression: callExpression(c.node),
      status,
      reasonCode: primary?.code ?? "status",
      reason: primary?.text ?? `Status ${rawLabel(status)}`,
      reasonsText: c.reasons.map((r) => `${r.text} ${r.detail ?? ""}`).join(" "),
      ide: c.node.ide,
      fileState: c.node.file_state,
      targets: c.targets,
      endpointId: null,
      project: callProjectNote(c.project),
    };
  });
}

/** itemsFromNever lists the never-called endpoints of a transport. */
export function itemsFromNever(rows: EndpointRow[]): SheetItem[] {
  // Project mode (T022 B9 R1): only the endpoints without a caller in the
  // whole project; a row called from another member is not listed.
  return rows
    .filter((r) => isNeverRow(r) && !r.flags.includes("unread_use"))
    .map((r) => ({
      key: r.id,
      title: endpointLabel(r),
      file: r.file,
      line: r.line,
      method: null,
      root: null,
      expression: r.transport === "http" ? (r.path === null ? r.pattern : null) : r.handler,
      status: "",
      reasonCode: "never",
      reason: "No stored caller",
      reasonsText: "",
      ide: null,
      fileState: null,
      targets: [],
      endpointId: r.id,
      project:
        (r.project_caller_count ?? 0) > 0
          ? `No caller in this repository, but ${String(r.project_caller_count)} ${r.project_caller_count === 1 ? "call" : "calls"} of other repositories ${r.project_caller_count === 1 ? "is" : "are"} linked to it`
          : null,
    }));
}

/** searchItems keeps rows whose title, file, expression or reasons
 * contain q (case-insensitive). */
export function searchItems(items: SheetItem[], q: string): SheetItem[] {
  if (q === "") return items;
  const needle = q.toLowerCase();
  return items.filter((it) =>
    [it.title, it.file, it.expression ?? "", it.reason, it.reasonsText].some((s) => s.toLowerCase().includes(needle)),
  );
}

function dirOf(file: string): string {
  const i = file.lastIndexOf("/");
  return i === -1 ? "." : file.slice(0, i);
}

/** groupItems groups rows by the chosen key, largest group first (then by
 * key). */
export function groupItems(items: SheetItem[], grouping: Grouping): { key: string; label: string; items: SheetItem[] }[] {
  const m = new Map<string, { key: string; label: string; items: SheetItem[] }>();
  for (const it of items) {
    let key: string;
    let label: string;
    switch (grouping) {
      case "reason":
        key = it.reasonCode;
        label = it.reason;
        break;
      case "file":
        key = dirOf(it.file);
        label = `${key}/`;
        break;
      case "root":
        key = it.root ?? "(no root)";
        label = key;
        break;
      case "none":
        key = "";
        label = "All";
        break;
    }
    const g = m.get(key);
    if (g === undefined) m.set(key, { key, label, items: [it] });
    else g.items.push(it);
  }
  return [...m.values()].sort((a, b) => b.items.length - a.items.length || (a.key < b.key ? -1 : a.key > b.key ? 1 : 0));
}

function cell(s: string): string {
  return s.replace(/\\/g, "\\\\").replace(/\|/g, "\\|").replace(/\r?\n/g, " ");
}

/** copyText is the "Copy list" Markdown table. */
export function copyText(items: SheetItem[], never: boolean): string {
  const lines = [never ? "| file:line | endpoint | reason |" : "| file:line | status | reason |", "|---|---|---|"];
  for (const it of items) {
    const loc = it.line === null ? it.file : `${it.file}:${String(it.line)}`;
    lines.push(`| ${cell(loc)} | ${cell(never ? it.title : rawLabel(it.status))} | ${cell(it.reason)} |`);
  }
  return `${lines.join("\n")}\n`;
}

export interface GapSheetProps {
  gap: Gap;
  repository: string;
  /** The scope of the list and the calls. */
  scope: Scope;
  summary: TransportSummary | undefined;
  root: string | null;
  /** The raw statuses requested. */
  statuses: string[];
  /** Raw statuses of the category that cannot be requested (a comma in
   * the status); their rows are not listed and the sheet says so. */
  unlisted?: string[] | undefined;
  /** Set while the calls answer is from another index run than the list:
   * the rows are not shown and this note says why (D9). */
  mismatch?: string | null | undefined;
  /** The calls request; unused for the never-called sheet. */
  calls: RequestState<CallsResponse>;
  ide: IdeStatus;
  onRetry: () => void;
  onClose: () => void;
  onSelectEndpoint: (id: string) => void;
}

/** GapSheet renders the open sheet; mounting it opens the dialog and
 * unmounting closes it and returns focus to the opener. */
export function GapSheet(props: GapSheetProps) {
  const { gap, summary } = props;
  const never = gap.category === "never";
  const ref = useRef<HTMLDialogElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const [grouping, setGrouping] = useState<Grouping>(never ? "file" : "reason");
  const [copied, setCopied] = useState<"idle" | "done" | "failed">("idle");
  const onClosed = useEffectEvent(props.onClose);

  useEffect(() => {
    const dialog = ref.current;
    if (dialog === null) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    if (typeof dialog.showModal === "function") {
      if (!dialog.open) dialog.showModal();
    } else {
      dialog.setAttribute("open", "");
    }
    // showModal focuses the first focusable element (the close button);
    // the search box is where the keyboard is wanted.
    search.current?.focus();
    const closed = () => {
      onClosed();
    };
    dialog.addEventListener("close", closed);
    return () => {
      dialog.removeEventListener("close", closed);
      if (dialog.open && typeof dialog.close === "function") dialog.close();
      opener?.focus();
    };
  }, []);

  const items = useMemo(() => {
    if (never) return itemsFromNever(summary?.endpoints ?? []);
    return props.calls.kind === "ok" ? itemsFromCalls(props.calls.data) : [];
  }, [never, summary, props.calls]);
  const shown = useMemo(() => searchItems(items, query), [items, query]);
  const groups = useMemo(() => groupItems(shown, never ? "file" : grouping), [shown, grouping, never]);
  // Nothing is requested when every status of the category contains a
  // comma: then only the cannot-be-listed note is shown (no count, no
  // empty state).
  const unrequested = !never && props.statuses.length === 0;
  const onlyUnlisted = unrequested && props.unlisted !== undefined && props.unlisted.length > 0;
  const loading = !unrequested && !never && props.calls.kind === "loading";

  const neverScope = summary !== undefined ? neverScoped(summary) : null;
  const label = gap.category === "never" ? neverCalledLabel(props.scope, undefined, neverScope?.project === true) : `${CATEGORY_LABEL[gap.category]} ${callNoun(gap.transport, 2)}`;
  const tone = gap.category === "never" ? "never" : CATEGORY_TONE[gap.category];
  const Icon = CATEGORY_ICON[gap.category];
  const ofTotal = never
    ? `of ${String(summary?.endpoint_count ?? 0)} ${endpointNoun(gap.transport, summary?.endpoint_count ?? 0)}`
    : `of ${String(summary?.calls.total ?? 0)} ${callNoun(gap.transport, summary?.calls.total ?? 0)}`;
  const count = never ? (neverScope?.count ?? items.length) : unrequested ? 0 : props.calls.kind === "ok" ? props.calls.data.total : null;

  const close = () => {
    const dialog = ref.current;
    if (dialog !== null && dialog.open && typeof dialog.close === "function") dialog.close();
    else props.onClose();
  };

  const copy = () => {
    const text = copyText(shown, never);
    navigator.clipboard.writeText(text).then(
      () => {
        setCopied("done");
      },
      () => {
        setCopied("failed");
      },
    );
  };

  const groupOptions: { value: Grouping; label: string }[] = never
    ? [{ value: "file", label: "File" }]
    : [
        { value: "reason", label: "Reason" },
        { value: "file", label: "File" },
        { value: "root", label: "Root" },
        { value: "none", label: "None" },
      ];

  return (
    <dialog
      ref={ref}
      aria-labelledby="gap-title"
      className={cn(
        "fixed inset-y-0 right-0 left-auto m-0 h-dvh max-h-none w-[min(680px,calc(100vw-160px))] max-w-none border-0 border-l bg-popover p-0 text-foreground shadow-[-12px_0_32px_rgba(0,0,0,.2)]",
        "backdrop:bg-overlay open:flex open:flex-col open:animate-in open:duration-200 open:slide-in-from-right",
      )}
      onCancel={(e) => {
        // Esc: let the dialog close itself; the close event updates the view.
        e.stopPropagation();
      }}
    >
      <button
        type="button"
        title="Close (Esc)"
        aria-label="Close"
        className="absolute top-3.5 right-3.5 inline-flex size-7 items-center justify-center rounded-md hover:bg-accent"
        onClick={close}
      >
        <X aria-hidden="true" className="size-4" />
      </button>
      <div className="grid gap-1.5 border-b px-5 pt-[18px] pb-3">
        <h2 id="gap-title" className="m-0 flex flex-wrap items-center gap-2 pr-10 text-lg font-semibold tracking-tight">
          {label}
          {!onlyUnlisted && (
            <span className={cn(BADGE, TONE_CLASS[tone])}>
              <Icon aria-hidden="true" className="size-3" />
              {count ?? "…"}
            </span>
          )}
          <TransportBadge transport={gap.transport} />
          {props.root !== null && (
            <span className={cn(BADGE, "border-border font-mono font-medium")} title="Source root filter">
              root {props.root}
            </span>
          )}
        </h2>
        <p className="m-0 text-[13px] text-muted-foreground">
          {props.repository} · {ofTotal}
          {props.root !== null ? ` in root ${props.root}` : ""}
          {!never && props.statuses.length > 0 ? ` · raw status ${props.statuses.map(rawLabel).join(", ")}` : ""}.{" "}
          {explanation(gap.category, props.scope, neverScope?.project === true)}
        </p>
        {props.unlisted !== undefined && props.unlisted.length > 0 && (
          <p role="note" data-testid="unlisted-statuses" className="m-0 rounded-md border border-stale/55 bg-stale/10 px-2.5 py-1.5 text-[12.5px]">
            Rows with raw status {props.unlisted.map((u) => `"${u}"`).join(", ")} cannot be listed: the status contains a comma, which
            the calls request cannot carry. They are still counted in the coverage.
          </p>
        )}
        {props.mismatch !== undefined && props.mismatch !== null && (
          <p role="note" data-testid="run-mismatch" className="m-0 rounded-md border border-stale/55 bg-stale/10 px-2.5 py-1.5 text-[12.5px]">
            {props.mismatch}{" "}
            <button type="button" className="font-medium text-brand-text underline underline-offset-[3px]" onClick={props.onRetry}>
              Reload
            </button>
          </p>
        )}
        <div className="mt-1.5 flex flex-wrap items-center gap-2">
          <label className="flex h-8 min-w-[200px] flex-1 items-center gap-2 rounded-md border border-input px-2.5 text-[13px] text-muted-foreground focus-within:border-ring focus-within:ring-2 focus-within:ring-ring/50">
            <Search aria-hidden="true" className="size-3.5" />
            <input
              ref={search}
              type="search"
              aria-label="Search the list"
              placeholder={never ? "Search endpoint or file" : "Search names, files, reasons"}
              className="min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-muted-foreground"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
              }}
            />
          </label>
          <span className="text-xs text-muted-foreground">Group</span>
          <div role="group" aria-label="Group by" className="inline-flex h-[30px] items-center rounded-lg bg-muted p-[3px] text-muted-foreground">
            {groupOptions.map((o) => {
              const on = (never ? "file" : grouping) === o.value;
              return (
                <button
                  key={o.value}
                  type="button"
                  aria-pressed={on}
                  className={cn(
                    "h-6 rounded-md px-[9px] text-[12.5px] font-medium",
                    on && "bg-background text-foreground shadow-[0_1px_3px_rgba(0,0,0,.1)]",
                  )}
                  onClick={() => {
                    setGrouping(o.value);
                  }}
                >
                  {o.label}
                </button>
              );
            })}
          </div>
          <button
            type="button"
            className="inline-flex h-7 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-[12.5px] font-medium shadow-xs hover:bg-accent disabled:opacity-50 [&_svg]:size-3.5"
            disabled={loading || shown.length === 0}
            onClick={copy}
          >
            {copied === "done" ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
            {copied === "done" ? "Copied" : copied === "failed" ? "Copy failed" : "Copy list"}
          </button>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto" data-testid="gap-body">
        {onlyUnlisted ? null : !unrequested && !never && props.calls.kind === "failed" ? (
          <div className="grid justify-items-start gap-2 px-5 py-6 text-[13px]">
            <span>Could not load the list: {props.calls.error.message}</span>
            <button type="button" className="h-7 rounded-md border border-input px-2.5 text-[12.5px] font-medium hover:bg-accent" onClick={props.onRetry}>
              Retry
            </button>
          </div>
        ) : loading ? (
          <div className="grid gap-2 px-5 py-4" aria-busy="true">
            <span className="sr-only">Loading</span>
            {[0, 1, 2, 3, 4].map((i) => (
              <Skeleton key={i} className="h-12" />
            ))}
          </div>
        ) : groups.length === 0 ? (
          <div className="px-5 py-6 text-[13px] text-muted-foreground">
            {items.length === 0 ? "Nothing to list." : `Nothing matches "${query}".`}
          </div>
        ) : (
          groups.map((g) => (
            <section key={g.key} aria-label={`${g.label}: ${String(g.items.length)}`}>
              {(groups.length > 1 || grouping !== "none") && (
                <div className="sticky top-0 z-[2] flex h-[34px] items-center gap-2 border-b bg-muted/95 px-5 text-[12.5px] font-semibold backdrop-blur-sm">
                  <span className={cn("min-w-0 truncate", (never || grouping === "file" || grouping === "root") && "font-mono font-medium")}>
                    {g.label}
                  </span>
                  <span className="ml-auto font-medium text-muted-foreground tabular-nums">{g.items.length}</span>
                </div>
              )}
              {g.items.map((it) => (
                <div key={it.key} data-item={it.key} className="grid grid-cols-[minmax(0,1fr)_56px_auto] items-start gap-x-3 gap-y-1 border-b px-5 py-[9px] text-[13px]">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-1.5 font-medium [overflow-wrap:anywhere]">
                      <Highlight text={it.title} query={query} />
                      <FileStateChip state={it.fileState} />
                      {it.root !== null && (
                        <span className={cn(BADGE, "border-border font-mono text-[11px] font-medium text-muted-foreground")} title="Source root">
                          {it.root}
                        </span>
                      )}
                    </div>
                    <div className="mt-px text-foreground/82">
                      <WrappedPath file={it.file} line={it.line} highlight={query} />
                    </div>
                  </div>
                  <div className="pt-0.5">{it.endpointId === null && <Method method={it.method} />}</div>
                  <div className="row-span-2 flex items-start gap-1">
                    {it.endpointId !== null ? (
                      <button
                        type="button"
                        className="inline-flex h-6 items-center rounded-md px-2 text-xs font-medium text-brand-text hover:bg-accent"
                        onClick={() => {
                          const id = it.endpointId;
                          if (id !== null) props.onSelectEndpoint(id);
                          close();
                        }}
                      >
                        Show
                      </button>
                    ) : (
                      <IdeButton ide={it.ide} status={props.ide} place={`gap:${it.key}`} />
                    )}
                  </div>
                  <div className="col-span-2 grid gap-1">
                    {it.expression !== null && (
                      <div className="rounded-[4px] border bg-background/70 px-[7px] py-[3px] font-mono text-[11.5px] [overflow-wrap:anywhere]">
                        <Highlight text={it.expression} query={query} />
                      </div>
                    )}
                    <div className="text-[12.5px] text-muted-foreground">
                      <Highlight text={it.reason} query={query} />
                      {it.targets.length > 0 && (
                        <span> · one alternative links to {it.targets.length === 1 ? "an endpoint" : `${String(it.targets.length)} endpoints`}</span>
                      )}
                    </div>
                    {it.project !== null && (
                      <div className="text-[12.5px] text-brand-text" data-testid="gap-project">
                        {it.project}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </section>
          ))
        )}
      </div>
      <div className="flex flex-none items-center gap-2.5 border-t px-5 py-2 text-xs text-muted-foreground">
        <span>
          <b className="text-foreground">{shown.length}</b> shown of {items.length} · {groups.length}{" "}
          {groups.length === 1 ? "group" : "groups"}
        </span>
        <span className="flex-1" />
        <span>{TRANSPORT_LABEL[gap.transport]} · search and grouping run in this page</span>
      </div>
    </dialog>
  );
}
