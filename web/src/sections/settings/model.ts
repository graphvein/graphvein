// The Settings page's pure model (T039, variant A): the value
// ladder of each key (this browser / repository / built-in, row ST-49), the
// filter, the draft of unsaved repository values and the operations it
// sends, and which form field each line of graphvein.yaml feeds. Everything
// here is a pure function of the GET /api/v0/config answer and the
// browser's stored choices, so each rule is a unit test.

import type { Config, ConfigKey, EditResult, MapEntry, Operation, SettingValue } from "@/api/config";

/** SectionId names a section of the page, in page order. It is also the
 * section's stable anchor (#/settings?at=editor): the editor chooser and
 * the Help guide link to it, so an id never changes once published. */
export type SectionId = "editor" | "labels" | "indexing" | "matching" | "explorer" | "tryit" | "architecture" | "other" | "unlisted";

export const SECTIONS: readonly { id: SectionId; title: string; note: string }[] = [
  { id: "editor", title: "Editor", note: "which editor the file links open" },
  { id: "labels", title: "Labels and views", note: "per browser, with a repository default" },
  { id: "indexing", title: "Indexing and scan", note: "read by the index run" },
  { id: "matching", title: "Matching", note: "how calls and specs meet routes" },
  { id: "explorer", title: "Explorer path values", note: "edited in the Explorer" },
  { id: "tryit", title: "Try it environments", note: "edited in the Explorer's try it" },
  { id: "architecture", title: "Architecture", note: "the map" },
  { id: "other", title: "Other keys", note: "edited in the YAML view" },
  // A form key this UI does not describe yet (a newer server): shown by
  // its name, never dropped.
  { id: "unlisted", title: "Keys this page does not describe", note: "from a newer server" },
];

/** Field describes one form key: its label, help and section. */
export interface Field {
  label: string;
  help: string;
  section: SectionId;
}

/** FIELDS describes the form keys of the settings claim contract
 * (ST-1..ST-11, ST-31, ST-32). A key the server sends that is not here goes to the
 * "unlisted" section under its own name. */
export const FIELDS: Readonly<Record<string, Field>> = {
  "ide.editor": { label: "Editor for links", help: "Which editor the IDE buttons open.", section: "editor" },
  "ui.labels": { label: "Label style", help: "How ids, kinds and confidences are worded.", section: "labels" },
  "ui.impact_depth": { label: "Impact depth", help: "How many steps the Impact tab's change impact starts with (1 to 10).", section: "labels" },
  "ui.chain_depth": { label: "Chain depth", help: "How many steps the architecture evidence chain starts with (1 to 10).", section: "labels" },
  "index.shape_depth": { label: "Route shape depth", help: "How many proven calls the route shape readers follow from a handler (1 to 10).", section: "indexing" },
  "tests.include": { label: "Test files: include", help: "Extra test patterns; exclude wins over include and the built-in test rules.", section: "indexing" },
  "tests.exclude": { label: "Test files: exclude", help: "Patterns that are never test files.", section: "indexing" },
  "openapi.base_paths": { label: "OpenAPI base paths", help: "Base path for spec files or directories; replaces the servers the spec names.", section: "matching" },
  "clients.base_paths": { label: "Client base paths", help: "Base path for client calls in a directory whose base Graphvein could not read.", section: "matching" },
  "openapi.include": { label: "OpenAPI files: include", help: "JSON or YAML files read as specs besides the ones found by name and content.", section: "matching" },
  "openapi.exclude": { label: "OpenAPI files: exclude", help: "Files never read as specs.", section: "matching" },
  "clients.link_generated_output": { label: "Link generated output", help: "Link calls in committed bundler output (off: listed as not linked by design).", section: "matching" },
  "architecture.hide": { label: "Hidden components", help: "Component directories hidden by default on the map.", section: "architecture" },
};

/** fieldOf describes a key, falling back to its own name. */
export function fieldOf(key: string): Field {
  return FIELDS[key] ?? { label: key, help: "A key this page does not describe; its value is shown as the server sends it.", section: "unlisted" };
}

/** BROWSER_KEYS have a per-browser choice that wins over the repository. */
export const BROWSER_KEYS: readonly string[] = ["ui.labels", "ide.editor", "ui.impact_depth", "ui.chain_depth"];

/** sameValue compares two values (lists and maps by their entries in
 * order: the file's order is what the form shows). */
export function sameValue(a: SettingValue, b: SettingValue): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

function isMap(v: SettingValue): v is MapEntry[] {
  return Array.isArray(v) && v.length > 0 && typeof v[0] === "object";
}

/** formatValue words a value for the ladder. */
export function formatValue(value: SettingValue): string {
  if (value === null) return "not set";
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") return String(value);
  if (typeof value === "string") return value;
  if (value.length === 0) return "none";
  if (isMap(value)) return value.map((e) => `${e.key}: ${e.value}`).join(", ");
  return value.join(", ");
}

/** Scope is a rung of the ladder. */
export type Scope = "browser" | "repository" | "builtin";

/** Rung is one row of a key's ladder. */
export interface Rung {
  scope: Scope;
  /** The value as words ("not set" when the scope sets nothing). */
  text: string;
  /** The scope sets a value. */
  set: boolean;
  /** The value in use: bold. At most one rung is in use. */
  inUse: boolean;
  /** Repository rung: the key's line in graphvein.yaml. */
  line: number | null;
  /** Repository rung: the origin is unknown (the file does not load, or no
   * checkout), so neither a value nor its absence is known. */
  unknown: boolean;
  /** Repository rung: another key of the file decides instead of this one
   * (ide.link_template for ide.editor); line is that key's line. */
  lockedBy: string | null;
}

/** LOCK_WORDS names what a locking key decides, for the ladder. */
const LOCK_WORDS: Readonly<Record<string, string>> = { "ide.link_template": "the link template" };

/** BrowserChoice is a key's per-browser rung: null when the key has none,
 * else the stored choice in words (null text: nothing chosen). */
export type BrowserChoice = { text: string | null } | null;

/**
 * ladder derives a key's rungs (row ST-49): This browser (only keys with a
 * per-browser choice), Repository, Built-in. The value in use is the first
 * rung that sets one, in that order. An unknown repository origin proves
 * nothing about the file, so the built-in rung is never marked in use
 * then: only a browser choice is. A key another key of the file locks
 * (ide.editor while ide.link_template is set) has that key as its
 * repository value, at that key's line: neither its own value nor the
 * built-in default is in use then.
 */
export function ladder(key: ConfigKey, browser: BrowserChoice, format: (v: SettingValue) => string = formatValue): Rung[] {
  const rungs: Rung[] = [];
  const browserSet = browser !== null && browser.text !== null;
  if (browser !== null) {
    rungs.push({ scope: "browser", text: browser.text ?? "not set", set: browserSet, inUse: browserSet, line: null, unknown: false, lockedBy: null });
  }
  const unknown = key.origin === "unknown";
  const lock = unknown ? null : key.locked_by;
  const repoSet = key.origin === "file" || lock !== null;
  let text = unknown ? "unknown" : repoSet ? format(key.value) : "not set";
  if (lock !== null) {
    text = `${LOCK_WORDS[lock.key] ?? "the value"} (${lock.key})`;
    if (key.origin === "file") text += `; ${key.key}: ${format(key.value)} is not used`;
  }
  rungs.push({
    scope: "repository",
    text,
    set: repoSet,
    inUse: repoSet && !browserSet,
    line: lock !== null ? lock.line : repoSet ? key.line : null,
    unknown,
    lockedBy: lock?.key ?? null,
  });
  const builtinSet = key.default !== null;
  rungs.push({
    scope: "builtin",
    text: builtinSet ? format(key.default) : "none",
    set: builtinSet,
    inUse: builtinSet && !browserSet && !repoSet && !unknown,
    line: null,
    unknown: false,
    lockedBy: null,
  });
  return rungs;
}

/** inUseWords says which value applies, for the line under a ladder. */
export function inUseWords(rungs: readonly Rung[]): string {
  const r = rungs.find((x) => x.inUse);
  if (r !== undefined) {
    if (r.lockedBy !== null) return `${r.text}, from graphvein.yaml${r.line !== null ? ` line ${String(r.line)}` : ""}`;
    const from = r.scope === "browser" ? "this browser" : r.scope === "repository" ? "graphvein.yaml" : "the built-in default";
    return `${r.text}, from ${from}`;
  }
  if (rungs.some((x) => x.unknown)) return "unknown: graphvein.yaml could not be read";
  return "nothing: not set";
}

/** Filter is the page's filter (variant B's, kept in A). */
export type Filter = "all" | "changed" | "browser";

export const FILTERS: readonly { id: Filter; label: string }[] = [
  { id: "all", label: "All" },
  { id: "changed", label: "Changed from default" },
  { id: "browser", label: "This browser only" },
];

/** changedFromDefault is true when the value in use is not the built-in
 * default: a browser choice, or a repository value that differs from the
 * default. An unknown origin proves no change. */
export function changedFromDefault(key: ConfigKey, browser: BrowserChoice): boolean {
  if (browser !== null && browser.text !== null) return true;
  return key.origin === "file" && !sameValue(key.value, key.default);
}

/** keyShown applies the filter to a form key; a key with an unsaved
 * change is always shown, so an edit never disappears under the filter. */
export function keyShown(filter: Filter, key: ConfigKey, browser: BrowserChoice, drafted: boolean): boolean {
  if (drafted) return true;
  if (filter === "browser") return BROWSER_KEYS.includes(key.key);
  if (filter === "changed") return changedFromDefault(key, browser);
  return true;
}

/** DraftEntry is an unsaved change of a repository value. */
export type DraftEntry = { op: "set"; value: Exclude<SettingValue, null> } | { op: "remove" };

export type Draft = Readonly<Record<string, DraftEntry>>;

function empty(v: SettingValue): boolean {
  return v === null || (Array.isArray(v) && v.length === 0);
}

/**
 * withDraft records that the form now holds next for key. A value equal to
 * the file's drops the entry (nothing to save); an empty value removes a
 * key the file sets and is no change for a key it does not set.
 */
export function withDraft(draft: Draft, key: ConfigKey, next: SettingValue): Draft {
  const out: Record<string, DraftEntry> = Object.fromEntries(Object.entries(draft).filter(([k]) => k !== key.key));
  const inFile = key.origin === "file";
  if (empty(next)) {
    if (inFile) out[key.key] = { op: "remove" };
    return out;
  }
  if (inFile && sameValue(key.value, next)) return out;
  out[key.key] = { op: "set", value: next as Exclude<SettingValue, null> };
  return out;
}

/** draftValue is what the form shows for key: the draft, else the file's
 * value (null: not set in the file). */
export function draftValue(draft: Draft, key: ConfigKey): SettingValue {
  const d = draft[key.key];
  if (d === undefined) return key.origin === "file" ? key.value : null;
  return d.op === "remove" ? null : d.value;
}

/** operations turns the draft into the typed edit, in form order. */
export function operations(config: Config, draft: Draft): Operation[] {
  const ops: Operation[] = [];
  for (const k of config.keys) {
    const d = draft[k.key];
    if (d === undefined) continue;
    ops.push(d.op === "remove" ? { op: "remove", key: k.key } : { op: "set", key: k.key, value: d.value });
  }
  return ops;
}

/** draftSummary counts the unsaved changes and when they apply. */
export function draftSummary(config: Config, draft: Draft): { count: number; nextRun: number } {
  let count = 0;
  let nextRun = 0;
  for (const k of config.keys) {
    if (draft[k.key] === undefined) continue;
    count++;
    if (k.applies === "next_index_run") nextRun++;
  }
  return { count, nextRun };
}

/** appliesWords words a draft's timing for the save bar. */
export function appliesWords(summary: { count: number; nextRun: number }): string {
  if (summary.nextRun === 0) return summary.count === 1 ? "applies at once" : "all apply at once";
  if (summary.nextRun === summary.count) return summary.count === 1 ? "applies at the next index run" : summary.count === 2 ? "both apply at the next index run" : "all apply at the next index run";
  return `${String(summary.nextRun)} apply at the next index run, the others at once`;
}

/** linesOf and parseLines turn lists ("one per line") and maps ("key:
 * value" per line) into text and back; a malformed map line is null. */
export function linesOf(value: SettingValue, kind: "list" | "map"): string {
  if (!Array.isArray(value)) return "";
  if (kind === "map") return (value as { key: string; value: string }[]).map((e) => `${e.key}: ${e.value}`).join("\n");
  return (value as string[]).join("\n");
}

export function parseLines(text: string, kind: "list" | "map"): SettingValue | undefined {
  const lines = text
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "");
  if (lines.length === 0) return null;
  if (kind === "list") return lines;
  const entries: { key: string; value: string }[] = [];
  for (const l of lines) {
    const i = l.indexOf(": ");
    if (i <= 0) return undefined;
    entries.push({ key: l.slice(0, i).trim(), value: l.slice(i + 2).trim() });
  }
  return entries;
}

/** LineField names what one line of graphvein.yaml feeds. */
export interface LineField {
  text: string;
  /** "field": a form field; "editor": a key another page edits; "none":
   * a key no form edits. */
  tone: "field" | "editor" | "none";
}

const EDITED_ELSEWHERE: Readonly<Record<string, string>> = {
  "explorer.path_values": "Explorer path values",
  "tryit.environments": "Try it environments",
};

/** lineFields maps line numbers of graphvein.yaml to the field each feeds:
 * the form keys at their key's line, and every key outside the form (its
 * editor, or "no form"). Nothing is said for a file that does not load. */
export function lineFields(config: Config): Map<number, LineField> {
  const out = new Map<number, LineField>();
  if (config.config_error !== null) return out;
  for (const k of config.keys) {
    if (k.origin === "file" && k.line !== null) out.set(k.line, { text: fieldOf(k.key).label, tone: "field" });
  }
  for (const o of config.other_keys) {
    if (out.has(o.line)) continue;
    const editor = EDITED_ELSEWHERE[o.key];
    out.set(o.line, editor !== undefined ? { text: editor, tone: "editor" } : { text: "no form", tone: "none" });
  }
  return out;
}

/** shortHash shows the first characters of a "sha256:..." hash. */
export function shortHash(hash: string | null): string {
  if (hash === null) return "unknown";
  if (hash === "absent") return "no file yet";
  const [algo, hex] = hash.includes(":") ? hash.split(":", 2) : ["", hash];
  return `${algo === "" ? "" : `${algo ?? ""}:`}${(hex ?? "").slice(0, 4)}…`;
}

/** keptWords says what the result keeps, only as far as the answer
 * proves it: the comments lost are the server's comparison of both texts
 * (listed with the changes), keys outside the form its other_keys_changed.
 * It never says comments are kept (row ST-27): with no lost comment it
 * says nothing about comments. */
export function keptWords(result: EditResult): string {
  const parts: string[] = [];
  if (result.reformatted) parts.push("The file could not be edited line by line: it is re-encoded with two-space indentation and without blank lines.");
  const lost = result.lost_comments;
  if (lost !== null && lost.length > 0) {
    parts.push(`${lost.length === 1 ? "1 comment" : `${String(lost.length)} comments`} of graphvein.yaml will be lost (listed above).`);
  }
  if (!result.other_keys_changed) parts.push("Keys the form does not edit are unchanged.");
  return parts.join(" ");
}
