// The cell and value rules of the schema view (T018b2 section 5 and its
// Revision 1), as pure functions. Each function returns the text to show
// and the id of the rule row it applied; CELL_RULES lists every row, and
// cells.test.ts checks that the rows and the branches are 1:1 (rule 33
// style): every modelled value maps to exactly one row, and everything
// else falls to that field's single unknown row.
//
// Precision (VISION, principle 6): null, a missing attribute, `unknown` and a
// value this UI does not know never render as a known value: never as
// "nullable", "none", "no key", 0 or "Complete".

import { attrBool3, attrInt, attrObjects, attrString, attrStringList, rawText, type SchemaGroup } from "@/api/schema";

/** A rendered value: its text, the rule row that produced it, whether it
 * is shown in the unknown style, and an optional tooltip and second line. */
export interface Cell {
  rule: RuleId;
  text: string;
  unknown: boolean;
  title?: string;
  second?: string;
  /** The end of text shown in the unknown style (a type family's
   * "(precision unknown)", ADR 0065). */
  tail?: string;
}

/** CELL_RULES is the rule table of T018b2 section 5 (plus Revision 1),
 * one entry per rendered outcome. */
export const CELL_RULES = {
  "type.written": "type string: as written",
  "type.unknown": "type missing or another JSON type: unknown",
  "type.mapped": "model column type string: the mapped type with its stored length, precision, scale or timezone, annotation below (T018f)",
  "type.family": "model column type_family {name, unknown} and no type: the family name with each unknown modifier, e.g. 'numeric (precision, scale unknown)', or 'geometry (modifiers unknown)' for the generic modifiers of an extension type (ADR 0062 Revision 2); never a plain type (ADR 0065)",
  "tool_type.written": "model column tool_type string: '<tool> type: <value>', secondary, never a database fact (ADR 0065)",
  "tool_type.none": "model column tool_type missing or not a string: nothing",
  "auto.true": "model column autoincrement true: AI chip (the database generates the value; ADR 0065)",
  "auto.false": "model column autoincrement false on a complete table: nothing",
  "auto.unknown": "model column autoincrement null, other, or false on a table not complete: dashed AI? chip",
  "dialects.agree": "per-dialect values all known and equal on every dialect: one value 'on a, b, c' (ADR 0065)",
  "dialects.each": "per-dialect values otherwise: one line per dialect, a null value unknown on that dialect (ADR 0065)",
  "nullable.false": "nullable false: not null",
  "nullable.true": "nullable true: nullable",
  "nullable.unknown": "nullable null, missing or other: unknown",
  "default.written": "default_state written: the default text",
  "default.implied": "default_state implied: <default_implied> · implied",
  "default.none": "default_state none on a complete table: none",
  "default.none_in_source": "default_state none on a complete_in_source table: none ◑",
  "default.python": "model default_state python: the stored default value (or callable) with a PY chip; the database has none (T018f)",
  "default.client": "model default_state client: 'set by Prisma (client)' with the stored default function (uuid(), cuid(), ...); the Prisma client fills it in, the database has none (T018g1)",
  "default.server": "model default_state server: the stored server default text, or 'server default' when its text is not stored (T018f)",
  "default.unknown": "default_state unknown, other, or a value the table state does not allow: unknown",
  "key.true": "column primary_key/unique/index true: PK/UQ/IX chip",
  "key.false": "column primary_key/unique/index false: nothing",
  "key.unknown": "column primary_key/unique/index null, missing or other: dashed PK?/UQ?/IX? chip",
  "fk.present": "column with a stored foreign_key edge or a reference entry (linked, unresolved or unreadable): the entries; an unresolved one as red text and a dashed FK chip (T018f Revision 2, A1)",
  "fk.none": "column proven to have no foreign key (migration references null and no additional_references on a complete or complete_in_source table; model foreign_keys []): empty References cell, no FK chip (T018f Revision 2, C5)",
  "fk.unknown": "column whose foreign keys are not proven (model foreign_keys null or missing; migration references missing, or null on a partial or unknown table): unknown with a why popover and a dashed FK? chip (T018f Revision 2, C5)",
  "pk.list": "table primary_key list: (a, b)",
  "pk.none": "table primary_key [] on a complete table: none",
  "pk.none_in_source": "table primary_key [] on a complete_in_source table: none ◑",
  "pk.unknown": "table primary_key null, missing, other, or [] on a partial or unknown table: unknown",
  "uk.list": "table unique_keys list: listed",
  "uk.empty": "table unique_keys [] on a complete or complete_in_source table: nothing",
  "uk.unknown": "table unique_keys null, missing, other, or [] on a partial or unknown table: unique keys unknown",
  "schema.name": "table schema string: the name",
  "schema.unqualified": "table schema null: unqualified (search_path)",
  "schema.unknown": "table schema missing or other: unknown",
  "completeness.complete": "completeness complete: check Complete",
  "completeness.in_source": "completeness complete_in_source: half circle, N opaque after",
  "completeness.partial": "completeness partial: amber Partial · N reasons",
  "completeness.unknown": "completeness unknown or other: dashed Unknown",
  "absence.proven": "an absence (no FK, no referencing table, no column, ...) on a complete table: stated plainly",
  "absence.in_source": "an absence on a complete_in_source table: stated with ◑",
  "absence.unresolved": "an outgoing absence with unresolved references: the unresolved table texts, never 'none'",
  "absence.unknown": "an absence on a partial, unknown or other table: none known (last known)",
  "absence.incoming.proven":
    "an incoming absence (no table references X) on a complete X in a set whose table list is complete, whose every table is complete or complete_in_source and none has unresolved references: stated plainly (Review fixes R2)",
  "absence.incoming.unknown": "an incoming absence otherwise: none known, with every reason it cannot be proven (Review fixes R2)",
  "count.value": "statement count integer: the number",
  "count.unknown": "statement count null, missing or other: not analysed",
  "applied.true": "applied true: applied",
  "applied.false": "applied false: not applied",
  "applied.unknown": "applied null, missing or other: unknown whether it applies",
  "order.value": "order integer: order N",
  "order.none": "order null: no order",
  "order.unknown": "order missing or another JSON type: order unknown",
  "found.sets": "summary found sets: the home",
  "found.none": "summary found none: No migrations found",
  "found.unknown": "summary found unknown or other: Schema not known (never none found)",
  "table_list.complete": "table_list complete: Table list complete",
  "table_list.may_be_incomplete": "table_list may_be_incomplete: may be incomplete + one sentence per reason",
  "table_list.not_modeled": "table_list not_modeled: Tables not extracted + reasons",
  "table_list.unknown": "table_list null or other: unknown badge with the raw value",
  "tl_reason.opaque_statements": "table_list_reasons opaque_statements",
  "tl_reason.member_unreadable": "table_list_reasons member_unreadable",
  "tl_reason.unsupported_config": "table_list_reasons unsupported_config",
  "tl_reason.unsupported_statements": "table_list_reasons unsupported_statements (Revision 1)",
  "tl_reason.statement_counts_unknown": "table_list_reasons statement_counts_unknown (Revision 1)",
  "tl_reason.model_unread": "table_list_reasons model_unread (T018f)",
  "tl_reason.model_core_tables": "table_list_reasons model_core_tables (T018f)",
  "tl_reason.model_counts_unknown": "table_list_reasons model_counts_unknown (T018f)",
  "tl_reason.model_not_claimed": "table_list_reasons model_not_claimed (T018f)",
  "tl_reason.python_unread": "table_list_reasons python_unread (T018f Revision 2, B1)",
  "tl_reason.python_scan_gap": "table_list_reasons python_scan_gap (T018f Revision 2, B1)",
  "tl_reason.unknown": "table_list_reasons other: raw reason, may be incomplete",
  "nm_reason.dialect_unsupported": "not_modeled_reasons dialect_unsupported",
  "nm_reason.order_failed": "not_modeled_reasons order_failed",
  "nm_reason.not_analysed": "not_modeled_reasons not_analysed",
  "nm_reason.unknown": "not_modeled_reasons other: raw reason",
} as const;

export type RuleId = keyof typeof CELL_RULES;

function cell(rule: RuleId, text: string, unknown: boolean, extra: Partial<Pick<Cell, "title" | "second" | "tail">> = {}): Cell {
  return { rule, text, unknown, ...extra };
}

type Attrs = Record<string, unknown>;

/** typeCell: the type as written plus the stored serial resolution and
 * collation (D6: no normalisation). */
export function typeCell(attrs: Attrs): Cell {
  const t = attrString(attrs, "type");
  if (t === null) return cell("type.unknown", "unknown", true, { title: `type: ${rawText(attrs.type)}` });
  const resolved = attrString(attrs, "type_resolved");
  const collation = attrString(attrs, "collation");
  return cell("type.written", resolved === null ? t : `${t} → ${resolved}`, false, collation === null ? {} : { second: `collate ${collation}` });
}

/** FAMILY_MODIFIERS are the modifiers a type family may list unknown
 * (ADR 0065; time precision is precision). */
const FAMILY_MODIFIERS = ["length", "precision", "scale"] as const;

/** GENERIC_MODIFIERS is the kit's generic unknown modifier (ADR 0062
 * Revision 2): an extension type whose own modifiers the source drops
 * (PostGIS geometry, pgvector vector), listed alone; it has no attribute. */
const GENERIC_MODIFIERS = "modifiers";

/** typeFamily reads a type_family attribute: {name, unknown} with a
 * non-empty name and a non-empty list of distinct modifiers; null for
 * null, a missing attribute or any other shape. */
export function typeFamily(v: unknown): { name: string; unknown: string[] } | null {
  if (v === null || typeof v !== "object" || Array.isArray(v)) return null;
  const o = v as Record<string, unknown>;
  const u = o.unknown;
  if (typeof o.name !== "string" || o.name === "" || !Array.isArray(u) || u.length === 0) return null;
  const mods = [...FAMILY_MODIFIERS, GENERIC_MODIFIERS] as readonly unknown[];
  if (!u.every((m) => mods.includes(m)) || new Set(u).size !== u.length) return null;
  if (u.includes(GENERIC_MODIFIERS) && u.length > 1) return null;
  return { name: o.name, unknown: u as string[] };
}

/** modelTypeCell: a model column's mapped type (T018f): the stored type
 * with the size attributes it has, and the annotation as the second line.
 * A type family (ADR 0065) shows its name with every unknown modifier and
 * the modifiers that are known; a family beside a type, or of another
 * shape, is unknown. */
export function modelTypeCell(attrs: Attrs): Cell {
  const t = attrString(attrs, "type");
  const ann = attrString(attrs, "annotation");
  const extra = ann === null ? {} : { second: ann };
  const hasFamily = attrs.type_family !== null && attrs.type_family !== undefined;
  if (hasFamily) {
    const fam = typeFamily(attrs.type_family);
    if (fam === null || t !== null) return cell("type.unknown", "unknown", true, { title: t !== null ? `type ${t} beside a type family (not known which holds)` : `type_family: ${rawText(attrs.type_family)}`, ...extra });
    const known = FAMILY_MODIFIERS.filter((m) => !fam.unknown.includes(m))
      .map((m) => [m, attrInt(attrs, m)] as const)
      .filter((x): x is readonly [(typeof FAMILY_MODIFIERS)[number], number] => x[1] !== null)
      .map(([m, n]) => `${m} ${String(n)}`);
    const tail = `${fam.unknown.join(", ")} unknown`;
    const inner = [...known, tail].join("; ");
    return cell("type.family", `${fam.name} (${inner})`, false, {
      tail: `(${inner})`,
      title: `A ${fam.name} type whose ${fam.unknown.join(" and ")} the source does not state; not a plain ${fam.name}`,
      ...extra,
    });
  }
  if (t === null) return cell("type.unknown", "unknown", true, { title: `type: ${rawText(attrs.type)}`, ...extra });
  const len = attrInt(attrs, "length");
  const prec = attrInt(attrs, "precision");
  const scale = attrInt(attrs, "scale");
  let args = "";
  if (len !== null) args = `(${String(len)})`;
  else if (prec !== null) args = scale !== null ? `(${String(prec)}, ${String(scale)})` : `(${String(prec)})`;
  else if (attrs.timezone === true) args = "(timezone=True)";
  return cell("type.mapped", `${t}${args}`, false, extra);
}

/** ToolTypeCell is the tool-side type cell: its label ("Diesel type") and
 * value ("Nullable<Int4>") apart, so the view styles them differently
 * without cutting the text. */
export interface ToolTypeCell extends Cell {
  label: string;
  value: string;
}

/** toolTypeCell: the ORM's own type as written (ADR 0065), labelled as
 * the tool's; never claimed or verified, never a database fact. */
export function toolTypeCell(attrs: Attrs, tool: string | null): ToolTypeCell {
  const v = attrString(attrs, "tool_type");
  const label = toolTypeLabel(tool);
  if (v === null) return { ...cell("tool_type.none", "", false), label, value: "" };
  return { ...cell("tool_type.written", `${label}: ${v}`, false, { title: "The ORM's own type as written in the source; not a database fact (not claimed, not verified)" }), label, value: v };
}

/** toolTypeLabel names an ORM's own type ("Diesel type"). */
export function toolTypeLabel(tool: string | null): string {
  const names: Record<string, string> = { diesel: "Diesel", gorm: "GORM", typeorm: "TypeORM", sqlmodel: "SQLModel", sqlalchemy: "SQLAlchemy", prisma: "Prisma", drizzle: "Drizzle", sequelize: "Sequelize", supabase_types: "TypeScript" };
  if (tool === null) return "ORM type";
  return `${names[tool] ?? tool} type`;
}

/** autoCell renders an autoincrement fact (ADR 0065): true an AI chip;
 * false nothing, but only where a complete table proves it; anything else
 * a dashed AI? chip. */
export function autoCell(v: unknown, completeness: string): Cell {
  if (v === true) return cell("auto.true", "AI", false, { title: "Autoincrement: the database generates the value" });
  if (v === false && completeness === "complete") return cell("auto.false", "", false);
  return cell("auto.unknown", "AI?", true, { title: v === false ? `autoincrement false on a ${completeness} table (not proven)` : `autoincrement: ${rawText(v)} (not known)` });
}

/** dialectDefaultCell renders one dialect's default (ADR 0065) by the
 * server's model default rule (modelDefaultState in
 * internal/server/schemamodels.go) applied to that dialect's
 * server_default and the column's flat ORM-side default (T018g0b Revision
 * 1, A2): true the stored text (or "server default"); false with a
 * Python-side or client default that default, as the flat path shows it;
 * false with default "absent" "none", only on a complete table; anything
 * else (null, a missing entry, false beside an unknown default) unknown. */
export function dialectDefaultCell(entry: Attrs, attrs: Attrs, completeness: string): Cell {
  const v = entry.server_default;
  if (v === true) {
    const t = attrString(entry, "server_default_text");
    return cell("default.server", t ?? "server default", false, { title: t === null ? "A server default whose text is not stored" : "Server default" });
  }
  if (v !== false) return cell("default.unknown", "unknown", true, { title: `server_default: ${rawText(v)} (not known)` });
  const def = attrs.default;
  if (def === "scalar" || def === "callable") return defaultCell("python", attrs, completeness);
  if (def === "client") return defaultCell("client", attrs, completeness);
  if (def === "absent" && completeness === "complete") return cell("default.none", "none", false, { title: "No default" });
  if (def === "absent") return cell("default.unknown", "unknown", true, { title: `no server default, default absent on a ${completeness} table (not proven)` });
  return cell("default.unknown", "unknown", true, { title: `no server default; default: ${rawText(def)} (not known)` });
}

/** DialectLine is one line of a per-dialect value: the dialects it holds
 * for (one, or all when they agree) and each part's cell. */
export interface DialectLine {
  dialects: string[];
  parts: Cell[];
}

/** dialectLines groups per-dialect values (ADR 0065): one line "on a, b,
 * c" when every dialect's parts are known and equal, else one line per
 * dialect in the group's order. */
export function dialectLines(dialects: readonly string[], parts: (dialect: string) => Cell[]): { rule: RuleId; lines: DialectLine[] } {
  const each = dialects.map((d) => ({ dialects: [d], parts: parts(d) }));
  const key = (l: DialectLine) => JSON.stringify(l.parts.map((p) => [p.rule, p.text]));
  const first = each[0];
  const agree = first !== undefined && each.length > 1 && each.every((l) => !l.parts.some((p) => p.unknown) && key(l) === key(first));
  if (agree) return { rule: "dialects.agree", lines: [{ dialects: [...dialects], parts: first.parts }] };
  return { rule: "dialects.each", lines: each };
}

/** byDialect reads a column's by_dialect entry for one dialect; an
 * entry that is missing or not an object reads as an empty one (every
 * fact unknown). */
export function byDialect(attrs: Attrs, dialect: string): Attrs {
  const b = attrs.by_dialect;
  if (b === null || typeof b !== "object" || Array.isArray(b)) return {};
  const e = (b as Record<string, unknown>)[dialect];
  return e !== null && typeof e === "object" && !Array.isArray(e) ? (e as Attrs) : {};
}

/** dialectFacts names the per-dialect facts a column states: the keys of
 * its by_dialect entries (ADR 0065: every entry lists the same facts). */
export function dialectFacts(attrs: Attrs): Set<string> {
  const out = new Set<string>();
  const b = attrs.by_dialect;
  if (b === null || typeof b !== "object" || Array.isArray(b)) return out;
  for (const e of Object.values(b as Record<string, unknown>)) {
    if (e !== null && typeof e === "object" && !Array.isArray(e)) for (const k of Object.keys(e)) out.add(k);
  }
  return out;
}

/** nullCell: no per-cell marker (D5); null and anything else is unknown. */
export function nullCell(attrs: Attrs): Cell {
  const v = attrBool3(attrs, "nullable");
  if (v === false) return cell("nullable.false", "not null", false);
  if (v === true) return cell("nullable.true", "nullable", false);
  return cell("nullable.unknown", "unknown", true, { title: `nullable: ${rawText(attrs.nullable)} (not known)` });
}

/** defaultCell renders a column's derived default_state. `none` is plain
 * only in a complete table and `none ◑` in a complete_in_source one; in
 * any other table it would contradict the API rule, so it reads unknown. */
export function defaultCell(defaultState: string, attrs: Attrs, completeness: string): Cell {
  switch (defaultState) {
    case "written": {
      const d = attrString(attrs, "default");
      return d === null ? cell("default.unknown", "unknown", true, { title: `default: ${rawText(attrs.default)}` }) : cell("default.written", d, false);
    }
    case "implied": {
      const d = attrString(attrs, "default_implied");
      return d === null
        ? cell("default.unknown", "unknown", true, { title: `default_implied: ${rawText(attrs.default_implied)}` })
        : cell("default.implied", `${d} · implied`, false, { title: "Implied by the type (no DEFAULT written)" });
    }
    case "none":
      if (completeness === "complete") return cell("default.none", "none", false);
      if (completeness === "complete_in_source")
        return cell("default.none_in_source", "none ◑", false, { title: "No default as far as the migrations show (opaque statements run after this table is created)" });
      return cell("default.unknown", "unknown", true, { title: `default_state none on a ${completeness} table` });
    case "python": {
      // A Python-side default (T018f): the database has none.
      const kind = attrString(attrs, "default");
      const v = attrs.default_value;
      const title = "Python-side default (SQLModel/SQLAlchemy sets it on insert); the database has no default";
      if (kind === "callable") return cell("default.python", typeof v === "string" ? v : "callable", false, { title });
      if (kind === "scalar") {
        if (!Object.hasOwn(attrs, "default_value")) return cell("default.unknown", "unknown", true, { title: "default_value: missing" });
        return cell("default.python", typeof v === "string" ? JSON.stringify(v) : rawText(v), false, { title });
      }
      return cell("default.unknown", "unknown", true, { title: `default: ${rawText(attrs.default)}` });
    }
    case "client": {
      // A Prisma client default (T018g1): the client generates the value
      // on insert; the database has no default.
      const fn = attrString(attrs, "default_value");
      const title = `Set by the Prisma client on insert${fn === null ? "" : ` (${fn})`}; the database has no default`;
      return cell("default.client", fn === null ? "set by Prisma (client)" : `${fn} · set by Prisma (client)`, false, { title });
    }
    case "server": {
      const t = attrString(attrs, "server_default_text");
      return cell("default.server", t ?? "server default", false, { title: t === null ? "A server default whose text is not stored" : "Server default" });
    }
    default:
      return cell("default.unknown", "unknown", true, { title: defaultState === "unknown" ? "Default not known" : `default_state: ${defaultState}` });
  }
}

/** keyChip renders a column's primary_key or unique fact: true a chip,
 * false nothing (text ""), null or other a dashed "not known" chip. */
export function keyChip(attrs: Attrs, key: "primary_key" | "unique" | "index"): Cell {
  const label = key === "primary_key" ? "PK" : key === "unique" ? "UQ" : "IX";
  const v = attrBool3(attrs, key);
  if (v === true) return cell("key.true", label, false, { title: key === "primary_key" ? "Primary key" : key === "unique" ? "Unique" : "Indexed" });
  if (v === false) return cell("key.false", "", false);
  return cell("key.unknown", `${label}?`, true, { title: `${key}: ${rawText(attrs[key])} (not known)` });
}

/** RefEntry is one reference entry of a column: an object with whether it
 * links to a stored column, or a value of another JSON type (unreadable). */
export type RefEntry = { ok: true; ref: Record<string, unknown>; linked: boolean } | { ok: false; raw: unknown };

/** referenceEntries lists a column's reference entries by the record's
 * source, as the API's columnReferences does (T018f Revision 2, A1): a
 * model column's `foreign_keys` elements (linked only for status
 * "resolved"; "unresolved" and "ambiguous" are unresolved), or a migration
 * column's `references` plus each element of `additional_references`
 * (linked only for status "linked"). A value of another JSON type counts as
 * one unreadable entry, as the API counts it. A model column's null or
 * missing `foreign_keys` has no entries: whether it has a foreign key is
 * unknown (foreignKeyCell). */
export function referenceEntries(attrs: Attrs): RefEntry[] {
  const out: RefEntry[] = [];
  const entry = (x: unknown, linkedStatus: string): RefEntry =>
    typeof x === "object" && x !== null && !Array.isArray(x) ? { ok: true, ref: x as Record<string, unknown>, linked: (x as Record<string, unknown>).status === linkedStatus } : { ok: false, raw: x };
  if (attrs.source === "model") {
    const fks = attrs.foreign_keys;
    if (fks === null || fks === undefined) return out;
    if (!Array.isArray(fks)) return [{ ok: false, raw: fks }];
    for (const x of fks as unknown[]) out.push(entry(x, "resolved"));
    return out;
  }
  const r = attrs.references;
  if (r !== null && r !== undefined) out.push(entry(r, "linked"));
  if (Object.hasOwn(attrs, "additional_references") && attrs.additional_references !== null) {
    const raw = attrs.additional_references;
    if (Array.isArray(raw)) for (const x of raw as unknown[]) out.push(entry(x, "linked"));
    else out.push({ ok: false, raw });
  }
  return out;
}

/** foreignKeyCell decides whether a column's References and Keys cells may
 * be empty (T018f Revision 2, C5): empty only when the column is proven to
 * have no foreign key. links is the number of stored foreign_key edges. */
export function foreignKeyCell(attrs: Attrs, completeness: string, links: number): Cell {
  if (links > 0 || referenceEntries(attrs).length > 0) return cell("fk.present", "", false);
  const unknown = (title: string) => cell("fk.unknown", "unknown", true, { title });
  if (attrs.source === "model") {
    const fks = attrs.foreign_keys;
    return Array.isArray(fks) ? cell("fk.none", "", false) : unknown(`foreign_keys: ${rawText(fks)} (not known)`);
  }
  if (!Object.hasOwn(attrs, "references")) return unknown("references: missing (not known)");
  if (completeness !== "complete" && completeness !== "complete_in_source") return unknown(`No reference is known, but the table is ${completeness}, so its last known facts cannot prove there is none`);
  return cell("fk.none", "", false);
}

/** primaryKeyCell renders the table primary_key attribute. */
export function primaryKeyCell(attrs: Attrs, completeness: string): Cell {
  const pk = attrStringList(attrs, "primary_key");
  if (pk.kind === "list" && pk.items.length > 0) {
    const last = completeness === "complete" || completeness === "complete_in_source" ? {} : { title: "last known from source" };
    return cell("pk.list", `(${pk.items.join(", ")})`, false, last);
  }
  if (pk.kind === "list") {
    if (completeness === "complete") return cell("pk.none", "none", false);
    if (completeness === "complete_in_source") return cell("pk.none_in_source", "none ◑", false, { title: "No primary key as far as the migrations show" });
    return cell("pk.unknown", "unknown", true, { title: "last known from source: none; the table is not complete" });
  }
  return cell("pk.unknown", "unknown", true, { title: `primary_key: ${rawText(attrs.primary_key)} (not known)` });
}

/** uniqueKeysCell renders the table unique_keys attribute. An empty list
 * is an absence, shown only when the table's completeness proves it. */
export function uniqueKeysCell(attrs: Attrs, completeness: string): Cell {
  const uk = attrObjects(attrs, "unique_keys");
  if (uk === null || uk === "invalid") return cell("uk.unknown", "unique keys unknown", true, { title: `unique_keys: ${rawText(attrs.unique_keys)}` });
  if (uk.length === 0) {
    if (completeness === "complete" || completeness === "complete_in_source") return cell("uk.empty", "", false);
    return cell("uk.unknown", "unique keys unknown", true, { title: "last known from source: none; the table is not complete" });
  }
  const text = uk
    .map((k) => {
      const cols = Array.isArray(k.columns) && k.columns.every((c) => typeof c === "string") ? `(${k.columns.join(", ")})` : "(columns unknown)";
      return typeof k.name === "string" ? `${k.name} ${cols}` : cols;
    })
    .join(", ");
  return cell("uk.list", text, false);
}

/** schemaCell renders the table schema attribute. */
export function schemaCell(attrs: Attrs): Cell {
  if (!Object.hasOwn(attrs, "schema")) return cell("schema.unknown", "unknown", true, { title: "schema: missing" });
  const s = attrs.schema;
  if (typeof s === "string") return cell("schema.name", s, false);
  if (s === null) return cell("schema.unqualified", "unqualified (search_path)", false, { title: "Resolved through search_path when the migrations run; not 'no schema' and not public" });
  return cell("schema.unknown", "unknown", true, { title: `schema: ${rawText(s)}` });
}

/** The completeness badge: text, short word for quiet places and glyph. */
export interface Completeness extends Cell {
  /** "complete" | "in_source" | "partial" | "unknown": the badge style. */
  tone: "complete" | "in_source" | "partial" | "unknown";
  short: string;
}

/** completenessBadge renders a derived completeness. opaqueAfter and
 * reasons come from the table's attributes (only shown, never used to
 * derive the state). */
export function completenessBadge(completeness: string, attrs: Attrs): Completeness {
  switch (completeness) {
    case "complete":
      return { ...cell("completeness.complete", "Complete", false), tone: "complete", short: "complete" };
    case "complete_in_source": {
      const n = attrInt(attrs, "opaque_after");
      const tail = n === null ? "" : ` · ${String(n)} opaque after`;
      return {
        ...cell("completeness.in_source", `Complete as far as the migrations show${tail}`, false, { title: "Opaque statements run after this table is created; their effects are not visible in source" }),
        tone: "in_source",
        short: "complete as far as the migrations show",
      };
    }
    case "partial": {
      const r = attrs.reasons;
      const n = Array.isArray(r) ? r.length : null;
      const tail = n === null ? " · reasons unknown" : ` · ${String(n)} ${n === 1 ? "reason" : "reasons"}`;
      return { ...cell("completeness.partial", `Partial${tail}`, false), tone: "partial", short: "partial" };
    }
    default:
      return {
        ...cell("completeness.unknown", "Unknown", true, {
          title: `completeness ${completeness}; state ${rawText(attrs.state)}, opaque_after ${rawText(attrs.opaque_after)}`,
        }),
        tone: "unknown",
        short: "unknown",
      };
  }
}

/** absenceCell states that a table has none of something (no foreign key,
 * no referencing table, no column, no ALTER): plainly only when its
 * completeness is exactly `complete`, with ◑ for `complete_in_source`,
 * and "none known (last known)" for every other state (Review fixes R1).
 * unresolved names this table's unresolved reference texts: when there
 * are any, an outgoing absence is never stated, they are named instead. */
export function absenceCell(completeness: string, text: string, unresolved: readonly string[] = []): Cell {
  if (unresolved.length > 0) {
    return cell("absence.unresolved", `no linked table; unresolved: ${unresolved.join(", ")}`, false, {
      title: "References that did not resolve to a stored table; they are never drawn as links",
    });
  }
  if (completeness === "complete") return cell("absence.proven", text, false);
  if (completeness === "complete_in_source") return cell("absence.in_source", `${text} ◑`, false, { title: "As far as the migrations show (opaque statements run after this table is created)" });
  return cell("absence.unknown", "none known (last known)", true, { title: `The table is ${completeness}, so its last known facts cannot prove an absence` });
}

/** IncomingSet is the part of a /sets group an incoming absence depends on
 * (all of it is in the one /sets response; no request per interaction). */
export type IncomingSet = Pick<SchemaGroup, "table_list"> & {
  tables: readonly { node: { identity: string }; completeness: string; unresolved_references: number }[];
};

/** incomingAbsenceCell states that no table references this one ("no
 * table references X"). That is a claim about the whole set, not only
 * about X (Review fixes R2): an unresolved reference anywhere in the set
 * may point at X, a table the list does not show may reference it, and a
 * table that is not complete may hold a reference its last known facts
 * miss. So it is plain only when X is exactly `complete` (no opaque
 * statement runs after X exists), the set's `table_list` is `complete`,
 * every table of the set is `complete` or `complete_in_source` and no
 * table has unresolved references; any count that is not a non-negative
 * integer counts as unknown. Otherwise it reads "none known" with every
 * reason. */
export function incomingAbsenceCell(identity: string, completeness: string, text: string, set: IncomingSet): Cell {
  const reasons: string[] = [];
  if (completeness === "complete_in_source") reasons.push("opaque statements run after this table is created");
  else if (completeness !== "complete") reasons.push("last known");
  if (set.table_list !== "complete") reasons.push(set.table_list === "not_modeled" ? "tables not extracted" : "table list may be incomplete");
  let unresolved = 0;
  let unresolvedUnknown = false;
  let notComplete = 0;
  for (const t of set.tables) {
    const n = t.unresolved_references;
    if (typeof n === "number" && Number.isInteger(n) && n >= 0) unresolved += n;
    else unresolvedUnknown = true;
    if (t.node.identity !== identity && t.completeness !== "complete" && t.completeness !== "complete_in_source") notComplete++;
  }
  if (unresolvedUnknown) reasons.push("unresolved references in this set not known");
  else if (unresolved > 0) reasons.push(`${String(unresolved)} unresolved ${unresolved === 1 ? "reference" : "references"} in this set`);
  if (notComplete > 0) reasons.push(`${String(notComplete)} other ${notComplete === 1 ? "table" : "tables"} in this set not complete`);
  if (reasons.length === 0) return cell("absence.incoming.proven", text, false);
  return cell("absence.incoming.unknown", `none known (${reasons.join("; ")})`, true, {
    title: "Whether any table references this one is a claim about the whole set; the set does not prove it",
  });
}

/** countCell renders a migration's statement count: null is "not
 * analysed", never 0. */
export function countCell(attrs: Attrs, key: string): Cell {
  const n = attrInt(attrs, key);
  if (n === null) return cell("count.unknown", "not analysed", true, { title: `${key}: ${rawText(attrs[key])}` });
  return cell("count.value", String(n), false);
}

/** appliedCell renders a migration's applied attribute. */
export function appliedCell(attrs: Attrs): Cell {
  const v = attrBool3(attrs, "applied");
  if (v === true) return cell("applied.true", "applied", false);
  if (v === false) return cell("applied.false", "not applied", false);
  return cell("applied.unknown", "unknown whether it applies", true, { title: `applied: ${rawText(attrs.applied)}` });
}

/** orderCell renders a migration's order attribute. */
export function orderCell(attrs: Attrs): Cell {
  const v = attrs.order;
  if (typeof v === "number" && Number.isInteger(v)) return cell("order.value", `order ${String(v)}`, false);
  if (v === null) return cell("order.none", "no order", false);
  return cell("order.unknown", "order unknown", true, { title: `order: ${rawText(v)}` });
}

/** foundRule names the page a summary `found` leads to. */
export function foundRule(found: string): RuleId {
  if (found === "sets") return "found.sets";
  if (found === "none") return "found.none";
  return "found.unknown";
}

/** tableListRule names the table-list line of a group. */
export function tableListRule(tableList: string | null): RuleId {
  if (tableList === "complete") return "table_list.complete";
  if (tableList === "may_be_incomplete") return "table_list.may_be_incomplete";
  if (tableList === "not_modeled") return "table_list.not_modeled";
  return "table_list.unknown";
}

/** tableListReason writes the sentence for one table_list_reasons entry.
 * opaqueTotal is the group's verbatim opaque_total; tool is the model
 * group's tool, so a Diesel group's reasons speak of its schema file and
 * table! blocks, not of classes (ADR 0062 Revision 2). */
export function tableListReason(reason: string, opaqueTotal: unknown, tool: string | null = null): Cell {
  if (tool === "diesel") {
    switch (reason) {
      case "model_unread":
        return cell("tl_reason.model_unread", "Some definitions in the schema file were not read, or the file is not proven to be print-schema's whole output (see Model files).", false);
      case "model_counts_unknown":
        return cell("tl_reason.model_counts_unknown", "A schema file's count of unread definitions is not readable.", false);
      case "model_not_claimed":
        return cell("tl_reason.model_not_claimed", "Some table! blocks do not compile and were not claimed; they may still name tables.", false);
    }
  }
  switch (reason) {
    case "opaque_statements": {
      const known = typeof opaqueTotal === "number" && Number.isInteger(opaqueTotal);
      const text = !known ? "Opaque statements" : opaqueTotal === 1 ? "1 opaque statement" : `${String(opaqueTotal)} opaque statements`;
      return cell("tl_reason.opaque_statements", `${text} can create or change objects the migrations do not show.`, false);
    }
    case "member_unreadable":
      return cell("tl_reason.member_unreadable", "A file or directory of the set could not be read.", false);
    case "unsupported_config":
      return cell("tl_reason.unsupported_config", "The package's sqlx.toml configures something Graphvein does not read.", false);
    case "unsupported_statements":
      return cell(
        "tl_reason.unsupported_statements",
        "Some statements are not modeled (for example SELECT … INTO, CREATE TABLE … AS, CREATE SCHEMA … CREATE TABLE); they may create tables the list does not show.",
        false,
      );
    case "statement_counts_unknown":
      return cell("tl_reason.statement_counts_unknown", "A migration's statement counts are not known, so unmodeled statements cannot be ruled out.", false);
    case "model_unread":
      return cell("tl_reason.model_unread", "Some classes that may be tables were not read (see Model files).", false);
    case "model_core_tables":
      return cell("tl_reason.model_core_tables", "Some files define SQLAlchemy Core tables, which are not read.", false);
    case "model_counts_unknown":
      return cell("tl_reason.model_counts_unknown", "A file's count of unread classes or core tables is not readable.", false);
    case "model_not_claimed":
      return cell("tl_reason.model_not_claimed", "Some classes could not be claimed or imported; a class with no table claim may still be a table.", false);
    case "python_unread":
      return cell("tl_reason.python_unread", "Some .py files were not read (too large, not parsed or not readable); they may define model tables.", false);
    case "python_scan_gap":
      return cell("tl_reason.python_scan_gap", "The scan skipped a directory, link or path, or could not read an ignore file; .py files there may define model tables.", false);
    default:
      return cell("tl_reason.unknown", `Reason "${reason}" (not known to this UI).`, true);
  }
}

/** notModeledReason writes the sentence for one not_modeled_reasons entry. */
export function notModeledReason(reason: string): Cell {
  switch (reason) {
    case "dialect_unsupported":
      return cell("nm_reason.dialect_unsupported", "The set's dialect is not one Graphvein models (it models Postgres), or its migrations disagree on it.", false);
    case "order_failed":
      return cell("nm_reason.order_failed", "The migrations could not be put in order (for example a duplicate or unreadable version).", false);
    case "not_analysed":
      return cell("nm_reason.not_analysed", "The set's statements were not analysed (its opaque-statement record is missing or differs between migrations).", false);
    default:
      return cell("nm_reason.unknown", `Reason "${reason}" (not known to this UI).`, true);
  }
}
