// T020 review: a Precise model group whose facts are not all checked
// against a database says so beside its chip on the source card: "dialect
// unknown" when the group states no dialect (a Prisma provider that is
// not a literal), "mapped: unverified" when it states one. Only /sets
// fields decide it (dialect, dialects, conflicts, the tables'
// verification and verification_counts).
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ScopeLabel } from "@/api/endpoints";
import type { SchemaGroup, SchemaTable } from "@/api/schema";
import { DialectNote, dialectHint } from "@/sections/schema/precision";

const PRECISE: ScopeLabel = { label: "precise", row: "PL-F3", reason: null };

function table(verification: string, counts: Record<string, number>): SchemaTable {
  return { node: { attributes: { source: "model", verification } }, verification_counts: counts } as unknown as SchemaTable;
}

function group(over: Partial<SchemaGroup>): SchemaGroup {
  return { source: "models", tool: "prisma", dialect: null, dialects: null, conflicts: [], tables: [], ...over } as unknown as SchemaGroup;
}

// From the prisma-models fixture as /schema/sets serves it: the dynamic
// schema's table is mapped with oracle column facts, the mysql one mixed.
const dynamic = group({ tables: [table("mapped", { oracle: 15 })] });
const mysql = group({ dialect: "mysql", tables: [table("mapped", { mapped: 37, oracle: 84 })] });
const postgres = group({ dialect: "postgresql", tables: [table("oracle", { oracle: 18 })] });

describe("dialect hint (C3)", () => {
  it("names an unknown dialect and a mapped one", () => {
    expect(dialectHint(dynamic, PRECISE)?.text).toBe("dialect unknown");
    expect(dialectHint(mysql, PRECISE)?.text).toBe("mapped: unverified");
    expect(dialectHint(mysql, PRECISE)?.title).toContain("prisma's mapping for mysql");
    const runtime = group({ tool: "gorm", dialects: ["mysql", "postgres"], tables: [table("oracle", { mapped: 2, oracle: 4 })] });
    expect(dialectHint(runtime, PRECISE)?.title).toContain("gorm's mapping for mysql or postgres");
    const conflict = group({ conflicts: ["dialect"], tables: [table("mapped", {})] });
    expect(dialectHint(conflict, PRECISE)?.text).toBe("mapped: unverified");
  });

  it("says nothing where every fact is oracle-verified, also without a dialect (SQLModel)", () => {
    expect(dialectHint(postgres, PRECISE)).toBeNull();
    expect(dialectHint(group({ tool: "sqlmodel", tables: [table("oracle", { oracle: 9 })] }), PRECISE)).toBeNull();
    expect(dialectHint(group({ tables: [] }), PRECISE)).toBeNull();
  });

  it("only beside a Precise model group's chip", () => {
    expect(dialectHint(dynamic, { label: "best_effort", row: "D", reason: "default_branch" })).toBeNull();
    expect(dialectHint(dynamic, { label: "best_effort", row: "D", reason: "external_indexer" })).toBeNull();
    expect(dialectHint(dynamic, null)).toBeNull();
    expect(dialectHint({ ...dynamic, source: "migrations" }, PRECISE)).toBeNull();
  });

  it("renders the hint with its title, or nothing", () => {
    const { rerender } = render(<DialectNote group={dynamic} label={PRECISE} />);
    const note = screen.getByTestId("card-dialect-hint");
    expect(note.textContent).toBe("dialect unknown");
    expect(note.getAttribute("title")).toContain("states no database dialect");
    rerender(<DialectNote group={postgres} label={PRECISE} />);
    expect(screen.queryByTestId("card-dialect-hint")).toBeNull();
  });
});
