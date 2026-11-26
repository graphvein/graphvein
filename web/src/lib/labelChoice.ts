// The label style chosen in this browser (T025b): plain words (the
// default, T025) or the technical ids, kinds and confidence numbers.
// Stored like the editor choice (ADR 0050, lib/editorChoice.ts):
// - key graphvein.labels.v1 holds "plain" or "technical"; an absent key
//   means nothing was chosen yet (the repository default from
//   graphvein.yaml's ui.labels applies, else plain, and the first-run
//   banner may show); any other value is ignored (read as no choice);
// - key graphvein.labels-banner.v1 holds "dismissed" once the first-run
//   banner was closed without choosing;
// - a later format uses new keys (.v2); this reader never parses another
//   version's key;
// - every access is wrapped: blocked storage keeps the choice in memory
//   for this page and reports it as not saved.

/** LABELS_KEY is the localStorage key of the label style. */
export const LABELS_KEY = "graphvein.labels.v1";

/** BANNER_KEY is the localStorage key that records a dismissed first-run
 * banner. */
export const BANNER_KEY = "graphvein.labels-banner.v1";

/** LabelStyle is how the UI words ids, kinds and confidences: "plain"
 * (T025's words, ids in tooltips) or "technical" (ids and numbers as the
 * primary text, the words in tooltips). */
export type LabelStyle = "plain" | "technical";

/** StoredLabels is what storage holds now. style is null when nothing
 * was chosen (or the stored value is not a style); saved is false when
 * storage could not be read. */
export interface StoredLabels {
  style: LabelStyle | null;
  bannerDismissed: boolean;
  saved: boolean;
}

function storage(): Storage {
  return window.localStorage;
}

/** parseStyle reads a stored value: only the two styles count. */
export function parseStyle(value: string | null): LabelStyle | null {
  return value === "plain" || value === "technical" ? value : null;
}

/** readLabels reads the stored style and the banner flag. */
export function readLabels(): StoredLabels {
  try {
    const s = storage();
    return { style: parseStyle(s.getItem(LABELS_KEY)), bannerDismissed: s.getItem(BANNER_KEY) === "dismissed", saved: true };
  } catch {
    return { style: null, bannerDismissed: false, saved: false };
  }
}

/** writeStyle stores a chosen style, or removes the key for null ("not
 * set in this browser": the repository default applies again). It returns
 * false when storage is blocked; the caller keeps the value in memory. */
export function writeStyle(style: LabelStyle | null): boolean {
  try {
    if (style === null) storage().removeItem(LABELS_KEY);
    else storage().setItem(LABELS_KEY, style);
    return true;
  } catch {
    return false;
  }
}

/** writeBannerDismissed records that the first-run banner was closed. It
 * returns false when storage is blocked. */
export function writeBannerDismissed(): boolean {
  try {
    storage().setItem(BANNER_KEY, "dismissed");
    return true;
  } catch {
    return false;
  }
}

/** subscribeLabels calls onChange whenever another tab of this origin
 * changes the style or the banner flag (the storage event). */
export function subscribeLabels(onChange: () => void): () => void {
  const listener = (e: StorageEvent) => {
    // A null key means the whole storage was cleared.
    if (e.key === LABELS_KEY || e.key === BANNER_KEY || e.key === null) onChange();
  };
  window.addEventListener("storage", listener);
  return () => {
    window.removeEventListener("storage", listener);
  };
}
