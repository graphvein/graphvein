// The Schema entry's tag in the sidebar (D1, round 3 variant Y for Q4-Q6,
// T018b2 Revision 1). One pure function from the summary, so the tag and
// its tests cannot drift. Nothing says "none found" unless found is exactly
// "none"; while the summary is loading or failed there is no tag.
//
// Precedence when several apply (Y): unknown reasons, then unreadable,
// then tests only.

import type { SchemaSummary } from "@/api/schema";
import { unreadableCount } from "@/sections/schema/state";

export interface EntryTag {
  text: string;
  /** "muted" for plain notes, "warn" for the unreadable warning. */
  tone: "muted" | "warn";
  /** Tooltip; also completes the accessible name ("Schema, <text>"). */
  title: string;
}

function plural(n: number, one: string, many: string): string {
  return `${String(n)} ${n === 1 ? one : many}`;
}

/** UNKNOWN_TITLE names each summary unknown_reason for the tooltip. */
export function unknownTitle(reason: string | null, s: SchemaSummary): string {
  switch (reason) {
    case "version_ambiguous":
      return "Schema not known: this index was built with extractors-27, before or after schema extraction existed; re-index";
    case "predates_schema":
      return "Schema not known: this index was built before schema extraction existed; re-index";
    case "external_indexer":
      return "Schema not known: this index comes from another indexer, so no migration set says nothing";
    case "version_unreadable":
      return "Schema not known: the index's extractor version cannot be read";
    case "scan_incomplete":
      return `Schema not known: ${s.scan_gaps === null ? "some paths" : plural(s.scan_gaps, "path", "paths")} not scanned may hide a Cargo manifest`;
    case "manifest_unreadable":
      return `Schema not known: ${s.unreadable_manifests === null ? "a Cargo manifest" : plural(s.unreadable_manifests, "Cargo manifest", "Cargo manifests")} could not be read`;
    default:
      return `Schema not known (reason ${reason ?? "none given"})`;
  }
}

/** schemaEntryTag is the Schema entry's tag, or null for no tag. summary
 * is null while loading or after a failed request. */
export function schemaEntryTag(summary: SchemaSummary | null): EntryTag | null {
  if (summary === null) return null;
  const f = summary.found;
  if (f === "none") {
    // T018f: a tool found but not read is never "none found".
    const unread = summary.unread_tools ?? [];
    if (unread.length > 0) return { text: "not read yet", tone: "muted", title: `${unread.some((t) => t.tool === "alembic") ? "Alembic migrations" : unread.some((t) => t.tool === "prisma-migrate") ? "Prisma migrations" : unread.some((t) => t.tool === "drizzle-kit") ? "Drizzle migrations" : "Migrations"} found that Graphvein does not read yet` };
    return { text: "none found", tone: "muted", title: "No sqlx migration set among the indexed manifests (this does not mean there is no database)" };
  }
  if (f !== "sets") {
    const reason = f === "unknown" ? summary.unknown_reason : null;
    if (reason === "not_indexed") return null;
    const title = f === "unknown" ? unknownTitle(reason, summary) : `Schema state "${f}" is not known to this UI`;
    if (reason === "version_ambiguous" || reason === "predates_schema") return { text: "re-index", tone: "muted", title };
    return { text: "unknown", tone: "muted", title };
  }
  const unreadable = unreadableCount(summary);
  if (unreadable === null) return { text: "unknown", tone: "muted", title: "Whether every migration set could be read is not known (counts missing)" };
  // Scan gaps are never added to "N unreadable" (a Cargo.toml gap is
  // already an unreadable manifest, and the server answers found unknown,
  // scan_incomplete, for any gap); a missing count is unknown (rule 14).
  if (summary.scan_gaps === null) return { text: "unknown", tone: "muted", title: "Whether every path was scanned is not known (scan gap count missing)" };
  if (unreadable > 0) {
    const parts: string[] = [];
    if ((summary.failed_sets ?? 0) > 0) parts.push(plural(summary.failed_sets ?? 0, "set the schema pass failed on", "sets the schema pass failed on"));
    if ((summary.sets_without_records ?? 0) > 0) parts.push(plural(summary.sets_without_records ?? 0, "set with nothing stored", "sets with nothing stored"));
    if ((summary.unreadable_manifests ?? 0) > 0) parts.push(plural(summary.unreadable_manifests ?? 0, "Cargo manifest not read", "Cargo manifests not read"));
    return { text: `${String(unreadable)} unreadable`, tone: "warn", title: parts.join(", ") };
  }
  if (summary.sets !== null && summary.sets > 0 && summary.sets === summary.test_only_sets) {
    return { text: "tests only", tone: "muted", title: `Every migration set (${String(summary.sets)}) is test-only` };
  }
  return null;
}
