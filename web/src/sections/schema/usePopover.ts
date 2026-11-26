// The "why" popover state of the schema section (T018f 2.2, R-5): one
// popover at a time for the whole section. A trigger is a button; opening
// another closes the first; Escape or the close button closes and returns
// focus to the trigger; a click outside closes. The popover is placed with
// `position: fixed`, so opening and closing changes no other element's box.

import { createContext, useCallback, useContext, useId, useMemo, useState, type MouseEvent } from "react";

import type { Why } from "@/sections/schema/WhyPopover";

/** OpenPopover is the one open popover: its trigger and its content,
 * built when it opens. */
export interface OpenPopover {
  id: string;
  trigger: HTMLElement;
  why: () => Why;
}

export interface PopoverApi {
  open: OpenPopover | null;
  /** toggle opens the popover of trigger id, or closes it when it is the
   * open one. */
  toggle: (id: string, trigger: HTMLElement, why: () => Why) => void;
  /** close closes the open popover; restore returns focus to its trigger. */
  close: (restore: boolean) => void;
}

const noop: PopoverApi = { open: null, toggle: () => undefined, close: () => undefined };

export const PopoverContext = createContext<PopoverApi>(noop);

/** usePopoverHost holds the section's popover state. */
export function usePopoverHost(): PopoverApi {
  const [open, setOpen] = useState<OpenPopover | null>(null);
  const toggle = useCallback((id: string, trigger: HTMLElement, why: () => Why) => {
    setOpen((cur) => (cur !== null && cur.id === id ? null : { id, trigger, why }));
  }, []);
  const close = useCallback(
    (restore: boolean) => {
      const t = open?.trigger ?? null;
      setOpen(null);
      // After the popover leaves the DOM, so focus does not land inside it.
      if (restore && t !== null)
        queueMicrotask(() => {
          if (t.isConnected) t.focus({ preventScroll: true });
        });
    },
    [open],
  );
  return useMemo(() => ({ open, toggle, close }), [open, toggle, close]);
}

/** usePopoverTrigger gives a trigger button its props: the dialog popup
 * attributes and the click that toggles its popover (Enter and Space click
 * a button). */
export function usePopoverTrigger(why: () => Why): {
  "aria-haspopup": "dialog";
  "aria-expanded": boolean;
  "aria-controls": string | undefined;
  onClick: (e: MouseEvent<HTMLElement>) => void;
} {
  const id = useId();
  const api = useContext(PopoverContext);
  const expanded = api.open?.id === id;
  return {
    "aria-haspopup": "dialog",
    "aria-expanded": expanded,
    "aria-controls": expanded ? WHY_POPOVER_ID : undefined,
    onClick: (e) => {
      api.toggle(id, e.currentTarget, why);
    },
  };
}

/** WHY_POPOVER_ID is the element id of the one popover. */
export const WHY_POPOVER_ID = "schema-why-popover";
