// Card and label sizes of the Flow tab, from the text they show, so ELK
// reserves room for every word and nothing is cut off. Every text is at
// least 12 px at zoom 1 (T016-ui-decisions "Graph views"). In a browser the
// width comes from canvas measureText with the fonts the cards use; jsdom
// has no canvas, so tests get a fixed per-character estimate.

import type { GraphNode } from "@/sections/api/flow/graph";

const SANS = 'Inter, "Inter Variable", system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';
const MONO = '"Geist Mono", ui-monospace, "SF Mono", "Cascadia Mono", Menlo, Consolas, monospace';

/** FONT is the CSS font of each text in a card; FlowNode.tsx uses the
 * same sizes (text-[13px] mono titles, 12px everything else). */
export const FONT = {
  title: `500 13px ${MONO}`,
  method: `600 12px ${MONO}`,
  sub: `400 12px ${MONO}`,
  badge: `600 12px ${SANS}`,
  note: `400 12px ${SANS}`,
  label: `500 12px ${SANS}`,
  caption: `600 12px ${SANS}`,
} as const;

/** The smallest text size in the canvas at zoom 1. */
export const MIN_TEXT_PX = 12;

const ESTIMATE: Record<keyof typeof FONT, number> = { title: 8, method: 7.4, sub: 7.4, badge: 7, note: 6.8, label: 6.8, caption: 7.4 };

let ctx: CanvasRenderingContext2D | null | undefined;

function context(): CanvasRenderingContext2D | null {
  if (ctx !== undefined) return ctx;
  ctx = null;
  // jsdom reports itself in the user agent and logs an error for
  // getContext; it has no layout, so the estimate is as good there.
  if (typeof document !== "undefined" && !navigator.userAgent.includes("jsdom")) {
    try {
      ctx = document.createElement("canvas").getContext("2d");
    } catch {
      ctx = null;
    }
  }
  return ctx;
}

/** textWidth measures text in one of the card fonts, rounded up. */
export function textWidth(text: string, font: keyof typeof FONT): number {
  const c = context();
  if (c === null) return Math.ceil(text.length * ESTIMATE[font]);
  c.font = FONT[font];
  return Math.ceil(c.measureText(text).width);
}

export const PAD_X = 12;
export const PAD_Y = 7;
const GAP = 7;
const BADGE_PAD = 12;
const ROW_TITLE = 20;
const ROW = 18;
const ROW_CHIPS = 22;
const DOT = 9;
const MIN_W = 150;

/** chips lists the "+N more" chips of a card. */
export function chips(n: GraphNode): string[] {
  const out: string[] = [];
  if (n.omitted.callers > 0) out.push(`+${String(n.omitted.callers)} more ${n.omitted.callers === 1 ? "caller" : "callers"}`);
  if (n.omitted.callees > 0) out.push(`+${String(n.omitted.callees)} more ${n.omitted.callees === 1 ? "callee" : "callees"}`);
  return out;
}

/** CARD_MAX_W caps a card's width at zoom 1 (T016i D2.1): at 1440x900 the
 * Flow canvas is 1,284 px, and the first view must hold the call-site card,
 * the endpoint card, the two layer gaps with their labels (about 144 px
 * each on Langflow's gated routes) and the handler's name (NAME_ROOM) with
 * 24 px padding each side. Long texts are cut in the middle instead. */
export const CARD_MAX_W = 360;

/** CARD_MIN_CAP is the floor of the responsive cap (T016i2 C2): on a
 * canvas too narrow for the chain even at this width, cards stay this wide
 * (their names read, D2.1) and the rest pans. */
export const CARD_MIN_CAP = 240;

/** CAP_STEP is the step between caps cardCap tries, from CARD_MIN_CAP up,
 * so a window resize lays out again only every few pixels. */
export const CAP_STEP = 8;

/** CHAIN_PAD is the room the first view leaves at each side of the chain
 * (fitViewport's padding). */
export const CHAIN_PAD = 24;

/** MIN_TEXT_ROOM is the least room a card leaves for its title text: when
 * the fixed parts of a line (method, badges, the changed dot, the wrapper
 * pill, the "+N more" chips) leave less inside the cap, the card grows to
 * fit them plus this much, the only way past CARD_MAX_W. */
export const MIN_TEXT_ROOM = 80;

/** ELLIPSIS is the cut mark of truncateMiddle (U+2026). */
export const ELLIPSIS = "\u2026";

// Border (1 px each side), padding and the 4 px the natural width always
// reserved.
const FRAME = 2 * PAD_X + 2 + 4;

/** longestFit returns the largest k in [0, max] with fits(k), or -1 when
 * not even fits(0) holds. fits must be monotone (true up to some k). */
function longestFit(max: number, fits: (k: number) => boolean): number {
  if (!fits(0)) return -1;
  let lo = 0;
  let hi = max;
  while (lo < hi) {
    const mid = Math.ceil((lo + hi) / 2);
    if (fits(mid)) lo = mid;
    else hi = mid - 1;
  }
  return lo;
}

/** cutAt keeps the tail from the last separator and cuts the head from
 * its end: head[0..k] + "…" + tail, the largest k that fits. When the tail
 * does not fit after "…", the result is "…" plus the longest end of the
 * tail that fits (so ":line" and the end of a file name stay). */
function cutAt(text: string, at: number, ok: (s: string) => boolean): string {
  const head = text.slice(0, at);
  const tail = text.slice(at);
  const k = longestFit(head.length, (n) => ok(head.slice(0, n) + ELLIPSIS + tail));
  if (k >= 0) return head.slice(0, k) + ELLIPSIS + tail;
  const j = longestFit(tail.length, (n) => ok(ELLIPSIS + tail.slice(tail.length - n)));
  return j >= 0 ? ELLIPSIS + tail.slice(tail.length - j) : ELLIPSIS;
}

/** truncateMiddle cuts text in the middle so that ok(result) holds
 * (T016i D2.3); a text that is already ok is returned unchanged.
 *  - With a "/" (paths, file:line, route templates, call expressions): the
 *    tail from the last "/" is kept, the head is cut from its end.
 *  - Else, for a qualified name (`qualified`) with a ".": the same at the
 *    last ".".
 *  - Else: head + "…" + tail of equal length (the head one longer when
 *    odd), the largest that fits. */
export function truncateMiddle(text: string, ok: (s: string) => boolean, qualified = false): string {
  if (ok(text)) return text;
  const slash = text.lastIndexOf("/");
  if (slash !== -1) return cutAt(text, slash, ok);
  const dot = qualified ? text.lastIndexOf(".") : -1;
  if (dot > 0) return cutAt(text, dot, ok);
  // Equal halves: n characters in all, the head taking the odd one.
  const n = longestFit(text.length - 1, (m) => ok(text.slice(0, Math.ceil(m / 2)) + ELLIPSIS + text.slice(text.length - Math.floor(m / 2))));
  if (n < 0) return ELLIPSIS;
  return text.slice(0, Math.ceil(n / 2)) + ELLIPSIS + text.slice(text.length - Math.floor(n / 2));
}

/** SUB_SEP separates a name from its place in a sub text. */
const SUB_SEP = " \u00b7 ";

/** NAME_MIN is how far the name part of a `<name> · <file:line>` sub is
 * cut before the path part is. */
const NAME_MIN = 8;

/** truncateSub cuts a sub text; a `<name> · <file:line>` sub cuts the name
 * part first, down to NAME_MIN characters, then the path part. */
export function truncateSub(text: string, ok: (s: string) => boolean): string {
  if (ok(text)) return text;
  const i = text.indexOf(SUB_SEP);
  if (i === -1) return truncateMiddle(text, ok);
  const name = text.slice(0, i);
  const path = text.slice(i + SUB_SEP.length);
  const cutName = truncateMiddle(name, (s) => s.length <= NAME_MIN || ok(s + SUB_SEP + path));
  if (ok(cutName + SUB_SEP + path)) return cutName + SUB_SEP + path;
  const short = name.length <= NAME_MIN ? name : truncateMiddle(name, (s) => s.length <= NAME_MIN);
  return short + SUB_SEP + truncateMiddle(path, (s) => ok(short + SUB_SEP + s));
}

/** What a card shows: its size and the texts as drawn (cut to the card's
 * room when needed). GraphNode keeps the full texts; the tooltip and the
 * accessible name use those. */
export interface CardLayout {
  width: number;
  height: number;
  title: string;
  sub: string;
  notes: string[];
  /** Some text is cut. */
  truncated: boolean;
}

/** isQualifiedTitle says whether a card's title is the qualified name of a
 * function, method or class (a folded owner, or such a node), where the
 * last "." separates the cut. */
function isQualifiedTitle(n: GraphNode): boolean {
  if (n.node === null) return false;
  if (n.members.length > 1) return true;
  return !["client_call", "ipc_call", "action_call", "route", "ipc_command", "server_action", "file"].includes(n.node.kind);
}

// Per cap, the layouts already measured (a card is measured at a few caps
// while cardCap searches, then drawn at one).
const layouts = new Map<number, WeakMap<GraphNode, CardLayout>>();

/** cardLayout sizes a card from its text (border included) with the cap
 * (CARD_MAX_W unless the canvas asks for less, cardCap) and cuts the texts
 * that do not fit (T016i D2). ELK's box and the drawn texts come from the
 * same measurement at the same cap, so they agree. */
export function cardLayout(n: GraphNode, cap: number = CARD_MAX_W): CardLayout {
  let known = layouts.get(cap);
  if (known === undefined) {
    known = new WeakMap();
    layouts.set(cap, known);
  }
  const hit = known.get(n);
  if (hit !== undefined) return hit;
  let fixedTitle = 0;
  if (n.method !== null) fixedTitle += textWidth(n.method, "method") + GAP;
  for (const b of n.badges) fixedTitle += textWidth(b.text, "badge") + BADGE_PAD + GAP;
  if (n.changed) fixedTitle += DOT + GAP;
  const fixedSub = n.wrapper !== null ? textWidth(n.wrapper, "badge") + BADGE_PAD + 2 + GAP : 0;
  const chipList = chips(n);
  const chipW = chipList.reduce((w, c) => w + textWidth(c, "badge") + BADGE_PAD + GAP, 0);
  const title = textWidth(n.title, "title");
  const sub = textWidth(n.sub, "sub");
  const notes = n.notes.map((t) => textWidth(t, "note"));
  const natural = Math.max(fixedTitle + title, fixedSub + sub, chipW, ...notes);
  const height = 2 * PAD_Y + 2 + ROW_TITLE + ROW + n.notes.length * ROW + (chipList.length > 0 ? ROW_CHIPS : 0);
  let out: CardLayout;
  if (Math.max(MIN_W, natural + FRAME) <= cap) {
    out = { width: Math.max(MIN_W, natural + FRAME), height, title: n.title, sub: n.sub, notes: n.notes, truncated: false };
  } else {
    // The room inside the capped card; fixed parts never shrink.
    const inner = Math.max(cap - FRAME, fixedTitle + MIN_TEXT_ROOM, fixedSub + MIN_TEXT_ROOM, chipW);
    const titleText = truncateMiddle(n.title, (s) => textWidth(s, "title") <= inner - fixedTitle, isQualifiedTitle(n));
    const subText = truncateSub(n.sub, (s) => textWidth(s, "sub") <= inner - fixedSub);
    const noteTexts = n.notes.map((t) => truncateMiddle(t, (s) => textWidth(s, "note") <= inner));
    const used = Math.max(fixedTitle + textWidth(titleText, "title"), fixedSub + textWidth(subText, "sub"), chipW, ...noteTexts.map((t) => textWidth(t, "note")));
    out = {
      width: Math.max(MIN_W, Math.min(natural, Math.max(inner, used)) + FRAME),
      height,
      title: titleText,
      sub: subText,
      notes: noteTexts,
      truncated: titleText !== n.title || subText !== n.sub || noteTexts.some((t, i) => t !== n.notes[i]),
    };
  }
  known.set(n, out);
  return out;
}

/** cardSize is the size a card needs for its text (border included),
 * capped at `cap` unless its fixed parts need more (cardLayout). */
export function cardSize(n: GraphNode, cap: number = CARD_MAX_W): { width: number; height: number } {
  const l = cardLayout(n, cap);
  return { width: l.width, height: l.height };
}

/** nameEnd is the distance from a card's left edge to the end of its
 * name as drawn (border, padding, method and the possibly cut title): how
 * much of the card must show for the name to read. */
export function nameEnd(n: GraphNode, cap: number = CARD_MAX_W): number {
  let w = 1 + PAD_X + textWidth(cardLayout(n, cap).title, "title");
  if (n.method !== null) w += textWidth(n.method, "method") + GAP;
  return w;
}

/** captionWidth is a column caption's width at zoom 1: uppercase, 12 px
 * semibold with 0.05em letter spacing (FlowCaption), plus 2 px so a
 * rounding difference never lets the clamp cut its last letter. */
export function captionWidth(text: string): number {
  return textWidth(text.toUpperCase(), "caption") + Math.ceil(0.6 * text.length) + 2;
}

/** labelSize is the box ELK reserves for an edge label. */
export function labelSize(text: string): { width: number; height: number } {
  return { width: textWidth(text, "label") + 12, height: 20 };
}

/** chainWidth is the width the chain columns need at a cap: per column its
 * widest card at that cap (fixed-part growth included), plus `gap`, the
 * sum of the layer gaps between them. */
export function chainWidth(columns: GraphNode[][], gap: number, cap: number): number {
  let w = gap;
  for (const list of columns) w += list.reduce((m, n) => Math.max(m, cardLayout(n, cap).width), 0);
  return w;
}

/** cardCap is the card width cap for a canvas `width` px wide (T016i2 D2):
 * the largest cap from CARD_MIN_CAP up in CAP_STEP steps, CARD_MAX_W at
 * most, at which the chain columns (call sites, endpoint, handler; layout's
 * chainColumns) and their gaps fit with CHAIN_PAD at each side; when none
 * fits, CARD_MIN_CAP. */
export function cardCap(width: number, columns: GraphNode[][], gap: number): number {
  const room = width - 2 * CHAIN_PAD;
  if (chainWidth(columns, gap, CARD_MAX_W) <= room) return CARD_MAX_W;
  for (let cap = CARD_MIN_CAP + Math.floor((CARD_MAX_W - 1 - CARD_MIN_CAP) / CAP_STEP) * CAP_STEP; cap > CARD_MIN_CAP; cap -= CAP_STEP) {
    if (chainWidth(columns, gap, cap) <= room) return cap;
  }
  return CARD_MIN_CAP;
}
