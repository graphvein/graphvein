// The words of the precision chips (T020, Variant A, wording O10 a). Every
// label shown comes from the API; these functions only say it in words and
// never decide one.

import type { FileLabel, ScopeLabel } from "@/api/endpoints";

/** PRECISE_TEXT is the Precise tooltip and popover lead (task wording). */
export const PRECISE_TEXT =
  "Graphvein's claim contract covers these forms, checked by golden fixtures, corpus checks, an independent review and an oracle where one is cheap. What it cannot read is shown as unknown. Precise does not mean complete.";

/** BEST_EFFORT_TEXT is the Best effort lead of the default branch
 * (reason default_branch): no row of the rule table names the scope. */
export const BEST_EFFORT_TEXT =
  "Read from the syntax without a claim contract. Facts shown are usually right but have not met Graphvein's verification bar, and a missing item does not mean it is absent.";

/** REFUSED_TEXT is the Best effort lead for every other reason: a row may
 * exist, but Graphvein cannot apply its rule table to these facts. It
 * makes no claim about whether a claim contract exists. */
export const REFUSED_TEXT =
  "Graphvein cannot apply its rule table to these facts, so it does not vouch for them here. Facts shown are usually right, and a missing item does not mean it is absent.";

/** bestEffortLead is the tooltip and popover lead of a Best effort label,
 * chosen by the label's reason. */
export function bestEffortLead(reason: string | null): string {
  return reason === null || reason === "default_branch" ? BEST_EFFORT_TEXT : REFUSED_TEXT;
}

/** NEVER_COMPLETE is the popover footer: a guarantee is not completeness. */
export const NEVER_COMPLETE = "Precise never means complete: unknowns, unverified (mapped dialect) facts and confidences still show on each fact.";

/** REPORTED_TEXT explains a file label an external indexer reported. */
export const REPORTED_TEXT =
  "The external indexer that built this index reported this file label. Graphvein did not check it and applies no precision label of its own to this run.";

/** Side is the comparison side whose run decided a change item's label
 * (/changes/items scope_side); null outside a comparison. */
export type Side = "head" | "base" | null;

/** REASON_TEXT says why a label is Best effort, for every reason the
 * server can send. Its keys are the server's list
 * (internal/server/testdata/precision-reasons.golden.json); a test keeps
 * them 1:1. */
export const REASON_TEXT: Record<string, string> = {
  default_branch: "No row of the label rule table names this scope (default branch D).",
  marker_conflict:
    "The records of this group name more than one tool or dialect, so no single row of the rule table can be applied to the group as a whole.",
  extractor_mismatch:
    "This index was built with another extractor version than this binary's, so the rule table that earned a label may not be the one that produced these facts. Labels return after graphvein index.",
  external_indexer: "This index was built by an external indexer; Graphvein applies no precision label of its own to it.",
  no_run: "Nothing is indexed for this repository yet, so there are no facts to label. Labels appear after graphvein index.",
  stored_label_invalid: "The index stores a file label this binary's rule table does not give this language.",
  structure_refused:
    "The rule table labels this language Precise, but Graphvein read nothing of this file's structure (for Go: go/parser rejects the file or it holds a lone carriage return; for Python: CPython 3.14 rejects the file; for TypeScript, TSX and JavaScript: tree-sitter rejects the file; for Rust: tree-sitter reports a syntax error or the file holds a lone carriage return; or it could not be parsed), so the file claims no declaration or import and its label is Best effort.",
  structure_unrooted:
    "The rule table labels Rust Precise, but no crate root of a Cargo.toml Graphvein reads reaches this file: its declarations are read, its imports are not resolved as part of a crate and no build configuration compiles it, so its label is Best effort.",
  scope_unknown: "The head side cannot show this record's group, so its scope cannot be named.",
  not_compared:
    "This fact is outside its row's Precise scope: no oracle compares it (for Go routes: the handler, middleware, conditional and test scope), so it stays Best effort.",
};

/** BASE_REASON_TEXT replaces REASON_TEXT when the refused run is a
 * comparison's base side: reindexing relabels the head, never a base. */
const BASE_REASON_TEXT: Record<string, string> = {
  extractor_mismatch:
    "The comparison's base side was indexed with another extractor version than this binary's, so these base-side facts cannot be labelled. Reindexing does not relabel them: a stored base keeps the version it was built with.",
  external_indexer: "The comparison's base side was built by an external indexer; Graphvein applies no precision label of its own to its facts.",
};

/** reasonText says why a label is best effort, from the server's reason and
 * the comparison side the label is about. */
export function reasonText(reason: string | null, side: Side = null): string {
  const key = reason ?? "default_branch";
  if (side === "base") {
    const base = BASE_REASON_TEXT[key];
    if (base !== undefined) return base;
  }
  return REASON_TEXT[key] ?? `Reason: ${key}.`;
}

/** reasonShort is the tooltip footer's reason in a few words. */
export function reasonShort(reason: string | null, side: Side = null): string {
  if (reason === null || reason === "default_branch") return "default branch D";
  const words = reason === "extractor_mismatch" ? "refusal: extractor mismatch" : reason.replace(/_/g, " ");
  return side === "base" ? `${words} (base side)` : words;
}

/** ChipState is what one chip shows. "none": no chip at all. */
export type ChipState =
  | { kind: "none" }
  | { kind: "precise"; row: string | null }
  | { kind: "best_effort"; row: string | null; reason: string | null; side: Side }
  | { kind: "reported"; value: string };

/** scopeChip is the chip of a scope label. An external run gets no scope
 * chip (O6 a): the label would not be Graphvein's; no label, no chip. */
export function scopeChip(label: ScopeLabel | null | undefined, side: Side = null): ChipState {
  if (label === null || label === undefined) return { kind: "none" };
  if (label.reason === "external_indexer") return { kind: "none" };
  if (label.label === "precise") return { kind: "precise", row: label.row };
  return { kind: "best_effort", row: label.row, reason: label.reason, side };
}

/** fileChip is the chip of a file label: Graphvein's label, or for an
 * external run the producer's value as "reported" (only when it sent one). */
export function fileChip(file: FileLabel | null | undefined): ChipState {
  if (file === null || file === undefined) return { kind: "none" };
  if (file.reason === "external_indexer") return file.reported !== null ? { kind: "reported", value: file.reported } : { kind: "none" };
  if (file.precision === "precise") return { kind: "precise", row: file.row };
  // Row D is the default branch: the popover names it by the reason.
  if (file.precision === "best_effort") return { kind: "best_effort", row: file.row === "D" ? null : file.row, reason: file.reason, side: null };
  return { kind: "none" };
}

/** FAMILY_NAME names the families the API labels with row D. A family the
 * UI does not know is shown by its key. */
export const FAMILY_NAME: Record<string, string> = {
  schema_other: "Other schema groups",
  routes: "Routes",
  client_calls: "Client calls",
  ipc_commands: "IPC commands",
  ipc_calls: "IPC calls",
  server_actions: "Server actions",
  action_calls: "Action uses",
  code_calls: "Code calls",
  table_access_other: "Other code-to-table links",
  cross_repository_links: "Links across repositories",
};

export function familyName(family: string): string {
  return FAMILY_NAME[family] ?? family;
}
