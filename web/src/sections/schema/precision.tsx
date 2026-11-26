// The precision chip of a schema group (T020, Variant A): the group's
// scope label as /schema/sets and /schema/table serve it. The chip's name
// is the group's tool and source; the label is the server's.

import type { ScopeLabel } from "@/api/endpoints";
import type { SchemaGroup } from "@/api/schema";
import { PrecisionChip } from "@/components/precision/PrecisionChip";
import { scopeChip } from "@/components/precision/words";
import { dialectsText } from "@/sections/schema/Dialects";
import { tableUnverified } from "@/sections/schema/Verification";

/** groupScopeName names a group's scope on its chip ("prisma models",
 * "sqlx migrations"). */
export function groupScopeName(g: Pick<SchemaGroup, "set" | "source" | "tool" | "conflicts">): string {
  const tool = g.conflicts.includes("tool") ? "mixed tools" : (g.tool ?? "unknown tool");
  // T024c3: Supabase's generated types are a schema source, not ORM models.
  if (g.source === "models" && tool === "supabase_types") return "Supabase generated types";
  if (g.source === "models") return `${tool} models`;
  if (g.set === null) return "Other producer tables";
  return `${tool} migrations`;
}

/** GroupChip is a group's chip, or nothing when the server sent no label.
 * A row D label belongs to the popover's "Other schema groups" column. */
export function GroupChip({ group, label, testId = "group-precision", bare = false }: { group: SchemaGroup; label: ScopeLabel | null; testId?: string; bare?: boolean }) {
  const column = label !== null && label.row !== null && label.row !== "D" ? label.row : "schema_other";
  return <PrecisionChip state={scopeChip(label)} scope={groupScopeName(group)} focus={{ column }} testId={testId} bare={bare} />;
}

/** DialectHint is what the dialect note beside a Precise model group's
 * chip says (T020 review): Precise vouches for the forms the contract
 * reads, while some facts of the group are not checked against a
 * database. "dialect unknown" when the group states no dialect (a Prisma
 * provider that is not a literal, a Sequelize instance whose dialect is
 * not read), "mapped: unverified" when it states one or more; null when
 * every table and column fact the strip counts is oracle-verified, the
 * label is not Precise, or the group is not a model group. Only fields
 * /schema/sets serves are read: dialect, dialects, conflicts, and the
 * tables' verification and verification_counts. */
export interface DialectHint {
  text: "dialect unknown" | "mapped: unverified";
  title: string;
}

/** dialectHint decides a group's DialectHint from /schema/sets fields. */
export function dialectHint(g: Pick<SchemaGroup, "source" | "tool" | "dialect" | "dialects" | "conflicts" | "tables">, label: ScopeLabel | null): DialectHint | null {
  if (g.source !== "models" || scopeChip(label).kind !== "precise") return null;
  if (!g.tables.some((t) => tableUnverified(t))) return null;
  const who = g.tool ?? "the ORM";
  const conflict = g.conflicts.includes("dialect") || g.conflicts.includes("dialects");
  const stated = g.dialect !== null || (g.dialects !== null && g.dialects.length > 0);
  if (!stated && !conflict) {
    return {
      text: "dialect unknown",
      title: `This group states no database dialect, so its dialect-dependent facts are unknown and its other facts are not checked against a database (unverified). Precise covers the forms ${who}'s contract reads; it never means complete.`,
    };
  }
  const which = g.dialect ?? (g.dialects !== null && g.dialects.length > 0 ? dialectsText(g.dialects) : "its dialects");
  return {
    text: "mapped: unverified",
    title: `Some facts of this group come from ${who}'s mapping for ${which} and were not checked against that database (unverified, marked in each table). Precise covers the forms ${who}'s contract reads; it never means complete.`,
  };
}

/** DialectNote renders dialectHint beside a group chip, or nothing. */
export function DialectNote({ group, label, testId = "card-dialect-hint" }: { group: SchemaGroup; label: ScopeLabel | null; testId?: string }) {
  const hint = dialectHint(group, label);
  if (hint === null) return null;
  return (
    <span
      className="inline-flex h-4 flex-none items-center rounded-[4px] border border-dashed border-dynamic/70 px-1 font-sans text-[10.5px] leading-none font-semibold whitespace-nowrap text-dynamic"
      title={hint.title}
      data-testid={testId}
    >
      {hint.text}
    </span>
  );
}
