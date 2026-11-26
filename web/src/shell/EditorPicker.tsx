// The editor picker (T016k, decisions K1 sidebar, picker A, K3-K5): the
// "Editor for links" control in the sidebar footer, or a tile button in the
// collapsed rail of graph views, opening a popover with one radio row per
// option. Selecting applies at once (no Save). Keyboard: Tab to the
// control, Enter or Space opens with focus on the checked row, Up and Down
// move, Space or Enter select, Esc closes and returns focus. Every action
// also works with the mouse on the same visible controls; there are no
// global shortcuts.

import { ChevronDown, Info, RotateCw, TriangleAlert } from "lucide-react";
import { Popover } from "radix-ui";
import { useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import type { EditorPreset } from "@/api/ide";
import { EditorMark, type MarkBadge, type MarkKind, type SourceMarker } from "@/components/EditorMark";
import { PresetStatusIcon } from "@/components/PresetNote";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { editorName, useEditor, type EditorState } from "@/hooks/useEditor";
import { OFF } from "@/lib/editorChoice";
import { cn } from "@/lib/utils";

/** The caveat prefix of a preset status that is not "verified". */
const CAVEAT: Record<string, string> = { partly_verified: "Partly verified", limited: "Limited" };

function caveatOf(p: EditorPreset | undefined): { word: string; note: string; badge: MarkBadge } | null {
  if (p === undefined || p.status === "verified") return null;
  const word = CAVEAT[p.status] ?? "Verification unknown";
  return { word, note: p.note, badge: p.status === "limited" ? "info" : "warn" };
}

/** Active is what the closed control says: the editor of the links, its
 * source in words and every warning, all derived from the choice and the
 * server's preset list. */
export interface Active {
  name: string;
  mark: MarkKind;
  marker: SourceMarker;
  badge: MarkBadge;
  /** "Zed, your choice in this browser". */
  words: string;
  warnings: string[];
  /** The picker header: "Links open in Zed, your choice in this browser." */
  header: ReactNode;
}

const RANK: Record<string, number> = { bad: 3, warn: 2, info: 1 };

function worst(a: MarkBadge, b: MarkBadge): MarkBadge {
  return (RANK[b ?? ""] ?? 0) > (RANK[a ?? ""] ?? 0) ? b : a;
}

function presetMark(editor: string): MarkKind {
  return { kind: "preset", editor };
}

/** forWhom says how long a browser choice lasts: in this browser, or for
 * this page only (storage blocked, or the first-click chooser's
 * "Remember for this browser" left unchecked). */
function forWhom(state: EditorState): string {
  return state.saved && state.remembered ? "your choice in this browser" : "your choice for this page";
}

/** describeActive derives the closed control (exported for tests). */
export function describeActive(state: EditorState): Active {
  if (state.info.kind !== "ok" && state.choice === OFF) {
    // A stored "off" holds without the preset list (useEditor).
    const forPage = forWhom(state);
    const failed = state.info.kind === "failed" ? [`The editor list could not be loaded (${state.info.error}); links stay off`] : [];
    return {
      name: "No editor links",
      mark: { kind: "off" },
      marker: "yours",
      badge: failed.length > 0 ? "warn" : null,
      words: `No editor links, ${forPage}`,
      warnings: failed,
      header: "Editor links are off in this browser.",
    };
  }
  if (state.info.kind === "loading") {
    return { name: "Loading", mark: { kind: "custom", letters: "…" }, marker: null, badge: null, words: "loading the editor list", warnings: [], header: "Loading the editor list." };
  }
  if (state.info.kind === "failed") {
    const w = `The editor list could not be loaded (${state.info.error}); links use the repository default`;
    return { name: "Repository default", mark: { kind: "custom", letters: "?" }, marker: null, badge: "warn", words: "the repository default", warnings: [w], header: "Links use the repository default." };
  }
  const { presets, repository: repo } = state.info;
  const find = (e: string | null) => presets.find((p) => p.editor === e);
  const warnings: string[] = [];
  let badge: MarkBadge = null;
  if (state.choice !== null) {
    const forPage = forWhom(state);
    let out: Omit<Active, "warnings" | "badge">;
    if (state.choice === OFF) {
      out = { name: "No editor links", mark: { kind: "off" }, marker: "yours", words: `No editor links, ${forPage}`, header: "Editor links are off in this browser." };
    } else {
      const name = editorName(state, state.choice);
      out = {
        name,
        mark: presetMark(state.choice),
        marker: "yours",
        words: `${name}, ${forPage}`,
        header: (
          <>
            Links open in <strong className="font-semibold text-foreground">{name}</strong>, {forPage}.
          </>
        ),
      };
      const c = caveatOf(find(state.choice));
      if (c !== null) {
        warnings.push(`${c.word}: ${c.note.replace(/\.$/, "")}`);
        badge = c.badge;
      }
    }
    if (!state.saved) {
      warnings.push("Not saved: browser storage is blocked; the choice lasts until the page reloads");
      badge = worst(badge, "warn");
    }
    if (!repo.available && state.choice !== OFF) {
      warnings.push(`Config error: graphvein.yaml could not be loaded (${repo.error ?? "unknown error"})`);
      badge = "bad";
    }
    return { ...out, badge, warnings };
  }
  if (state.ignored !== null) {
    warnings.push(`Saved choice ignored: your saved editor “${state.ignored.slice(0, 64)}” is not offered by this server`);
    badge = "warn";
  }
  if (!repo.available) {
    warnings.push(`Config error: graphvein.yaml could not be loaded (${repo.error ?? "unknown error"})`);
    return {
      name: "None",
      mark: { kind: "none" },
      marker: null,
      badge: "bad",
      words: "No editor, the repository default is unavailable",
      warnings,
      header: "No editor links: the repository default is unavailable.",
    };
  }
  if (repo.custom || repo.editor === null) {
    const name = repo.label?.replace(/^Open in /, "") ?? "Custom link";
    warnings.push("Not verified: custom link_template from graphvein.yaml, not verified by Graphvein");
    return {
      name,
      mark: { kind: "custom", letters: name.slice(0, 2) },
      marker: "repo",
      badge: "warn",
      words: `${name}, the repository default from graphvein.yaml`,
      warnings,
      header: (
        <>
          Links use the custom link of graphvein.yaml (<strong className="font-semibold text-foreground">{name}</strong>), the repository default.
        </>
      ),
    };
  }
  const name = editorName(state, repo.editor, repo.label);
  const builtin = repo.source === "builtin";
  const c = caveatOf(find(repo.editor));
  if (c !== null) {
    warnings.push(`${c.word}: ${c.note.replace(/\.$/, "")}`);
    badge = worst(badge, c.badge);
  }
  const words = builtin ? `${name}, Graphvein's default (graphvein.yaml sets no editor)` : `${name}, the repository default from graphvein.yaml`;
  return {
    name,
    mark: presetMark(repo.editor),
    marker: builtin ? null : "repo",
    badge,
    words,
    warnings,
    header: (
      <>
        Links open in <strong className="font-semibold text-foreground">{name}</strong>,{" "}
        {builtin ? "Graphvein's default (graphvein.yaml sets no editor)" : "the repository default from graphvein.yaml"}.
      </>
    ),
  };
}

/** controlName is the accessible name of the closed control. */
export function controlName(a: Active): string {
  return [`Editor for links: ${a.words}`, ...a.warnings].join(". ");
}

/** EditorControl is the picker trigger with its popover. rail draws the
 * collapsed sidebar's tile button; otherwise the labelled footer row.
 * browserRung is the Settings page's "This browser" rung: the closed
 * control then names only this browser's choice, "Not set" when there is
 * none, because the rung states the browser's value, not the editor the
 * links use (the ladder says that). */
export function EditorControl({ rail = false, browserRung = false }: { rail?: boolean; browserRung?: boolean }) {
  const state = useEditor();
  const described = describeActive(state);
  const unset = browserRung && state.choice === null;
  const active: Active = unset ? { ...described, name: "Not set", mark: { kind: "custom", letters: "–" }, marker: null, badge: null } : described;
  const [open, setOpen] = useState(false);
  const [tip, setTip] = useState(false);
  const content = useRef<HTMLDivElement>(null);
  const name = unset ? `Editor for links in this browser: not set. Links use ${described.words}` : controlName(active);
  const trigger = rail ? (
    <button
      type="button"
      aria-label={name}
      data-testid="editor-control"
      className={cn(
        "grid h-9 min-w-10 place-items-center rounded-md px-[3px] border border-input bg-background outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring",
        open && "bg-accent ring-2 ring-ring/45",
      )}
    >
      <EditorMark mark={active.mark} marker={active.marker} badge={active.badge} />
    </button>
  ) : (
    <button
      type="button"
      aria-label={name}
      data-testid="editor-control"
      className={cn(
        "flex h-[38px] w-full items-center gap-2.5 rounded-md border border-input bg-background px-2 text-left text-[13px] outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring",
        open && "bg-accent ring-2 ring-ring/45",
      )}
    >
      <EditorMark mark={active.mark} marker={active.marker} badge={active.badge} />
      <b className={cn("min-w-0 truncate font-semibold", unset && "font-normal text-muted-foreground")} data-testid="editor-control-name">
        {active.name}
      </b>
      <ChevronDown aria-hidden="true" className="ml-auto size-[13px] flex-none text-muted-foreground" />
    </button>
  );
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      {/* Its own provider, so the control also renders outside the shell. */}
      <TooltipProvider delayDuration={200}>
        <Tooltip open={tip && !open} onOpenChange={setTip}>
          <TooltipTrigger asChild>
            <Popover.Trigger asChild>{trigger}</Popover.Trigger>
          </TooltipTrigger>
          <TooltipContent side={rail ? "right" : "top"} sideOffset={8} className="max-w-[300px] text-left text-xs leading-[1.4]">
            <ControlTip active={active} />
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <Popover.Portal>
        <Popover.Content
          ref={content}
          side="right"
          align="end"
          sideOffset={10}
          collisionPadding={8}
          aria-label="Editor for links"
          className="z-50 max-h-(--radix-popover-content-available-height) w-[356px] overflow-auto rounded-[calc(var(--radius)+2px)] border bg-popover p-1.5 text-[13px] text-popover-foreground shadow-[0_10px_30px_rgba(0,0,0,.16),0_2px_6px_rgba(0,0,0,.08)] outline-none"
          data-testid="editor-picker"
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            content.current?.querySelector<HTMLElement>('[role="radio"][tabindex="0"]')?.focus();
          }}
        >
          <PickerPanel state={state} active={active} />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

function ControlTip({ active }: { active: Active }) {
  const [name, ...rest] = active.words.split(", ");
  return (
    <span className="grid gap-[3px]">
      <span>
        <b className="font-semibold">{name}</b>
        {rest.length > 0 ? ` · ${rest.join(", ")}` : ""}
      </span>
      {active.warnings.map((w) => (
        <span key={w} className="text-stale">
          {w}
        </span>
      ))}
      <span className="opacity-75">Enter or click: choose editor</span>
    </span>
  );
}

/** Option is one row of the picker (and of the first-click chooser). */
export interface Option {
  value: string | null;
  label: ReactNode;
  mark: MarkKind;
  marker: SourceMarker;
  note: string;
  noteTone: "plain" | "warn" | "bad";
  icon: "ok" | "warn" | "info" | "source" | "bad" | null;
  /** The icon button's accessible name: the full note. */
  iconName: string;
  /** A preset that is not verified: its status and note in words; null
   * for a verified preset and the rows that are not presets. */
  caveat?: string | null;
  /** The preset of a preset row (its status icon shows the full note);
   * absent for the rows that are not presets. */
  preset?: EditorPreset;
  /** The one short muted line under a preset's name ("macOS only"); ""
   * for a verified preset. */
  shortNote?: string;
}

/** presetOptions are the picker's editor rows, one per preset in the
 * server's order, with its mark and its verification note. The first-click
 * chooser (components/EditorLink.tsx) offers the same rows, so both name
 * and caveat an editor the same way. */
export function presetOptions(state: EditorState): Option[] {
  if (state.info.kind !== "ok") return [];
  return state.info.presets.map((p) => {
    const c = caveatOf(p);
    return {
      value: p.editor,
      label: p.label.replace(/^Open in /, ""),
      mark: presetMark(p.editor),
      marker: state.choice === p.editor ? "yours" : null,
      note: c !== null && c.badge === "warn" ? `${c.word}: ${p.note}` : p.note,
      noteTone: c !== null && c.badge === "warn" ? "warn" : "plain",
      icon: p.status === "verified" ? "ok" : p.status === "limited" ? "info" : "warn",
      iconName: `${c?.word ?? "Verified"}: ${p.note}`,
      caveat: c === null ? null : `${c.word}: ${p.note.replace(/\.$/, "")}`,
      preset: p,
      shortNote: p.short_note,
    };
  });
}

function options(state: EditorState): Option[] {
  if (state.info.kind !== "ok") return [];
  const repo = state.info.repository;
  let repoNote: string;
  let repoName = "";
  let repoMark: MarkKind;
  if (!repo.available) {
    repoNote = `Unavailable: graphvein.yaml could not be loaded (${repo.error ?? "unknown error"})`;
    repoMark = { kind: "none" };
  } else if (repo.custom || repo.editor === null) {
    repoName = repo.label?.replace(/^Open in /, "") ?? "custom link";
    repoNote = "graphvein.yaml link_template; not verified by Graphvein";
    repoMark = { kind: "custom", letters: repoName.slice(0, 2) };
  } else {
    repoName = editorName(state, repo.editor, repo.label);
    repoNote = repo.source === "builtin" ? "Graphvein's default; graphvein.yaml sets no editor" : `graphvein.yaml ide: ${repo.editor}`;
    repoMark = presetMark(repo.editor);
  }
  const out: Option[] = [
    {
      value: null,
      label: (
        <>
          Use repository default{repoName !== "" ? " " : ""}
          <b className="font-semibold">{repoName}</b>
        </>
      ),
      mark: repoMark,
      marker: repo.available && repo.source !== "builtin" ? "repo" : null,
      note: repoNote,
      noteTone: repo.available ? (repo.custom ? "warn" : "plain") : "bad",
      icon: repo.available ? "source" : "bad",
      iconName: `Repository default: ${repoNote}`,
    },
  ];
  out.push(...presetOptions(state));
  out.push({
    value: OFF,
    label: "No editor links",
    mark: { kind: "off" },
    marker: state.choice === OFF ? "yours" : null,
    note: "“Open in …” buttons stay, disabled, with the reason.",
    noteTone: "plain",
    icon: null,
    iconName: "",
  });
  return out;
}

function Notice({ tone, children, testId }: { tone: "warn" | "info" | "bad"; children: ReactNode; testId: string }) {
  const Icon = tone === "info" ? Info : TriangleAlert;
  return (
    <div
      role="note"
      data-testid={testId}
      className={cn(
        "mx-1 mb-1.5 flex items-start gap-2 rounded-lg border px-2.5 py-[7px] text-xs leading-[1.45]",
        tone === "warn" && "border-stale/55 bg-stale/10",
        tone === "info" && "border-border bg-muted/60",
        tone === "bad" && "border-unmatched/50 bg-unmatched-soft",
      )}
    >
      <Icon
        aria-hidden="true"
        className={cn("mt-px size-[15px] flex-none", tone === "warn" ? "text-dynamic" : tone === "bad" ? "text-unmatched" : "text-muted-foreground")}
      />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function PickerPanel({ state, active }: { state: EditorState; active: Active }) {
  const radios = useRef<(HTMLDivElement | null)[]>([]);
  const base = useId();
  const opts = options(state);
  const checkedIndex = Math.max(
    0,
    opts.findIndex((o) => o.value === state.choice),
  );
  const move = (e: KeyboardEvent<HTMLDivElement>) => {
    const i = radios.current.findIndex((r) => r === document.activeElement);
    if (i === -1) return;
    let next = -1;
    if (e.key === "ArrowDown") next = (i + 1) % opts.length;
    else if (e.key === "ArrowUp") next = (i - 1 + opts.length) % opts.length;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = opts.length - 1;
    if (next === -1) return;
    e.preventDefault();
    radios.current[next]?.focus();
  };
  const repo = state.info.kind === "ok" ? state.info.repository : null;
  return (
    <div>
      <div className="grid gap-y-0.5 px-2 pt-1.5 pb-2">
        <b className="text-[13.5px] font-semibold">Editor for links</b>
        <span className="text-xs leading-[1.4] text-muted-foreground" data-testid="editor-picker-now">
          {active.header}
        </span>
      </div>
      {state.info.kind === "failed" && (
        <Notice tone="bad" testId="editor-notice-failed">
          The editor list could not be loaded: {state.info.error}.{" "}
          {state.choice === OFF ? "Links stay off, as you chose." : "Links use the repository default."}{" "}
          <button type="button" onClick={state.retry} className="inline-flex items-center gap-1 font-medium underline underline-offset-2">
            <RotateCw aria-hidden="true" className="size-3" />
            Retry
          </button>
        </Notice>
      )}
      {repo !== null && !repo.available && (
        <Notice tone="bad" testId="editor-notice-config">
          <b className="font-semibold">graphvein.yaml could not be loaded</b> ({repo.error ?? "unknown error"}).{" "}
          {state.choice === OFF
            ? "Links are off by your choice (No editor links)."
            : state.choice !== null
              ? `Links use your choice, ${editorName(state, state.choice)}.`
              : "No links until you choose an editor."}
        </Notice>
      )}
      {state.ignored !== null && (
        <Notice tone="warn" testId="editor-notice-ignored">
          Your saved editor <b className="font-semibold">“{state.ignored.slice(0, 64)}”</b> is not offered by this server. Links use the repository
          default until you choose again.
        </Notice>
      )}
      {!state.saved && state.choice !== null && (
        <Notice tone="info" testId="editor-notice-notsaved">
          Browser storage is blocked, so this choice is <b className="font-semibold">not saved</b>. It lasts until the page reloads.
        </Notice>
      )}
      {opts.length > 0 && (
        <div role="radiogroup" aria-label="Editor" className="grid gap-px" onKeyDown={move}>
          {opts.map((o, i) => {
            const key = o.value ?? "";
            const checked = i === checkedIndex;
            const noteId = `${base}-note-${String(i)}`;
            // A preset row shows at most its short note; its full note is
            // the status icon's. The other rows show their note when checked
            // (or when it is an error), so the list stays compact.
            const line = o.preset !== undefined ? (o.shortNote ?? "") : checked || o.noteTone === "bad" ? o.note : "";
            return (
              <div key={key} className={cn(i === 1 || i === opts.length - 1 ? "mt-1 border-t pt-1" : "")}>
                <div
                  ref={(el) => {
                    radios.current[i] = el;
                  }}
                  role="radio"
                  aria-checked={checked}
                  {...(line !== "" ? { "aria-describedby": noteId } : {})}
                  tabIndex={checked ? 0 : -1}
                  data-testid={`editor-option-${o.value ?? "default"}`}
                  onClick={() => {
                    state.setChoice(o.value);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === " " || e.key === "Enter") {
                      e.preventDefault();
                      state.setChoice(o.value);
                    }
                  }}
                  className={cn(
                    "grid cursor-pointer grid-cols-[16px_45px_1fr] items-center gap-x-2 rounded-md py-[3px] pr-2 pl-2 outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
                    checked && "bg-brand-soft hover:bg-brand-soft",
                  )}
                >
                  <span aria-hidden="true" className={cn("grid size-4 place-items-center rounded-full border", checked ? "border-brand" : "border-muted-foreground/60")}>
                    {checked && <span className="size-2 rounded-full bg-brand" />}
                  </span>
                  <EditorMark mark={o.mark} marker={o.marker} />
                  <span className="flex min-w-0 items-center gap-1">
                    <span className="min-w-0 font-medium">{o.label}</span>
                    {o.preset !== undefined && <PresetStatusIcon preset={o.preset} name={typeof o.label === "string" ? o.label : (o.value ?? "")} />}
                  </span>
                  {line !== "" && (
                    <span
                      id={noteId}
                      data-testid={`editor-note-${o.value ?? "default"}`}
                      className={cn(
                        "col-start-3 -mt-0.5 text-[11.5px] leading-[1.35]",
                        o.noteTone === "bad" ? "text-unmatched" : o.preset === undefined && o.noteTone === "warn" ? "text-dynamic" : "text-muted-foreground",
                      )}
                    >
                      {line}
                    </span>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
      <div className="mt-1 grid gap-1 border-t px-2 pt-2 pb-1 text-[11.5px] leading-[1.4] text-muted-foreground [&_svg]:size-3">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1" data-testid="editor-legend">
          <span className="inline-flex items-center gap-1">
            <TriangleAlert aria-hidden="true" className="text-dynamic" />
            partly verified
          </span>
          <span className="inline-flex items-center gap-1">
            <Info aria-hidden="true" />
            limited
          </span>
          <span className="inline-flex items-center gap-1">
            <span aria-hidden="true" className="size-2 rounded-full bg-brand" />
            yours
          </span>
          <span className="inline-flex items-center gap-1">
            <span aria-hidden="true" className="size-2 rounded-full border-2 border-muted-foreground" />
            repo default
          </span>
        </div>
        <span>
          A status icon holds the editor&rsquo;s note. Saved in this browser only; other browsers, server addresses and the CLI use the
          repository default.
        </span>
      </div>
    </div>
  );
}
