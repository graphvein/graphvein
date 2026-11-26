import { describe, expect, it } from "vitest";

import { DEFAULT_SCHEMA_PARAMS, groupKeyOf, isSchemaGraphView, legacyEntry, parseSchemaParams, rowEntry, schemaHash } from "@/sections/schema/params";

describe("schema hash state", () => {
  it("leaves defaults out", () => {
    expect(schemaHash(DEFAULT_SCHEMA_PARAMS)).toBe("#/schema");
    expect(parseSchemaParams("#/schema")).toEqual(DEFAULT_SCHEMA_PARAMS);
  });

  it("round-trips every field", () => {
    const p = { tab: "migrations" as const, set: "a/migrations", sel: "table:a/1.sql#t", mig: "migration:a/1.sql#migration", tests: true, a: "E2" };
    expect(parseSchemaParams(schemaHash(p))).toEqual(p);
  });

  it("deep-links an Assumptions tab entry (T018f): a=, and as= as its synonym", () => {
    expect(parseSchemaParams("#/schema?tab=assumptions&a=E2")).toEqual({ ...DEFAULT_SCHEMA_PARAMS, tab: "assumptions", a: "E2" });
    expect(parseSchemaParams("#/schema?tab=assumptions&as=V7").a).toBe("V7");
    const row = rowEntry("models/typeorm", "TO-K1");
    expect(parseSchemaParams(schemaHash({ ...DEFAULT_SCHEMA_PARAMS, tab: "assumptions", a: row })).a).toBe("row:models/typeorm:TO-K1");
  });

  it("reads an old row:models:<id> deep link as SQLModel's row (ADR 0055 amendment)", () => {
    expect(parseSchemaParams("#/schema?tab=assumptions&a=row:models:K1b").a).toBe("row:models/sqlmodel:K1b");
    expect(parseSchemaParams("#/schema?tab=assumptions&as=row:models:M1").a).toBe("row:models/sqlmodel:M1");
    expect(legacyEntry("row:migrations:M1")).toBe("row:migrations:M1");
    expect(legacyEntry("row:models/prisma:D")).toBe("row:models/prisma:D");
    expect(legacyEntry(null)).toBeNull();
  });

  it("keys a model group apart from a migration set of the same text", () => {
    expect(groupKeyOf({ set: "models", source: "models" })).not.toBe(groupKeyOf({ set: "models", source: "migrations" }));
    expect(groupKeyOf({ set: null, source: null })).toBe("~");
  });

  it("ignores unknown values", () => {
    expect(parseSchemaParams("#/schema?tab=timeline&tests=yes&x=1")).toEqual(DEFAULT_SCHEMA_PARAMS);
  });

  it("the Overview tab is a graph view", () => {
    expect(isSchemaGraphView("#/schema?tab=overview")).toBe(true);
    expect(isSchemaGraphView("#/schema")).toBe(false);
  });
});
