// The walk depths chosen in this browser (T042): the Impact tab's change
// impact depth and the architecture chain's depth. Stored like the label
// style (lib/labelChoice.ts, ADR 0078):
// - key graphvein.impact-depth.v1 / graphvein.chain-depth.v1 holds an
//   integer 1 to 10; an absent key means nothing was chosen yet (the
//   repository default from graphvein.yaml's ui.impact_depth /
//   ui.chain_depth applies, else the built-in 3 / 6); any other value is
//   ignored (read as no choice);
// - a later format uses new keys (.v2);
// - every access is wrapped: blocked storage keeps the choice in memory
//   for this page and reports it as not saved.

/** DepthKind names the two walks whose depth the UI remembers. */
export type DepthKind = "impact" | "chain";

/** MAX_DEPTH is the deepest walk the server allows for one repository and
 * for a project (traverse.MaxDepth, T042). */
export const MAX_DEPTH = 10;

/** DEPTHS are the choices the depth switches offer. */
export const DEPTHS: readonly number[] = Array.from({ length: MAX_DEPTH }, (_, i) => i + 1);

/** BUILT_IN_DEPTH is each walk's depth when neither this browser nor the
 * repository sets one (changes.DefaultDepth, traverse.ChainDefaultDepth). */
export const BUILT_IN_DEPTH: Readonly<Record<DepthKind, number>> = { impact: 3, chain: 6 };

/** DEPTH_KEYS are the localStorage keys of the chosen depths. */
export const DEPTH_KEYS: Readonly<Record<DepthKind, string>> = { impact: "graphvein.impact-depth.v1", chain: "graphvein.chain-depth.v1" };

/** StoredDepth is what storage holds now: depth is null when nothing was
 * chosen; saved is false when storage is blocked. */
export interface StoredDepth {
  depth: number | null;
  saved: boolean;
}

/** In-memory choices for a page whose storage is blocked. */
const memory: Record<DepthKind, number | null> = { impact: null, chain: null };

/** parseDepth reads a depth from storage, a hash parameter or the server:
 * only an integer 1 to MAX_DEPTH counts. */
export function parseDepth(value: unknown): number | null {
  let n: number;
  if (typeof value === "number") n = value;
  else if (typeof value === "string" && /^[0-9]{1,2}$/.test(value)) n = Number(value);
  else return null;
  return Number.isInteger(n) && n >= 1 && n <= MAX_DEPTH ? n : null;
}

/** readDepth reads the stored depth of one walk. */
export function readDepth(kind: DepthKind): StoredDepth {
  try {
    return { depth: parseDepth(window.localStorage.getItem(DEPTH_KEYS[kind])), saved: true };
  } catch {
    return { depth: memory[kind] ?? null, saved: false };
  }
}

/** Listeners of this page's depth writes (subscribeDepths). */
const listeners = new Set<() => void>();

function changed(): void {
  for (const l of listeners) l();
}

/** writeDepth stores a chosen depth. It returns false when storage is
 * blocked; the choice then lasts for this page. */
export function writeDepth(kind: DepthKind, depth: number): boolean {
  memory[kind] = depth;
  try {
    window.localStorage.setItem(DEPTH_KEYS[kind], String(depth));
    return true;
  } catch {
    return false;
  } finally {
    changed();
  }
}

/** clearDepth removes this browser's choice (Settings' "Not set"), so the
 * repository default, else the built-in depth, applies again. It returns
 * false when storage is blocked. */
export function clearDepth(kind: DepthKind): boolean {
  memory[kind] = null;
  try {
    window.localStorage.removeItem(DEPTH_KEYS[kind]);
    return true;
  } catch {
    return false;
  } finally {
    changed();
  }
}

/** subscribeDepths calls onChange when this page writes or clears a depth
 * (Settings and the depth switches) or another tab of this origin changes
 * one (the storage event). It returns the unsubscribe function. */
export function subscribeDepths(onChange: () => void): () => void {
  const onStorage = (e: StorageEvent) => {
    // A null key means the whole storage was cleared.
    if (e.key === null || e.key === DEPTH_KEYS.impact || e.key === DEPTH_KEYS.chain) onChange();
  };
  listeners.add(onChange);
  window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(onChange);
    window.removeEventListener("storage", onStorage);
  };
}
