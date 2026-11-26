// The "unverified" mark (T018g0, ADR 0058): a model fact whose claim is
// verification "mapped" comes from the ORM's own per-dialect mapping
// source and was not checked against that database (developer decision
// 2026-11-09). The table header and the column carry an "unverified" chip;
// its popover says where the fact comes from and what was not checked.
// Facts verified by the ORM's oracle ("oracle") carry no mark; anything
// else (mapped, missing or another value) is unverified (T018g0 Revision
// 2: an unknown verification never looks verified).

import type { Claim, SchemaTable } from "@/api/schema";
import { claimSources } from "@/sections/schema/RowChip";
import { WhyButton, type Why } from "@/sections/schema/WhyPopover";

/** ORACLE is the verification of a fact the ORM's oracle checked; the
 * only value that carries no mark. */
export const ORACLE = "oracle";

/** MAPPED is the verification of a fact taken from the ORM's mapping. */
export const MAPPED = "mapped";

/** mappedClaims are the claims of a model record that are not
 * oracle-verified: mapped, or with a missing or unknown verification. */
export function mappedClaims(claims: readonly Claim[] | null): Claim[] {
  return claims === null ? [] : claims.filter((c) => c.verification !== ORACLE);
}

/** unrecorded says whether a verification value is neither oracle nor
 * mapped: not recorded (an older index) or not known to the view. */
export function unrecorded(v: unknown): boolean {
  return v !== ORACLE && v !== MAPPED;
}

/** mappedCount is the number of a model table's column facts that are not
 * oracle-verified, from /sets' verification_counts (T018g0 Revision 1:
 * /sets serves no model claims; /table does). 0 when the counts are not
 * sent. */
export function mappedCount(t: SchemaTable): number {
  let n = 0;
  for (const [k, v] of Object.entries(t.verification_counts ?? {})) if (k !== ORACLE) n += v;
  return n;
}

/** countsUnrecorded says whether the counts hold a verification other than
 * oracle and mapped. */
export function countsUnrecorded(t: SchemaTable): boolean {
  return Object.entries(t.verification_counts ?? {}).some(([k, v]) => v > 0 && unrecorded(k));
}

/** tableUnverified says whether a model table carries the "unverified"
 * mark in the list: a column fact or its own facts not oracle-verified. */
export function tableUnverified(t: SchemaTable): boolean {
  return t.node.attributes.source === "model" && (mappedCount(t) > 0 || t.node.attributes.verification !== ORACLE);
}

/** countLabel names the mapped column facts by count, for a view that has
 * the counts but not yet the claims. */
export function countLabel(n: number): string {
  return n === 1 ? "1 column fact" : `${String(n)} column facts`;
}

/** UnverifiedMark is the list row's "unverified" mark (T018g0 Revision 1):
 * from the counts only, so a plain mark with a title; the facts, rows and
 * sources are in the table's popover. */
export function UnverifiedMark({ t, tool }: { t: SchemaTable; tool: string | null }) {
  if (!tableUnverified(t)) return null;
  const dialect = typeof t.node.attributes.dialect === "string" ? t.node.attributes.dialect : null;
  const n = mappedCount(t);
  const own = t.node.attributes.verification !== ORACLE;
  const what = [...(own ? ["table facts"] : []), ...(n > 0 ? [countLabel(n)] : [])].join(" and ");
  const note = own && unrecorded(t.node.attributes.verification) || countsUnrecorded(t) ? ` ${UNRECORDED}` : "";
  return (
    <span
      className="ml-1.5 rounded-[3px] border border-dashed border-dynamic/70 px-1 font-sans text-[11px] font-semibold text-dynamic"
      title={`${mappedLine(tool, dialect)}${note} Unverified: ${what}. Open the table for the facts and their sources.`}
      data-testid="row-unverified-mark"
    >
      unverified
    </span>
  );
}

/** mappedLine is the popover line of a mapped fact: from which mapping it
 * comes and which database did not check it. */
export function mappedLine(tool: string | null, dialect: string | null): string {
  const who = tool === null ? "the ORM" : tool;
  return dialect === null
    ? `From ${who}'s own mapping; not checked against a database.`
    : `From ${who}'s ${dialect} mapping; not checked against a ${dialect} database.`;
}

/** factLabel names a claim's fact, with its dialect for a per-dialect
 * fact (ADR 0065): "type (mysql)". */
export function factLabel(c: Pick<Claim, "fact" | "dialect">): string {
  return c.dialect === null ? c.fact : `${c.fact} (${c.dialect})`;
}

/** mappedLines are the popover's mapping lines: one per dialect of the
 * per-dialect claims (ADR 0065), and the table's line for the single
 * facts (or when there are no claims). */
export function mappedLines(tool: string | null, dialect: string | null, claims: readonly Claim[]): string[] {
  const ds = [...new Set(claims.map((c) => c.dialect).filter((d): d is string => d !== null))].sort();
  const single = claims.length === 0 || claims.some((c) => c.dialect === null);
  return [...(single ? [mappedLine(tool, dialect)] : []), ...ds.map((d) => mappedLine(tool, d))];
}

/** UNRECORDED is the popover note for a verification that is neither
 * oracle nor mapped (T018g0 Revision 2). */
export const UNRECORDED = "Some verification is not recorded (an index built before extractors-31, or an extractor error); not treated as verified.";

/** verificationWhy is the popover of an "unverified" chip. facts names the
 * unverified facts ("type", "users.id type", ...); note adds UNRECORDED. */
export function verificationWhy(name: string, source: string, tool: string | null, dialect: string | null, facts: readonly string[], claims: readonly Claim[], note = false): Why {
  return {
    kind: "verification",
    title: `${name}: mapped, not verified`,
    source,
    lead: (
      <>
        {mappedLines(tool, dialect, claims).map((l) => (
          <span key={l}>
            <span data-testid="mapped-line">{l}</span>{" "}
          </span>
        ))}
        {(note || claims.some((c) => unrecorded(c.verification))) && <span data-testid="unrecorded-line">{UNRECORDED} </span>}
        {facts.length > 0 && <span className="text-muted-foreground">Facts: {facts.join(", ")}.</span>}
      </>
    ),
    rows: [...new Set(claims.map((c) => c.row).filter((r): r is string => r !== null))].sort(),
    sources: claimSources(claims),
    entry: null,
  };
}

/** UnverifiedChip is the "unverified" chip; nothing when every fact is
 * oracle-verified and the record itself is. note marks a record-level
 * verification that is not recorded. */
export function UnverifiedChip({ name, source, tool, dialect, facts, claims, own = false, note = false, testId = "unverified-chip" }: { name: string; source: string; tool: string | null; dialect: string | null; facts: readonly string[]; claims: readonly Claim[]; own?: boolean; note?: boolean; testId?: string }) {
  if (facts.length === 0 && !own) return null;
  return (
    <WhyButton
      testId={testId}
      label={`${name}: unverified, why`}
      className="ml-1 inline-flex h-[18px] items-center rounded-[4px] border border-dashed border-dynamic/70 px-1 font-sans text-[11px] font-semibold whitespace-nowrap text-dynamic hover:brightness-95"
      why={() => verificationWhy(name, source, tool, dialect, facts, claims, note)}
    >
      unverified
    </WhyButton>
  );
}
