// T018g0 (ADR 0058): a model fact whose claim is verification "mapped"
// (from the ORM's own per-dialect mapping, not checked against that
// database) shows an "unverified" chip on its table and its column, and the
// chip's popover names the mapping and the database that did not check it.
// No mapped fact exists yet (SQLModel's are all "oracle"), so the tests
// mark claims of a golden record mapped.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { SchemaClient, SchemaSets, TableDetail } from "@/api/schema";
import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, setsGolden, summaryGolden, tableGolden } from "@/test/schemaFixtures";
import { countLabel, mappedClaims, mappedLine } from "@/sections/schema/Verification";

/** countedSets is the models golden with Team's counts marking n column
 * facts mapped (crafted, extends schema-sets-models): /sets serves counts,
 * never model claims (T018g0 Revision 1). */
function countedSets(n: number): SchemaSets {
  const sets = setsGolden("models");
  for (const g of sets.sets) {
    for (const t of g.tables) {
      if (t.node.identity === TEAM) t.verification_counts = { ...(t.verification_counts ?? {}), mapped: n };
    }
  }
  return sets;
}

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
const enc = encodeURIComponent;
const TEAM = "table:app/models.py#Team";

function show(detail: TableDetail | null, sets: SchemaSets = setsGolden("models")): SchemaClient {
  const client = fakeSchemaClient({ summary: summaryGolden("models"), sets, tables: detail === null ? {} : { [TEAM]: detail }, pending: true });
  window.history.replaceState(null, "", `#/schema?set=${enc("@models/sqlmodel/.")}&sel=${enc(TEAM)}`);
  render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
  return client;
}

/** mapped marks the given facts of the first column mapped and states a
 * dialect on the table, as a Prisma mysql record would. */
function mapped(facts: string[], tableVerification = "oracle"): { detail: TableDetail; column: string } {
  const d = tableGolden("schema-table-model");
  const first = d.columns[0];
  if (first === undefined) throw new Error("no column");
  const columns = d.columns.map((c, i) => (i === 0 ? { ...c, claims: (c.claims ?? []).map((cl) => (facts.includes(cl.fact) ? { ...cl, verification: "mapped" } : cl)) } : c));
  const table = { ...d.table, attributes: { ...d.table.attributes, dialect: "mysql", verification: tableVerification } };
  return { detail: { ...d, table, columns }, column: first.node.name };
}

function row(name: string): HTMLElement {
  const r = screen.getAllByTestId("column-row").find((x) => x.getAttribute("data-column") === name);
  if (r === undefined) throw new Error(`no row ${name}`);
  return r;
}

beforeEach(() => {
  window.history.replaceState(null, "", "#/schema");
});

describe("verification", () => {
  it("SQLModel's golden claims are all oracle-verified: no chip", async () => {
    const d = tableGolden("schema-table-model");
    expect(d.columns.every((c) => c.claims !== null && c.claims.length > 0 && c.claims.every((cl) => cl.verification === "oracle"))).toBe(true);
    expect(d.table.attributes.verification).toBe("oracle");
    show(d);
    await screen.findByTestId("table-detail");
    await waitFor(() => {
      expect(screen.getAllByTestId("column-row").length).toBeGreaterThan(0);
    });
    expect(screen.queryByTestId("table-unverified-chip")).toBeNull();
    expect(screen.queryByTestId("unverified-chip")).toBeNull();
  });

  it("a mapped fact gives the table and its column an unverified chip with the popover line", async () => {
    const { detail, column } = mapped(["type", "default"]);
    show(detail);
    const chip = await screen.findByTestId("table-unverified-chip");
    expect(chip.textContent).toBe("unverified");
    await waitFor(() => {
      expect(within(row(column)).getByTestId("unverified-chip")).toBeTruthy();
    });
    // Only the column with mapped claims carries the chip.
    expect(screen.getAllByTestId("unverified-chip")).toHaveLength(1);
    fireEvent.click(within(row(column)).getByTestId("unverified-chip"));
    const pop = await screen.findByTestId("why-popover");
    expect(pop.getAttribute("data-kind")).toBe("verification");
    expect(within(pop).getByTestId("mapped-line").textContent).toBe("From sqlmodel's mysql mapping; not checked against a mysql database.");
    expect(within(pop).getByTestId("why-lead").textContent).toContain("Facts: type, default.");
    // The source line is the claim's file and line, linked.
    expect(within(pop).getAllByTestId("why-source-link")[0]?.textContent).toMatch(/\.py:\d+/);
  });

  it("a mapped table fact alone marks the table, not the columns", async () => {
    const { detail } = mapped([], "mapped");
    show(detail);
    const chip = await screen.findByTestId("table-unverified-chip");
    expect(screen.queryByTestId("unverified-chip")).toBeNull();
    fireEvent.click(chip);
    const pop = await screen.findByTestId("why-popover");
    expect(within(pop).getByTestId("why-lead").textContent).toContain("table facts");
  });

  it("/sets serves model columns without claims, with counts per table", () => {
    const sets = setsGolden("models");
    const models = sets.sets.flatMap((g) => g.tables).filter((t) => t.node.attributes.source === "model");
    expect(models.length).toBeGreaterThan(0);
    for (const t of models) {
      expect(t.verification_counts).not.toBeNull();
      expect(t.verification_counts?.mapped).toBeUndefined();
      for (const c of t.columns) expect(c.node.attributes.claims).toBeUndefined();
    }
  });

  it("SQLModel's counts are all oracle: no list mark", async () => {
    show(null);
    await screen.findByTestId("table-detail");
    expect(screen.queryByTestId("row-unverified-mark")).toBeNull();
    expect(screen.queryByTestId("table-unverified-chip")).toBeNull();
  });

  it("mapped counts mark the list row, and the header before /table loads", async () => {
    show(null, countedSets(3));
    const rows = await screen.findAllByTestId("row-unverified-mark");
    expect(rows).toHaveLength(1);
    expect(rows[0]?.getAttribute("title")).toBe("From sqlmodel's own mapping; not checked against a database. Unverified: 3 column facts. Open the table for the facts and their sources.");
    const chip = await screen.findByTestId("table-unverified-chip");
    // No claims yet: the columns carry no chip until /table answers.
    expect(screen.queryByTestId("unverified-chip")).toBeNull();
    fireEvent.click(chip);
    const pop = await screen.findByTestId("why-popover");
    expect(within(pop).getByTestId("why-lead").textContent).toContain(`Facts: ${countLabel(3)}.`);
  });

  it("a missing or unknown verification never looks verified (Revision 2)", async () => {
    const d = tableGolden("schema-table-model");
    const first = d.columns[0];
    if (first === undefined) throw new Error("no column");
    const columns = d.columns.map((c, i) => (i === 0 ? { ...c, claims: (c.claims ?? []).map((cl) => (cl.fact === "type" ? { ...cl, verification: null } : cl)) } : c));
    const attributes: Record<string, unknown> = { ...d.table.attributes };
    delete attributes.verification;
    show({ ...d, table: { ...d.table, attributes }, columns });
    const chip = await screen.findByTestId("table-unverified-chip");
    await waitFor(() => {
      expect(within(row(first.node.name)).getByTestId("unverified-chip")).toBeTruthy();
    });
    expect(screen.getAllByTestId("unverified-chip")).toHaveLength(1);
    fireEvent.click(chip);
    const pop = await screen.findByTestId("why-popover");
    expect(within(pop).getByTestId("why-lead").textContent).toContain("table facts");
    expect(within(pop).getByTestId("unrecorded-line")).toBeTruthy();
  });

  it("an unknown count marks the list row (Revision 2)", async () => {
    const sets = setsGolden("models");
    for (const g of sets.sets) for (const t of g.tables) if (t.node.identity === TEAM) t.verification_counts = { ...(t.verification_counts ?? {}), unknown: 2 };
    show(null, sets);
    const rows = await screen.findAllByTestId("row-unverified-mark");
    expect(rows).toHaveLength(1);
    expect(rows[0]?.getAttribute("title")).toContain("not recorded");
    expect(rows[0]?.getAttribute("title")).toContain("Unverified: 2 column facts.");
  });

  it("mappedClaims and mappedLine", () => {
    const claim = (verification: string | null) => ({ fact: "type", row: "PR-F1", migration: null, file: "prisma/schema.prisma", span: null, link: null, migration_ref: null, verification, dialect: null });
    // Revision 2: anything but oracle is unverified, a missing value too.
    expect(mappedClaims([claim("oracle"), claim("mapped"), claim(null), claim("")])).toHaveLength(3);
    expect(mappedClaims(null)).toEqual([]);
    expect(mappedLine("prisma", "sqlserver")).toBe("From prisma's sqlserver mapping; not checked against a sqlserver database.");
    expect(mappedLine(null, null)).toBe("From the ORM's own mapping; not checked against a database.");
    expect(countLabel(1)).toBe("1 column fact");
    expect(countLabel(4)).toBe("4 column facts");
  });
});
