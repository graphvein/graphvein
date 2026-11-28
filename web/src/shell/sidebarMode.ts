// The sidebar's width (T064, design record T061 D2 variant A): one width on
// every section, chosen by the user. The collapse button, the rail's expand
// button and the "[" key switch between the full sidebar and the 56 px
// icon rail; the choice is remembered per browser (graphvein.sidebar =
// "expanded" | "rail"). Without a stored choice the window decides: the
// full sidebar from 1100 px, the rail below. Below 1100 px the full sidebar
// never pushes the content (the API list would fold again): expanding opens
// it over the content, and navigation or Esc closes it.

import { useCallback, useEffect, useState, useSyncExternalStore } from "react";

import { isTypingTarget } from "@/sections/help/shortcut";

/** SIDEBAR_KEY is the browser storage key of the user's choice. */
export const SIDEBAR_KEY = "graphvein.sidebar";

/** WIDE_QUERY is the width from which the full sidebar is the default and
 * sits beside the content: 232 px + the API split's 774 px + paddings. */
export const WIDE_QUERY = "(min-width: 1100px)";

export type SidebarChoice = "expanded" | "rail";

/** readChoice returns the stored choice, null when there is none, it is
 * not one of the two values, or storage is blocked. */
export function readChoice(): SidebarChoice | null {
  try {
    const v = window.localStorage.getItem(SIDEBAR_KEY);
    return v === "expanded" || v === "rail" ? v : null;
  } catch {
    return null;
  }
}

function writeChoice(choice: SidebarChoice): void {
  try {
    window.localStorage.setItem(SIDEBAR_KEY, choice);
  } catch {
    // Blocked storage: the choice lasts until the page reloads.
  }
}

function subscribeWide(onChange: () => void): () => void {
  if (typeof window.matchMedia !== "function") return () => undefined;
  const m = window.matchMedia(WIDE_QUERY);
  m.addEventListener("change", onChange);
  return () => {
    m.removeEventListener("change", onChange);
  };
}

/** useWideWindow says whether the window is at least 1100 px wide; true
 * where matchMedia is missing (tests), so the full sidebar shows. */
export function useWideWindow(): boolean {
  return useSyncExternalStore(subscribeWide, () => typeof window.matchMedia !== "function" || window.matchMedia(WIDE_QUERY).matches, () => true);
}

/** sidebarLayout is the rule as a pure function: whether the grid column
 * is the rail, and whether the full sidebar is open over the content. */
export function sidebarLayout(choice: SidebarChoice | null, wide: boolean, overlayOpen: boolean): { rail: boolean; overlay: boolean } {
  if (wide) return { rail: choice === "rail", overlay: false };
  return { rail: true, overlay: overlayOpen };
}

/** isSidebarShortcut reports whether a keydown is "[" outside a text
 * field, without Ctrl, Cmd or Alt. */
export function isSidebarShortcut(e: Pick<KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey" | "defaultPrevented" | "isComposing" | "target">): boolean {
  if (e.key !== "[" || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented || e.isComposing) return false;
  return !isTypingTarget(e.target);
}

export interface SidebarMode {
  /** The grid column is the 56 px rail. */
  rail: boolean;
  /** The full sidebar is open over the content (below 1100 px). */
  overlay: boolean;
  /** Collapse or expand: the stored choice from 1100 px, the overlay
   * below. */
  toggle: () => void;
  /** Closes the overlay (navigation, Esc, a click beside it). */
  close: () => void;
}

/** useSidebarMode owns the choice, the overlay and the "[" and Esc keys.
 * hash closes the overlay when it changes (navigation). */
export function useSidebarMode(hash: string): SidebarMode {
  const wide = useWideWindow();
  const [choice, setChoice] = useState<SidebarChoice | null>(readChoice);
  const [overlayOpen, setOverlayOpen] = useState(false);
  const [openedAt, setOpenedAt] = useState(hash);
  // Navigation closes the overlay (state adjusted during render).
  if (overlayOpen && openedAt !== hash) setOverlayOpen(false);
  const layout = sidebarLayout(choice, wide, overlayOpen);

  const toggle = useCallback(() => {
    if (wide) {
      const next: SidebarChoice = choice === "rail" ? "expanded" : "rail";
      writeChoice(next);
      setChoice(next);
      return;
    }
    setOpenedAt(window.location.hash);
    setOverlayOpen((o) => !o);
  }, [wide, choice]);
  const close = useCallback(() => {
    setOverlayOpen(false);
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (isSidebarShortcut(e)) {
        e.preventDefault();
        toggle();
      } else if (e.key === "Escape" && layout.overlay && !e.defaultPrevented) {
        setOverlayOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [toggle, layout.overlay]);

  return { ...layout, toggle, close };
}
