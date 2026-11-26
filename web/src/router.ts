// Hash routing (ADR 0041): the server owns /api/, so UI pages live in the
// fragment (#/api). An empty fragment or "#/" goes to the API section. A
// section hash may carry a query (#/api?sel=...); the section reads it with
// parseHashQuery and writes it back with replaceState, without a history
// entry per click.

import { useSyncExternalStore } from "react";

/** A section of the shell. Sections that are not available yet are shown
 * disabled and are not links. */
export interface Section {
  id: string;
  label: string;
  available: boolean;
}

export const SECTIONS: readonly Section[] = [
  { id: "api", label: "API", available: true },
  { id: "explorer", label: "Explorer", available: true },
  { id: "architecture", label: "Architecture", available: true },
  { id: "schema", label: "Schema", available: true },
  { id: "changes", label: "Changes", available: true },
  // "What Graphvein couldn't read" (T040): before Review, no count tag
  // (a total would add routes, calls and files).
  { id: "unread", label: "Couldn't read", available: true },
  { id: "review", label: "Review", available: false },
];

/** SETTINGS_SECTION is the Settings page (T039): a route, linked at the
 * bottom of the sidebar rather than among the sections. */
export const SETTINGS_SECTION: Section = { id: "settings", label: "Settings", available: true };

/** HELP_SECTION is the Help page (T045): a route, linked at the bottom of
 * the sidebar rather than among the sections, and opened by "?". */
export const HELP_SECTION: Section = { id: "help", label: "Help", available: true };

/** helpHash addresses the Help page, or one heading of it by its anchor
 * (#/help?at=<slug>), so a copied link opens that heading. */
export function helpHash(slug: string | null = null): string {
  return slug === null ? "#/help" : `#/help?at=${encodeURIComponent(slug)}`;
}

/** settingsHash addresses the Settings page, or one of its sections or
 * key rows by its anchor (#/settings?at=editor, #/settings?at=ide.editor),
 * so the editor chooser and the Help guide can link straight to a row. */
export function settingsHash(at: string | null = null): string {
  return at === null ? "#/settings" : `#/settings?at=${encodeURIComponent(at)}`;
}

export const DEFAULT_HASH = "#/api";

export type Route = { kind: "section"; section: Section } | { kind: "not_found"; hash: string };

/** sectionHash is the link target of a section. */
export function sectionHash(section: Section): string {
  return `#/${section.id}`;
}

/** parseRoute maps a location hash to a route. Only available sections are
 * routes; everything else is not found. A query after "?" does not change
 * the route. */
export function parseRoute(hash: string): Route {
  const target = hash === "" || hash === "#" || hash === "#/" ? DEFAULT_HASH : hash;
  const q = target.indexOf("?");
  const path = q === -1 ? target : target.slice(0, q);
  const section = [...SECTIONS, SETTINGS_SECTION, HELP_SECTION].find((s) => s.available && sectionHash(s) === path);
  return section ? { kind: "section", section } : { kind: "not_found", hash };
}

/** parseHashQuery returns the query of a hash ("" when there is none). */
export function parseHashQuery(hash: string): URLSearchParams {
  const q = hash.indexOf("?");
  return new URLSearchParams(q === -1 ? "" : hash.slice(q + 1));
}

// replaceState fires no event; replaceHash dispatches this one so useHash
// re-renders.
const REPLACED = "graphvein:hashreplaced";

/** replaceHash writes a hash without adding a history entry, and tells
 * useHash about it. */
export function replaceHash(hash: string): void {
  if (window.location.hash === hash) return;
  window.history.replaceState(window.history.state, "", hash);
  window.dispatchEvent(new Event(REPLACED));
}

/** notifyLocation tells useHash that the URL changed through pushState or
 * replaceState (a workspace switch writes the query and the hash at once,
 * T038). */
export function notifyLocation(): void {
  window.dispatchEvent(new Event(REPLACED));
}

/** normaliseHash rewrites an empty hash to the default section without
 * adding a history entry. */
export function normaliseHash(): void {
  const { hash } = window.location;
  if (hash === "" || hash === "#" || hash === "#/") {
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${window.location.search}${DEFAULT_HASH}`);
  }
}

function subscribe(onChange: () => void): () => void {
  const listener = () => {
    normaliseHash();
    onChange();
  };
  window.addEventListener("hashchange", listener);
  window.addEventListener("popstate", listener);
  window.addEventListener(REPLACED, onChange);
  return () => {
    window.removeEventListener("hashchange", listener);
    window.removeEventListener("popstate", listener);
    window.removeEventListener(REPLACED, onChange);
  };
}

function snapshot(): string {
  return window.location.hash;
}

/** useHash returns the current hash and re-renders on hashchange and
 * replaceHash. */
export function useHash(): string {
  return useSyncExternalStore(subscribe, snapshot);
}

/** useLocationKey returns the page query and hash together and re-renders
 * when either changes. A workspace switch (T038) can change only the query
 * (two entries both on #/api), which useHash alone would not notice. */
export function useLocationKey(): string {
  return useSyncExternalStore(subscribe, locationKey);
}

function locationKey(): string {
  return `${window.location.search}${window.location.hash}`;
}

/** useRoute returns the current route and re-renders on hash changes. */
export function useRoute(): Route {
  return parseRoute(useHash());
}
