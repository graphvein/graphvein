// Per-dialect facts of a model column (ADR 0065, T018g0b). A group
// whose dialect is chosen at run time (GORM: mysql, postgres or sqlite)
// claims its dialect-dependent facts once per dialect, each with its own
// verification. The grid shows them per dialect, never as one bare value:
// one line per dialect, or one line "on a, b, c" when every dialect's value
// is known and equal. Each line carries the verification of its own claims
// (a mapped mysql claim is marked even when postgres is oracle-verified); a
// line is verified only when every known value on it has an oracle claim.

import { Check } from "lucide-react";

import type { Claim } from "@/api/schema";
import { autoCell, byDialect, dialectDefaultCell, dialectFacts, dialectLines, modelTypeCell, type Cell, type DialectLine, type RuleId } from "@/sections/schema/cells";
import { CellText, KeyChipView } from "@/sections/schema/parts";
import { mappedLine, ORACLE, MAPPED, UNRECORDED } from "@/sections/schema/Verification";
import { WhyButton, type Why } from "@/sections/schema/WhyPopover";

/** TYPE_FACTS are the per-dialect facts that make up a column's type. */
export const TYPE_FACTS = ["type", "type_family", "length", "precision", "scale", "timezone"] as const;

/** A per-dialect value of one column: its rule, its lines and the claim
 * facts the lines show (for their verification). */
export interface DialectValue {
  rule: RuleId;
  lines: DialectLine[];
  facts: readonly string[];
}

/** columnDialects lists the dialects a column's per-dialect facts are
 * read for: the table's dialects, else (an inconsistent record) the
 * column's own by_dialect keys. */
export function columnDialects(attrs: Record<string, unknown>, table: readonly string[] | null): string[] {
  if (table !== null && table.length > 0) return [...table];
  const b = attrs.by_dialect;
  return b !== null && typeof b === "object" && !Array.isArray(b) ? Object.keys(b).sort() : [];
}

/** dialectType is the Type cell of a column with per-dialect type or
 * autoincrement facts; null when it states none (the flat type applies). */
export function dialectType(attrs: Record<string, unknown>, dialects: readonly string[], completeness: string): DialectValue | null {
  const facts = dialectFacts(attrs);
  const typed = TYPE_FACTS.some((f) => facts.has(f));
  const auto = facts.has("autoincrement");
  if (dialects.length === 0 || (!typed && !auto)) return null;
  const { rule, lines } = dialectLines(dialects, (d) => {
    const e = byDialect(attrs, d);
    return [...(typed ? [modelTypeCell(e)] : []), ...(auto ? [autoCell(e.autoincrement, completeness)] : [])];
  });
  return { rule, lines, facts: [...(typed ? TYPE_FACTS : []), ...(auto ? ["autoincrement"] : [])] };
}

/** dialectDefault is the Default cell of a column with a per-dialect
 * server default. Its default_state is "unknown" (the flat server_default
 * is null, ADR 0065), so only that state reads by_dialect; null otherwise. */
export function dialectDefault(attrs: Record<string, unknown>, defaultState: string, dialects: readonly string[], completeness: string): DialectValue | null {
  if (defaultState !== "unknown" || dialects.length === 0 || !dialectFacts(attrs).has("server_default")) return null;
  const { rule, lines } = dialectLines(dialects, (d) => [dialectDefaultCell(byDialect(attrs, d), attrs, completeness)]);
  return { rule, lines, facts: ["server_default"] };
}

/** LineVerification is what a line's claims say: verified (every known
 * value on every dialect of the line has an oracle claim), unverified
 * (with the dialects whose claims are mapped, whether some claim is
 * missing or not recorded, and the line's dialects that are unverified),
 * or nothing to tell (null). */
export type LineVerification = { kind: "oracle"; dialects: string[] } | { kind: "unverified"; mapped: string[]; unrecorded: boolean; dialects: string[] } | null;

/** lineVerification reads the claims of a line's facts on its dialects
 * (T018g0b Revision 1, A1): for every dialect of the line and every fact
 * whose value is known (non-null) in that dialect's entry, an oracle claim
 * for that fact and dialect must exist. A mapped claim makes the line
 * unverified; a missing claim, or one whose verification is not
 * recorded, too (never treated as verified). No claims at all (/sets
 * serves none) or no known value shows no mark. */
export function lineVerification(claims: readonly Claim[] | null, attrs: Record<string, unknown>, line: DialectLine, facts: readonly string[]): LineVerification {
  if (claims === null) return null;
  const mapped = new Set<string>();
  const unverified = new Set<string>();
  let unrecorded = false;
  let known = 0;
  for (const d of line.dialects) {
    const entry = byDialect(attrs, d);
    for (const f of facts) {
      const v = entry[f];
      if (v === null || v === undefined) continue;
      known++;
      const c = claims.find((x) => x.dialect === d && x.fact === f);
      if (c !== undefined && c.verification === ORACLE) continue;
      unverified.add(d);
      if (c !== undefined && c.verification === MAPPED) mapped.add(d);
      else unrecorded = true;
    }
  }
  if (known === 0) return null;
  if (unverified.size === 0) return { kind: "oracle", dialects: [...line.dialects] };
  return { kind: "unverified", mapped: line.dialects.filter((d) => mapped.has(d)), unrecorded, dialects: line.dialects.filter((d) => unverified.has(d)) };
}

/** unclaimedFacts are a column's per-dialect facts whose value is known
 * but which have no claim for that dialect (T018g0b Revision 1): the lines
 * count them as unverified, so the column's "unverified" chip must too.
 * Mapped and unrecorded claims are already in the chip's claims; only the
 * missing ones are listed here. No claims at all (/sets) lists none, as
 * the lines then show no mark. */
export function unclaimedFacts(claims: readonly Claim[] | null, attrs: Record<string, unknown>, values: readonly (DialectValue | null)[]): { fact: string; dialect: string }[] {
  if (claims === null) return [];
  const out: { fact: string; dialect: string }[] = [];
  for (const value of values) {
    if (value === null) continue;
    for (const d of value.lines.flatMap((l) => l.dialects)) {
      const entry = byDialect(attrs, d);
      for (const f of value.facts) {
        const v = entry[f];
        if (v === null || v === undefined) continue;
        if (!claims.some((x) => x.dialect === d && x.fact === f)) out.push({ fact: f, dialect: d });
      }
    }
  }
  return out;
}

/** columnUnclaimed is unclaimedFacts for a model column's Type and
 * Default cells, read as the grid reads them. */
export function columnUnclaimed(attrs: Record<string, unknown>, defaultState: string, claims: readonly Claim[] | null, tableDialects: readonly string[] | null, completeness: string): { fact: string; dialect: string }[] {
  const ds = columnDialects(attrs, tableDialects);
  return unclaimedFacts(claims, attrs, [dialectType(attrs, ds, completeness), dialectDefault(attrs, defaultState, ds, completeness)]);
}

/** VerificationMark is a line's verification: a dashed "unverified" tag
 * for a fact from the ORM's mapping (the wording of the unverified
 * popover), a quiet check for an oracle-verified one. On a joined line
 * whose dialects differ in verification, the tag names the unverified
 * ones ("unverified on mysql"); of is the line's dialects. */
function VerificationMark({ v, tool, of }: { v: LineVerification; tool: string | null; of: readonly string[] }) {
  if (v === null) return null;
  if (v.kind === "oracle") {
    return (
      <span className="inline-flex text-muted-foreground" title={`Verified on ${v.dialects.join(", ")}`} data-testid="dialect-oracle">
        <Check aria-label="oracle-verified" className="size-3" />
      </span>
    );
  }
  const why = [...v.mapped.map((d) => mappedLine(tool, d)), ...(v.unrecorded ? [UNRECORDED] : [])].join(" ");
  return (
    <span
      className="rounded-[3px] border border-dashed border-dynamic/70 px-0.5 font-sans text-[10px] leading-[13px] font-semibold text-dynamic"
      title={`${v.mapped.length > 0 ? "Mapped, not verified." : "Not verified."} ${why}`}
      data-testid="dialect-unverified"
    >
      {v.dialects.length < of.length ? `unverified on ${v.dialects.join(", ")}` : "unverified"}
    </span>
  );
}

/** DialectValues renders a per-dialect value as compact lines. why opens
 * the popover of an unknown part (its fact and dialect). */
export function DialectValues({ value, attrs, claims, tool, why, testId, mono = true }: { value: DialectValue; attrs: Record<string, unknown>; claims: readonly Claim[] | null; tool: string | null; why: (c: Cell, dialects: string[]) => Why; testId: string; mono?: boolean }) {
  const agree = value.rule === "dialects.agree";
  // Inline flow: a line wraps between words, so a narrow column never
  // pushes the grid wider than its longest word; each line starts with its
  // dialect.
  return (
    <span className="grid gap-y-0.5" data-testid={testId} data-rule={value.rule}>
      {value.lines.map((l) => (
        <span key={l.dialects.join(",")} className="block" data-testid="dialect-line" data-dialect={l.dialects.join(",")}>
          {!agree && <span className="mr-1.5 font-sans text-[11px] text-muted-foreground">{l.dialects[0]}</span>}
          {l.parts.map((p, i) => {
            const sep = i > 0 ? " " : null;
            if (p.rule.startsWith("auto.")) {
              return p.text === "" ? null : (
                <span key={i}>
                  {sep}
                  <KeyChipView c={p} />
                </span>
              );
            }
            if (p.unknown) {
              return (
                <span key={i}>
                  {sep}
                  <WhyButton why={() => why(p, l.dialects)} className="text-muted-foreground italic underline decoration-dotted underline-offset-[3px]" label={`${p.text} on ${l.dialects.join(", ")}: why`} testId="unknown-cell">
                    <span title={p.title} data-rule={p.rule}>
                      {p.text}
                    </span>
                  </WhyButton>
                </span>
              );
            }
            return (
              <span key={i}>
                {sep}
                {mono ? <CellText c={p} className="font-mono text-http" /> : <CellText c={p} />}
                {p.rule === "default.python" && (
                  <span className="ml-1 rounded-[3px] bg-ipc-soft px-1 font-sans text-[10.5px] font-semibold text-ipc" title="Python-side default; the database has none" data-testid="py-chip">
                    PY
                  </span>
                )}
              </span>
            );
          })}
          {agree && (
            <>
              {" "}
              <span className="font-sans text-[11px] text-muted-foreground" data-testid="dialect-agree">
                on {l.dialects.join(", ")}
              </span>
            </>
          )}{" "}
          <VerificationMark v={lineVerification(claims, attrs, l, value.facts)} tool={tool} of={l.dialects} />
        </span>
      ))}
    </span>
  );
}

/** dialectsText is a group's or table's run-time dialects as a reader
 * says them: "mysql, postgres or sqlite". */
export function dialectsText(ds: readonly string[]): string {
  if (ds.length <= 1) return ds.join("");
  return `${ds.slice(0, -1).join(", ")} or ${ds[ds.length - 1] ?? ""}`;
}
