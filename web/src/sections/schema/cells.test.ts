// Every row of the section 5 rule table (with Revision 1), and the check
// that rows and branches are 1:1: every CELL_RULES row is produced by at
// least one case, and no case produces a rule outside the table.
import { describe, expect, it } from "vitest";

import {
  CELL_RULES,
  absenceCell,
  appliedCell,
  autoCell,
  dialectDefaultCell,
  dialectLines,
  toolTypeCell,
  toolTypeLabel,
  typeFamily,
  completenessBadge,
  countCell,
  defaultCell,
  foreignKeyCell,
  foundRule,
  incomingAbsenceCell,
  keyChip,
  modelTypeCell,
  notModeledReason,
  nullCell,
  orderCell,
  primaryKeyCell,
  referenceEntries,
  schemaCell,
  tableListReason,
  tableListRule,
  typeCell,
  uniqueKeysCell,
  type RuleId,
} from "@/sections/schema/cells";
import { spanText } from "@/sections/schema/TableDetail";

/** inSet builds the part of a /sets group an incoming absence reads. */
function inSet(tableList: string | null, tables: [string, string, number][]) {
  return { table_list: tableList, tables: tables.map(([identity, completeness, unresolved_references]) => ({ node: { identity }, completeness, unresolved_references })) };
}

type Case = [string, () => { rule: RuleId; text?: string; unknown?: boolean } | RuleId, RuleId, string | null];

const CASES: Case[] = [
  ["type string", () => typeCell({ type: "serial", type_resolved: "integer", collation: "C" }), "type.written", "serial → integer"],
  ["type missing", () => typeCell({}), "type.unknown", "unknown"],
  ["type number", () => typeCell({ type: 4 }), "type.unknown", "unknown"],
  ["nullable false", () => nullCell({ nullable: false }), "nullable.false", "not null"],
  ["nullable true", () => nullCell({ nullable: true }), "nullable.true", "nullable"],
  ["nullable null", () => nullCell({ nullable: null }), "nullable.unknown", "unknown"],
  ["nullable missing", () => nullCell({}), "nullable.unknown", "unknown"],
  ["nullable string", () => nullCell({ nullable: "no" }), "nullable.unknown", "unknown"],
  ["default written", () => defaultCell("written", { default: "now()" }, "complete"), "default.written", "now()"],
  ["default written, not a string", () => defaultCell("written", { default: 5 }, "complete"), "default.unknown", "unknown"],
  ["default implied", () => defaultCell("implied", { default: null, default_implied: "sequence" }, "complete"), "default.implied", "sequence · implied"],
  ["default implied, missing text", () => defaultCell("implied", { default: null }, "complete"), "default.unknown", "unknown"],
  ["default none, complete", () => defaultCell("none", { default: null }, "complete"), "default.none", "none"],
  ["default none, in source", () => defaultCell("none", { default: null }, "complete_in_source"), "default.none_in_source", "none ◑"],
  ["default none, partial", () => defaultCell("none", { default: null }, "partial"), "default.unknown", "unknown"],
  ["default unknown", () => defaultCell("unknown", {}, "partial"), "default.unknown", "unknown"],
  ["default other", () => defaultCell("maybe", {}, "complete"), "default.unknown", "unknown"],
  ["model type", () => modelTypeCell({ type: "AutoString", length: 255, annotation: "str" }), "type.mapped", "AutoString(255)"],
  ["model type, timezone", () => modelTypeCell({ type: "DateTime", length: null, timezone: true }), "type.mapped", "DateTime(timezone=True)"],
  ["model type, numeric", () => modelTypeCell({ type: "Numeric", precision: 10, scale: 2 }), "type.mapped", "Numeric(10, 2)"],
  ["model type, length none", () => modelTypeCell({ type: "AutoString", length: "none" }), "type.mapped", "AutoString"],
  ["model type null", () => modelTypeCell({ type: null, annotation: "int" }), "type.unknown", "unknown"],
  // ADR 0065 (T018g0b K2): type families, tool-side types, autoincrement,
  // per-dialect server defaults and per-dialect lines.
  ["type family", () => modelTypeCell({ type: null, type_family: { name: "numeric", unknown: ["precision", "scale"] }, precision: null, scale: null }), "type.family", "numeric (precision, scale unknown)"],
  ["type family, time precision", () => modelTypeCell({ type: null, type_family: { name: "timestamp with time zone", unknown: ["precision"] }, timezone: true }), "type.family", "timestamp with time zone (precision unknown)"],
  ["type family, a known modifier", () => modelTypeCell({ type: null, type_family: { name: "numeric", unknown: ["scale"] }, precision: 10 }), "type.family", "numeric (precision 10; scale unknown)"],
  ["type family beside a type", () => modelTypeCell({ type: "numeric", type_family: { name: "numeric", unknown: ["precision"] } }), "type.unknown", "unknown"],
  ["type family, empty unknown", () => modelTypeCell({ type: null, type_family: { name: "numeric", unknown: [] } }), "type.unknown", "unknown"],
  ["type family, other modifier", () => modelTypeCell({ type: null, type_family: { name: "numeric", unknown: ["width"] } }), "type.unknown", "unknown"],
  ["type family, not an object", () => modelTypeCell({ type: null, type_family: "numeric" }), "type.unknown", "unknown"],
  ["tool type", () => toolTypeCell({ tool_type: "Nullable<Int4>" }, "diesel"), "tool_type.written", "Diesel type: Nullable<Int4>"],
  ["tool type, unknown tool", () => toolTypeCell({ tool_type: "uint" }, "synthetic"), "tool_type.written", "synthetic type: uint"],
  ["tool type missing", () => toolTypeCell({}, "gorm"), "tool_type.none", ""],
  ["tool type not a string", () => toolTypeCell({ tool_type: 4 }, "gorm"), "tool_type.none", ""],
  ["autoincrement true", () => autoCell(true, "partial"), "auto.true", "AI"],
  ["autoincrement false, complete", () => autoCell(false, "complete"), "auto.false", ""],
  ["autoincrement false, partial", () => autoCell(false, "partial"), "auto.unknown", "AI?"],
  ["autoincrement null", () => autoCell(null, "complete"), "auto.unknown", "AI?"],
  ["dialect default text", () => dialectDefaultCell({ server_default: true, server_default_text: "'0'" }, { default: "absent" }, "complete"), "default.server", "'0'"],
  ["dialect default, text not stored", () => dialectDefaultCell({ server_default: true }, { default: null }, "complete"), "default.server", "server default"],
  ["dialect default false, absent, complete", () => dialectDefaultCell({ server_default: false, server_default_text: null }, { default: "absent" }, "complete"), "default.none", "none"],
  ["dialect default false, absent, partial", () => dialectDefaultCell({ server_default: false }, { default: "absent" }, "partial"), "default.unknown", "unknown"],
  ["dialect default false, python scalar", () => dialectDefaultCell({ server_default: false }, { default: "scalar", default_value: 0 }, "complete"), "default.python", "0"],
  ["dialect default false, python callable", () => dialectDefaultCell({ server_default: false }, { default: "callable", default_value: "uuid4" }, "partial"), "default.python", "uuid4"],
  ["dialect default false, client", () => dialectDefaultCell({ server_default: false }, { default: "client", default_value: "cuid()" }, "complete"), "default.client", "cuid() · set by Prisma (client)"],
  ["dialect default false, flat default null", () => dialectDefaultCell({ server_default: false }, { default: null }, "complete"), "default.unknown", "unknown"],
  ["dialect default false, flat default missing", () => dialectDefaultCell({ server_default: false }, {}, "complete"), "default.unknown", "unknown"],
  ["dialect default null", () => dialectDefaultCell({ server_default: null }, { default: "absent" }, "complete"), "default.unknown", "unknown"],
  ["dialect default, no entry", () => dialectDefaultCell({}, { default: "scalar", default_value: 1 }, "complete"), "default.unknown", "unknown"],
  ["dialects agree", () => dialectLines(["mysql", "sqlite"], () => [autoCell(true, "complete")]), "dialects.agree", null],
  ["dialects differ", () => dialectLines(["mysql", "sqlite"], (d) => [modelTypeCell({ type: d === "mysql" ? "int" : "integer" })]), "dialects.each", null],
  ["dialects agree but unknown", () => dialectLines(["mysql", "sqlite"], () => [modelTypeCell({ type: null })]), "dialects.each", null],
  ["model default scalar", () => defaultCell("python", { default: "scalar", default_value: "new" }, "complete"), "default.python", '"new"'],
  ["model default scalar bool", () => defaultCell("python", { default: "scalar", default_value: true }, "complete"), "default.python", "true"],
  ["model default scalar, value missing", () => defaultCell("python", { default: "scalar" }, "complete"), "default.unknown", "unknown"],
  ["model default callable", () => defaultCell("python", { default: "callable" }, "complete"), "default.python", "callable"],
  ["model default python, other kind", () => defaultCell("python", { default: null }, "complete"), "default.unknown", "unknown"],
  ["model default client (Prisma)", () => defaultCell("client", { default: "client", default_value: "uuid()", server_default: false }, "partial"), "default.client", "uuid() · set by Prisma (client)"],
  ["model default client, no function", () => defaultCell("client", { default: "client", server_default: false }, "complete"), "default.client", "set by Prisma (client)"],
  ["model server default", () => defaultCell("server", { server_default: true, server_default_text: "now()" }, "complete"), "default.server", "now()"],
  ["model server default, text not stored", () => defaultCell("server", { server_default: true }, "partial"), "default.server", "server default"],
  ["index true", () => keyChip({ index: true }, "index"), "key.true", "IX"],
  ["index null", () => keyChip({ index: null }, "index"), "key.unknown", "IX?"],
  ["pk true", () => keyChip({ primary_key: true }, "primary_key"), "key.true", "PK"],
  ["unique true", () => keyChip({ unique: true }, "unique"), "key.true", "UQ"],
  ["pk false", () => keyChip({ primary_key: false }, "primary_key"), "key.false", ""],
  ["pk null", () => keyChip({ primary_key: null }, "primary_key"), "key.unknown", "PK?"],
  ["unique missing", () => keyChip({}, "unique"), "key.unknown", "UQ?"],
  ["table pk list", () => primaryKeyCell({ primary_key: ["a", "b"] }, "complete"), "pk.list", "(a, b)"],
  ["table pk [] complete", () => primaryKeyCell({ primary_key: [] }, "complete"), "pk.none", "none"],
  ["table pk [] in source", () => primaryKeyCell({ primary_key: [] }, "complete_in_source"), "pk.none_in_source", "none ◑"],
  ["table pk [] partial", () => primaryKeyCell({ primary_key: [] }, "partial"), "pk.unknown", "unknown"],
  ["table pk null", () => primaryKeyCell({ primary_key: null }, "complete"), "pk.unknown", "unknown"],
  ["table pk missing", () => primaryKeyCell({}, "complete"), "pk.unknown", "unknown"],
  ["unique keys list", () => uniqueKeysCell({ unique_keys: [{ columns: ["slug"], name: null }] }, "complete"), "uk.list", "(slug)"],
  ["unique keys [] complete", () => uniqueKeysCell({ unique_keys: [] }, "complete"), "uk.empty", ""],
  ["unique keys [] in source", () => uniqueKeysCell({ unique_keys: [] }, "complete_in_source"), "uk.empty", ""],
  ["unique keys [] partial", () => uniqueKeysCell({ unique_keys: [] }, "partial"), "uk.unknown", "unique keys unknown"],
  ["unique keys null", () => uniqueKeysCell({ unique_keys: null }, "complete"), "uk.unknown", "unique keys unknown"],
  ["unique keys other", () => uniqueKeysCell({ unique_keys: "x" }, "complete"), "uk.unknown", "unique keys unknown"],
  ["schema string", () => schemaCell({ schema: "app" }), "schema.name", "app"],
  ["schema null", () => schemaCell({ schema: null }), "schema.unqualified", "unqualified (search_path)"],
  ["schema missing", () => schemaCell({}), "schema.unknown", "unknown"],
  ["schema number", () => schemaCell({ schema: 1 }), "schema.unknown", "unknown"],
  ["complete", () => completenessBadge("complete", {}), "completeness.complete", "Complete"],
  ["in source", () => completenessBadge("complete_in_source", { opaque_after: 5 }), "completeness.in_source", "Complete as far as the migrations show · 5 opaque after"],
  ["partial", () => completenessBadge("partial", { reasons: [{}, {}] }), "completeness.partial", "Partial · 2 reasons"],
  ["unknown", () => completenessBadge("unknown", {}), "completeness.unknown", "Unknown"],
  ["completeness other", () => completenessBadge("complete!", {}), "completeness.unknown", "Unknown"],
  ["count int", () => countCell({ statements: 0 }, "statements"), "count.value", "0"],
  ["count null", () => countCell({ statements: null }, "statements"), "count.unknown", "not analysed"],
  ["count string", () => countCell({ opaque: "2" }, "opaque"), "count.unknown", "not analysed"],
  ["applied true", () => appliedCell({ applied: true }), "applied.true", "applied"],
  ["applied false", () => appliedCell({ applied: false }), "applied.false", "not applied"],
  ["applied null", () => appliedCell({ applied: null }), "applied.unknown", "unknown whether it applies"],
  ["applied missing", () => appliedCell({}), "applied.unknown", "unknown whether it applies"],
  ["order int", () => orderCell({ order: 4 }), "order.value", "order 4"],
  ["order null", () => orderCell({ order: null }), "order.none", "no order"],
  ["order string", () => orderCell({ order: "4" }), "order.unknown", "order unknown"],
  ["order missing", () => orderCell({}), "order.unknown", "order unknown"],
  ["absence complete", () => absenceCell("complete", "pointer references no table"), "absence.proven", "pointer references no table"],
  ["absence in source", () => absenceCell("complete_in_source", "no foreign keys"), "absence.in_source", "no foreign keys ◑"],
  ["absence partial", () => absenceCell("partial", "pointer references no table"), "absence.unknown", "none known (last known)"],
  ["absence unknown", () => absenceCell("unknown", "no foreign keys"), "absence.unknown", "none known (last known)"],
  ["absence other state", () => absenceCell("complete!", "no foreign keys"), "absence.unknown", "none known (last known)"],
  ["absence with unresolved", () => absenceCell("complete", "pointer references no table", ["app.target"]), "absence.unresolved", "no linked table; unresolved: app.target"],
  ["incoming proven", () => incomingAbsenceCell("t:a", "complete", "no table references a", inSet("complete", [["t:a", "complete", 0], ["t:b", "complete_in_source", 0]])), "absence.incoming.proven", "no table references a"],
  ["incoming in source", () => incomingAbsenceCell("t:a", "complete_in_source", "no table references a", inSet("complete", [["t:a", "complete_in_source", 0]])), "absence.incoming.unknown", "none known (opaque statements run after this table is created)"],
  ["incoming partial", () => incomingAbsenceCell("t:a", "partial", "no table references a", inSet("complete", [["t:a", "partial", 0]])), "absence.incoming.unknown", "none known (last known)"],
  ["incoming table list", () => incomingAbsenceCell("t:a", "complete", "no table references a", inSet("may_be_incomplete", [["t:a", "complete", 0]])), "absence.incoming.unknown", "none known (table list may be incomplete)"],
  ["incoming table list null", () => incomingAbsenceCell("t:a", "complete", "no table references a", inSet(null, [["t:a", "complete", 0]])), "absence.incoming.unknown", "none known (table list may be incomplete)"],
  ["incoming not modeled", () => incomingAbsenceCell("t:a", "complete", "no table references a", inSet("not_modeled", [["t:a", "complete", 0]])), "absence.incoming.unknown", "none known (tables not extracted)"],
  ["incoming unresolved elsewhere", () => incomingAbsenceCell("t:a", "complete", "no table references a", inSet("complete", [["t:a", "complete", 0], ["t:b", "complete", 2], ["t:c", "complete", 1]])), "absence.incoming.unknown", "none known (3 unresolved references in this set)"],
  ["incoming unresolved on itself", () => incomingAbsenceCell("t:a", "complete", "no table references a", inSet("complete", [["t:a", "complete", 1]])), "absence.incoming.unknown", "none known (1 unresolved reference in this set)"],
  ["incoming unresolved count unknown", () => incomingAbsenceCell("t:a", "complete", "no table references a", inSet("complete", [["t:a", "complete", 0], ["t:b", "complete", -1]])), "absence.incoming.unknown", "none known (unresolved references in this set not known)"],
  ["incoming other table partial", () => incomingAbsenceCell("t:a", "complete", "no table references a", inSet("complete", [["t:a", "complete", 0], ["t:b", "partial", 0], ["t:c", "unknown", 0]])), "absence.incoming.unknown", "none known (2 other tables in this set not complete)"],
  ["incoming every reason", () => incomingAbsenceCell("t:a", "partial", "no table references a", inSet("may_be_incomplete", [["t:a", "partial", 0], ["t:b", "complete", 4]])), "absence.incoming.unknown", "none known (last known; table list may be incomplete; 4 unresolved references in this set)"],
  ["found sets", () => foundRule("sets"), "found.sets", null],
  ["found none", () => foundRule("none"), "found.none", null],
  ["found unknown", () => foundRule("unknown"), "found.unknown", null],
  ["found other", () => foundRule("None"), "found.unknown", null],
  ["table_list complete", () => tableListRule("complete"), "table_list.complete", null],
  ["table_list may", () => tableListRule("may_be_incomplete"), "table_list.may_be_incomplete", null],
  ["table_list not_modeled", () => tableListRule("not_modeled"), "table_list.not_modeled", null],
  ["table_list null", () => tableListRule(null), "table_list.unknown", null],
  ["table_list other", () => tableListRule("complete "), "table_list.unknown", null],
  ["reason opaque", () => tableListReason("opaque_statements", 6), "tl_reason.opaque_statements", "6 opaque statements can create or change objects the migrations do not show."],
  ["reason opaque, total unknown", () => tableListReason("opaque_statements", null), "tl_reason.opaque_statements", "Opaque statements can create or change objects the migrations do not show."],
  ["reason member", () => tableListReason("member_unreadable", 0), "tl_reason.member_unreadable", null],
  ["reason config", () => tableListReason("unsupported_config", 0), "tl_reason.unsupported_config", null],
  ["reason unsupported", () => tableListReason("unsupported_statements", 0), "tl_reason.unsupported_statements", null],
  ["reason counts", () => tableListReason("statement_counts_unknown", 0), "tl_reason.statement_counts_unknown", null],
  ["reason model unread", () => tableListReason("model_unread", null), "tl_reason.model_unread", null],
  ["reason model core", () => tableListReason("model_core_tables", null), "tl_reason.model_core_tables", null],
  ["reason model counts", () => tableListReason("model_counts_unknown", null), "tl_reason.model_counts_unknown", null],
  ["reason model not claimed", () => tableListReason("model_not_claimed", null), "tl_reason.model_not_claimed", null],
  ["reason python unread", () => tableListReason("python_unread", null), "tl_reason.python_unread", null],
  ["reason python scan gap", () => tableListReason("python_scan_gap", null), "tl_reason.python_scan_gap", null],
  ["reason other", () => tableListReason("future", 0), "tl_reason.unknown", null],
  // T018f Revision 2 (A1, C5): an FK cell is empty only when no foreign
  // key is proven; an unresolved model reference is present, never none.
  ["fk linked edge", () => foreignKeyCell({ references: null }, "partial", 1), "fk.present", ""],
  ["fk migration unresolved", () => foreignKeyCell({ references: { table: "x", status: "unresolved" } }, "complete", 0), "fk.present", ""],
  ["fk model unresolved", () => foreignKeyCell({ source: "model", foreign_keys: [{ table: "legacy_users", column: "id", status: "unresolved" }] }, "complete", 0), "fk.present", ""],
  ["fk model malformed", () => foreignKeyCell({ source: "model", foreign_keys: "x" }, "complete", 0), "fk.present", ""],
  ["fk migration none, complete", () => foreignKeyCell({ references: null }, "complete", 0), "fk.none", ""],
  ["fk migration none, in source", () => foreignKeyCell({ references: null, additional_references: [] }, "complete_in_source", 0), "fk.none", ""],
  ["fk model none", () => foreignKeyCell({ source: "model", foreign_keys: [] }, "partial", 0), "fk.none", ""],
  ["fk migration null, partial", () => foreignKeyCell({ references: null }, "partial", 0), "fk.unknown", "unknown"],
  ["fk migration missing", () => foreignKeyCell({}, "complete", 0), "fk.unknown", "unknown"],
  ["fk model null", () => foreignKeyCell({ source: "model", foreign_keys: null }, "complete", 0), "fk.unknown", "unknown"],
  ["fk model missing", () => foreignKeyCell({ source: "model" }, "complete", 0), "fk.unknown", "unknown"],
  ["nm dialect", () => notModeledReason("dialect_unsupported"), "nm_reason.dialect_unsupported", null],
  ["nm order", () => notModeledReason("order_failed"), "nm_reason.order_failed", null],
  ["nm analysed", () => notModeledReason("not_analysed"), "nm_reason.not_analysed", null],
  ["nm other", () => notModeledReason("future"), "nm_reason.unknown", null],
];

describe("cell rules", () => {
  it.each(CASES)("%s", (_, run, rule, text) => {
    const out = run();
    const got = typeof out === "string" ? { rule: out } : out;
    expect(got.rule).toBe(rule);
    if (text !== null) expect(got.text).toBe(text);
    // An unknown rule always renders in the unknown style, and a known one
    // never does (rule 14).
    if (typeof out !== "string" && "unknown" in out) {
      const unknownRule = /\.unknown$/.test(rule) && rule !== "found.unknown";
      expect(out.unknown, rule).toBe(unknownRule);
    }
  });

  it("covers every rule row exactly through the cases (1:1)", () => {
    const produced = new Set(CASES.map(([, run]) => {
      const out = run();
      return typeof out === "string" ? out : out.rule;
    }));
    expect([...produced].sort()).toEqual(Object.keys(CELL_RULES).sort());
  });
});

describe("per-dialect helpers (ADR 0065)", () => {
  it("reads a type family only in its valid shape", () => {
    expect(typeFamily({ name: "numeric", unknown: ["precision", "scale"] })).toEqual({ name: "numeric", unknown: ["precision", "scale"] });
    expect(typeFamily({ name: "numeric", unknown: ["precision", "precision"] })).toBeNull();
    expect(typeFamily({ name: "", unknown: ["precision"] })).toBeNull();
    expect(typeFamily(null)).toBeNull();
  });
  it("styles the unknown part of a family apart, never shows the bare family", () => {
    const c = modelTypeCell({ type: null, type_family: { name: "numeric", unknown: ["precision"] } });
    expect(c.text).not.toBe("numeric");
    expect(c.tail).toBe("(precision unknown)");
  });
  it("one line on every dialect when they agree, else one per dialect in order", () => {
    const agree = dialectLines(["mysql", "postgres"], () => [modelTypeCell({ type: "text" })]);
    expect(agree.lines).toEqual([{ dialects: ["mysql", "postgres"], parts: [expect.objectContaining({ text: "text" })] }]);
    const each = dialectLines(["mysql", "postgres"], (d) => [modelTypeCell(d === "mysql" ? { type: "text" } : {})]);
    expect(each.lines.map((l) => [l.dialects, l.parts.map((p) => p.text)])).toEqual([[["mysql"], ["text"]], [["postgres"], ["unknown"]]]);
    // One dialect never collapses into an "on" line.
    expect(dialectLines(["mysql"], () => [modelTypeCell({ type: "text" })]).rule).toBe("dialects.each");
  });
  it("keeps the tool type's label and value apart", () => {
    expect(toolTypeCell({ tool_type: "Nullable<Int4>" }, "diesel")).toMatchObject({ label: "Diesel type", value: "Nullable<Int4>", text: "Diesel type: Nullable<Int4>" });
    // A value with its own colon is never cut.
    expect(toolTypeCell({ tool_type: "a: b" }, "gorm")).toMatchObject({ label: "GORM type", value: "a: b" });
  });
  it("labels the tool's own type by tool", () => {
    expect(toolTypeLabel("gorm")).toBe("GORM type");
    expect(toolTypeLabel("typeorm")).toBe("TypeORM type");
    expect(toolTypeLabel(null)).toBe("ORM type");
  });
});

describe("spanText (Review fixes R1)", () => {
  it("prints one line with its columns, and a span across lines with both ends", () => {
    expect(spanText({ start_line: 4, end_line: 4, start_column: 21, end_column: 55 })).toBe("line 4, columns 21-55");
    expect(spanText({ start_line: 2, end_line: 5, start_column: 0, end_column: 62 })).toBe("line 2 column 0 to line 5 column 62");
    expect(spanText({ start_line: 2, end_line: 5 })).toBe("lines 2-5");
    expect(spanText(null)).toBe("");
    expect(spanText({ start_line: "2" })).toBe("");
  });
});

describe("referenceEntries (T018f Revision 2, A1)", () => {
  it("reads a model column's foreign_keys, linked only when resolved", () => {
    const fk = (status: string) => ({ table: "t", column: "id", status });
    const got = referenceEntries({ source: "model", foreign_keys: [fk("resolved"), fk("unresolved"), fk("ambiguous"), fk("linked")], references: { status: "linked" } });
    expect(got.map((e) => (e.ok ? e.linked : "raw"))).toEqual([true, false, false, false]);
    expect(referenceEntries({ source: "model", foreign_keys: null })).toEqual([]);
    expect(referenceEntries({ source: "model", foreign_keys: 3 })).toEqual([{ ok: false, raw: 3 }]);
  });
  it("reads a migration column's references, linked only when linked", () => {
    const got = referenceEntries({ references: { status: "linked" }, additional_references: [{ status: "unresolved" }, 4], foreign_keys: [{ status: "resolved" }] });
    expect(got.map((e) => (e.ok ? e.linked : "raw"))).toEqual([true, false, "raw"]);
  });
});
