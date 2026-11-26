// The editor chosen in this browser for IDE links (T016k, ADR 0050).
//
// Storage rules (normative, the T016k task record (internal) "Storage"):
// - key graphvein.editor.v1; the value is a preset name or "off"; an
//   absent key means "use the repository default";
// - a later format uses a new key (.v2); this reader never parses another
//   version's key;
// - a stored value that the server does not list (or that is longer than
//   64 characters) is ignored, not deleted: a later server that offers it
//   works again, and choosing any option overwrites it;
// - every access is wrapped: blocked storage keeps the choice in memory
//   for this page and reports it as not saved.

/** EDITOR_KEY is the localStorage key of the choice, versioned in its name. */
export const EDITOR_KEY = "graphvein.editor.v1";

/** OFF is the stored value of "No editor links". */
export const OFF = "off";

/** MAX_STORED bounds a stored value; a longer one is ignored. */
export const MAX_STORED = 64;

/** StoredChoice is what storage holds now. saved is false when storage
 * could not be read or written: the value then lives only in memory. */
export interface StoredChoice {
  value: string | null;
  saved: boolean;
}

function storage(): Storage {
  return window.localStorage;
}

/** readChoice reads the stored value. */
export function readChoice(): StoredChoice {
  try {
    return { value: storage().getItem(EDITOR_KEY), saved: true };
  } catch {
    return { value: null, saved: false };
  }
}

/** writeChoice stores value (a preset name or OFF), or removes the key for
 * null ("use the repository default"). It returns false when storage is
 * blocked; the caller keeps the value in memory. */
export function writeChoice(value: string | null): boolean {
  try {
    if (value === null) storage().removeItem(EDITOR_KEY);
    else storage().setItem(EDITOR_KEY, value);
    return true;
  } catch {
    return false;
  }
}

/** ResolvedChoice is a stored value checked against the server's presets:
 * choice is a listed preset, OFF or null (the repository default); ignored
 * is a stored value that cannot apply, kept for the notice. */
export interface ResolvedChoice {
  choice: string | null;
  ignored: string | null;
}

/** resolveChoice applies the invalid-value rule. presets are the names the
 * server lists. */
export function resolveChoice(value: string | null, presets: readonly string[]): ResolvedChoice {
  if (value === null) return { choice: null, ignored: null };
  if (value.length <= MAX_STORED && (value === OFF || presets.includes(value))) return { choice: value, ignored: null };
  return { choice: null, ignored: value };
}

/** subscribeChoice calls onChange with the new value whenever another tab
 * of this origin changes the choice (the storage event). */
export function subscribeChoice(onChange: (value: string | null) => void): () => void {
  const listener = (e: StorageEvent) => {
    // A null key means the whole storage was cleared.
    if (e.key === EDITOR_KEY || e.key === null) onChange(e.key === null ? null : e.newValue);
  };
  window.addEventListener("storage", listener);
  return () => {
    window.removeEventListener("storage", listener);
  };
}
