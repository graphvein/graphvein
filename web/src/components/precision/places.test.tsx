// T020: the chips in each place Variant A puts them, from the
// server goldens (built-in index: labels; external snapshot: no scope
// chips), and the O13 rule that a Precise scope chip never hides a fact's
// `unverified` chip.
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { parseItem } from "@/api/changes";
import type { ScopeLabel } from "@/api/endpoints";
import type { Status } from "@/api/status";
import { CodePeek } from "@/components/CodePeek";
import type { StatusFetcher } from "@/hooks/useStatus";
import { EndpointDetailView } from "@/sections/api/EndpointDetail";
import { Legend } from "@/sections/api/flow/Legend";
import { SummaryStrip } from "@/sections/api/SummaryStrip";
import { CoverageCards } from "@/sections/api/CoverageCards";
import { ItemDetail } from "@/sections/changes/ItemDetail";
import { AppShell } from "@/shell/AppShell";
import { detailGolden, endpointsGolden, fakeClient } from "@/test/apiFixtures";
import { rawItems } from "@/test/changesFixtures";
import { goldenStatus } from "@/test/fixtures";
import { fakeSchemaClient, setsGolden, summaryGolden, tableGolden } from "@/test/schemaFixtures";
import { flowGolden } from "@/test/flowFixtures";
import { goldenNode, sourceBody, stubSource } from "@/test/sourceFixtures";

const fresh: StatusFetcher = () => Promise.resolve<Status>(goldenStatus("fresh"));
const enc = encodeURIComponent;
const actions = { openGap: vi.fn(), setRoot: vi.fn(), activeGap: null, root: null };

function chipText(id: string): string[] {
  return screen.queryAllByTestId(id).map((c) => `${c.textContent} [${c.dataset.label ?? ""}]`);
}

describe("API section", () => {
  it("coverage cards: Routes and Client calls, Best effort from row D", () => {
    const { transports } = endpointsGolden("api-endpoints-all");
    render(<CoverageCards transports={transports} repository="fixture" scope="all" actions={actions} />);
    expect(chipText("precision-endpoints")).toEqual(["Routes·Best effort [best_effort]", "IPC commands·Best effort [best_effort]", "Server actions·Best effort [best_effort]"]);
    expect(chipText("precision-calls")).toEqual(["Client calls·Best effort [best_effort]", "IPC calls·Best effort [best_effort]", "Action uses·Best effort [best_effort]"]);
  });

  it("the transport strips leave the chips to the cards behind the disclosure (T065)", () => {
    const { transports } = endpointsGolden("api-endpoints-all");
    render(<SummaryStrip transports={transports} scope="all" actions={actions} open={false} onOpen={vi.fn()} />);
    expect(screen.getAllByTestId(/^strip-/).length).toBeGreaterThan(0);
    expect(screen.queryByTestId("precision-endpoints")).toBeNull();
  });

  it("an external run shows no scope chip", () => {
    const { transports } = endpointsGolden("api-statuses-endpoints");
    expect(transports[0]?.precision?.reason).toBe("external_indexer");
    render(<CoverageCards transports={transports} repository="fixture" scope="all" actions={actions} />);
    expect(screen.queryByTestId("precision-endpoints")).toBeNull();
    expect(screen.queryByTestId("precision-calls")).toBeNull();
  });

  it("endpoint detail and callers: the route family and its calls", () => {
    const d = detailGolden("api-endpoint-route");
    const all = endpointsGolden("api-endpoints-all");
    render(<EndpointDetailView detail={d} summary={all.transports[0]} openGap={vi.fn()} tab="callers" />);
    expect(chipText("detail-precision")).toEqual(["Routes·Best effort [best_effort]"]);
    expect(chipText("callers-precision")).toEqual(["Client calls·Best effort [best_effort]"]);
  });

  it("endpoint detail of an external run: no chip", () => {
    const d = detailGolden("api-statuses-endpoint-route");
    render(<EndpointDetailView detail={d} summary={undefined} openGap={vi.fn()} tab="callers" />);
    expect(screen.queryByTestId("detail-precision")).toBeNull();
    expect(screen.queryByTestId("callers-precision")).toBeNull();
  });

  it("flow legend: one Precision line with both families; none without labels", () => {
    const label: ScopeLabel = { label: "best_effort", row: "D", reason: "default_branch" };
    const { unmount } = render(<Legend counts={new Map()} tiers={new Set()} onToggle={vi.fn()} onGap={vi.fn()} transport="http" precision={label} callersPrecision={label} defaultOpen />);
    // T073: the strip shows the calls' label (the route's is in the
    // header); the full legend has both families.
    expect(screen.getByTestId("flow-legend-precision").textContent).toContain("Precision");
    expect(chipText("flow-callers-precision")).toEqual(["Client calls·Best effort [best_effort]"]);
    expect(screen.getByTestId("flow-legend-precision-all").textContent).toContain("Precision");
    expect(chipText("flow-precision")).toEqual(["Routes·Best effort [best_effort]"]);
    unmount();
    render(<Legend counts={new Map()} tiers={new Set()} onToggle={vi.fn()} onGap={vi.fn()} defaultOpen />);
    expect(screen.queryByTestId("flow-legend-precision")).toBeNull();
    expect(screen.queryByTestId("flow-legend-precision-all")).toBeNull();
  });

  it("flow legend: the code calls chip from the server's code_calls_precision (T020 O-S1-5)", () => {
    const f = flowGolden("flow-route");
    expect(f.code_calls_precision).toEqual({ label: "best_effort", row: "D", reason: "default_branch" });
    render(<Legend counts={new Map()} tiers={new Set()} onToggle={vi.fn()} onGap={vi.fn()} transport={f.transport} precision={f.precision} callersPrecision={f.callers_precision} codeCallsPrecision={f.code_calls_precision} />);
    expect(chipText("flow-code-calls-precision")).toEqual(["Code calls·Best effort [best_effort]"]);
  });

  it("flow legend of an external run: labels without chips, so no Precision heading", () => {
    const external: ScopeLabel = { label: "best_effort", row: "D", reason: "external_indexer" };
    render(<Legend counts={new Map()} tiers={new Set()} onToggle={vi.fn()} onGap={vi.fn()} transport="http" precision={external} callersPrecision={external} defaultOpen />);
    expect(screen.queryByTestId("flow-legend-precision")).toBeNull();
    expect(screen.queryByTestId("flow-legend-precision-all")).toBeNull();
    expect(screen.queryByText("Precision")).toBeNull();
  });
});

describe("code peek", () => {
  async function peek(file: Record<string, unknown>, extra: Record<string, unknown> = {}) {
    const body = sourceBody() as { source: { file: Record<string, unknown> } };
    Object.assign(body.source.file, file);
    Object.assign(body, extra);
    render(<CodePeek id={goldenNode().identity as string} client={stubSource(() => body)} />);
    const panel = await screen.findByTestId("code-peek");
    await waitFor(() => {
      expect(panel.getAttribute("data-state")).not.toBe("loading");
    });
    return panel;
  }

  it("names the file's language and its label", async () => {
    const panel = await peek({ language: "typescript", precision: "best_effort", precision_reason: "default_branch", reported_precision: null });
    expect(within(panel).getByTestId("peek-precision").textContent).toBe("typescript·Best effort");
  });

  it("an external run: reported by the indexer, never Graphvein's label", async () => {
    const panel = await peek({ language: "typescript", precision: "best_effort", precision_reason: "external_indexer", reported_precision: "precise" });
    const chip = within(panel).getByTestId("peek-precision");
    expect(chip.dataset.label).toBe("reported");
    expect(chip.textContent).toMatch(/^typescript·reported by .+: precise$/);
  });

  it("no file record or an older server: no chip", async () => {
    const panel = await peek({ language: null, precision: null, precision_reason: null, reported_precision: null });
    expect(within(panel).queryByTestId("peek-precision")).toBeNull();
  });

  it("a function: the code calls chip stays Best effort beside a Precise file chip (T020 O-S1-5)", async () => {
    const panel = await peek(
      { language: "go", precision: "precise", precision_reason: null, precision_row: "PL-L1", reported_precision: null },
      { code_calls_precision: { label: "best_effort", row: "D", reason: "default_branch" } },
    );
    const file = within(panel).getByTestId("peek-precision");
    expect(file.dataset.label).toBe("precise");
    expect(file.textContent).toBe("go·Precise");
    expect(file.dataset.row).toBe("PL-L1");
    const calls = within(panel).getByTestId("peek-calls-precision");
    expect(calls.textContent).toBe("Code calls·Best effort");
    expect(calls.dataset.label).toBe("best_effort");
  });

  it("a Go file whose structure was refused: Best effort, naming row PL-L1 (T020 S1)", async () => {
    const panel = await peek({ language: "go", precision: "best_effort", precision_reason: "structure_refused", precision_row: "PL-L1", reported_precision: null });
    const chip = within(panel).getByTestId("peek-precision");
    expect(chip.textContent).toBe("go·Best effort");
    expect(chip.dataset.label).toBe("best_effort");
    expect(chip.dataset.row).toBe("PL-L1");
  });

  it("a Rust file no crate root reaches: Best effort, naming row PL-L4 and its reason (T020 S4)", async () => {
    const panel = await peek({ language: "rust", precision: "best_effort", precision_reason: "structure_unrooted", precision_row: "PL-L4", reported_precision: null });
    const chip = within(panel).getByTestId("peek-precision");
    expect(chip.textContent).toBe("rust·Best effort");
    expect(chip.dataset.label).toBe("best_effort");
    expect(chip.dataset.row).toBe("PL-L4");
  });

  it("no code calls label (another node kind, an older server): no calls chip", async () => {
    const panel = await peek({ language: "go", precision: "precise", precision_reason: null, precision_row: "PL-L1", reported_precision: null });
    expect(within(panel).queryByTestId("peek-calls-precision")).toBeNull();
  });
});

describe("Changes item", () => {
  // crafted (extends the previous-rev1-rev2 change-impact golden, a CLI
  // golden without the server's labels): the `precision` object as
  // /changes/items serves it (docs/cli/serve.md).
  function item(precision: unknown) {
    const raw = rawItems("previous-rev1-rev2").find((i) => Array.isArray(i.fields) && (i.fields as unknown[]).length > 0);
    if (raw === undefined) throw new Error("no item with fields");
    const field = ((raw.fields as { name: string }[])[0] ?? { name: "x" }).name;
    return { it: parseItem({ ...raw, precision: typeof precision === "function" ? (precision as (f: string) => unknown)(field) : precision }, "item"), field };
  }

  it("the scope and file chips, and a field chip only when its family differs", () => {
    const { it: i, field } = item((f: string) => ({
      file: { language: "typescript", precision: "best_effort", precision_reason: "default_branch", reported_precision: null },
      scope: { label: "best_effort", row: "D", reason: "default_branch" },
      fields: { [f]: { label: "precise", row: "PL-F9", reason: null } },
    }));
    render(<ItemDetail item={i} tab="change" depth={3} onTab={vi.fn()} onDepth={vi.fn()} onOpen={vi.fn()} back={null} />);
    expect(screen.getByTestId("item-scope-precision").dataset.label).toBe("best_effort");
    expect(screen.getByTestId("item-file-precision").textContent).toBe("typescript·Best effort");
    const fieldChip = screen.getByTestId("field-precision");
    expect(fieldChip.textContent).toBe(`${field}·Precise`);
    expect(fieldChip.dataset.row).toBe("PL-F9");
  });

  it("a refused base side: the scope chip says the base is refused", async () => {
    const mismatch = { label: "best_effort", row: "PL-F1", reason: "extractor_mismatch" };
    const { it: i } = item((f: string) => ({
      file: { language: "sql", precision: "best_effort", precision_reason: "default_branch", reported_precision: null },
      scope: mismatch,
      fields: { [f]: mismatch },
      scope_side: "base",
    }));
    expect(i.precision?.scope_side).toBe("base");
    render(<ItemDetail item={i} tab="change" depth={3} onTab={vi.fn()} onDepth={vi.fn()} onOpen={vi.fn()} back={null} />);
    const chip = screen.getByTestId("item-scope-precision");
    act(() => {
      chip.focus();
    });
    const tip = (await screen.findAllByTestId("precision-tip"))[0];
    expect(tip?.textContent).toContain("base side");
    expect(tip?.textContent).not.toContain("Labels return after graphvein index");
  });

  it("no label from the server: no chip", () => {
    const { it: i } = item(undefined);
    render(<ItemDetail item={i} tab="change" depth={3} onTab={vi.fn()} onDepth={vi.fn()} onOpen={vi.fn()} back={null} />);
    expect(screen.queryByTestId("item-scope-precision")).toBeNull();
    expect(screen.queryByTestId("item-file-precision")).toBeNull();
    expect(screen.queryByTestId("field-precision")).toBeNull();
  });
});

describe("Schema section", () => {
  const ORDER = "table:app/models.go#Order";

  function showDialects(label: ScopeLabel | null) {
    const sets = setsGolden("model_dialects");
    const table = tableGolden("schema-table-model-dialects");
    if (label !== null) {
      // crafted: the model-dialects goldens are an external snapshot (no
      // scope label); this is the label B2's ScopeLabel gives a gorm group
      // of a built-in run at this binary's version (row PL-F7).
      for (const g of sets.sets) g.precision = label;
      table.precision = label;
    }
    const client = fakeSchemaClient({ summary: summaryGolden("models"), sets, tables: { [ORDER]: table } });
    window.history.replaceState(null, "", `#/schema?set=${enc("@models/synthetic/app")}&sel=${enc(ORDER)}`);
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
  }

  it("O13: a Precise scope chip keeps the unverified chip of mapped facts, scope chip first", async () => {
    showDialects({ label: "precise", row: "PL-F7", reason: null });
    const header = await screen.findByTestId("detail-header");
    const scope = await within(header).findByTestId("table-precision");
    expect(scope.dataset.label).toBe("precise");
    expect(scope.dataset.row).toBe("PL-F7");
    const unverified = within(header).getByTestId("table-unverified-chip");
    expect(unverified.textContent).toBe("unverified");
    // Order on the line: the scope chip, then the fact label; never merged.
    expect(scope.compareDocumentPosition(unverified) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(scope.contains(unverified)).toBe(false);
    expect(screen.getByTestId("card-precision").dataset.label).toBe("precise");
    // C3: the card says some facts are mapped, beside the chip.
    expect(screen.getByTestId("card-dialect-hint").textContent).toBe("mapped: unverified");
  });

  it("the real external golden: no scope chip, the unverified chip stays", async () => {
    showDialects(null);
    const header = await screen.findByTestId("detail-header");
    await within(header).findByTestId("table-unverified-chip");
    expect(within(header).queryByTestId("table-precision")).toBeNull();
    expect(screen.queryByTestId("card-precision")).toBeNull();
    expect(screen.queryByTestId("card-dialect-hint")).toBeNull();
  });

  it("a built-in index: the SQLModel group is Precise (PL-F2), the migration set PL-F1", async () => {
    const sets = setsGolden("models");
    const client = fakeSchemaClient({ summary: summaryGolden("models"), sets });
    window.history.replaceState(null, "", "#/schema");
    render(<AppShell fetcher={fresh} client={fakeClient()} schemaClient={client} />);
    await waitFor(() => {
      expect(screen.getAllByTestId("card-precision").length).toBeGreaterThan(0);
    });
    const rows = screen.getAllByTestId("card-precision").map((c) => c.dataset.row);
    expect(rows.sort()).toEqual(["PL-F1", "PL-F2"]);
  });
});
