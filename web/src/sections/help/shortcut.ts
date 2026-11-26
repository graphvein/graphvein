// The "?" shortcut (T045): opens the Help page from anywhere in the app,
// except while the user types (a field, a text area, a select, an editable
// element or a widget that takes text) or holds Ctrl, Cmd or Alt.

import { useEffect } from "react";

import { HELP_SECTION, sectionHash } from "@/router";

/** TYPING_TARGET matches the elements where "?" is text, not a shortcut. */
const TYPING_TARGET = 'input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"], [role="combobox"], [role="searchbox"]';

/** isTypingTarget reports whether a key event's target takes text. */
export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false;
  if (target instanceof HTMLElement && target.isContentEditable) return true;
  return target.closest(TYPING_TARGET) !== null;
}

/** isHelpShortcut reports whether a keydown is the Help shortcut. */
export function isHelpShortcut(e: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey" | "defaultPrevented" | "isComposing" | "target">): boolean {
  if (e.key !== "?" || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented || e.isComposing) return false;
  return !isTypingTarget(e.target);
}

/** useHelpShortcut opens the Help page on "?"; on the Help page itself it
 * does nothing, so the reader's place is kept. */
export function useHelpShortcut(): void {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!isHelpShortcut(e)) return;
      const hash = window.location.hash;
      const target = sectionHash(HELP_SECTION);
      if (hash === target || hash.startsWith(`${target}?`)) return;
      e.preventDefault();
      window.location.hash = target;
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, []);
}
