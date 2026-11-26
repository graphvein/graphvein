// The Schema section on the T018b1 goldens (fixture, states) and the
// crafted variants of schemaFixtures.ts, rendered through the shell.
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { SchemaClient, SchemaSets } from "@/api/schema";
import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { EDITOR_KEY } from "@/lib/editorChoice";
import { fakeClient } from "@/test/apiFixtures";
import { ideFetcher } from "@/test/ideFixtures";
import { goldenStatus } from "@/test/fixtures";
import columnGridSrc from "@/sections/schema/ColumnGrid.tsx?raw";
import emptySrc from "@/sections/schema/EmptyStates.tsx?raw";
import mapSrc from "@/sections/schema/map.ts?raw";
import migrationsSrc from "@/sections/schema/MigrationsTab.tsx?raw";
import miniMapSrc from "@/sections/schema/MiniMap.tsx?raw";
import overviewSrc from "@/sections/schema/OverviewTab.tsx?raw";
import partsSrc from "@/sections/schema/parts.tsx?raw";
import sectionSrc from "@/sections/schema/SchemaSection.tsx?raw";
import stripSrc from "@/sections/schema/SetStrip.tsx?raw";
import detailSrc from "@/sections/schema/TableDetail.tsx?raw";
import listSrc from "@/sections/schema/TableList.tsx?raw";
import { effectsText, footerText } from "@/sections/schema/MigrationsTab";
import { names as buildNames } from "@/sections/schema/names";
import { TableDetailView } from "@/sections/schema/TableDetail";
import {
  craftedAllTestOnly,
  craftedBigSet,
  craftedFailedOnly,
  craftedSummary,
  fakeSchemaClient,
  migrationGolden,
  setsGolden,
  summaryGolden,
  tableGolden,
} from "@/test/schemaFixtures";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));

function show(hash: string, client: SchemaClient, fetcher: StatusFetcher = fresh) {
  window.history.replaceState(null, "", hash);
  return render(<AppShell fetcher={fetcher} client={fakeClient()} schemaClient={client} />);
}

const enc = encodeURIComponent;

beforeEach(() => {
  window.history.replaceState(null, "", "#/schema");
});

describe("Schema section pages", () => {
  it("found unknown shows 'Schema not known', never 'No migrations found', and does not request /sets", async () => {
    const calls: string[] = [];
    show("#/schema", fakeSchemaClient({ summary: craftedSummary({ found: "unknown", unknown_reason: "version_ambiguous" }), calls }));
    const page = await screen.findByTestId("schema-unknown");
    expect(page.textContent).toContain("Schema not known for this index");
    expect(page.textContent).toContain('This is not "no migrations found"');
    expect(screen.queryByText("No migrations found")).toBeNull();
    expect(calls).not.toContain("sets");
    const nav = screen.getByRole("navigation", { name: "Sections" });
    expect(within(nav).getByRole("link", { name: "Schema, re-index" })).toBeTruthy();
  });

  it("scan_incomplete lists the scan gaps from /sets (Revision 1)", async () => {
    show("#/schema", fakeSchemaClient({ summary: craftedSummary({ found: "unknown", unknown_reason: "scan_incomplete", scan_gaps: 2 }), sets: craftedFailedOnly() }));
    const page = await screen.findByTestId("schema-unknown");
    await within(page).findByTestId("scan-gaps");
    expect(page.textContent).toContain("vendor-link");
    expect(page.textContent).toContain("(path not recorded)");
    expect(page.textContent).not.toContain("No migrations found");
  });

  it("found none shows the D1 page and the 'none found' tag", async () => {
    show("#/schema", fakeSchemaClient({ summary: craftedSummary({ found: "none" }) }));
    expect((await screen.findByTestId("schema-none")).textContent).toContain("This does not mean the project has no database");
    expect(screen.getByRole("link", { name: "Schema, none found" })).toBeTruthy();
  });

  it("not indexed shows the API section's text", async () => {
    show("#/schema", fakeSchemaClient({ summary: summaryGolden("not_indexed") }));
    expect((await screen.findByTestId("not-indexed")).textContent).toContain("has no index yet");
  });

  it("failed-only lists failed, record-less and unreadable entries (Q6 Y)", async () => {
    show("#/schema", fakeSchemaClient({ summary: summaryGolden("states"), sets: craftedFailedOnly() }));
    const page = await screen.findByTestId("schema-failed-only");
    expect(within(page).getAllByTestId("failed-set")).toHaveLength(1);
    expect(within(page).getAllByTestId("recordless-set")).toHaveLength(1);
    expect(within(page).getAllByTestId("unreadable-manifest")).toHaveLength(1);
    expect(page.textContent).toContain("g/Cargo.toml");
    expect(page.textContent).toContain("schema_manifest_unreadable");
  });

  it("all test-only: the page's button includes tests through the hash (Q5 Y)", async () => {
    show("#/schema", fakeSchemaClient({ summary: craftedSummary({ found: "sets", sets: 6, test_only_sets: 6 }), sets: craftedAllTestOnly(), pending: true }));
    const page = await screen.findByTestId("schema-tests-only");
    // The crafted response also holds a failed set, a set without records
    // and an unreadable manifest: their scope is unknown (A3).
    expect(page.textContent).toContain("All 6 stored sets are test-only, and 3 whose scope is unknown");
    expect(page.textContent).not.toContain("All 6 migration sets are test-only");
    expect(screen.getByTestId("hidden-tests").textContent).toBe("6 test-only sets hidden");
    expect(screen.getByRole("link", { name: "Schema, tests only" })).toBeTruthy();
    fireEvent.click(screen.getByTestId("include-tests-button"));
    expect(window.location.hash).toBe("#/schema?tests=1");
    expect(await screen.findByTestId("set-strip")).toBeTruthy();
  });
});

describe("Schema home", () => {
  it("strip numbers equal the lists; unreadable strips and the tag (fixture)", async () => {
    const sets = setsGolden("fixture");
    show(`#/schema?set=${enc("pg-app/migrations")}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets, pending: true }));
    const strip = await screen.findByTestId("set-strip");
    const g = sets.sets.find((x) => x.set === "pg-app/migrations");
    if (g === undefined) throw new Error("golden changed");
    expect(within(strip).getByTestId("count-tables").textContent).toBe(String(g.tables.length));
    expect(within(strip).getByTestId("count-columns").textContent).toBe(String(g.tables.reduce((n, t) => n + t.columns.length, 0)));
    expect(within(strip).getByTestId("count-pairs").textContent).toBe(String(g.foreign_keys.length));
    expect(within(strip).getByTestId("count-migrations").textContent).toBe(String(g.migrations.length));
    expect(within(strip).getByTestId("table-list").textContent).toContain("Table list may be incomplete");
    expect(strip.textContent).toContain("Some statements are not modeled");
    expect(screen.getAllByTestId("recordless-set")).toHaveLength(1);
    expect(screen.getAllByTestId("unreadable-manifest")).toHaveLength(1);
    expect(screen.getByRole("link", { name: "Schema, 2 unreadable" })).toBeTruthy();
  });

  it("quiet glyph only for the common state, full badges otherwise; inconsistency banners (states)", async () => {
    show("#/schema", fakeSchemaClient({ summary: summaryGolden("states"), sets: setsGolden("states"), pending: true }));
    await screen.findByTestId("set-strip");
    const rows = screen.getAllByTestId("table-row");
    const state = (name: string) => rows.find((r) => r.getAttribute("data-name") === name)?.querySelector("[data-completeness]")?.getAttribute("data-completeness");
    expect(state("t_linked")).toBe("complete");
    expect(rows.find((r) => r.getAttribute("data-name") === "t_linked")?.textContent).not.toContain("Complete");
    expect(state("t_partial_empty")).toBe("partial");
    expect(rows.find((r) => r.getAttribute("data-name") === "t_partial_empty")?.textContent).toContain("Partial · 3 reasons");
    expect(state("t_nostate")).toBe("unknown");
    expect(state("t_stropaque")).toBe("unknown");
    const banners = screen.getAllByTestId("inconsistency").map((b) => b.textContent);
    expect(banners.some((b) => b.includes("1 column has a reference marked linked but no stored foreign key"))).toBe(true);
    expect(banners.some((b) => b.includes("1 column belongs to no stored table") && b.includes("1 index belongs"))).toBe(true);
    // The set: null group is shown and headed.
    expect(screen.getAllByTestId("group-header").some((h) => h.textContent.includes("No migration set"))).toBe(true);
  });

  it("a partial table lists every reason inline, including set-level and unstored migrations", async () => {
    const id = "table:a/migrations/2_more.sql#t_partial_empty";
    show(`#/schema?sel=${enc(id)}`, fakeSchemaClient({ summary: summaryGolden("states"), sets: setsGolden("states"), tables: { [id]: tableGolden("schema-states-table") } }));
    const reasons = await screen.findByTestId("reasons");
    await waitFor(() => {
      expect(reasons.textContent).toContain("2_more.sql");
    });
    const items = within(reasons).getAllByRole("listitem").map((li) => li.textContent);
    expect(items).toHaveLength(3);
    expect(items[0]).toContain("2_more.sql");
    expect(items[1]).toContain("migration not stored");
    expect(items[2]).toContain("set-level");
    expect(screen.getByTestId("no-columns").textContent).toContain("No columns known");
    expect(screen.getByTestId("column-grid").textContent).toContain("last known from source");
  });

  it("unresolved references are never links; linked entries without an edge are not links", async () => {
    const id = "table:a/migrations/2_more.sql#t_multi";
    show(`#/schema?sel=${enc(id)}`, fakeSchemaClient({ summary: summaryGolden("states"), sets: setsGolden("states"), pending: true }));
    const grid = await screen.findByTestId("column-grid");
    const unresolved = within(grid).getByTestId("unresolved-ref");
    expect(unresolved.textContent).toContain("unresolved: elsewhere");
    expect(unresolved.querySelector("a, button")).toBeNull();
    expect(within(grid).getAllByTestId("fk-link").length).toBeGreaterThan(0);
  });

  it("none ◑ only in complete_in_source, null nullability and keys read unknown", async () => {
    const sets = setsGolden("fixture");
    const g = sets.sets.find((x) => x.set === "pg-app/migrations");
    const inSource = g?.tables.find((t) => t.completeness === "complete_in_source" && t.columns.some((c) => c.default_state === "none"));
    const partial = g?.tables.find((t) => t.completeness === "partial" && t.columns.some((c) => c.node.attributes.nullable === null));
    if (inSource === undefined || partial === undefined) throw new Error("golden changed");
    const client = fakeSchemaClient({ summary: summaryGolden("fixture"), sets, pending: true });
    const { unmount } = show(`#/schema?sel=${enc(inSource.node.identity)}`, client);
    let grid = await screen.findByTestId("column-grid");
    const defaults = within(grid).getAllByTestId("cell-default").map((c) => c.textContent);
    expect(defaults).toContain("none ◑");
    expect(defaults).not.toContain("none");
    expect(screen.getByTestId("half-key")).toBeTruthy();
    unmount();
    show(`#/schema?sel=${enc(partial.node.identity)}`, client);
    grid = await screen.findByTestId("column-grid");
    const nulls = within(grid).getAllByTestId("cell-null").map((c) => c.textContent);
    expect(nulls).toContain("unknown");
    expect(within(grid).getAllByTestId("cell-default").map((c) => c.textContent)).not.toContain("none");
    expect(within(grid).getAllByTestId("cell-keys").some((c) => c.textContent.includes("PK?"))).toBe(true);
    expect(screen.getByTestId("partial-key")).toBeTruthy();
  });

  it("each shortcut has a labelled control: filter (/) and Open in IDE (o)", async () => {
    const id = "table:pg-app/migrations/2_relations.sql#tasks";
    show(`#/schema?set=${enc("pg-app/migrations")}&sel=${enc(id)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [id]: tableGolden("schema-table-complete") } }));
    const filter = await screen.findByRole("searchbox", { name: "Filter tables and columns" });
    expect(filter.getAttribute("aria-keyshortcuts")).toBe("/");
    const ide = await screen.findByTestId("table-ide");
    await waitFor(() => {
      expect(screen.getByTestId("table-ide").tagName).toBe("A");
    });
    expect(ide.getAttribute("aria-keyshortcuts")).toBe("o");
    act(() => {
      fireEvent.keyDown(document.body, { key: "/" });
    });
    expect(document.activeElement).toBe(filter);
    fireEvent.change(filter, { target: { value: "owner" } });
    expect(screen.getAllByTestId("table-row").length).toBeGreaterThan(0);
  });

  it("neighbourhood mode above 25 tables (Q7 X)", async () => {
    show(`#/schema?sel=${enc("table:big/migrations/1.sql#t10")}`, fakeSchemaClient({ summary: craftedSummary({ found: "sets", sets: 1 }), sets: craftedBigSet(), pending: true }));
    expect((await screen.findByTestId("map-header")).textContent).toBe("Neighbourhood · 1 hop · 3 of 26 tables");
    expect(screen.getByTestId("map-outside").textContent).toContain("2 foreign-key pairs lead to tables outside this view.");
  });
});

describe("Migrations tab", () => {
  it("null counts read 'not analysed', a down migration is 'not applied'", async () => {
    const id = "migration:pg-app/migrations/10_reversible.down.sql#migration";
    show(`#/schema?tab=migrations&mig=${enc(id)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), migrations: { [id]: migrationGolden("schema-migration-down") } }));
    const counts = await screen.findByTestId("statement-counts");
    expect(within(counts).getAllByText("not analysed")).toHaveLength(6);
    expect(screen.getByTestId("migration-badge").textContent).toContain("not applied");
    expect(screen.getByTestId("migration-badge").textContent).toContain("no order");
    await waitFor(() => {
      expect(screen.getByTestId<HTMLButtonElement>("mig-previous").disabled).toBe(true);
    });
  });

  it("applied null reads 'unknown whether it applies'", async () => {
    const sets = setsGolden("fixture");
    const m = sets.sets.flatMap((g) => g.migrations).find((x) => x.attributes.applied === null);
    if (m === undefined) throw new Error("golden changed");
    show(`#/schema?tab=migrations&mig=${enc(m.id)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets, pending: true }));
    expect((await screen.findByTestId("migration-badge")).textContent).toContain("unknown whether it applies");
  });

  it("diagnostics beyond the cap say 'showing L of N' (crafted, extends schema-migration-applied)", async () => {
    const id = "migration:pg-app/migrations/6_opaque.sql#migration";
    const d = migrationGolden("schema-migration-applied");
    d.diagnostics_total = 700;
    show(`#/schema?tab=migrations&mig=${enc(id)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), migrations: { [id]: d } }));
    const table = await screen.findByTestId("diagnostics");
    expect(table.closest("section")?.textContent).toContain("showing 2 of 700");
    expect(screen.getByTestId("migration-footer").textContent).toBe("Applied in sqlx order (version ascending).");
  });
});

describe("requests", () => {
  it("requests /sets once per index run, not per tab, selection or toggle", async () => {
    const calls: string[] = [];
    show("#/schema", fakeSchemaClient({ summary: summaryGolden("states"), sets: setsGolden("states"), pending: true, calls }));
    await screen.findByTestId("set-strip");
    fireEvent.click(screen.getByTestId("tab-migrations"));
    fireEvent.click(screen.getByTestId("tab-tables"));
    fireEvent.click(screen.getByTestId("include-tests"));
    fireEvent.click(screen.getAllByTestId("table-row")[1] as HTMLElement);
    await screen.findByTestId("set-strip");
    expect(calls.filter((c) => c === "sets")).toHaveLength(1);
    expect(calls.filter((c) => c === "summary")).toHaveLength(1);
  });
});

describe("lazy loading", () => {
  it("the home imports React Flow only through the lazy Overview chunk", () => {
    const home: Record<string, string> = { sectionSrc, miniMapSrc, listSrc, detailSrc, columnGridSrc, migrationsSrc, stripSrc, emptySrc, mapSrc, partsSrc };
    for (const [f, src] of Object.entries(home)) {
      expect(src, f).not.toMatch(/from "@xyflow\/react"/);
    }
    const section = sectionSrc;
    expect(section).toMatch(/lazy\(\(\) => import\("@\/sections\/schema\/OverviewTab"\)\)/);
    expect(section).toMatch(/lazy\(\(\) => import\("@\/sections\/schema\/MiniMap"\)\)/);
    expect(section).not.toMatch(/from "@\/sections\/schema\/(MiniMap|OverviewTab)"/);
    // The map uses the page's one ELK worker (flow/elk.ts pageElk).
    expect(miniMapSrc).toMatch(/pageElk\(\)/);
    expect(overviewSrc).toMatch(/pageElk\(\)/);
    expect(miniMapSrc).not.toMatch(/new Worker/);
  });
});

describe("Review fixes R2: an incoming absence is a claim about the whole set", () => {
  const serialId = "table:pg-serial/migrations/1_serials.sql#counters";
  /** serialSet returns the fixture's sets with counters' group changed. */
  const serialSet = (change: (g: SchemaSets["sets"][number]) => void) => {
    const sets = setsGolden("fixture");
    const g = sets.sets.find((x) => x.tables.some((t) => t.node.identity === serialId));
    if (g === undefined) throw new Error("golden changed");
    change(g);
    return { sets, g };
  };

  it("a table list that may be incomplete turns a complete table's incoming absence into none known", async () => {
    const { sets } = serialSet((g) => { g.table_list = "may_be_incomplete"; });
    show(`#/schema?sel=${enc(serialId)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets, tables: { [serialId]: tableGolden("schema-table-serial") } }));
    expect((await screen.findByTestId("incoming-none")).textContent).toBe("none known (table list may be incomplete)");
    expect(screen.getByTestId("related-none").textContent).toBe("none known (table list may be incomplete)");
    // The outgoing absence keeps the R1 rule (the table itself is complete).
    expect(screen.getByTestId("outgoing-none").textContent).toBe("counters references no table");
  });

  it("an unresolved reference of another table in the set is named in the Overview panel's Related strip", () => {
    const { sets, g } = serialSet((x) => {
      const first = x.tables[0];
      if (first === undefined) throw new Error("golden changed");
      x.tables.push({ ...first, node: { ...first.node, identity: "table:pg-serial/migrations/1_serials.sql#other" }, unresolved_references: 2 });
    });
    const t = g.tables.find((x) => x.node.identity === serialId);
    if (t === undefined) throw new Error("golden changed");
    render(
      <TableDetailView table={t} group={g} detail={tableGolden("schema-table-serial")} names={buildNames(sets)} changed={new Set()} multiSet={false} onTable={vi.fn()} onMigration={vi.fn()} visible={() => true} onClose={vi.fn()} />,
    );
    expect(screen.getByTestId("related-none").textContent).toBe("none known (2 unresolved references in this set)");
    expect(screen.getByTestId("table-detail").textContent).not.toMatch(/no table references|no foreign keys/);
  });
});

describe("Review fixes R1: absences only where the API proves them", () => {
  // notes: still partial after T018a2 C3 (DROP CONSTRAINT IF EXISTS
  // forgets its keys); accounts became complete there.
  const partialId = "table:pg-drops/migrations/1_base.sql#notes";
  const serialId = "table:pg-serial/migrations/1_serials.sql#counters";

  it("A1: a partial table states no absence of foreign keys or referencing tables", async () => {
    show(`#/schema?sel=${enc(partialId)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [partialId]: tableGolden("schema-table-partial-notes") } }));
    expect((await screen.findByTestId("outgoing-none")).textContent).toBe("none known (last known)");
    // Review fixes R2: the incoming absence also names the set's reasons.
    const setReasons = "none known (last known; 1 unresolved reference in this set; 1 other table in this set not complete)";
    expect(screen.getByTestId("incoming-none").textContent).toBe(setReasons);
    expect(screen.getByTestId("related-none").textContent).toBe(setReasons);
    expect(screen.getByTestId("table-detail").textContent).not.toMatch(/references no table|no table references|no foreign keys/);
  });

  it("A1: a complete table states it plainly, complete_in_source with ◑, unresolved references are named", async () => {
    const complete = tableGolden("schema-table-serial");
    const { unmount } = show(`#/schema?sel=${enc(serialId)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [serialId]: complete } }));
    expect((await screen.findByTestId("outgoing-none")).textContent).toBe("counters references no table");
    expect(screen.getByTestId("incoming-none").textContent).toBe("no table references counters");
    expect(screen.getByTestId("related-none").textContent).toBe("no foreign keys");
    unmount();
    const inSource = { ...tableGolden("schema-table-serial"), completeness: "complete_in_source" };
    const second = show(`#/schema?sel=${enc(serialId)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [serialId]: inSource } }));
    expect((await screen.findByTestId("outgoing-none")).textContent).toBe("counters references no table ◑");
    // Review fixes R2: an incoming absence is never stated with ◑.
    expect(screen.getByTestId("incoming-none").textContent).toBe("none known (opaque statements run after this table is created)");
    second.unmount();
    const unresolved = { ...tableGolden("schema-table-serial"), unresolved_references: [{ column: "column:x", reference: { table: "app.target", status: "unresolved", reason: "no such table" } }] };
    show(`#/schema?sel=${enc(serialId)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [serialId]: unresolved } }));
    expect((await screen.findByTestId("outgoing-none")).textContent).toBe("no linked table; unresolved: app.target");
  });

  it("A1: the Overview panel of a partial table states no foreign-key absence", () => {
    const sets = setsGolden("fixture");
    const g = sets.sets.find((x) => x.set === "pg-drops/migrations");
    const t = g?.tables.find((x) => x.node.identity === partialId);
    if (g === undefined || t === undefined) throw new Error("golden changed");
    render(
      <TableDetailView table={t} group={g} detail={null} names={buildNames(sets)} changed={new Set()} multiSet={false} onTable={vi.fn()} onMigration={vi.fn()} visible={() => true} onClose={vi.fn()} />,
    );
    expect(screen.getByTestId("panel-fk-none").textContent).toBe("none known (last known)");
  });

  it("A2: an unanalysed migration's Creates reads not extracted, never 'No table' or 0", async () => {
    const id = "migration:sqlite-app/migrations/1_init.sql#migration";
    show(`#/schema?tab=migrations&mig=${enc(id)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), migrations: { [id]: migrationGolden("schema-migration-sqlite-set") } }));
    const creates = await screen.findByTestId("creates-none");
    expect(creates.textContent).toContain("not extracted");
    expect(creates.closest("section")?.textContent).not.toMatch(/Creates ?0|No table\./);
    expect(screen.getByTestId("alters-none").textContent).toContain("not extracted");
    // The row's opaque/unsupported badge reads unknown, not nothing.
    const row = screen.getAllByTestId("migration-row").find((r) => r.textContent.includes("1_init.sql"));
    expect(row?.querySelector("[data-testid=counts-unknown]")?.textContent).toBe("counts unknown");
  });

  it("A2: an analysed migration with opaque statements does not say 'No table'", () => {
    expect(effectsText("creates", 0, "extracted", { statements: 2, opaque: 1, unsupported: 0, errors: 0 })?.text).toBe("No modeled CREATE TABLE; 1 opaque, unsupported or failed statement may create tables.");
    expect(effectsText("creates", 0, "extracted", { statements: 2, opaque: 0, unsupported: 0, errors: 0 })?.text).toBe("No table.");
    expect(effectsText("creates", 0, "unknown", {})?.unknown).toBe(true);
    expect(effectsText("creates", 2, "extracted", {})).toBeNull();
  });

  it("A3: test-only page without unknown-scope entries keeps 'All N migration sets'", async () => {
    const sets = { ...craftedAllTestOnly(), failed_sets: [], sets_without_records: [], unreadable_manifests: [] };
    show("#/schema", fakeSchemaClient({ summary: craftedSummary({ found: "sets", sets: 6, test_only_sets: 6 }), sets, pending: true }));
    expect((await screen.findByTestId("schema-tests-only")).textContent).toContain("All 6 migration sets are test-only");
  });

  it("A4: a not_modeled group shows 'not extracted', never 0 tables or 'No tables in this group'", async () => {
    show(`#/schema?set=${enc("sqlite-app/migrations")}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), pending: true }));
    const strip = await screen.findByTestId("set-strip");
    for (const k of ["count-tables", "count-columns", "count-pairs"]) expect(within(strip).getByTestId(k).textContent).toBe("not extracted");
    expect(within(strip).getByTestId("count-migrations").textContent).toBe("1");
    expect(strip.textContent).not.toContain("No tables in this group");
    const header = screen.getAllByTestId("group-header").find((h) => h.textContent.includes("sqlite-app"));
    expect(header?.querySelector("[data-testid=group-header-count]")?.textContent).toBe("not extracted");
    expect(header?.textContent).not.toMatch(/\b0 tables/);
  });

  it("C: a partial table without columns reads 'columns unknown' in the list", async () => {
    show("#/schema", fakeSchemaClient({ summary: summaryGolden("states"), sets: setsGolden("states"), pending: true }));
    await screen.findByTestId("set-strip");
    const row = screen.getAllByTestId("table-row").find((r) => r.getAttribute("data-name") === "t_partial_empty");
    expect(row?.querySelector("[data-testid=row-columns]")?.textContent).toBe("columns unknown");
  });

  it("C: an absent migration_ref reads 'migration unknown' once /table answered, never a skeleton", async () => {
    // members: a partial table with several reasons, so the reasons
    // after the changed first one still show their spans.
    const membersId = "table:pg-app/migrations/1_base.sql#members";
    const d = tableGolden("schema-table-renamed-altered");
    const first = d.reasons[0];
    if (first === undefined || d.reasons.length < 2) throw new Error("golden changed");
    delete first.migration_ref;
    show(`#/schema?sel=${enc(membersId)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [membersId]: d } }));
    const unknown = await screen.findByTestId("migration-ref-unknown");
    expect(unknown.textContent).toBe("migration unknown");
    expect(within(screen.getByTestId("reasons")).queryByText("", { selector: "[data-slot=skeleton]" })).toBeNull();
    expect(screen.getAllByTestId("reason-span")[0]?.textContent).toBe("line 3, columns 0-31");
  });

  it("C: wrong-typed index name and using, and a missing column position, read unknown", async () => {
    const sets = setsGolden("fixture");
    const t = sets.sets.find((g) => g.set === "pg-app/migrations")?.tables.find((x) => x.indexes.length > 0);
    if (t === undefined) throw new Error("golden changed");
    const ix = t.indexes[0];
    const col = t.columns[0];
    if (ix === undefined || col === undefined) throw new Error("golden changed");
    ix.attributes = { ...ix.attributes, name: 5 };
    delete ix.attributes.using;
    delete col.node.attributes.position;
    show(`#/schema?sel=${enc(t.node.identity)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets, pending: true }));
    const name = (await screen.findAllByTestId("index-name"))[0];
    expect(name?.textContent).toBe("name unknown");
    expect(screen.getAllByTestId("index-using")[0]?.textContent).toBe("using unknown");
    expect(screen.getAllByTestId("cell-position")[0]?.textContent).toBe("?");
  });

  it("B5: /sets is requested once per index run across #/schema → #/api → #/schema", async () => {
    const calls: string[] = [];
    show("#/schema", fakeSchemaClient({ summary: summaryGolden("states"), sets: setsGolden("states"), pending: true, calls }));
    await screen.findByTestId("set-strip");
    act(() => {
      window.location.hash = "#/api";
    });
    expect(await screen.findByRole("heading", { name: "API" })).toBeTruthy();
    act(() => {
      window.location.hash = "#/schema";
    });
    await screen.findByTestId("set-strip");
    expect(calls.filter((c) => c === "sets")).toHaveLength(1);
    expect(calls.filter((c) => c === "summary")).toHaveLength(1);
  });

  it("B5: /sets is not requested before the Schema section is shown", async () => {
    const calls: string[] = [];
    show("#/api", fakeSchemaClient({ summary: summaryGolden("states"), sets: setsGolden("states"), pending: true, calls }));
    expect(await screen.findByRole("heading", { name: "API" })).toBeTruthy();
    await waitFor(() => {
      expect(calls).toContain("summary");
    });
    expect(calls).not.toContain("sets");
  });

  it("B7: the unreadable strips sit under the set strip", async () => {
    show(`#/schema?set=${enc("pg-app/migrations")}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), pending: true }));
    const strip = await screen.findByTestId("set-strip");
    const unread = screen.getByTestId("unread-strips");
    expect(strip.compareDocumentPosition(unread) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("C: tabs name their panel and move with the arrow keys", async () => {
    show("#/schema", fakeSchemaClient({ summary: summaryGolden("states"), sets: setsGolden("states"), pending: true }));
    await screen.findByTestId("set-strip");
    const tabs = screen.getAllByRole("tab");
    expect(tabs).toHaveLength(4);
    for (const tab of tabs) expect(document.getElementById(tab.getAttribute("aria-controls") ?? "")?.getAttribute("role")).toBe("tabpanel");
    expect(tabs.map((t) => t.tabIndex)).toEqual([0, -1, -1, -1]);
    fireEvent.keyDown(screen.getByTestId("tab-tables"), { key: "ArrowRight" });
    await waitFor(() => {
      expect(window.location.hash).toBe("#/schema?tab=migrations");
    });
    expect(document.activeElement).toBe(screen.getByTestId("tab-migrations"));
    fireEvent.keyDown(screen.getByTestId("tab-migrations"), { key: "ArrowLeft" });
    await waitFor(() => {
      expect(window.location.hash).toBe("#/schema");
    });
    expect(screen.getByRole("tabpanel").getAttribute("aria-labelledby")).toBe("schema-tab-tables");
  });
});

describe("Review fixes R3: a group not extracted is never counted as 0", () => {
  const fixtureSets = () => setsGolden("fixture");
  const onlyNotModeled = (): SchemaSets => {
    const s = fixtureSets();
    return { ...s, sets: s.sets.filter((g) => g.objects === "not_modeled") };
  };

  it("A1: with mixed groups the Tables tab states a lower bound and why", async () => {
    const sets = fixtureSets();
    const extracted = sets.sets.filter((g) => g.objects === "modeled" || (g.objects === null && g.set === null));
    const notExtracted = sets.sets.filter((g) => g.objects === "not_modeled").length;
    if (notExtracted === 0 || extracted.length === 0) throw new Error("golden changed");
    const n = extracted.reduce((k, g) => k + g.tables.length, 0);
    show("#/schema", fakeSchemaClient({ summary: summaryGolden("fixture"), sets, pending: true }));
    const count = await screen.findByTestId("tab-tables-count");
    const note = `${String(notExtracted)} ${notExtracted === 1 ? "group" : "groups"} not extracted`;
    expect(count.firstChild?.textContent).toBe(`${String(n)}+`);
    expect(count.getAttribute("title")).toBe(note);
    expect(count.textContent).toBe(`${String(n)}+ (${note})`);
  });

  it("A1: with only a not_modeled group the Tables tab reads '?', never 0", async () => {
    const sets = onlyNotModeled();
    const k = sets.sets.length;
    show("#/schema", fakeSchemaClient({ summary: summaryGolden("fixture"), sets, pending: true }));
    const count = await screen.findByTestId("tab-tables-count");
    expect(count.textContent).toBe(`? (${String(k)} ${k === 1 ? "group" : "groups"} not extracted)`);
    expect(screen.getByTestId("tab-tables").textContent).not.toMatch(/Tables\s*0/);
  });

  it("A2: the Overview set switcher reads '(not extracted)' for a not_modeled group, counts for the others", async () => {
    const sets = fixtureSets();
    show("#/schema?tab=overview", fakeSchemaClient({ summary: summaryGolden("fixture"), sets, pending: true }));
    const sw = await screen.findByTestId("set-switcher");
    const opts = [...sw.querySelectorAll("option")].map((o) => o.textContent);
    expect(opts).toContain("sqlite-app/migrations/ (not extracted)");
    expect(opts.some((o) => /\(0 tables\)/.test(o))).toBe(false);
    const pg = sets.sets.find((g) => g.set === "pg-app/migrations");
    if (pg === undefined) throw new Error("golden changed");
    expect(opts).toContain(`pg-app/migrations/ (${String(pg.tables.length)} tables)`);
  });

  it("an unknown objects value reads unknown in the switcher, the tab and the Overview, never 'No tables stored'", async () => {
    const s = fixtureSets();
    const sets = { ...s, sets: s.sets.map((g) => (g.objects === "not_modeled" ? { ...g, objects: "maybe" } : g)) };
    show(`#/schema?tab=overview&set=${enc("sqlite-app/migrations")}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets, pending: true }));
    const sw = await screen.findByTestId("set-switcher");
    expect([...sw.querySelectorAll("option")].map((o) => o.textContent)).toContain("sqlite-app/migrations/ (tables unknown)");
    const k = sets.sets.filter((g) => g.objects === "maybe").length;
    expect(screen.getByTestId("tab-tables-count").getAttribute("title")).toBe(`${String(k)} ${k === 1 ? "group" : "groups"} unknown`);
    const empty = screen.getByTestId("overview-empty");
    expect(empty.textContent).toContain("Whether tables were extracted for this set is unknown");
    expect(empty.textContent).not.toContain("No tables stored");
  });

  it("footer lists known tools only and counts the groups whose tool is unknown", () => {
    expect(footerText(["sqlx"], 1)).toBe("Order by tool: sqlx, unknown tool for 1 group.");
    expect(footerText([], 2)).toBe("Order by tool: unknown tool for 2 groups.");
  });

  it("source guards: no '1 migrations', 'no order'/'no version' in History, 'did not read 0 paths', diagnostics not listed", () => {
    expect(migrationsSrc).toContain('g.migrations.length === 1 ? "migration" : "migrations"');
    expect(migrationsSrc).toContain('data-testid="diagnostics-not-listed"');
    expect(detailSrc).not.toContain('"no version"');
    expect(detailSrc).not.toContain('", no order"');
    expect(emptySrc).toContain("summary.scan_gaps <= 0");
    expect(miniMapSrc).toContain("whether all were extracted is unknown");
  });
});

describe("Schema links follow the editor choice (ADR 0050)", () => {
  const tableId = "table:pg-app/migrations/2_relations.sql#tasks";
  const migId = "migration:pg-app/migrations/6_opaque.sql#migration";

  function showWithEditor(hash: string, client: SchemaClient) {
    window.history.replaceState(null, "", hash);
    return render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} ideFetcher={ideFetcher()} />);
  }

  afterEach(() => {
    window.localStorage.clear();
  });

  it("sends the browser's editor with the table and migration requests, and a new choice reloads them", async () => {
    window.localStorage.setItem(EDITOR_KEY, "zed");
    const calls: string[] = [];
    const client = fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [tableId]: tableGolden("schema-table-complete") }, calls });
    const { unmount } = showWithEditor(`#/schema?set=${enc("pg-app/migrations")}&sel=${enc(tableId)}`, client);
    await waitFor(() => {
      expect(calls).toContain(`table:${tableId}@zed`);
    });
    // Nothing goes out before the preset list answered.
    expect(calls.filter((c) => c.startsWith("table:"))).toEqual([`table:${tableId}@zed`]);
    // The picker lives in Settings since T039: a choice made there (or in
    // another tab) reaches this view through the same stored key.
    act(() => {
      window.localStorage.setItem(EDITOR_KEY, "jetbrains");
      window.dispatchEvent(new StorageEvent("storage", { key: EDITOR_KEY, newValue: "jetbrains" }));
    });
    await waitFor(() => {
      expect(calls).toContain(`table:${tableId}@jetbrains`);
    });
    unmount();

    const migCalls: string[] = [];
    showWithEditor(`#/schema?tab=migrations&mig=${enc(migId)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), migrations: { [migId]: migrationGolden("schema-migration-applied") }, calls: migCalls }));
    await waitFor(() => {
      expect(migCalls).toContain(`migration:${migId}@jetbrains`);
    });
  });

  it("sends no editor for the repository default", async () => {
    const calls: string[] = [];
    showWithEditor(`#/schema?set=${enc("pg-app/migrations")}&sel=${enc(tableId)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [tableId]: tableGolden("schema-table-complete") }, calls }));
    await waitFor(() => {
      expect(calls).toContain(`table:${tableId}`);
    });
    expect(calls.some((c) => c.includes("@"))).toBe(false);
  });

  it("No editor links leaves no link in the table and migration views", async () => {
    window.localStorage.setItem(EDITOR_KEY, "off");
    const calls: string[] = [];
    const { unmount } = showWithEditor(
      `#/schema?set=${enc("pg-app/migrations")}&sel=${enc(tableId)}`,
      fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [tableId]: tableGolden("schema-table-complete") }, calls }),
    );
    await waitFor(() => {
      expect(calls).toContain(`table:${tableId}`);
    });
    const ide = await screen.findByTestId("table-ide");
    await waitFor(() => {
      expect(screen.getByTestId("table-ide").getAttribute("aria-label")).toBe("Editor links are off in this browser (your choice)");
    });
    expect(ide.tagName).toBe("BUTTON");
    expect((ide as HTMLButtonElement).disabled).toBe(true);
    expect(document.querySelectorAll('a[href^="vscode:"], a[href^="zed:"], a[href^="idea:"]')).toHaveLength(0);
    unmount();

    showWithEditor(`#/schema?tab=migrations&mig=${enc(migId)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), migrations: { [migId]: migrationGolden("schema-migration-applied") } }));
    await waitFor(() => {
      expect(screen.getByTestId("migration-ide").getAttribute("aria-label")).toBe("Editor links are off in this browser (your choice)");
    });
    expect(screen.getByTestId("migration-ide").tagName).toBe("BUTTON");
    expect(document.querySelectorAll('a[href^="vscode:"]')).toHaveLength(0);
  });
});
