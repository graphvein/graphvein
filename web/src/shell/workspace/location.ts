// The viewed workspace entry lives in the page URL (T038): `?ws=<entry
// id>` and, in a project, `&repository=<member>`, before the section's hash
// (#/api?sel=...). A switch writes both with one history entry and no
// reload; a copied link holds all of it, so it opens the same view
// (claim row WS-50).

import { notifyLocation } from "@/router";

/** WS_PARAM and MEMBER_PARAM are the page-query parameters. */
export const WS_PARAM = "ws";
export const MEMBER_PARAM = "repository";

/** PageLocation is the part of the URL the workspace owns. */
export interface PageLocation {
  ws: string | null;
  member: string | null;
}

/** readLocation reads the entry and member of the page URL. */
export function readLocation(): PageLocation {
  const q = new URLSearchParams(window.location.search);
  return { ws: q.get(WS_PARAM) || null, member: q.get(MEMBER_PARAM) || null };
}

/** locationSearch builds the page query for an entry and member, keeping
 * any other parameter the page carries. */
export function locationSearch(ws: string | null, member: string | null, base: string = window.location.search): string {
  const q = new URLSearchParams(base);
  if (ws === null) q.delete(WS_PARAM);
  else q.set(WS_PARAM, ws);
  if (member === null) q.delete(MEMBER_PARAM);
  else q.set(MEMBER_PARAM, member);
  const s = q.toString();
  return s === "" ? "" : `?${s}`;
}

/** writeLocation writes the entry, member and hash: "push" adds a history
 * entry (a switch, so Back returns), "replace" does not (the member or
 * the first write of the current entry). */
export function writeLocation(loc: PageLocation, hash: string, mode: "push" | "replace"): void {
  const url = `${window.location.pathname}${locationSearch(loc.ws, loc.member)}${hash}`;
  if (url === `${window.location.pathname}${window.location.search}${window.location.hash}`) return;
  if (mode === "push") window.history.pushState(null, "", url);
  else window.history.replaceState(window.history.state, "", url);
  notifyLocation();
}

/** viewLink is the absolute link to the view at loc and hash. */
export function viewLink(loc: PageLocation, hash: string): string {
  return `${window.location.origin}${window.location.pathname}${locationSearch(loc.ws, loc.member, "")}${hash}`;
}
