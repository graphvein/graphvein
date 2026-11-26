import { describe, expect, it } from "vitest";

import { attrBool3, attrInt, attrString, attrStringList, migrationPath, parseMigrationDetail, parseSchemaSets, parseSchemaSummary, parseTableDetail, tablePath } from "@/api/schema";
import { MIGRATION_GOLDENS, SETS_GOLDENS, SUMMARY_GOLDENS, TABLE_GOLDENS, raw } from "@/test/schemaFixtures";

describe("schema guards", () => {
  it.each(Object.entries(SUMMARY_GOLDENS))("accepts the summary golden %s", (_, g) => {
    const s = parseSchemaSummary(raw(g));
    expect(s.found).toBe(g.found);
    expect(s.sets_without_records).toBe(g.sets_without_records);
    expect(s.scan_gaps).toBe(g.scan_gaps);
  });

  it.each(Object.entries(SETS_GOLDENS))("accepts the sets golden %s", (_, g) => {
    const s = parseSchemaSets(raw(g));
    expect(s.sets.length).toBe(g.sets.length);
    expect(s.sets_without_records.length).toBe(g.sets_without_records.length);
    expect(s.unreadable_manifests.map((m) => m.code)).toEqual(g.unreadable_manifests.map((m) => m.code));
  });

  it.each(Object.entries(TABLE_GOLDENS))("accepts the table golden %s", (_, g) => {
    expect(parseTableDetail(raw(g)).columns.length).toBe(g.columns.length);
  });

  it.each(Object.entries(MIGRATION_GOLDENS))("accepts the migration golden %s", (_, g) => {
    expect(parseMigrationDetail(raw(g)).diagnostics_total).toBe(g.diagnostics_total);
  });

  it("rejects malformed shapes", () => {
    const s = raw(SUMMARY_GOLDENS.fixture) as Record<string, unknown>;
    s.sets = "19";
    expect(() => parseSchemaSummary(s)).toThrow(/sets/);
    const sets = raw(SETS_GOLDENS.states) as unknown as Record<string, unknown>;
    delete sets.scan_gaps;
    expect(() => parseSchemaSets(sets)).toThrow(/scan_gaps/);
    const t = raw(TABLE_GOLDENS["schema-states-table"]) as unknown as Record<string, unknown>;
    t.columns = {};
    expect(() => parseTableDetail(t)).toThrow(/columns/);
    const m = raw(MIGRATION_GOLDENS["schema-migration-down"]) as unknown as Record<string, unknown>;
    m.diagnostics_total = -1;
    expect(() => parseMigrationDetail(m)).toThrow(/diagnostics_total/);
  });

  it("keeps unknown derived values raw", () => {
    const s = raw(SUMMARY_GOLDENS.fixture) as Record<string, unknown>;
    s.found = "maybe";
    s.unknown_reason = "new_reason";
    expect(parseSchemaSummary(s)).toMatchObject({ found: "maybe", unknown_reason: "new_reason" });
    const sets = raw(SETS_GOLDENS.states);
    const g0 = sets.sets[0];
    if (g0 === undefined || g0.tables[0] === undefined) throw new Error("golden changed");
    g0.tables[0].completeness = "complete_ish";
    g0.table_list = "partly";
    const p = parseSchemaSets(sets);
    expect(p.sets[0]?.tables[0]?.completeness).toBe("complete_ish");
    expect(p.sets[0]?.table_list).toBe("partly");
  });

  it("reads an older summary without the Revision 1 counts as unknown, never 0", () => {
    const s = raw(SUMMARY_GOLDENS.fixture) as Record<string, unknown>;
    delete s.sets_without_records;
    delete s.unreadable_manifests;
    delete s.scan_gaps;
    expect(parseSchemaSummary(s)).toMatchObject({ sets_without_records: null, unreadable_manifests: null, scan_gaps: null });
  });
});

describe("attribute readers", () => {
  it("never default a missing or mistyped attribute", () => {
    const a = { s: "x", n: 3, f: 1.5, t: true, z: null, l: ["a"], bad: [1] };
    expect(attrString(a, "s")).toBe("x");
    expect(attrString(a, "n")).toBeNull();
    expect(attrInt(a, "n")).toBe(3);
    expect(attrInt(a, "f")).toBeNull();
    expect(attrInt(a, "missing")).toBeNull();
    expect(attrBool3(a, "t")).toBe(true);
    expect(attrBool3(a, "z")).toBeNull();
    expect(attrBool3(a, "missing")).toBe("invalid");
    expect(attrBool3(a, "s")).toBe("invalid");
    expect(attrStringList(a, "l")).toEqual({ kind: "list", items: ["a"] });
    expect(attrStringList(a, "z")).toEqual({ kind: "null" });
    expect(attrStringList(a, "bad").kind).toBe("invalid");
    expect(attrStringList(a, "missing").kind).toBe("invalid");
  });
});

describe("schema request paths", () => {
  it("add editor to the table and migration paths only when one is chosen (ADR 0050)", () => {
    expect(tablePath({ id: "table:a.sql#t" })).toBe("/api/v0/schema/table?id=table%3Aa.sql%23t");
    expect(tablePath({ id: "table:a.sql#t", editor: "zed" })).toBe("/api/v0/schema/table?id=table%3Aa.sql%23t&editor=zed");
    expect(migrationPath({ id: "migration:1.sql#migration" })).toBe("/api/v0/schema/migration?id=migration%3A1.sql%23migration");
    expect(migrationPath({ id: "migration:1.sql#migration", editor: "vscode-insiders" })).toBe("/api/v0/schema/migration?id=migration%3A1.sql%23migration&editor=vscode-insiders");
  });
});
