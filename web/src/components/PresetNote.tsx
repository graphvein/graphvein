// The status icon of an editor preset that is not fully verified, beside
// the editor's name in the first-click chooser and in the Settings editor
// picker (developer decision 2026-11-25): the rows stay one name plus at
// most one short muted line (the preset's short_note, "macOS only"), and
// the full note opens from this icon in a small popover on hover, on
// keyboard focus and on click or tap. Activating the icon never selects
// the editor (its events stop here), and Esc closes the note before the
// chooser or picker it sits in (the note is the innermost layer).

import { Info, TriangleAlert } from "lucide-react";
import { Popover } from "radix-ui";
import { useRef, useState, type SyntheticEvent } from "react";

import type { EditorPreset } from "@/api/ide";
import { cn } from "@/lib/utils";

/** STATUS_WORDS names a preset status that is not "verified". Another
 * string from a newer server reads "Verification unknown", never verified. */
const STATUS_WORDS: Record<string, string> = { partly_verified: "Partly verified", limited: "Limited" };

/** presetStatusWord is the status in words, or null for a verified preset. */
export function presetStatusWord(status: string): string | null {
  if (status === "verified") return null;
  return STATUS_WORDS[status] ?? "Verification unknown";
}

function stop(e: SyntheticEvent) {
  e.stopPropagation();
}

/** PresetStatusIcon is the icon button and its note; nothing for a
 * verified preset. name is the editor's name ("Cursor"). */
export function PresetStatusIcon({ preset, name }: { preset: EditorPreset; name: string }) {
  const word = presetStatusWord(preset.status);
  // pinned: opened by a click or tap, so leaving the icon keeps it open.
  const [pinned, setPinned] = useState(false);
  const [hover, setHover] = useState(false);
  const [focus, setFocus] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  if (word === null) return null;
  const open = pinned || hover || focus;
  const close = () => {
    setPinned(false);
    setHover(false);
    setFocus(false);
  };
  const limited = preset.status === "limited";
  const Icon = limited ? Info : TriangleAlert;
  return (
    <Popover.Root
      open={open}
      onOpenChange={(o) => {
        if (!o) close();
      }}
    >
      <Popover.Trigger asChild>
        <button
          ref={button}
          type="button"
          aria-label={`About ${name} support`}
          aria-expanded={open}
          data-testid={`preset-status-${preset.editor}`}
          data-status={preset.status}
          onPointerEnter={(e) => {
            if (e.pointerType === "mouse") setHover(true);
          }}
          onPointerLeave={(e) => {
            if (e.pointerType === "mouse") setHover(false);
          }}
          onFocus={() => {
            setFocus(true);
          }}
          onBlur={() => {
            setFocus(false);
            setPinned(false);
          }}
          onClick={(e) => {
            // Never the row's pick, never the link behind the chooser.
            e.preventDefault();
            e.stopPropagation();
            setPinned(true);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              if (open) {
                // The note first; the chooser stays.
                e.preventDefault();
                e.stopPropagation();
                close();
                setFocus(false);
              }
              return;
            }
            // Enter or Space shows the note (the click handler); the row's
            // own keys (select, arrows) never see them.
            e.stopPropagation();
          }}
          onPointerDown={stop}
          onMouseDown={stop}
          className={cn(
            "inline-grid size-5 flex-none place-items-center rounded outline-none hover:bg-background focus-visible:ring-2 focus-visible:ring-ring [&_svg]:size-3.5",
            limited ? "text-muted-foreground" : "text-dynamic",
          )}
        >
          <Icon aria-hidden="true" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          side="right"
          align="start"
          sideOffset={6}
          collisionPadding={8}
          role="note"
          data-testid="preset-note"
          aria-label={`About ${name} support`}
          className="z-[60] w-[260px] max-w-[calc(100vw-16px)] rounded-md border bg-popover px-2.5 py-2 text-[12px] leading-[1.45] text-popover-foreground shadow-[0_8px_24px_rgba(0,0,0,.16)] outline-none"
          // Focus stays on the icon: the note is read, not operated.
          onOpenAutoFocus={(e) => {
            e.preventDefault();
          }}
          onCloseAutoFocus={(e) => {
            e.preventDefault();
          }}
          onEscapeKeyDown={(e) => {
            // Only the note closes; the chooser or picker stays open.
            e.stopPropagation();
            close();
            button.current?.focus();
          }}
          onClick={stop}
          onPointerDown={stop}
          onMouseDown={stop}
          onKeyDown={stop}
        >
          <b className={cn("font-semibold", limited ? "text-foreground" : "text-dynamic")}>{word}.</b> {preset.note}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
