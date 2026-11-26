// T020: the precision chip in every state the API can send
// (precise, best effort, refused, external, absent), its tooltip, and the
// Evidence popover's keyboard path and coverage table, against the
// server's /precision goldens.
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import precisionApi from "../../../../internal/server/testdata/precision-api.golden.json";
import precisionMismatch from "../../../../internal/server/testdata/precision-mismatch.golden.json";
import precisionModels from "../../../../internal/server/testdata/precision-models.golden.json";
import precisionReasons from "../../../../internal/server/testdata/precision-reasons.golden.json";
import precisionStates from "../../../../internal/server/testdata/precision-states.golden.json";

import { parseFileLabel, parseScopeLabel, type FileLabel, type ScopeLabel } from "@/api/endpoints";
import { parsePrecision, type PrecisionLoader, type PrecisionResponse } from "@/api/precision";
import { layoutOf } from "@/components/precision/CoverageTable";
import { PrecisionChip } from "@/components/precision/PrecisionChip";
import { PrecisionLoaderContext, type Focus } from "@/components/precision/parts";
import { BEST_EFFORT_TEXT, bestEffortLead, fileChip, REASON_TEXT, reasonShort, reasonText, REFUSED_TEXT, scopeChip, type ChipState } from "@/components/precision/words";

const GOLDENS = { api: precisionApi, mismatch: precisionMismatch, models: precisionModels, states: precisionStates };

function golden(name: keyof typeof GOLDENS): PrecisionResponse {
  return parsePrecision(structuredClone(GOLDENS[name]));
}

function show(state: ChipState, data: PrecisionResponse, opts: { scope?: string; focus?: Focus; indexer?: string } = {}) {
  let calls = 0;
  const load: PrecisionLoader = () => {
    calls++;
    return Promise.resolve(data);
  };
  render(
    <PrecisionLoaderContext.Provider value={load}>
      <button type="button">before</button>
      <PrecisionChip state={state} scope={opts.scope ?? "sqlmodel models"} focus={opts.focus ?? {}} indexer={opts.indexer ?? null} />
    </PrecisionLoaderContext.Provider>,
  );
  return { calls: () => calls };
}

const PRECISE: ScopeLabel = { label: "precise", row: "PL-F2", reason: null };
const DEFAULT: ScopeLabel = { label: "best_effort", row: "D", reason: "default_branch" };
const MISMATCH: ScopeLabel = { label: "best_effort", row: "PL-F2", reason: "extractor_mismatch" };
const EXTERNAL: ScopeLabel = { label: "best_effort", row: "D", reason: "external_indexer" };

describe("chip states (words.ts reads only what the server sent)", () => {
  it("maps every scope label the API can send", () => {
    expect(scopeChip(PRECISE)).toEqual({ kind: "precise", row: "PL-F2" });
    expect(scopeChip(DEFAULT)).toEqual({ kind: "best_effort", row: "D", reason: "default_branch", side: null });
    expect(scopeChip(MISMATCH)).toEqual({ kind: "best_effort", row: "PL-F2", reason: "extractor_mismatch", side: null });
    expect(scopeChip(MISMATCH, "base")).toEqual({ kind: "best_effort", row: "PL-F2", reason: "extractor_mismatch", side: "base" });
    // External run (O6 a): no scope chip at all.
    expect(scopeChip(EXTERNAL)).toEqual({ kind: "none" });
    // Absent: no label, no chip.
    expect(scopeChip(null)).toEqual({ kind: "none" });
    expect(scopeChip(undefined)).toEqual({ kind: "none" });
  });

  it("never shows a label word it does not know", () => {
    expect(parseScopeLabel({ label: "verified", row: "PL-F2", reason: null }, "x")).toBeNull();
    expect(parseScopeLabel(undefined, "x")).toBeNull();
    const f = parseFileLabel({ language: "go", precision: "certain", precision_reason: null, reported_precision: null }, "x");
    expect(fileChip(f)).toEqual({ kind: "none" });
  });

  it("maps every file label the API can send", () => {
    const file = (p: Partial<FileLabel>): FileLabel => ({ language: "python", precision: "best_effort", reason: "default_branch", row: "D", reported: null, ...p });
    expect(fileChip(file({}))).toEqual({ kind: "best_effort", row: null, reason: "default_branch", side: null });
    // Go (T020 S1, row PL-L1): the server names the row, so the popover
    // shows its evidence; a file whose structure was refused names the row
    // that would apply.
    expect(fileChip(file({ language: "go", precision: "precise", reason: null, row: "PL-L1" }))).toEqual({ kind: "precise", row: "PL-L1" });
    expect(fileChip(file({ language: "go", reason: "structure_refused", row: "PL-L1" }))).toEqual({ kind: "best_effort", row: "PL-L1", reason: "structure_refused", side: null });
    // TypeScript, TSX and JavaScript (T020 S3, row PL-L3).
    expect(fileChip(file({ language: "tsx", precision: "precise", reason: null, row: "PL-L3" }))).toEqual({ kind: "precise", row: "PL-L3" });
    expect(fileChip(file({ language: "typescript", reason: "structure_refused", row: "PL-L3" }))).toEqual({ kind: "best_effort", row: "PL-L3", reason: "structure_refused", side: null });
    // Rust (T020 S4, row PL-L4): a file no crate root reaches has a reason
    // of its own.
    expect(fileChip(file({ language: "rust", precision: "precise", reason: null, row: "PL-L4" }))).toEqual({ kind: "precise", row: "PL-L4" });
    expect(fileChip(file({ language: "rust", reason: "structure_unrooted", row: "PL-L4" }))).toEqual({ kind: "best_effort", row: "PL-L4", reason: "structure_unrooted", side: null });
    // An older server sends no row.
    expect(fileChip(file({ precision: "precise", reason: null, row: null }))).toEqual({ kind: "precise", row: null });
    expect(fileChip(file({ reason: "extractor_mismatch", row: null }))).toEqual({ kind: "best_effort", row: null, reason: "extractor_mismatch", side: null });
    // External: only the producer's own value, as "reported".
    expect(fileChip(file({ reason: "external_indexer", reported: "precise" }))).toEqual({ kind: "reported", value: "precise" });
    expect(fileChip(file({ reason: "external_indexer", reported: null }))).toEqual({ kind: "none" });
    // No file record: all five null.
    expect(fileChip({ language: null, precision: null, reason: null, row: null, reported: null })).toEqual({ kind: "none" });
    expect(fileChip(null)).toEqual({ kind: "none" });
  });
});

describe("reason words", () => {
  const reasons: string[] = precisionReasons;

  it("every reason the server can send has its own words, and no other reason does", () => {
    expect(Object.keys(REASON_TEXT).sort()).toEqual([...reasons].sort());
    for (const r of reasons) {
      expect(reasonText(r), r).toBe(REASON_TEXT[r]);
      expect(reasonText(r), r).not.toContain("Reason:");
    }
  });

  it("only the default branch says there is no claim contract", () => {
    for (const r of reasons) {
      const lead = bestEffortLead(r);
      if (r === "default_branch") {
        expect(lead).toBe(BEST_EFFORT_TEXT);
      } else {
        expect(lead, r).toBe(REFUSED_TEXT);
        expect(lead, r).not.toMatch(/claim contract/);
      }
    }
    expect(bestEffortLead(null)).toBe(BEST_EFFORT_TEXT);
  });

  it("a refused base side is named, and reindexing is not offered as the fix", () => {
    for (const r of reasons) {
      expect(reasonText(r, "base"), r).not.toContain("Labels return after graphvein index");
    }
    expect(reasonText("extractor_mismatch", "head")).toContain("Labels return after graphvein index");
    expect(reasonText("extractor_mismatch", "base")).toContain("base side");
    expect(reasonShort("extractor_mismatch", "base")).toBe("refusal: extractor mismatch (base side)");
  });
});

describe("PrecisionChip", () => {
  it("precise: the label, the tooltip on focus, never 'complete'", async () => {
    show(scopeChip(PRECISE), golden("models"));
    const chip = screen.getByTestId("precision-chip");
    expect(chip.textContent).toBe("sqlmodel models·Precise");
    expect(chip.dataset.label).toBe("precise");
    expect(chip.getAttribute("aria-label")).toBe("sqlmodel models: Precise. Show evidence");
    act(() => {
      chip.focus();
    });
    const tip = (await screen.findAllByTestId("precision-tip"))[0];
    expect(tip?.textContent).toContain("Precise does not mean complete.");
    expect(tip?.textContent).toContain("Click or Enter for evidence · row PL-F2");
  });

  it("best effort: the default branch in the tooltip", async () => {
    show(scopeChip(DEFAULT), golden("api"), { scope: "Routes" });
    const chip = screen.getByTestId("precision-chip");
    expect(chip.textContent).toBe("Routes·Best effort");
    expect(chip.dataset.label).toBe("best_effort");
    act(() => {
      chip.focus();
    });
    const tip = (await screen.findAllByTestId("precision-tip"))[0];
    expect(tip?.textContent).toContain("a missing item does not mean it is absent");
    expect(tip?.textContent).toContain("default branch D");
  });

  it("refused: an extractor mismatch is Best effort with the reason in the tooltip and the popover", async () => {
    show(scopeChip(MISMATCH), golden("mismatch"));
    const chip = screen.getByTestId("precision-chip");
    expect(chip.dataset.label).toBe("best_effort");
    act(() => {
      chip.focus();
    });
    const tip = (await screen.findAllByTestId("precision-tip"))[0];
    expect(tip?.textContent).toContain("another extractor version");
    expect(tip?.textContent).toContain("refusal: extractor mismatch");
    fireEvent.click(chip);
    const pop = await screen.findByTestId("precision-popover");
    expect(within(pop).getByTestId("precision-why").textContent).toContain("another extractor version");
    await waitFor(() => {
      expect(within(pop).getByTestId("precision-run").textContent).toContain("Indexed with extractors-0; this binary has extractors-62");
    });
    expect(within(pop).getByTestId("precision-refused-row").textContent).toContain("Row PL-F2");
    // No column is labelled Precise in a refused run.
    expect(within(pop).queryByText("Precise", { selector: "th *" })).toBeNull();
  });

  it("refused by a conflict: the lead makes no claim about a contract", async () => {
    show(scopeChip({ label: "best_effort", row: "D", reason: "marker_conflict" }), golden("models"));
    const chip = screen.getByTestId("precision-chip");
    act(() => {
      chip.focus();
    });
    const tip = (await screen.findAllByTestId("precision-tip"))[0];
    expect(tip?.textContent).toContain(REFUSED_TEXT);
    expect(tip?.textContent).not.toContain("without a claim contract");
    expect(tip?.textContent).toContain("more than one tool or dialect");
    fireEvent.click(chip);
    const pop = await screen.findByTestId("precision-popover");
    expect(pop.textContent).not.toContain("without a claim contract");
    expect(within(pop).getByTestId("precision-why").textContent).toContain("more than one tool or dialect");
  });

  it("refused base side: the popover says the base is refused and reindexing does not relabel it", async () => {
    show(scopeChip(MISMATCH, "base"), golden("models"));
    fireEvent.click(screen.getByTestId("precision-chip"));
    const pop = await screen.findByTestId("precision-popover");
    const why = within(pop).getByTestId("precision-why").textContent;
    expect(why).toContain("base side");
    expect(why).not.toContain("Labels return after graphvein index");
  });

  it("mismatch: files are counted by Graphvein's label, not the stored one", () => {
    const data = golden("mismatch");
    for (const l of data.files?.languages ?? []) {
      expect(Object.keys(l.by_label)).toEqual(["best_effort"]);
      expect(l.reported_by_label).toBeNull();
    }
  });

  it("external: a file label only as reported by the indexer, its files not checked", async () => {
    const f = parseFileLabel({ language: "sql", precision: "best_effort", precision_reason: "external_indexer", reported_precision: "precise" }, "x");
    show(fileChip(f), golden("states"), { scope: "sql", indexer: "crafted", focus: { language: "sql" } });
    const chip = screen.getByTestId("precision-chip");
    expect(chip.dataset.label).toBe("reported");
    expect(chip.textContent).toBe("sql·reported by crafted: precise");
    fireEvent.click(chip);
    const pop = await screen.findByTestId("precision-popover");
    const table = await within(pop).findByTestId("precision-coverage");
    expect(table.dataset.mode).toBe("reported");
    expect(table.textContent).toContain("(not checked)");
    // The producer's values come from reported_by_label, never by_label
    // (Graphvein's label: best effort for an external run).
    expect(golden("states").files?.languages.map((l) => l.by_label)).toEqual([{ best_effort: 11 }, { best_effort: 10 }]);
    expect(table.querySelector('[data-language="sql"]')?.textContent).toContain("11 precise (not checked)");
    expect(within(pop).getByTestId("precision-run").textContent).toContain("Graphvein did not check them");
  });

  it("absent: no chip where the API sends no label", () => {
    show(scopeChip(null), golden("models"));
    expect(screen.queryByTestId("precision-chip")).toBeNull();
    show(scopeChip(EXTERNAL), golden("states"));
    expect(screen.queryByTestId("precision-chip")).toBeNull();
  });

  it("keyboard: Tab reaches the chip, Enter opens the popover with focus inside, Escape closes it and returns focus", async () => {
    const { calls } = show(scopeChip(PRECISE), golden("models"), { focus: { column: "PL-F2" } });
    const before = screen.getByText("before");
    act(() => {
      before.focus();
    });
    // jsdom has no Tab order: the chip is the next button, focus it as Tab would.
    const chip = screen.getByTestId("precision-chip");
    expect(chip.tagName).toBe("BUTTON");
    expect(chip.tabIndex).toBe(0);
    act(() => {
      chip.focus();
    });
    // Enter on a button is a click.
    fireEvent.keyDown(chip, { key: "Enter" });
    fireEvent.click(chip);
    const pop = await screen.findByTestId("precision-popover");
    await waitFor(() => {
      expect(pop.contains(document.activeElement)).toBe(true);
    });
    expect(chip.getAttribute("aria-expanded")).toBe("true");
    const evidence = await within(pop).findByTestId("precision-evidence");
    expect(evidence.textContent).toContain("TestModelClaimContract");
    expect(evidence.textContent).toContain("TestModelOracle");
    expect(within(pop).getByTestId("precision-foot").textContent).toContain("Precise never means complete");
    expect(calls()).toBe(1);
    fireEvent.keyDown(document.activeElement ?? pop, { key: "Escape" });
    await waitFor(() => {
      expect(screen.queryByTestId("precision-popover")).toBeNull();
    });
    expect(document.activeElement).toBe(chip);
  });

  it("the close button closes the popover", async () => {
    show(scopeChip(DEFAULT), golden("api"), { scope: "Routes", focus: { column: "routes" } });
    fireEvent.click(screen.getByTestId("precision-chip"));
    const pop = await screen.findByTestId("precision-popover");
    fireEvent.click(within(pop).getByTestId("precision-close"));
    await waitFor(() => {
      expect(screen.queryByTestId("precision-popover")).toBeNull();
    });
  });
});

describe("coverage table", () => {
  it("languages and families come from the API; zero cells are dimmed; no percentage", async () => {
    const data = golden("models");
    show(scopeChip(PRECISE), data, { focus: { column: "PL-F2", language: "python" } });
    fireEvent.click(screen.getByTestId("precision-chip"));
    const pop = await screen.findByTestId("precision-popover");
    const table = await within(pop).findByTestId("precision-coverage");
    // Rows: the languages /precision lists with facts, the others folded.
    const langs = [...table.querySelectorAll("tbody tr")].map((r) => (r as HTMLElement).dataset.language);
    expect(langs).toEqual(["python", "sql", "files-only"]);
    expect(within(table).getByText("markdown, toml")).toBeTruthy();
    // Columns: the families with facts here (PL-F1, PL-F2 and the model
    // fixture's one code call); the rest are named in the note.
    const cols = [...table.querySelectorAll("thead th[data-column]")].map((c) => (c as HTMLElement).dataset.column);
    expect(cols).toEqual(["PL-F1", "PL-F2", "code_calls"]);
    expect(table.querySelector('[data-cell="python:PL-F2"]')?.textContent).toBe("19");
    const empty = table.querySelector<HTMLElement>('[data-cell="python:PL-F1"]');
    expect(empty?.dataset.empty).toBe("true");
    expect(empty?.textContent).toBe("0");
    const note = within(pop).getByTestId("precision-coverage-note").textContent;
    expect(note).toContain("No facts in this index: Prisma models (Precise, PL-F3)");
    expect(note).toContain("Routes (Best effort, D)");
    expect(pop.textContent).not.toMatch(/\d%/);
  });

  it("the focused column is kept even without facts", () => {
    const l = layoutOf(golden("models"), { column: "PL-F3" });
    expect(l.shown.map((c) => c.key)).toEqual(["PL-F1", "PL-F2", "PL-F3", "code_calls"]);
    expect(l.empty.map((c) => c.key)).not.toContain("PL-F3");
  });

  it("route and call families take columns where the index holds them", () => {
    const l = layoutOf(golden("api"), {});
    expect(l.shown.map((c) => c.key)).toEqual(["routes", "client_calls", "ipc_commands", "ipc_calls", "server_actions", "action_calls", "code_calls"]);
    expect(l.shown.every((c) => !c.precise)).toBe(true);
    expect(l.rows.map((r) => r.language)).toEqual(expect.arrayContaining(["typescript", "rust"]));
  });

  it("code calls take a Best effort row D column of edges, never Precise (T020 O-S1-5)", () => {
    const calls = layoutOf(golden("api"), {}).shown.find((c) => c.key === "code_calls");
    expect(calls).toMatchObject({ name: "Code calls", row: "D", precise: false });
    expect(calls?.facts).toEqual({ unit: "edges", total: 3, by_language: { tsx: 1, typescript: 2 } });
  });
});
