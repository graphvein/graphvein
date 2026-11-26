// T018f Revision 2 (2026-11-09, after the independent review): an
// unresolved model foreign key is shown as unresolved (A1), the table list
// names unread .py files (B1), model rows show no Go function as a form
// (C3), References and Keys cells are empty only when no foreign key is
// proven (C5), a link to a table Include tests hides carries the visible
// mark (C7), the popover keeps long text inside its box (C1) and the strip
// says when more lines follow (C8).
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { PresentedNode } from "@/api/endpoints";
import type { SchemaClient, SchemaSets } from "@/api/schema";
import type { Status } from "@/api/status";
import type { StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { assumptionsGolden, fakeSchemaClient, setsGolden, summaryGolden, tableGolden } from "@/test/schemaFixtures";
import { ColumnGrid, type GridColumn, type GridWhy } from "@/sections/schema/ColumnGrid";
import { names as buildNames } from "@/sections/schema/names";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
const enc = encodeURIComponent;
const HERO = "table:app/models.py#Hero";
const KEYED = "table:app/mutations.py#Keyed";

function show(hash: string, client: SchemaClient) {
  window.history.replaceState(null, "", hash);
  return render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
}

function modelsClient(sets: SchemaSets = setsGolden("models"), pending = false) {
  return fakeSchemaClient({
    summary: summaryGolden("models"),
    sets,
    tables: pending ? {} : { [HERO]: tableGolden("schema-table-model-unresolved"), [KEYED]: tableGolden("schema-table-model-partial") },
    pending,
  });
}

beforeEach(() => {
  window.history.replaceState(null, "", "#/schema");
});

function row(name: string): HTMLElement {
  const r = screen.getAllByTestId("column-row").find((x) => x.getAttribute("data-column") === name);
  if (r === undefined) throw new Error(`no row ${name}`);
  return r;
}

describe("A1: an unresolved model foreign key", () => {
  for (const pending of [false, true]) {
    it(`shows as unresolved in References and Keys (${pending ? "/sets only" : "/table"})`, async () => {
      show(`#/schema?set=${enc("@models/sqlmodel/.")}&sel=${enc(HERO)}`, modelsClient(undefined, pending));
      await screen.findByTestId("table-detail");
      await waitFor(() => {
        expect(within(row("rival_id")).getByTestId("unresolved-ref").textContent).toContain("nowhere");
      });
      const r = row("rival_id");
      expect(within(r).getByTestId("fk-unresolved-chip")).toBeTruthy();
      expect(within(r).queryByTestId("unknown-cell")).toBeNull();
      // A resolved model foreign key stays a link with its FK chip.
      expect(within(row("team_id")).getAllByTestId("fk-link")).toHaveLength(1);
      expect(within(row("team_id")).queryByTestId("unresolved-ref")).toBeNull();
      if (!pending) {
        expect(screen.getByTestId("unresolved-section").textContent).toContain("nowhere");
      }
    });
  }
});

describe("B1: unread .py files", () => {
  it("the table list line names python_unread", async () => {
    const s = setsGolden("models");
    const sets: SchemaSets = { ...s, sets: s.sets.map((g) => (g.source === "models" ? { ...g, table_list: "may_be_incomplete", table_list_reasons: ["python_unread"] } : g)) };
    show(`#/schema?set=${enc("@models/sqlmodel/.")}`, modelsClient(sets, true));
    const strip = await screen.findByTestId("set-strip");
    await waitFor(() => {
      expect(strip.textContent).toContain("Some .py files were not read");
    });
  });
});

describe("C3: model rows name no Go function", () => {
  it("serves branch null for model rows and the popover shows the claim only", async () => {
    const cat = assumptionsGolden();
    const modelSources = Object.keys(cat.rows).filter((k) => k.startsWith("models/"));
    expect(modelSources.length).toBeGreaterThan(0);
    for (const k of modelSources) for (const r of Object.values(cat.rows[k] ?? {})) expect(r.branch).toBeNull();
    for (const r of Object.values(cat.rows.migrations ?? {})) expect(typeof r.branch).toBe("string");
    show(`#/schema?set=${enc("@models/sqlmodel/.")}&sel=${enc(KEYED)}`, modelsClient());
    await screen.findByTestId("table-detail");
    await waitFor(() => {
      expect(screen.getAllByTestId("unknown-cell").length).toBeGreaterThan(0);
    });
    fireEvent.click(screen.getAllByTestId("unknown-cell")[0] as HTMLElement);
    const pop = await screen.findByTestId("why-popover");
    const line = within(pop).getAllByTestId("why-row")[0] as HTMLElement;
    expect(line.textContent).not.toMatch(/buildTable|fieldColumn|columnCall|\bsite\b ·/);
    // C1: the popover's column cannot grow past its box.
    expect(pop.className).toContain("grid-cols-[minmax(0,1fr)]");
    expect(pop.className).toContain("overflow-x-hidden");
  });
});

/** grid renders a ColumnGrid over crafted columns. */
function grid(cols: { name: string; attrs: Record<string, unknown>; links?: GridColumn["links"] }[], completeness: string, opts: { model?: boolean; visible?: (id: string) => boolean; hiddenTest?: (id: string) => boolean } = {}) {
  const columns: GridColumn[] = cols.map((c, i) => ({
    node: { identity: `column:t.sql#t.${c.name}`, kind: "column", name: c.name, file: "t.sql", span: { start_line: i + 1 }, attributes: { name: c.name, position: i + 1, nullable: true, primary_key: false, unique: false, ...c.attrs }, ide: null } as unknown as PresentedNode,
    default_state: "unknown",
    links: c.links ?? [],
    claims: null,
  }));
  const why: GridWhy = { table: "t", source: opts.model === true ? "models" : "migrations", restsOn: [], reasons: [], columnReasons: () => [] };
  render(
    <ColumnGrid
      columns={columns}
      completeness={completeness}
      names={buildNames(setsGolden("models"))}
      changed={new Set()}
      onTable={vi.fn()}
      visible={opts.visible ?? (() => true)}
      {...(opts.hiddenTest !== undefined ? { hiddenTest: opts.hiddenTest } : {})}
      model={opts.model === true}
      why={why}
    />,
  );
}

describe("C5: empty FK cells only when proven", () => {
  it("a migration column on a complete table with references null is empty", () => {
    grid([{ name: "a", attrs: { references: null } }], "complete");
    expect(within(row("a")).getByTestId("cell-refs").textContent).toBe("");
    expect(within(row("a")).getByTestId("cell-keys").textContent).toBe("");
  });
  it("on a partial table, or without the attribute, it reads unknown with a popover", () => {
    grid(
      [
        { name: "a", attrs: { references: null } },
        { name: "b", attrs: {} },
      ],
      "partial",
    );
    for (const n of ["a", "b"]) {
      expect(within(row(n)).getByTestId("cell-refs").textContent).toContain("unknown");
      expect(within(row(n)).getByTestId("cell-keys").textContent).toContain("FK?");
      const trigger = within(within(row(n)).getByTestId("cell-refs")).getByTestId("unknown-cell");
      expect(trigger.getAttribute("aria-haspopup")).toBe("dialog");
    }
  });
  it("a model column: [] is empty, null is unknown", () => {
    grid(
      [
        { name: "a", attrs: { source: "model", foreign_keys: [] } },
        { name: "b", attrs: { source: "model", foreign_keys: null } },
      ],
      "complete",
      { model: true },
    );
    expect(within(row("a")).getByTestId("cell-refs").textContent).toBe("");
    expect(within(row("b")).getByTestId("cell-refs").textContent).toContain("unknown");
    expect(within(row("b")).getByTestId("cell-keys").textContent).toContain("FK?");
  });
});

describe("C7: a link to a table Include tests hides", () => {
  it("carries the visible 'test, hidden' mark, not only a tooltip", () => {
    const target = "table:app/models.py#Team";
    const links = [{ target_column: "column:app/models.py#Team.id", target_table: target }];
    grid([{ name: "a", attrs: { source: "model", foreign_keys: [{ table: "team", column: "id", status: "resolved" }] }, links }], "complete", { model: true, visible: () => false, hiddenTest: (id) => id === target });
    expect(within(row("a")).getByTestId("test-hidden-mark").textContent).toBe("test, hidden");
  });
  it("a set hidden for another reason gets no test mark", () => {
    const links = [{ target_column: "column:app/models.py#Team.id", target_table: "table:app/models.py#Team" }];
    grid([{ name: "a", attrs: { source: "model", foreign_keys: [{ table: "team", column: "id", status: "resolved" }] }, links }], "complete", { model: true, visible: () => false, hiddenTest: () => false });
    expect(within(row("a")).queryByTestId("test-hidden-mark")).toBeNull();
  });
});

describe("C8: the strip says when more lines follow", () => {
  const sizes = { scrollHeight: 0, clientHeight: 0 };
  let restore: (() => void) | null = null;
  beforeEach(() => {
    const protoDesc = (k: "scrollHeight" | "clientHeight") => Object.getOwnPropertyDescriptor(HTMLElement.prototype, k);
    const saved = { scrollHeight: protoDesc("scrollHeight"), clientHeight: protoDesc("clientHeight") };
    for (const k of ["scrollHeight", "clientHeight"] as const) {
      Object.defineProperty(HTMLElement.prototype, k, {
        configurable: true,
        get(this: HTMLElement) {
          return this.getAttribute("data-testid") === "strip-info" ? sizes[k] : 0;
        },
      });
    }
    restore = () => {
      for (const k of ["scrollHeight", "clientHeight"] as const) {
        const d = saved[k];
        if (d !== undefined) Object.defineProperty(HTMLElement.prototype, k, d);
        else Reflect.deleteProperty(HTMLElement.prototype, k);
      }
    };
  });
  afterEach(() => {
    restore?.();
  });

  it("shows a fade and a 'more' button only when the lines overflow", async () => {
    sizes.scrollHeight = 300;
    sizes.clientHeight = 110;
    const scrollBy = vi.fn();
    Object.defineProperty(HTMLElement.prototype, "scrollBy", { configurable: true, value: scrollBy });
    show(`#/schema?set=${enc("@models/sqlmodel/.")}`, modelsClient(undefined, true));
    const more = await screen.findByTestId("strip-more");
    expect(screen.getByTestId("strip-fade")).toBeTruthy();
    fireEvent.click(more);
    expect(scrollBy).toHaveBeenCalled();
  });

  it("shows nothing when the lines fit", async () => {
    sizes.scrollHeight = 100;
    sizes.clientHeight = 110;
    show(`#/schema?set=${enc("@models/sqlmodel/.")}`, modelsClient(undefined, true));
    await screen.findByTestId("strip-info");
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByTestId("strip-more")).toBeNull();
  });
});
