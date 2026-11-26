// T018f Part 2 (R2): the new API fields' guards, the "Rests on" line and
// row chips (set difference), the why popovers (focus, Escape, one at a
// time, outside click), the source cards for every sources_read x
// unread_tools combination, the overlap note both ways, Include tests per
// model table (O3), the Assumptions tab deep link and the states of 2.1
// (loading, pre-bump, catalogue failed, catalogue version, unknown id).
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { parseSchemaSets, parseSchemaSummary, parseTableDetail, readClaims, type AssumptionCatalogue, type SchemaClient, type Overlap, type SchemaSets, type TableDetail } from "@/api/schema";
import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { LABELS_KEY } from "@/lib/labelChoice";
import { AppShell } from "@/shell/AppShell";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { assumptionsGolden, craftedSummary, fakeSchemaClient, raw, setsGolden, SETS_GOLDENS, SUMMARY_GOLDENS, summaryGolden, TABLE_GOLDENS, tableGolden } from "@/test/schemaFixtures";
import { catalogueView, rowChip } from "@/sections/schema/catalogue";
import { noneTitle } from "@/sections/schema/EmptyStates";
import { schemaEntryTag } from "@/sections/schema/entry";
import { scopeTables, sourceCards } from "@/sections/schema/state";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
const enc = encodeURIComponent;

function show(hash: string, client: SchemaClient) {
  window.history.replaceState(null, "", hash);
  return render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
}

beforeEach(() => {
  window.history.replaceState(null, "", "#/schema");
});

const TASKS = "table:pg-app/migrations/2_relations.sql#tasks";
const TEAM_MODEL = "table:app/models.py#Team";
const TEAM_MIGRATION = "table:migrations/0001_init.sql#team";

/** TEAM_PAIR is the migration side's overlap entry the server serves once
 * every model table is listed. The sqlmodel-link golden's model list may be
 * incomplete, so the server serves null there (F6 decision a); pairedSets
 * and pairedTable restore the entry to test the note in both directions. */
const TEAM_PAIR: Overlap = { table: TEAM_MODEL, set: "models", source: "models", columns_only_here: [], columns_only_there: ["created", "label"], completeness: null, name: null };

function pairedSets(): SchemaSets {
  const s = setsGolden("models");
  return { ...s, sets: s.sets.map((g) => (g.source !== "migrations" ? g : { ...g, tables: g.tables.map((t) => (t.node.identity === TEAM_MIGRATION ? { ...t, overlaps: [TEAM_PAIR] } : t)) })) };
}

function pairedTable(): TableDetail {
  const d = tableGolden("schema-table-model-overlap");
  return { ...d, overlaps: [{ ...TEAM_PAIR, completeness: "complete", name: "team" }] };
}

/** modelsClient serves the sqlmodel-link goldens (with paired, the
 * migration side's overlap restored). */
function modelsClient(opts: { sets?: SchemaSets; paired?: boolean; assumptions?: AssumptionCatalogue | (() => Promise<AssumptionCatalogue>) } = {}) {
  return fakeSchemaClient({
    summary: summaryGolden("models"),
    sets: opts.sets ?? (opts.paired === true ? pairedSets() : setsGolden("models")),
    tables: { [TEAM_MODEL]: tableGolden("schema-table-model"), [TEAM_MIGRATION]: opts.paired === true ? pairedTable() : tableGolden("schema-table-model-overlap"), "table:app/mutations.py#Keyed": tableGolden("schema-table-model-partial") },
    ...(opts.assumptions !== undefined ? { assumptions: opts.assumptions } : {}),
  });
}

describe("guards for the T018f fields", () => {
  it("reads the new summary fields and keeps unknown as null, never [] or 0", () => {
    const s = summaryGolden("models");
    expect(s.model_groups).toBe(1);
    expect(s.sources_read).toEqual(["migrations", "models"]);
    expect(s.unread_tools).toEqual([]);
    const old = raw(SUMMARY_GOLDENS.models) as Record<string, unknown>;
    delete old.model_groups;
    delete old.sources_read;
    delete old.unread_tools;
    const o = parseSchemaSummary(old);
    expect([o.model_groups, o.sources_read, o.unread_tools]).toEqual([null, null, null]);
    const bad = { ...(raw(SUMMARY_GOLDENS.models) as Record<string, unknown>), unread_tools: [{ tool: "alembic", path: "a", marker: null, files: 5 }, 7] };
    expect(parseSchemaSummary(bad).unread_tools).toBeNull();
    const tools = { ...(raw(SUMMARY_GOLDENS.models) as Record<string, unknown>), unread_tools: [{ tool: "flyway", path: null, marker: "x.conf", files: "?" }] };
    expect(parseSchemaSummary(tools).unread_tools).toEqual([{ tool: "flyway", path: null, marker: "x.conf", files: null }]);
  });

  it("reads model groups, rests_on, overlaps and opaque sites from /sets", () => {
    const s = setsGolden("models");
    const models = s.sets.find((g) => g.source === "models");
    expect(models?.files?.length).toBe(9);
    expect(models?.rests_on).toEqual(["V1", "V4", "V6", "V7"]);
    expect(models?.tables_by_scope).toEqual({ production: 19, test: 0 });
    const team = s.sets[0]?.tables[0];
    // The model list may be incomplete here, so the migration side is null.
    expect(team?.overlaps).toBeNull();
    expect(models?.tables.find((t) => t.node.identity === TEAM_MODEL)?.overlaps?.[0]?.columns_only_here).toEqual(["created", "label"]);
    expect(team?.opaque_sites).toEqual([]);
    expect(s.unread_tools).toEqual([]);
    // A pre-bump table: no rests_on, overlaps or opaque_sites at all.
    const old = raw(SETS_GOLDENS.models) as { sets: { tables: Record<string, unknown>[]; rests_on?: unknown }[] };
    for (const g of old.sets) {
      delete g.rests_on;
      for (const t of g.tables) {
        delete t.rests_on;
        delete t.overlaps;
        delete t.opaque_sites;
      }
    }
    const p = parseSchemaSets(old);
    expect(p.sets[0]?.rests_on).toBeNull();
    expect(p.sets[0]?.tables[0]?.rests_on).toBeNull();
    expect(p.sets[0]?.tables[0]?.overlaps).toBeNull();
    expect(p.sets[0]?.tables[0]?.opaque_sites).toBeNull();
  });

  it("reads claims with links on /table, and never a partial list", () => {
    const d = tableGolden("schema-table-complete");
    const ix = Object.values(d.index_claims)[0];
    expect(ix?.[0]?.row).toBe("M4");
    expect(d.columns[0]?.claims?.[0]?.link?.link).toContain("vscode://");
    expect(readClaims([{ fact: "type", row: "M2" }, { row: "M1" }])).toBeNull();
    expect(readClaims(null)).toBeNull();
    const old = raw(TABLE_GOLDENS["schema-table-complete"]) as { columns: Record<string, unknown>[] };
    for (const c of old.columns) delete c.claims;
    expect(parseTableDetail(old).columns[0]?.claims).toBeNull();
  });
});

describe("rests_on and row chips", () => {
  const cat = catalogueView(assumptionsGolden(), "extractors-30");
  const claim = (row: string | null) => ({ fact: "exists", row, migration: null, file: null, span: null, link: null, migration_ref: null, verification: null, dialect: null });

  it("a row chip is the claims' assumptions minus the table's rests_on", () => {
    expect(rowChip([claim("M4"), claim("M1")], "migrations", ["E1", "E4", "E5", "E6", "E8"], cat)).toEqual({ kind: "ids", ids: ["E2"], unknownRows: [] });
    expect(rowChip([claim("M1")], "migrations", ["E1", "E4", "E5", "E6", "E8"], cat)).toEqual({ kind: "ids", ids: [], unknownRows: [] });
    expect(rowChip([claim("Z9")], "migrations", ["E1"], cat)).toEqual({ kind: "ids", ids: [], unknownRows: ["Z9"] });
  });

  it("pre-bump and catalogue states never compute a chip", () => {
    expect(rowChip(null, "migrations", ["E1"], cat).kind).toBe("not_recorded");
    expect(rowChip([claim("M4")], "migrations", null, cat).kind).toBe("not_recorded");
    expect(rowChip([claim("M4")], "migrations", ["E1"], { kind: "loading" }).kind).toBe("unavailable");
    expect(rowChip([claim("M4")], "migrations", ["E1"], { kind: "failed", message: "x" }).kind).toBe("unavailable");
  });

  it("the table line shows rests_on, the group line the union, and row chips only where a row rests on more", async () => {
    show(`#/schema?sel=${enc(TASKS)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [TASKS]: tableGolden("schema-table-complete") } }));
    const line = await screen.findByTestId("table-rests-on");
    // T025: each chip says its assumption's title; the id stays in data-id,
    // the tooltip and the accessible name.
    const chips = within(line).getAllByTestId("rests-chip");
    expect(chips.map((c) => c.getAttribute("data-id"))).toEqual(["E1", "E4", "E5", "E6", "E8"]);
    expect(chips[0]?.textContent).toBe("Objects outside the set");
    expect(chips[0]?.getAttribute("title")).toBe("assumption E1: Objects outside the set");
    expect(chips[0]?.getAttribute("aria-label")).toBe("Objects outside the set (E1): what it means");
    expect(screen.getByTestId("group-rests-on").textContent).toContain("Facts in pg-app/migrations/ rest on");
    await waitFor(() => {
      expect(screen.getAllByTestId("row-chip").length).toBeGreaterThan(0);
    });
    for (const chip of screen.getAllByTestId("row-chip")) {
      expect(chip.textContent).toBe("+1 assumption");
      expect(chip.getAttribute("data-id")).toBe("E2");
    }
    // Columns whose claims add nothing have no chip.
    const rows = screen.getAllByTestId("column-row");
    expect(rows.some((r) => within(r).queryByTestId("row-chip") === null)).toBe(true);
  });

  it("technical labels (T025b): chips show ids, row chips the ids they rest on, titles in tooltips", async () => {
    window.localStorage.setItem(LABELS_KEY, "technical");
    try {
      show(`#/schema?sel=${enc(TASKS)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), tables: { [TASKS]: tableGolden("schema-table-complete") } }));
      const line = await screen.findByTestId("table-rests-on");
      await waitFor(() => {
        expect(within(line).getAllByTestId("rests-chip")[0]?.getAttribute("title")).toBe("assumption E1: Objects outside the set");
      });
      const chips = within(line).getAllByTestId("rests-chip");
      expect(chips.map((c) => c.textContent)).toEqual(["E1", "E4", "E5", "E6", "E8"]);
      expect(chips[0]?.getAttribute("aria-label")).toBe("E1, Objects outside the set: what it means");
      await waitFor(() => {
        expect(screen.getAllByTestId("row-chip").length).toBeGreaterThan(0);
      });
      for (const chip of screen.getAllByTestId("row-chip")) expect(chip.textContent).toBe("E2");
    } finally {
      window.localStorage.removeItem(LABELS_KEY);
    }
  });

  it("while the catalogue loads, the line shows its ids and no row chip", async () => {
    show(`#/schema?sel=${enc(TASKS)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), pending: true, assumptions: () => new Promise<never>(() => undefined) }));
    const line = await screen.findByTestId("table-rests-on");
    expect(within(line).getAllByTestId("rests-chip")).toHaveLength(5);
    expect(screen.queryAllByTestId("row-chip")).toHaveLength(0);
    expect(screen.queryByText(/none of the catalogue/)).toBeNull();
  });

  it("a pre-bump index says 'row not recorded; re-index' and shows no chip", async () => {
    const s = setsGolden("fixture");
    const old: SchemaSets = { ...s, sets: s.sets.map((g) => ({ ...g, rests_on: null, tables: g.tables.map((t) => ({ ...t, rests_on: null, columns: t.columns.map((c) => ({ ...c, node: { ...c.node, attributes: { ...c.node.attributes, claims: undefined } } })) })) })) };
    show(`#/schema?sel=${enc(TASKS)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: old, pending: true }));
    const line = await screen.findByTestId("table-rests-on");
    expect(line.getAttribute("data-state")).toBe("not_recorded");
    expect(line.textContent).toContain("row not recorded for this index; re-index");
    expect(screen.getByTestId("group-rests-on").getAttribute("data-state")).toBe("not_recorded");
    expect(screen.queryAllByTestId("rests-chip")).toHaveLength(0);
    expect(screen.queryAllByTestId("row-chip")).toHaveLength(0);
  });
});

describe("why popovers", () => {
  it("click opens and focuses; Escape closes and refocuses; one at a time; outside click closes", async () => {
    show(`#/schema?sel=${enc(TASKS)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), pending: true }));
    const line = await screen.findByTestId("table-rests-on");
    const [e1, e4] = within(line).getAllByTestId("rests-chip");
    if (e1 === undefined || e4 === undefined) throw new Error("chips missing");
    expect(e1.getAttribute("aria-haspopup")).toBe("dialog");
    expect(e1.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(e1);
    const pop = await screen.findByTestId("why-popover");
    expect(e1.getAttribute("aria-expanded")).toBe("true");
    expect(pop.contains(document.activeElement)).toBe(true);
    await waitFor(() => {
      expect(pop.textContent).toContain("Objects outside the set");
    });
    // Tab cycles inside: from the last focusable back to the first.
    const focusables = pop.querySelectorAll<HTMLElement>("button, a[href]");
    focusables[focusables.length - 1]?.focus();
    fireEvent.keyDown(pop, { key: "Tab" });
    expect(document.activeElement).toBe(focusables[0]);
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByTestId("why-popover")).toBeNull();
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(e1);
    });
    fireEvent.click(e1);
    await screen.findByTestId("why-popover");
    fireEvent.click(e4);
    await waitFor(() => {
      expect(screen.getAllByTestId("why-popover")).toHaveLength(1);
    });
    expect(screen.getByTestId("why-popover").textContent).toContain("E4");
    expect(e1.getAttribute("aria-expanded")).toBe("false");
    fireEvent.mouseDown(document.body);
    await waitFor(() => {
      expect(screen.queryByTestId("why-popover")).toBeNull();
    });
  });

  it("an unknown cell of a model table opens its reasons, contract row, assumptions and source link", async () => {
    const BOUND = "table:app/mutations.py#Keyed";
    show(`#/schema?set=${enc("@models/sqlmodel/.")}&sel=${enc(BOUND)}`, modelsClient());
    await screen.findByTestId("table-detail");
    await waitFor(() => {
      expect(screen.getByTestId("model-history").textContent).toContain("No migration Graphvein reads touches a model table");
    });
    const cells = screen.getAllByTestId("unknown-cell");
    expect(cells.length).toBeGreaterThan(0);
    fireEvent.click(cells[0] as HTMLElement);
    const pop = await screen.findByTestId("why-popover");
    expect(pop.getAttribute("data-kind")).toBe("cell");
    expect(within(pop).getAllByTestId("why-row")[0]?.getAttribute("data-row")).toBe("X1");
    expect(within(pop).getAllByTestId("why-assumption").map((a) => a.getAttribute("data-assumption"))).toEqual(["V1", "V4", "V6", "V7"]);
    expect(within(pop).getAllByTestId("why-reason")[0]?.textContent).toContain("app/mutations.py:");
    fireEvent.click(within(pop).getByTestId("why-entry"));
    await waitFor(() => {
      expect(window.location.hash).toContain("tab=assumptions");
    });
    expect(window.location.hash).toContain(`a=${enc("row:models/sqlmodel:X1")}`);
  });

  it("the catalogue failing keeps the ids: 'assumption text unavailable' and the row id", async () => {
    show(`#/schema?sel=${enc(TASKS)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: setsGolden("fixture"), pending: true, assumptions: () => Promise.reject(new Error("boom")) }));
    const line = await screen.findByTestId("table-rests-on");
    await screen.findByTestId("row-chips-unavailable");
    fireEvent.click(within(line).getAllByTestId("rests-chip")[0] as HTMLElement);
    const pop = await screen.findByTestId("why-popover");
    expect(pop.textContent).toContain("assumption text unavailable");
    expect(pop.textContent).toContain("E1");
    expect(within(pop).getByTestId("why-catalogue-failed")).toBeTruthy();
  });

  it("an id the catalogue lacks is shown raw; a different catalogue version is noted", async () => {
    // A catalogue version no index has, so the note shows whatever version
    // the goldens were built with (it moves with every extractor bump).
    const cat = { ...assumptionsGolden(), extractor: "extractors-0" };
    const s = setsGolden("fixture");
    const odd: SchemaSets = { ...s, sets: s.sets.map((g) => ({ ...g, tables: g.tables.map((t) => (t.node.identity === TASKS ? { ...t, rests_on: ["E1", "E99"] } : t)) })) };
    show(`#/schema?sel=${enc(TASKS)}`, fakeSchemaClient({ summary: summaryGolden("fixture"), sets: odd, pending: true, assumptions: cat }));
    const line = await screen.findByTestId("table-rests-on");
    const chip = within(line).getAllByTestId("rests-chip").find((c) => c.textContent === "E99");
    if (chip === undefined) throw new Error("E99 chip missing");
    fireEvent.click(chip);
    const pop = await screen.findByTestId("why-popover");
    await waitFor(() => {
      expect(pop.textContent).toContain("not in this build's catalogue");
    });
    expect(within(pop).getByTestId("why-version-note").textContent).toMatch(/the index was built by extractors-[1-9][0-9]*/);
  });
});

describe("source cards and unread tools", () => {
  const s = setsGolden("models");
  const migOnly: SchemaSets = { ...s, sets: s.sets.filter((g) => g.source === "migrations") };
  const modelsOnly: SchemaSets = { ...s, sets: s.sets.filter((g) => g.source === "models") };
  const alembic = [{ tool: "alembic", path: "backend/app/alembic", marker: "backend/alembic.ini", files: 5 }];
  const kinds = (sets: SchemaSets, read: string[] | null, unread: typeof alembic | [] | null) => sourceCards(sets.sets, sets, read, unread).map((c) => (c.kind === "group" ? `group:${c.group.source ?? ""}` : c.kind === "empty" ? `empty:${c.source}` : `unread:${c.tool.tool}`));

  it.each([
    ["both groups", s, ["migrations", "models"], [], ["group:migrations", "group:models"]],
    ["migrations only, models read", migOnly, ["migrations", "models"], [], ["group:migrations", "empty:models"]],
    ["migrations only, models not read (extractors-28)", migOnly, ["migrations"], [], ["group:migrations"]],
    ["models only, nothing unread", modelsOnly, ["migrations", "models"], [], ["group:models", "empty:migrations"]],
    ["models only, Alembic unread: never 'No migration set found'", modelsOnly, ["migrations", "models"], alembic, ["group:models", "unread:alembic"]],
    ["models only, unread unknown", modelsOnly, ["migrations", "models"], null, ["group:models"]],
    ["sources_read unknown", s, null, alembic, ["group:migrations", "group:models", "unread:alembic"]],
  ] as const)("%s", (_, sets, read, unread, want) => {
    expect(kinds(sets, read === null ? null : [...read], unread === null ? null : [...unread])).toEqual(want);
  });

  it("renders a dashed unread card with its popover, and the selected group's card", async () => {
    const sets = { ...modelsOnly, unread_tools: alembic };
    show("#/schema", modelsClient({ sets }));
    const cards = await screen.findAllByTestId("source-card");
    expect(cards.map((c) => c.getAttribute("data-kind"))).toEqual(["group", "unread"]);
    expect(cards[1]?.textContent).toContain("Alembic migrations found, not read yet");
    expect(cards[1]?.textContent).toContain("5 files");
    expect(screen.queryByText(/No migration set found/)).toBeNull();
    fireEvent.click(within(cards[1] as HTMLElement).getByTestId("unread-why"));
    expect((await screen.findByTestId("why-popover")).getAttribute("data-kind")).toBe("unread");
  });

  it("the models empty card carries model_list_reasons only for models", () => {
    const sets: SchemaSets = { ...migOnly, model_list_reasons: ["python_unread"] };
    const cards = sourceCards(sets.sets, sets, ["migrations", "models"], []);
    expect(cards.filter((c) => c.kind === "empty")).toEqual([{ kind: "empty", source: "models", reasons: ["python_unread"] }]);
    const none = sourceCards(migOnly.sets, { ...migOnly, model_list_reasons: null }, ["migrations", "models"], []);
    expect(none.filter((c) => c.kind === "empty")).toEqual([{ kind: "empty", source: "models", reasons: [] }]);
  });

  it.each([
    [["python_unread"], "Some .py files were not read."],
    [["python_scan_gap"], "Scan gaps may hide .py files."],
    [["python_scan_gap", "python_unread"], "Some .py files were not read."],
  ] as const)("the 'No model table claimed' card names %j", async (reasons, line) => {
    const sets: SchemaSets = { ...migOnly, model_list_reasons: [...reasons] };
    show("#/schema", modelsClient({ sets }));
    const cards = await screen.findAllByTestId("source-card");
    const empty = cards.find((c) => c.getAttribute("data-kind") === "empty") as HTMLElement;
    expect(empty.textContent).toContain("No model table claimed.");
    expect(within(empty).getByTestId("empty-reason").textContent).toBe(line);
    fireEvent.click(within(empty).getByTestId("source-why"));
    const pop = await screen.findByTestId("why-popover");
    expect(within(pop).getAllByTestId("empty-reason-text")).toHaveLength(reasons.length);
  });

  it("the 'No model table claimed' card says nothing more without reasons", async () => {
    show("#/schema", modelsClient({ sets: { ...migOnly, model_list_reasons: [] } }));
    const cards = await screen.findAllByTestId("source-card");
    const empty = cards.find((c) => c.getAttribute("data-kind") === "empty") as HTMLElement;
    expect(within(empty).queryByTestId("empty-reason")).toBeNull();
  });

  it("the none page and tag never say 'No migrations found' while a tool is unread", () => {
    const summary = craftedSummary({ found: "none", sources_read: ["migrations", "models"], unread_tools: alembic });
    expect(noneTitle(summary)).toBe("Graphvein found Alembic migrations but does not read them yet");
    expect(schemaEntryTag(summary)?.text).toBe("not read yet");
    expect(noneTitle({ unread_tools: [], sources_read: ["migrations"] })).toBe("No migrations found");
    expect(noneTitle({ unread_tools: [], sources_read: ["migrations", "models"] })).toBe("No migrations or model tables found");
  });
});

describe("model groups", () => {
  it("the overlap note links both ways; column lists only when both are sent", async () => {
    show(`#/schema?sel=${enc(TEAM_MIGRATION)}`, modelsClient({ paired: true }));
    const note = await screen.findByTestId("overlap-note");
    expect(note.textContent).toContain("models/sqlmodel/. also defines a table named team");
    expect(note.textContent).toContain("not merged and not compared");
    expect(within(note).getByTestId("overlap-columns").textContent).toContain("columns only in the model created, label; only in the migration none");
    fireEvent.click(within(note).getByTestId("overlap-open"));
    await waitFor(() => {
      expect(window.location.hash).toContain(enc(TEAM_MODEL));
    });
    const back = await screen.findByTestId("overlap-note");
    expect(back.textContent).toContain("migrations/ also defines a table named team");
    expect(within(back).getByTestId("overlap-columns").textContent).toContain("columns only in the model created, label");
    expect(screen.getAllByTestId("overlap-mark").length).toBeGreaterThan(0);
  });

  it("a null overlap list on the migration side shows no note", async () => {
    show(`#/schema?sel=${enc(TEAM_MIGRATION)}`, modelsClient());
    expect((await screen.findByTestId("detail-header")).textContent).toContain("team");
    expect(screen.queryByTestId("overlap-note")).toBeNull();
  });

  it("null column lists say nothing about columns", async () => {
    const s = pairedSets();
    const nulls: SchemaSets = { ...s, sets: s.sets.map((g) => ({ ...g, tables: g.tables.map((t) => ({ ...t, overlaps: t.overlaps?.map((o) => ({ ...o, columns_only_here: null, columns_only_there: null })) ?? null })) })) };
    show(`#/schema?sel=${enc(TEAM_MIGRATION)}`, fakeSchemaClient({ summary: summaryGolden("models"), sets: nulls, pending: true }));
    const note = await screen.findByTestId("overlap-note");
    expect(within(note).queryByTestId("overlap-columns")).toBeNull();
    expect(note.textContent).not.toContain("only in the");
  });

  it("model columns: mapped type, PY defaults, no position; the Model files tab", async () => {
    show(`#/schema?sel=${enc(TEAM_MODEL)}`, modelsClient());
    await screen.findByTestId("model-order-key");
    expect(screen.queryAllByTestId("cell-position")).toHaveLength(0);
    expect(screen.getAllByTestId("py-chip").length).toBeGreaterThan(0);
    expect(screen.getByTestId("source-chip").textContent).toBe("model · sqlmodel");
    expect(screen.getByTestId("tab-migrations").textContent).toContain("Model files");
    fireEvent.click(screen.getByTestId("tab-migrations"));
    const files = await screen.findAllByTestId("model-file");
    expect(files).toHaveLength(9);
  });

  it("Include tests governs test-scope model tables per table (O3)", () => {
    const s = setsGolden("models");
    const models = s.sets.find((g) => g.source === "models");
    if (models === undefined) throw new Error("golden changed");
    const first = models.tables[0];
    if (first === undefined) throw new Error("golden changed");
    const g = { ...models, tables: models.tables.map((t) => (t === first ? { ...t, node: { ...t.node, attributes: { ...t.node.attributes, scope: "test" } } } : t)) };
    const off = scopeTables([g], false);
    expect(off.groups[0]?.tables).toHaveLength(models.tables.length - 1);
    expect(off.hidden.get(off.groups[0] as never)).toBe(1);
    expect(off.hiddenIds.has(first.node.identity)).toBe(true);
    const on = scopeTables([g], true);
    expect(on.groups[0]).toBe(g);
    expect(on.hiddenIds.size).toBe(0);
  });

  it("the strip says how many test tables are hidden, never as an absence", async () => {
    const s = setsGolden("models");
    const withTest: SchemaSets = { ...s, sets: s.sets.map((g) => (g.source !== "models" ? g : { ...g, tables: g.tables.map((t, i) => (i < 2 ? { ...t, node: { ...t.node, attributes: { ...t.node.attributes, scope: "test" } } } : t)) })) };
    show(`#/schema?set=${enc("@models/sqlmodel/.")}`, fakeSchemaClient({ summary: summaryGolden("models"), sets: withTest, pending: true }));
    const hidden = await screen.findByTestId("hidden-test-tables");
    expect(hidden.textContent).toContain("2 test tables hidden");
    fireEvent.click(within(hidden).getByRole("button", { name: "Include tests" }));
    expect(window.location.hash).toContain("tests=1");
  });
});

describe("the Assumptions tab", () => {
  it("deep link scrolls to and highlights the entry; folded entries sit under their target", async () => {
    show("#/schema?tab=assumptions&a=E2", modelsClient());
    const tab = await screen.findByTestId("assumptions-tab");
    await waitFor(() => {
      expect(within(tab).getAllByTestId("assumption-entry").length).toBeGreaterThan(0);
    });
    const e2 = within(tab).getAllByTestId("assumption-entry").find((e) => e.getAttribute("data-entry") === "E2");
    expect(e2?.getAttribute("data-selected")).toBe("true");
    const e7 = within(tab).getAllByTestId("assumption-entry").find((e) => e.getAttribute("data-entry") === "E7");
    expect(e7?.getAttribute("data-folded-into")).toBe("E8");
    // T019b part 2: the code-to-table assumptions (TA-V ids) are a third source.
    // Model rows are listed per tool after the shared Models assumptions,
    // each tool with its own default row D (T018g6 Revision 3 addendum 3).
    expect(within(tab).getAllByTestId("assumption-source").map((x) => x.getAttribute("data-source"))).toEqual(["migrations", "models", "models/diesel", "models/drizzle", "models/gorm", "models/prisma", "models/sequelize", "models/sqlmodel", "models/supabase_types", "models/typeorm", "table_access"]);
    expect(within(tab).getByRole("heading", { name: "Models: typeorm contract" })).toBeTruthy();
    const dRows = within(tab).getAllByTestId("contract-row").filter((r) => r.getAttribute("data-entry")?.startsWith("row:models/") === true && r.getAttribute("data-entry")?.endsWith(":D") === true);
    expect(dRows.map((r) => r.getAttribute("data-entry"))).toEqual(["row:models/diesel:D", "row:models/drizzle:D", "row:models/gorm:D", "row:models/prisma:D", "row:models/sequelize:D", "row:models/sqlmodel:D", "row:models/supabase_types:D", "row:models/typeorm:D"]);
    expect(dRows[7]?.textContent).toContain("typeorm");
    expect(dRows[5]?.textContent).not.toContain("typeorm");
    expect(within(tab).getByRole("heading", { name: "Code to tables" })).toBeTruthy();
    // The table list stays beside the tab.
    expect(screen.getByTestId("table-list-rows")).toBeTruthy();
  });

  it("'What these mean' opens the tab", async () => {
    show(`#/schema?sel=${enc(TEAM_MODEL)}`, modelsClient());
    const line = await screen.findByTestId("table-rests-on");
    act(() => {
      fireEvent.click(within(line).getByTestId("what-these-mean"));
    });
    await screen.findByTestId("assumptions-tab");
    expect(window.location.hash).toContain("tab=assumptions");
  });
});
