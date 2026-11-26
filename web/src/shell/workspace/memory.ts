// View memory (T038, claim row WS-49): the last view of each workspace
// entry, kept per browser in localStorage and keyed by the registry entry
// id (stable across restarts, WS-4): the section hash with its query
// (selection, tab, filters, depth choices), the project member and the
// scroll positions of the page's scroll areas. Blocked or full storage
// remembers nothing and claims nothing; every access is in try/catch.
//
// A remembered item that a re-index removed is not checked here: the
// section that reads the restored hash reports it with its own notice (the
// API section's "no longer in the index").

/** VIEW_MEMORY_KEY is the localStorage key, versioned in its name. */
export const VIEW_MEMORY_KEY = "graphvein.view.v1";

/** MAX_ENTRIES bounds the stored views; the least recently saved go. */
export const MAX_ENTRIES = 64;

/** ViewMemory is one entry's last view. */
export interface ViewMemory {
  hash: string;
  member: string | null;
  /** scrollTop by scroll-area key (scrollKeys). */
  scroll: Record<string, number>;
  savedAt: number;
}

type Store = Record<string, ViewMemory>;

function load(): Store {
  try {
    const raw = window.localStorage.getItem(VIEW_MEMORY_KEY);
    if (raw === null) return {};
    const v: unknown = JSON.parse(raw);
    if (typeof v !== "object" || v === null || Array.isArray(v)) return {};
    const out: Store = {};
    for (const [id, m] of Object.entries(v as Record<string, unknown>)) {
      const view = parseView(m);
      if (view !== null) out[id] = view;
    }
    return out;
  } catch {
    return {};
  }
}

function parseView(m: unknown): ViewMemory | null {
  if (typeof m !== "object" || m === null) return null;
  const o = m as Record<string, unknown>;
  if (typeof o.hash !== "string" || !o.hash.startsWith("#/")) return null;
  const scroll: Record<string, number> = {};
  if (typeof o.scroll === "object" && o.scroll !== null) {
    for (const [k, n] of Object.entries(o.scroll as Record<string, unknown>)) {
      if (typeof n === "number" && Number.isFinite(n) && n >= 0) scroll[k] = n;
    }
  }
  return { hash: o.hash, member: typeof o.member === "string" ? o.member : null, scroll, savedAt: typeof o.savedAt === "number" ? o.savedAt : 0 };
}

/** readView returns the remembered view of an entry, or null. */
export function readView(id: string): ViewMemory | null {
  return load()[id] ?? null;
}

/** saveView remembers an entry's view; scroll positions not given keep
 * their stored values. */
export function saveView(id: string, view: { hash: string; member: string | null; scroll?: Record<string, number> }): void {
  try {
    const store = load();
    const prev = store[id];
    store[id] = { hash: view.hash, member: view.member, scroll: view.scroll ?? prev?.scroll ?? {}, savedAt: Date.now() };
    // Keep the MAX_ENTRIES most recently saved views.
    const kept = Object.entries(store)
      .sort(([, a], [, b]) => b.savedAt - a.savedAt)
      .slice(0, MAX_ENTRIES);
    window.localStorage.setItem(VIEW_MEMORY_KEY, JSON.stringify(Object.fromEntries(kept)));
  } catch {
    // Blocked or full storage: the view is not remembered.
  }
}

/** SCROLL_ATTR marks an element whose scroll position is remembered. */
export const SCROLL_ATTR = "data-scroll-memory";

function scrollAreas(root: ParentNode): Map<string, HTMLElement> {
  const seen = new Map<string, number>();
  const out = new Map<string, HTMLElement>();
  for (const el of root.querySelectorAll<HTMLElement>(`[${SCROLL_ATTR}]`)) {
    const name = el.getAttribute(SCROLL_ATTR) || el.getAttribute("aria-label") || "area";
    const n = seen.get(name) ?? 0;
    seen.set(name, n + 1);
    out.set(`${name}#${String(n)}`, el);
  }
  return out;
}

/** readScroll collects the scroll positions of the page's marked areas. */
export function readScroll(root: ParentNode = document): Record<string, number> {
  const out: Record<string, number> = {};
  for (const [key, el] of scrollAreas(root)) {
    if (el.scrollTop > 0) out[key] = Math.round(el.scrollTop);
  }
  return out;
}

/** applyScroll sets every remembered position whose area exists and is
 * tall enough; it returns the keys still waiting (content not loaded). */
export function applyScroll(want: Record<string, number>, root: ParentNode = document): string[] {
  const areas = scrollAreas(root);
  const waiting: string[] = [];
  for (const [key, top] of Object.entries(want)) {
    const el = areas.get(key);
    if (el === undefined || el.scrollHeight - el.clientHeight < top) {
      waiting.push(key);
      continue;
    }
    el.scrollTop = top;
  }
  return waiting;
}

/** restoreScroll applies positions as the restored view's content loads:
 * each frame until every area is placed or `limitMs` passed (then the
 * remaining areas are placed as far as they go). It returns a cancel. */
export function restoreScroll(want: Record<string, number>, limitMs = 3000): () => void {
  if (Object.keys(want).length === 0) return () => undefined;
  const start = Date.now();
  let pending = { ...want };
  let frame = 0;
  const step = () => {
    const waiting = applyScroll(pending);
    if (waiting.length === 0) return;
    if (Date.now() - start > limitMs) {
      const areas = scrollAreas(document);
      for (const key of waiting) {
        const el = areas.get(key);
        if (el !== undefined) el.scrollTop = pending[key] ?? 0;
      }
      return;
    }
    pending = Object.fromEntries(waiting.map((k) => [k, pending[k] ?? 0]));
    frame = window.requestAnimationFrame(step);
  };
  frame = window.requestAnimationFrame(step);
  return () => {
    window.cancelAnimationFrame(frame);
  };
}
