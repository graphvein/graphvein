// T018g0b (ADR 0065): the Schema view against the server goldens
// schema-sets-model-dialects and schema-table-model-dialects. A group whose
// dialect is chosen at run time shows its dialects; a per-dialect fact is
// shown per dialect with its own verification; a type family names its
// unknown modifiers; the ORM's own type is labelled as the tool's.
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import type { Claim } from "@/api/schema";
import { lineVerification } from "@/sections/schema/Dialects";
import { UNRECORDED } from "@/sections/schema/Verification";
import { fakeSchemaClient, setsGolden, summaryGolden, tableGolden } from "@/test/schemaFixtures";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
const enc = encodeURIComponent;
const ORDER = "table:app/models.go#Order";

/** show opens Order; with detail false only /sets answers (the claims
 * are not loaded). edit changes a column's attributes in both goldens
 * (a case the goldens do not hold). */
function show(detail = true, edit?: (column: string, attrs: Record<string, unknown>) => void, editClaims?: (column: string, claims: Claim[]) => Claim[]) {
  const sets = setsGolden("model_dialects");
  const table = tableGolden("schema-table-model-dialects");
  if (edit !== undefined) {
    for (const c of [...(sets.sets[0]?.tables[0]?.columns ?? []), ...table.columns]) edit(c.node.name, c.node.attributes);
  }
  if (editClaims !== undefined) {
    for (const c of table.columns) c.claims = editClaims(c.node.name, c.claims ?? []);
  }
  const client = fakeSchemaClient({ summary: summaryGolden("models"), sets, tables: detail ? { [ORDER]: table } : {}, pending: true });
  window.history.replaceState(null, "", `#/schema?set=${enc("@models/synthetic/app")}&sel=${enc(ORDER)}`);
  render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
}

function row(name: string): HTMLElement {
  const r = screen.getAllByTestId("column-row").find((x) => x.getAttribute("data-column") === name);
  if (r === undefined) throw new Error(`no row ${name}`);
  return r;
}

/** lines lists a cell's per-dialect lines as [dialects, text]. */
function lines(cell: HTMLElement): [string | null, string][] {
  return within(cell).getAllByTestId("dialect-line").map((l) => [l.getAttribute("data-dialect"), l.textContent.trim()]);
}

async function loaded() {
  await screen.findByTestId("table-detail");
  await waitFor(() => {
    expect(within(row("id")).queryAllByTestId("dialect-oracle").length).toBeGreaterThan(0);
  });
}

beforeEach(() => {
  window.history.replaceState(null, "", "#/schema");
});

describe("per-dialect facts (ADR 0065)", () => {
  it("the goldens hold the records this view reads", () => {
    const sets = setsGolden("model_dialects");
    expect(sets.sets[0]?.dialects).toEqual(["mysql", "postgres", "sqlite"]);
    const d = tableGolden("schema-table-model-dialects");
    expect(d.dialects).toEqual(["mysql", "postgres", "sqlite"]);
    expect(d.columns.flatMap((c) => c.claims ?? []).some((c) => c.dialect === "mysql" && c.verification === "mapped")).toBe(true);
  });

  it("the group strip and the list's group header name the run-time dialects", async () => {
    show();
    await loaded();
    expect(screen.getByTestId("group-dialects").textContent).toBe(" · runs on mysql, postgres or sqlite");
  });

  it("a per-dialect type shows one line per dialect, each with its own verification", async () => {
    show();
    await loaded();
    const cell = within(row("id")).getByTestId("cell-type");
    expect(lines(cell)).toEqual([
      ["mysql", "mysqlbigint unsigned AI unverified"],
      ["postgres", "postgresbigserial AI"],
      ["sqlite", "sqliteinteger AI"],
    ]);
    const [my, pg, sq] = within(cell).getAllByTestId("dialect-line");
    if (my === undefined || pg === undefined || sq === undefined) throw new Error("lines");
    expect(within(my).getByTestId("dialect-unverified").getAttribute("title")).toBe("Mapped, not verified. From synthetic's mysql mapping; not checked against a mysql database.");
    expect(within(my).queryByTestId("dialect-oracle")).toBeNull();
    expect(within(pg).getByTestId("dialect-oracle").getAttribute("title")).toBe("Verified on postgres");
    expect(within(sq).queryByTestId("dialect-unverified")).toBeNull();
    // Never one bare type.
    expect(cell.textContent).not.toMatch(/^bigserial/);
  });

  it("the mapped mysql claims mark the column; their popover names mysql", async () => {
    show();
    await loaded();
    fireEvent.click(within(row("id")).getByTestId("unverified-chip"));
    const pop = await screen.findByTestId("why-popover");
    const mappedLines = within(pop).getAllByTestId("mapped-line").map((l) => l.textContent);
    expect(mappedLines).toContain("From synthetic's mysql mapping; not checked against a mysql database.");
    expect(within(pop).getByTestId("why-lead").textContent).toContain("type (mysql)");
    expect(within(pop).getByTestId("why-lead").textContent).not.toContain("type (postgres)");
  });

  it("a per-dialect type family and server default: unknown parts stay visible, absence only where proven", async () => {
    show();
    await loaded();
    const r = row("amount");
    expect(lines(within(r).getByTestId("cell-type"))).toEqual([
      ["mysql", "mysqldecimal(10, 0) unverified"],
      ["postgres", "postgresnumeric (precision, scale unknown)"],
      ["sqlite", "sqliteunknown"],
    ]);
    // The postgres family claim is oracle-verified.
    const pg = within(within(r).getByTestId("cell-type")).getAllByTestId("dialect-line")[1];
    if (pg === undefined) throw new Error("postgres line");
    expect(within(pg).getByTestId("dialect-oracle")).toBeTruthy();
    expect(within(pg).getByTestId("cell-tail").textContent).toBe("(precision, scale unknown)");
    // default_state is unknown (decision 1): by_dialect gives the answer;
    // sqlite's false on a complete table is a proven absence.
    expect(lines(within(r).getByTestId("cell-default"))).toEqual([
      ["mysql", "mysql'0' unverified"],
      ["postgres", "postgres0"],
      ["sqlite", "sqlitenone"],
    ]);
  });

  it("a per-dialect type family names its unknown precision on its dialect", async () => {
    show();
    await loaded();
    expect(lines(within(row("created")).getByTestId("cell-type"))).toEqual([
      ["mysql", "mysqldatetime (precision unknown) unverified"],
      ["postgres", "postgrestimestamp with time zone (precision unknown)"],
      ["sqlite", "sqlitedatetime"],
    ]);
  });

  it("agreeing values join into one line, verified only if every dialect's claim is oracle (A1)", async () => {
    show();
    await loaded();
    // note: text on all three dialects, the mysql claim mapped.
    const cell = within(row("note")).getByTestId("cell-type");
    expect(lines(cell)).toEqual([["mysql,postgres,sqlite", "text on mysql, postgres, sqlite unverified on mysql"]]);
    expect(within(cell).queryByTestId("dialect-oracle")).toBeNull();
    expect(within(cell).getByTestId("dialect-unverified").getAttribute("title")).toBe("Mapped, not verified. From synthetic's mysql mapping; not checked against a mysql database.");
  });

  it("a joined line names its unverified dialects only when they differ (Revision 1)", async () => {
    // Every dialect mapped: the whole line is unverified, one plain tag.
    show(true, undefined, (name, cs) => (name === "note" ? cs.map((c) => (c.fact === "type" ? { ...c, verification: "mapped" } : c)) : cs));
    await loaded();
    const cell = within(row("note")).getByTestId("cell-type");
    expect(lines(cell)).toEqual([["mysql,postgres,sqlite", "text on mysql, postgres, sqlite unverified"]]);
    expect(within(cell).getByTestId("dialect-unverified").getAttribute("title")).toBe(
      "Mapped, not verified. From synthetic's mysql mapping; not checked against a mysql database. From synthetic's postgres mapping; not checked against a postgres database. From synthetic's sqlite mapping; not checked against a sqlite database.",
    );
  });

  it("the column chip counts a per-dialect value with no claim (Revision 1)", async () => {
    // note: every claim oracle, the sqlite type claim missing.
    show(true, undefined, (name, cs) => (name === "note" ? cs.filter((c) => !(c.fact === "type" && c.dialect === "sqlite")).map((c) => ({ ...c, verification: "oracle" })) : cs));
    await loaded();
    const note = row("note");
    expect(within(within(note).getByTestId("cell-type")).getByTestId("dialect-unverified").textContent).toBe("unverified on sqlite");
    fireEvent.click(within(note).getByTestId("unverified-chip"));
    const pop = await screen.findByTestId("why-popover");
    expect(within(pop).getByTestId("why-lead").textContent).toContain("Facts: type (sqlite).");
    expect(within(pop).getByTestId("unrecorded-line").textContent).toContain("not treated as verified");
  });

  it("the table chip lists a per-dialect value with no claim (Revision 1)", async () => {
    show(true, undefined, (name, cs) => (name === "note" ? cs.filter((c) => !(c.fact === "type" && c.dialect === "sqlite")) : cs));
    await loaded();
    fireEvent.click(screen.getByTestId("table-unverified-chip"));
    const pop = await screen.findByTestId("why-popover");
    expect(within(pop).getByTestId("why-lead").textContent).toContain("note.type (sqlite)");
  });

  it("a column whose every per-dialect value has an oracle claim carries no chip", async () => {
    show(true, undefined, (name, cs) => (name === "note" ? cs.map((c) => ({ ...c, verification: "oracle" })) : cs));
    await loaded();
    expect(within(row("note")).queryByTestId("unverified-chip")).toBeNull();
    expect(within(within(row("note")).getByTestId("cell-type")).getByTestId("dialect-oracle")).toBeTruthy();
  });

  it("the flat annotation stays beside per-dialect types (C3)", async () => {
    show(true, (name, a) => {
      if (name === "note") a.annotation = "string";
    });
    await loaded();
    const cell = within(row("note")).getByTestId("cell-type");
    expect(within(cell).getAllByTestId("dialect-line")).toHaveLength(1);
    expect(cell.textContent).toBe("text on mysql, postgres, sqlite unverified on mysqlstring");
    expect(within(row("id")).getByTestId("cell-type").textContent).not.toContain("string");
  });

  it("a per-dialect absence of a server default shows the flat ORM-side default (A2)", async () => {
    show(true, (name, a) => {
      if (name === "amount") {
        a.default = "scalar";
        a.default_value = 0;
      }
    });
    await loaded();
    const cell = within(row("amount")).getByTestId("cell-default");
    expect(lines(cell)).toEqual([
      ["mysql", "mysql'0' unverified"],
      ["postgres", "postgres0"],
      ["sqlite", "sqlite0PY"],
    ]);
    expect(screen.getByTestId("model-order-key").textContent).toContain("PY: a Python-side default");
  });

  it("a per-dialect absence of a server default beside an unknown flat default is unknown (A2)", async () => {
    show(true, (name, a) => {
      if (name === "amount") a.default = null;
    });
    await loaded();
    const sq = lines(within(row("amount")).getByTestId("cell-default"))[2];
    expect(sq).toEqual(["sqlite", "sqliteunknown"]);
  });

  it("the grid header of a model group says what it shows", async () => {
    show();
    await loaded();
    expect(screen.getAllByRole("columnheader").map((h) => h.textContent)).toContain("Type / annotation");
  });

  it("the tool's own type is labelled as the tool's, apart from the database type", async () => {
    show();
    await loaded();
    const tool = within(row("amount")).getByTestId("tool-type");
    expect(tool.textContent).toBe("synthetic type: decimal.Decimal");
    expect(tool.getAttribute("title")).toContain("not a database fact");
    expect(within(row("id")).getByTestId("tool-type").textContent).toBe("synthetic type: uint");
    expect(within(row("note")).queryByTestId("tool-type")).toBeNull();

  });

  it("before /table loads, the /sets values show without any verification mark", async () => {
    show(false);
    await screen.findByTestId("table-detail");
    await waitFor(() => {
      expect(screen.getAllByTestId("dialect-line").length).toBeGreaterThan(0);
    });
    expect(lines(within(row("id")).getByTestId("cell-type"))[0]).toEqual(["mysql", "mysqlbigint unsigned AI"]);
    expect(screen.queryByTestId("dialect-oracle")).toBeNull();
    expect(screen.queryByTestId("dialect-unverified")).toBeNull();
  });
});

describe("lineVerification (Revision 1, A1)", () => {
  const claim = (fact: string, dialect: string, verification: string | null): Claim => ({ fact, dialect, verification, row: "R", migration: null, file: "f", span: null, link: null, migration_ref: null });
  const attrs = { by_dialect: { mysql: { type: "text" }, postgres: { type: "text" }, sqlite: { type: "text" } } };
  const line = { dialects: ["mysql", "postgres", "sqlite"], parts: [] };

  it("an agreeing line with a mapped mysql claim is not verified", () => {
    const v = lineVerification([claim("type", "mysql", "mapped"), claim("type", "postgres", "oracle"), claim("type", "sqlite", "oracle")], attrs, line, ["type"]);
    expect(v).toEqual({ kind: "unverified", mapped: ["mysql"], unrecorded: false, dialects: ["mysql"] });
  });

  it("a known value without its claim counts as unverified, not as verified", () => {
    const v = lineVerification([claim("type", "postgres", "oracle"), claim("type", "sqlite", "oracle")], attrs, line, ["type"]);
    expect(v).toEqual({ kind: "unverified", mapped: [], unrecorded: true, dialects: ["mysql"] });
    expect(lineVerification([claim("type", "mysql", null), claim("type", "postgres", "oracle"), claim("type", "sqlite", "oracle")], attrs, line, ["type"])).toEqual({ kind: "unverified", mapped: [], unrecorded: true, dialects: ["mysql"] });
    expect(UNRECORDED).toContain("not treated as verified");
  });

  it("verified only when every known value of every dialect has an oracle claim", () => {
    const all = [claim("type", "mysql", "oracle"), claim("type", "postgres", "oracle"), claim("type", "sqlite", "oracle")];
    expect(lineVerification(all, attrs, line, ["type"])).toEqual({ kind: "oracle", dialects: ["mysql", "postgres", "sqlite"] });
    // A second known fact (precision) without a claim breaks it.
    const withPrecision = { by_dialect: { ...attrs.by_dialect, mysql: { type: "text", precision: 3 } } };
    expect(lineVerification(all, withPrecision, line, ["type", "precision"])).toEqual({ kind: "unverified", mapped: [], unrecorded: true, dialects: ["mysql"] });
  });

  it("no claims (/sets) or no known value shows no mark", () => {
    expect(lineVerification(null, attrs, line, ["type"])).toBeNull();
    expect(lineVerification([], { by_dialect: { mysql: { type: null } } }, { dialects: ["mysql"], parts: [] }, ["type"])).toBeNull();
  });
});
