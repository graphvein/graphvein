// The one editor link of the app: every "Open in <editor>" button and every
// ":line" link that opens a file in the editor renders through EditorLink,
// so all of them share one tooltip (editorTip) and one first-click chooser
// (developer decision 2026-11-25). EditorLink.test.tsx fails when another
// component renders an editor link of its own.
//
// The chooser: while neither this browser nor graphvein.yaml chose an
// editor (chooserNeeded), the first activation (click, Enter, the "o"
// shortcut's click) does not navigate. It opens a popover anchored to the
// link: "Open with" and the editor presets (the picker's rows: mark, name,
// a status icon whose popover holds the full note, and at most one short
// muted line), a checked "Remember for this browser" and "More in
// Settings". Picking an editor stores the choice as Settings does (or for
// this page only, unchecked) and opens this link in that editor. Esc or a
// click outside closes it without opening anything, and focus returns to
// the link. Once a choice exists, links open directly.
//
// The server builds each link for one editor, so a link for the picked
// editor exists only after the view has asked again with editor= (the
// choice changes every data request's key). The pick therefore records
// which link it opens (place) and the link it replaced; the EditorLink of
// that place opens its new link once it arrives. Picking the editor the
// links already use opens the link at once.

import { Popover } from "radix-ui";
import {
  useEffect,
  useId,
  useRef,
  useState,
  useSyncExternalStore,
  type AnchorHTMLAttributes,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
} from "react";

import { EditorMark } from "@/components/EditorMark";
import { PresetStatusIcon } from "@/components/PresetNote";
import { useEditor, type EditorState } from "@/hooks/useEditor";
import { chooserNeeded, editorTip } from "@/lib/ide";
import { settingsHash } from "@/router";
import { presetOptions, type Option } from "@/shell/EditorPicker";

/** editorNav opens a link built for the picked editor. Tests replace open,
 * because jsdom cannot navigate to an editor's URL scheme. */
export const editorNav = {
  open(href: string): void {
    window.location.assign(href);
  },
};

/** PENDING_MS bounds the wait for the new link: a view that never asks
 * again (an endpoint without editor=) opens nothing rather than a late
 * surprise. */
export const PENDING_MS = 8000;

interface Pending {
  editor: string;
  place: string;
  from: string;
  until: number;
}

let pending: Pending | null = null;
const listeners = new Set<() => void>();

function setPending(next: Pending | null): void {
  pending = next;
  for (const l of listeners) l();
}

function subscribePending(onChange: () => void): () => void {
  listeners.add(onChange);
  return () => {
    listeners.delete(onChange);
  };
}

function readPending(): Pending | null {
  return pending;
}

/** resetEditorLinks drops a pending open (tests). */
export function resetEditorLinks(): void {
  setPending(null);
}

function stop(e: { stopPropagation: () => void }): void {
  e.stopPropagation();
}

/** EditorLinkProps are the anchor's own attributes plus what the shared
 * parts need. */
export interface EditorLinkProps extends Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href" | "title"> {
  /** The server-built link. */
  href: string;
  /** What the link opens, stable across editors (a node identity, a
   * file:line): the pick opens the link of this place once it is rebuilt. */
  place: string;
  /** The button's text, "Open in VS Code" (the tooltip's first line). */
  label: string;
  /** The rest of the tooltip (the link, its source, caveats). */
  detail?: string;
  /** False for a link the browser's choice does not build (the Changes
   * section's head link): it opens directly and never asks. */
  follows?: boolean;
  children?: ReactNode;
}

/** EditorLink is an anchor to the editor with the shared tooltip and the
 * first-click chooser. */
export function EditorLink({ href, place, label, detail, follows = true, onClick, children, ...rest }: EditorLinkProps) {
  const state = useEditor();
  const choosing = follows && chooserNeeded(state);
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLAnchorElement>(null);
  const waiting = useSyncExternalStore(subscribePending, readPending);

  // The pick's link, rebuilt for the picked editor, opens once it is here.
  useEffect(() => {
    if (waiting === null || waiting.place !== place) return;
    if (Date.now() > waiting.until) {
      setPending(null);
      return;
    }
    if (state.requestEditor !== waiting.editor || href === waiting.from) return;
    setPending(null);
    editorNav.open(href);
  }, [waiting, place, href, state.requestEditor]);

  const tip = editorTip(label, choosing);
  const title = detail !== undefined && detail !== "" ? `${tip}\n${detail}` : tip;

  const activate = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (!choosing || e.defaultPrevented) return;
    e.preventDefault();
    setOpen(true);
  };

  const pick = (editor: string, remember: boolean) => {
    const before = state.info.kind === "ok" ? state.info.repository.editor : null;
    state.setChoice(editor, remember);
    setOpen(false);
    if (editor === before) {
      // The link was built for this editor already.
      editorNav.open(href);
      return;
    }
    setPending({ editor, place, from: href, until: Date.now() + PENDING_MS });
  };

  const anchor = (
    <a
      ref={ref}
      {...rest}
      href={href}
      title={title}
      onClick={activate}
      {...(choosing ? { "aria-haspopup": "dialog" as const, "aria-expanded": open } : {})}
    >
      {children}
    </a>
  );
  // Always inside the popover's root, so the anchor element stays the same
  // when an editor is chosen (no remount, focus kept).
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Anchor asChild>{anchor}</Popover.Anchor>
      {open && (
        <EditorChooserContent
          state={state}
          onPick={pick}
          onClose={() => {
            setOpen(false);
          }}
          returnFocus={() => {
            ref.current?.focus();
          }}
        />
      )}
    </Popover.Root>
  );
}

/** EditorChooserContent is the chooser popover itself, for a
 * Popover.Root whose anchor is the editor link (EditorLink) or a row that
 * opens one (Jump-to): the presets, the remember checkbox and "More in
 * Settings". onPick gets the editor and whether to remember it; onClose
 * closes it without a pick; returnFocus puts focus back where it was. */
export function EditorChooserContent({ state, onPick, onClose, returnFocus }: { state: EditorState; onPick: (editor: string, remember: boolean) => void; onClose: () => void; returnFocus: () => void }) {
  const content = useRef<HTMLDivElement>(null);
  return (
    <Popover.Portal>
      <Popover.Content
        ref={content}
        side="bottom"
        align="start"
        sideOffset={6}
        collisionPadding={8}
        data-editor-chooser=""
        data-testid="editor-chooser"
        className="z-50 w-[300px] max-w-[calc(100vw-16px)] rounded-[calc(var(--radius)+2px)] border bg-popover p-1.5 text-[13px] text-popover-foreground shadow-[0_10px_30px_rgba(0,0,0,.16),0_2px_6px_rgba(0,0,0,.08)] outline-none"
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          content.current?.querySelector<HTMLElement>('[role="option"]')?.focus();
        }}
        // The popover is portalled, but React bubbles its events to the
        // anchor's parents (a chip's button, a row's keys, a section's
        // shortcuts): they stop here.
        onClick={stop}
        onMouseDown={stop}
        onPointerDown={stop}
        onKeyDown={stop}
        onCloseAutoFocus={(e) => {
          e.preventDefault();
          returnFocus();
        }}
      >
        <Chooser state={state} onPick={onPick} onSettings={onClose} />
      </Popover.Content>
    </Popover.Portal>
  );
}

/** Chooser is the popover's body: the presets as a list, the remember
 * checkbox and the link to the editor row of Settings. */
function Chooser({ state, onPick, onSettings }: { state: EditorState; onPick: (editor: string, remember: boolean) => void; onSettings: () => void }) {
  const [remember, setRemember] = useState(true);
  const [active, setActive] = useState(0);
  const rows = useRef<(HTMLDivElement | null)[]>([]);
  const base = useId();
  const opts = presetOptions(state).filter((o): o is Option & { value: string } => o.value !== null);
  const move = (e: KeyboardEvent<HTMLDivElement>) => {
    let next = -1;
    if (e.key === "ArrowDown") next = (active + 1) % opts.length;
    else if (e.key === "ArrowUp") next = (active - 1 + opts.length) % opts.length;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = opts.length - 1;
    if (next === -1) return;
    e.preventDefault();
    setActive(next);
    rows.current[next]?.focus();
  };
  return (
    <div className="grid gap-1">
      <b id={`${base}-h`} className="px-2 pt-1 text-[13.5px] font-semibold">
        Open with
      </b>
      <div role="listbox" aria-labelledby={`${base}-h`} className="grid gap-px" onKeyDown={move} data-testid="editor-chooser-list">
        {opts.map((o, i) => (
          <div
            key={o.value}
            ref={(el) => {
              rows.current[i] = el;
            }}
            role="option"
            aria-selected={i === active}
            {...(o.shortNote !== undefined && o.shortNote !== "" ? { "aria-describedby": `${base}-n${String(i)}` } : {})}
            tabIndex={i === active ? 0 : -1}
            data-testid={`editor-chooser-${o.value}`}
            onFocus={() => {
              setActive(i);
            }}
            onClick={() => {
              onPick(o.value, remember);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onPick(o.value, remember);
              }
            }}
            className="grid cursor-pointer grid-cols-[45px_1fr] items-center gap-x-2 rounded-md px-2 py-[3px] outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset aria-selected:bg-accent/60"
          >
            <EditorMark mark={o.mark} />
            <span className="flex min-w-0 items-center gap-1">
              <span className="min-w-0 truncate font-medium">{o.label}</span>
              {o.preset !== undefined && <PresetStatusIcon preset={o.preset} name={typeof o.label === "string" ? o.label : o.value} />}
            </span>
            {o.shortNote !== undefined && o.shortNote !== "" && (
              <span id={`${base}-n${String(i)}`} className="col-start-2 -mt-0.5 truncate text-[11.5px] leading-[1.3] text-muted-foreground" data-testid={`editor-chooser-short-${o.value}`}>
                {o.shortNote}
              </span>
            )}
          </div>
        ))}
      </div>
      <div className="mt-0.5 flex flex-wrap items-center justify-between gap-2 border-t px-2 pt-2 pb-1 text-[12.5px]">
        <label className="inline-flex cursor-pointer items-center gap-1.5">
          <input
            type="checkbox"
            checked={remember}
            onChange={(e) => {
              setRemember(e.target.checked);
            }}
            className="size-3.5 accent-brand"
            data-testid="editor-chooser-remember"
          />
          Remember for this browser
        </label>
        <a href={settingsHash("editor")} onClick={onSettings} className="text-brand-text underline underline-offset-2" data-testid="editor-chooser-settings">
          More in Settings
        </a>
      </div>
    </div>
  );
}
