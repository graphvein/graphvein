// Arrow-key focus for the try-it widgets that carry a composite ARIA role
// (the environment picker's listbox, the label radiogroup), following the
// WAI-ARIA Authoring Practices: one item is in the Tab order, and the
// arrow keys, Home and End move focus between the items.

import type { KeyboardEvent } from "react";

const NEXT = new Set(["ArrowDown", "ArrowRight"]);
const PREV = new Set(["ArrowUp", "ArrowLeft"]);

/** rovingKey moves focus among the container's items matching selector
 * for an arrow key, Home or End (wrapping at the ends), and returns the
 * item it focused; null for any other key, which is left alone. */
export function rovingKey(e: KeyboardEvent<HTMLElement>, selector: string): HTMLElement | null {
  if (!NEXT.has(e.key) && !PREV.has(e.key) && e.key !== "Home" && e.key !== "End") return null;
  const items = [...e.currentTarget.querySelectorAll<HTMLElement>(selector)].filter((el) => !(el as HTMLButtonElement).disabled);
  if (items.length === 0) return null;
  const at = items.findIndex((el) => el === document.activeElement);
  let next: number;
  if (e.key === "Home") next = 0;
  else if (e.key === "End") next = items.length - 1;
  else if (NEXT.has(e.key)) next = at < 0 ? 0 : (at + 1) % items.length;
  else next = at < 0 ? items.length - 1 : (at - 1 + items.length) % items.length;
  e.preventDefault();
  const el = items[next] ?? null;
  el?.focus();
  return el;
}
