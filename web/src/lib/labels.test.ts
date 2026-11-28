import { describe, expect, it } from "vitest";

import { factLabel, label, LABELS, RANK_NOTE, rowSamples, SHAPE_NOTE, storedEdge } from "@/lib/labels";

// The rows of the T025 task record (internal), "Plainer default wording"
// (1-19), then the two leftovers T025b added (20-21, the Unknowns line),
// then project mode's rows (22-25, T022 B9), manifest editing's (26-27,
// T022 B10) and the sidebar member list's (28-30, T022 MR-UI), then the
// architecture section's rows (31-41, T026 AG-C1; 42-53, AG-C2), then the
// Flow tab's table chip and line (54-55, AG-F) and the database card's
// accessor counts (56), then the overview's links line and
// project heading (57-58, TOP-2 R1 of 2026-11-18), then the Explorer's rows
// (59-64, T029) and its Try it out (65-68, T029 part B), then the quieter
// API list, route header, callers and Flow legend strip (69-73, T073).
const T025_ROWS = Array.from({ length: 73 }, (_, i) => i + 1);

describe("label table (T025b)", () => {
  it("covers every row of the T025 table and the T025b additions exactly once, in both styles", () => {
    const samples = rowSamples();
    expect(samples.map((s) => s.row)).toEqual(T025_ROWS);
    for (const s of samples) {
      // Both variants exist; a tag the plain style hides (rows 11-13) and
      // the plain row 19 lead have empty text, every other text is set.
      expect(typeof s.plain.text, `row ${String(s.row)} plain`).toBe("string");
      expect(s.technical.text, `row ${String(s.row)} technical`).not.toBe("");
      if (![11, 12, 13, 19, 24, 64].includes(s.row)) expect(s.plain.text, `row ${String(s.row)} plain`).not.toBe("");
      // The styles differ, except where the table says they are the same.
      if (s.same) expect(s.technical.text, `row ${String(s.row)}`).toBe(s.plain.text);
      else expect(s.technical.text, `row ${String(s.row)}`).not.toBe(s.plain.text);
      // Tooltips stay in both styles (the footnote and the row 19 lead
      // are prose with no tooltip).
      if (![16, 19].includes(s.row)) {
        expect(s.plain.title, `row ${String(s.row)} plain title`).not.toBe("");
        expect(s.technical.title, `row ${String(s.row)} technical title`).not.toBe("");
      }
    }
    // Only rows 9 and 36 (the level tag) are declared the same; row 4 reads
    // "Inferred · 0.9" in the plain style since T041.
    expect(samples.filter((s) => s.same).map((s) => s.row)).toEqual([9, 36]);
  });

  it("plain is T025's New column and technical its Old column", () => {
    const byRow = new Map(rowSamples().map((s) => [s.row, s]));
    const pair = (row: number) => {
      const s = byRow.get(row);
      return [s?.plain.text, s?.technical.text];
    };
    expect(pair(1)).toEqual(["Exact path match · 0.9", "inferred 0.9 exact"]);
    expect(pair(2)).toEqual(["Path suffix match · 0.6", "inferred 0.6 suffix"]);
    expect(pair(3)).toEqual(["Fact · 1.0", "fact 1.0"]);
    expect(pair(4)).toEqual(["Inferred · 0.9", "inferred 0.9"]);
    expect(pair(5)).toEqual(["Fact · 1.0", "fact 1.0"]);
    expect(pair(6)).toEqual(["Exact path match · 0.9", "exact 0.9"]);
    expect(pair(7)).toEqual(["Path suffix match · 0.6", "suffix 0.6"]);
    expect(pair(8)).toEqual(["Exact path match · 0.9", "exact 0.9 / 0.7"]);
    expect(pair(10)).toEqual(["Inferred · 0.9", "inferred 0.9"]);
    expect(pair(11)).toEqual(["", "I4"]);
    expect(pair(12)).toEqual(["", "C3"]);
    expect(pair(13)).toEqual(["", "C4 C5"]);
    expect(pair(14)).toEqual(["exposes route → calls route", "exposes_route → calls_route"]);
    expect(pair(15)).toEqual(["client call", "client_call"]);
    expect(pair(16)[1]).toBe("Fact paths read “depends on”, inferred paths “may depend on”; confidence is the weakest step. Never “breaks”.");
    expect(pair(17)).toEqual(["Library versions", "V1"]);
    expect(pair(18)).toEqual(["+1 assumption", "E2"]);
    expect(pair(19)).toEqual(["", "SQLAlchemy receiver."]);
    // T025b additions: the Unknowns line.
    expect(pair(20)).toEqual(["exposes route", "exposes_route"]);
    expect(pair(21)).toEqual([
      "Architecture components: not extracted yet, so no component edge is traversed.",
      "Architecture components (I11): not extracted yet, so no component edge is traversed.",
    ]);
    expect(label("plain", "notExtracted", { relation: "I8", names: ["hero"] }).text).toBe("Code using hero: not extracted for this source; this is not “no users”.");
    expect(label("technical", "notExtracted", { relation: "I8", names: [] }).text).toBe("Code using these tables (I8): not extracted for this source; this is not “no users”.");
    expect(label("plain", "notExtracted", { relation: "I8", names: [] }).title).toBe("impact rule I8");
    // An unknown relation reads raw in both styles.
    expect(label("plain", "notExtracted", { relation: "I99", names: [] }).text).toBe("I99: not extracted.");
    // T022 B9: project mode.
    expect(pair(22)).toEqual(["Inferred · 0.7", "inferred 0.7"]);
    expect(pair(23)).toEqual(["Several possible routes", "ambiguous"]);
    expect(pair(24)).toEqual(["", "PL-R11"]);
    expect(pair(25)).toEqual(["crosses from express into react", "express → react"]);
    expect(pair(26)).toEqual(["Accept", "accept_suggestion"]);
    expect(pair(27)).toEqual(["path prefixes", "path_prefixes"]);
    // T022 MR-UI: the sidebar member list.
    expect(pair(28)).toEqual(["links fresh · pass 4 · 46 links", "link_pass fresh · pass 4 · 46 links, 0 stale"]);
    expect(pair(29)).toEqual(["stale: 3 files changed", "stale 3"]);
    expect(pair(30)).toEqual(["shown by default: choose a repository", "default: first member"]);
    const stale = { state: "stale", pass: 3, links: 27, stale_links: 27, stale_reasons: ["react was indexed again after the link pass"] };
    expect(label("plain", "linkStatus", stale).text).toBe("links stale · pass 3: re-index the project");
    expect(label("plain", "linkStatus", stale).title).toContain("react was indexed again");
    expect(label("technical", "linkStatus", stale).title).toContain("react was indexed again");
    expect(label("plain", "linkStatus", { state: "none", pass: 0, links: 0, stale_links: 0, stale_reasons: [] }).text).toBe("no link pass yet");
    expect(label("plain", "memberFreshness", { checkout: false, state: "unknown", changed_count: null }).text).toBe("no local checkout");
    expect(label("technical", "projectLink", { kind: "calls_route", provenance: "inferred", confidence: 0.7 }).title).toBe("calls_route project link, across repositories");
    expect(label("plain", "outcomeStatus", "brand_new").text).toBe("brand_new");
  });

  it("words the architecture rows (T026 AG-C1)", () => {
    const byRow = new Map(rowSamples().map((s) => [s.row, s]));
    const pair = (row: number) => {
      const s = byRow.get(row);
      return [s?.plain.text, s?.technical.text];
    };
    expect(pair(31)).toEqual(["FastAPI server", "server · fastapi"]);
    expect(pair(32)).toEqual(["In-process", "in_process CE4"]);
    expect(pair(33)).toEqual(["proven: route and client-call facts", "proven: route and client-call facts [AP1]"]);
    expect(pair(34)).toEqual(["As written", "as_written"]);
    expect(pair(35)).toEqual(["Inferred · 0.9", "inferred 0.9"]);
    // T041: a fact tier reads "Fact · 1.0", a whole number "1.0"; a form
    // the server never writes stays raw.
    expect(label("plain", "linkTier", "fact").text).toBe("Fact · 1.0");
    expect(label("plain", "linkTier", "inferred 1.0").text).toBe("Inferred · 1.0");
    expect(label("plain", "linkTier", "inferred 0.875").text).toBe("Inferred · 0.875");
    expect(label("plain", "linkTier", "inferred").text).toBe("inferred");
    expect(label("plain", "linkTier", "assessment 0.5").text).toBe("assessment 0.5");
    expect(label("plain", "linkTier", "assessment 0.5").title).toBe("assessment 0.5");
    expect(label("plain", "chainEdge", { wording: "called by client call", row: "CH3", tier: "fact", dashed: false }).text).toBe("called by client call · Fact · 1.0");
    expect(pair(36)).toEqual(["L2", "L2"]);
    expect(pair(37)).toEqual(["Hidden: 17 test-scope edges · docs 1 component · libraries 27", "Hidden: 17 test-scope edges (scope production) · docs 1 component (CV2) · libraries 27 (CV3)"]);
    expect(pair(38)).toEqual(["19 calls not linked", "not linked ×19 (CE1)"]);
    expect(pair(39)).toEqual(["documented, no route: 3 operations", "CE5: documented, no route: 3 operations"]);
    expect(pair(40)).toEqual(["Dockerfile backend/Dockerfile", "Dockerfile backend/Dockerfile (CD1)"]);
    expect(pair(41)).toEqual(["imports 12 external packages", "imports 12 external packages (CE6)"]);
    // A kind or family this UI does not know is shown in words, never as a known one.
    expect(label("plain", "archKind", { kind: "grpc_server", framework: null, row: null }).text).toBe("grpc server");
    expect(label("plain", "linkFamily", { family: "queue", row: "" }).text).toBe("queue");
    expect(label("plain", "hiddenNote", { tests: 0, docs: 0, configured: 0, libraries: 0, testEdges: 0, sdk: 0 }).text).toBe("Nothing hidden");
  });

  it("words the Try it out rows (T029 part B)", () => {
    const byRow = new Map(rowSamples().map((s) => [s.row, s]));
    const pair = (row: number) => {
      const s = byRow.get(row);
      return [s?.plain.text, s?.technical.text];
    };
    expect(pair(65)).toEqual(["Compose published port", "compose_published_port TI-31"]);
    expect(pair(66)).toEqual(["Bearer from $API_TOKEN · set", "Authorization: env API_TOKEN set"]);
    expect(pair(67)).toEqual(["a method override turns this GET or HEAD into another method", "method_override"]);
    expect(pair(68)).toEqual(["cut at the response size limit", "truncated size response_bytes"]);
    // T073: the quieter API list, route header, callers and legend strip.
    expect(pair(69)).toEqual(["0 linked", "never_called"]);
    expect(byRow.get(69)?.plain.title).toBe("No linked caller in production. Graphvein found no call it could link; unresolved calls may still reach it.");
    expect(pair(70)).toEqual(["266 path assumed", "266 path_assumed"]);
    expect(pair(71)).toEqual(["Both callers rest on one assumption.", "path_assumed on 2 callers:"]);
    expect(label("plain", "sharedAssumption", { count: 3, code: "path_assumed" }).text).toBe("All 3 callers rest on one assumption.");
    expect(pair(72)).toEqual(["4 parts unverified", "unverified: handler, middleware, conditional, scope"]);
    expect(byRow.get(72)?.plain.title).toMatch(/^Unverified: handler, middleware, conditional, scope\. /);
    expect(label("plain", "unverifiedCount", ["scope"]).text).toBe("1 part unverified");
    expect(pair(73)).toEqual(["+ 5 kinds with no line here", "+5 at 0"]);
    expect(label("plain", "legendHidden", 1).text).toBe("+ 1 kind with no line here");
    // A typed value is never shown, in either style.
    const session = { header: "X-Api-Key", env: "KEY", set: false, session: true, prefix: null };
    expect(label("plain", "tryAuth", session).text).toBe("X-Api-Key: a value typed for this session");
    expect(label("technical", "tryAuth", session).text).toBe("X-Api-Key: session value");
    expect(label("plain", "tryAuth", { header: "X-Api-Key", env: "KEY", set: false, session: false, prefix: null }).text).toBe("from $KEY · not set");
    // A framework default says it was not read from the code.
    expect(label("plain", "suggestionSource", { kind: "framework_default", row: "TI-41" }).text).toBe("Framework default, not read from your code");
  });

  it("words the Explorer rows (T029)", () => {
    const byRow = new Map(rowSamples().map((s) => [s.row, s]));
    const pair = (row: number) => {
      const s = byRow.get(row);
      return [s?.plain.text, s?.technical.text];
    };
    expect(pair(59)).toEqual(["Read in handler · 1.0", "read_in_handler 1.0 SH-E3"]);
    expect(pair(60)).toEqual(["Not read by Graphvein", "not_read"]);
    expect(pair(61)).toEqual(["path from a settings default", "settings_default"]);
    expect(pair(62)).toEqual(["read by a dependency", "read_by_dependency"]);
    expect(pair(63)).toEqual(["set by you, not used", "set_by_you unused"]);
    expect(pair(64)).toEqual(["", "OG-76"]);
    // Every kind of SM-6..SM-10 has a plain word; the tooltip keeps the rule.
    const kinds = ["fact", "read_in_handler", "intended", "returned", "mapped"].map((kind) => label("plain", "shapeKind", { kind, rule: "SH-F4", confidence: 1 }));
    expect(kinds.map((k) => k.text)).toEqual(["Fact · 1.0", "Read in handler · 1.0", "Intended · 1.0", "Returned · 1.0", "Mapped · 1.0"]);
    for (const k of kinds) expect(k.title).toContain("SH-F4");
    for (const k of kinds) expect(k.title).toContain(SHAPE_NOTE);
    expect(label("plain", "shapeKind", { kind: "mapped", rule: "SH-M1", confidence: 0.7 }).text).toBe("Mapped · 0.7");
    expect(label("technical", "shapeKind", { kind: "mapped", rule: "SH-M1", confidence: 0.7 }).text).toBe("mapped 0.7 SH-M1");
    // A section that was not read never reads as "none"; only provably_none does.
    expect(label("plain", "sectionState", { state: "not_read", section: "query" }).text).not.toMatch(/none/i);
    expect(label("plain", "sectionState", { state: "provably_none", section: "query" }).text).toBe("None, proven");
    // Unknown forms read raw.
    expect(label("plain", "shapeKind", { kind: "guessed", rule: "X", confidence: 1 }).text).toBe("guessed");
    expect(label("plain", "resolution", "from_env").text).toBe("from env");
  });

  it("each style's tooltip carries the other style's words", () => {
    const e = { kind: "calls_route", provenance: "inferred", confidence: 0.9, match: "exact" };
    expect(label("plain", "edge", e).title).toBe("Stored calls_route edge: inferred 0.9 exact. The number is a fixed rank per match rule, not a probability.");
    expect(label("technical", "edge", e).title).toBe("Stored calls_route edge: Exact path match");
    expect(label("plain", "flowTier", { tier: "exact", confidence: 0.7 })).toEqual({ text: "Exact path match · 0.7", title: "exact 0.7. The number is a fixed rank per match rule, not a probability." });
    expect(label("technical", "flowTier", { tier: "exact", confidence: 0.7 })).toEqual({ text: "exact 0.7", title: "Exact path match" });
    expect(label("plain", "provenance", { provenance: "fact", confidence: 1 })).toEqual({ text: "Fact · 1.0", title: "provenance fact, confidence 1.0 (the weakest step). The number is a fixed rank per match rule, not a probability." });
    expect(label("technical", "provenance", { provenance: "fact", confidence: 1 }).title).toContain("Fact");
    expect(label("plain", "nodeKind", "client_call").title).toBe("client_call");
    expect(label("technical", "nodeKind", "client_call").title).toBe("client call");
    expect(label("technical", "edgeKinds", [{ kind: "calls_route", removed: true }]).text).toBe("calls_route (removed, base)");
  });

  it("words the Flow tab's table chip and lines in both styles (T026 AG-F)", () => {
    const byRow = new Map(rowSamples().map((s) => [s.row, s]));
    const pair = (row: number) => {
      const s = byRow.get(row);
      return [s?.plain.text, s?.technical.text];
    };
    expect(pair(54)).toEqual(["Reads · 1.0, may write · 0.9", "R fact · W 0.9"]);
    expect(pair(55)).toEqual(["may write · 0.9", "writes_table inferred 0.9"]);
    const chip = (reads: string[], writes: string[], style: "plain" | "technical") => label(style, "tableAccess", { reads, writes, detail: "" }).text;
    expect(chip(["fact"], ["fact"], "plain")).toBe("Reads and writes · 1.0");
    expect(chip(["fact"], ["fact"], "technical")).toBe("R·W fact");
    expect(chip([], ["0.9"], "plain")).toBe("May write · 0.9");
    // A kind drawn as fact and inferred reads as the fact, with both numbers.
    expect(chip(["0.9", "fact"], [], "plain")).toBe("Reads · 1.0/0.9");
    // No plain chip carries a raw tier ("fact", R, W); numbers are in the one format.
    for (const t of [["fact"], ["0.9"], ["0.9", "fact"], ["0.875"]]) expect(chip(t, t, "plain")).not.toMatch(/fact|R·W|\bR\b|\bW\b|0\.90|1\.00/);
    expect(label("plain", "tableLine", { kind: "reads_table", provenance: "fact", confidence: 1, wording: "builds a query on" }).text).toBe("builds a query on · 1.0");
  });

  it("shows the number beside the rule name in the plain style, short, in one format (T041)", () => {
    const byRow = new Map(rowSamples().map((s) => [s.row, s]));
    // The rows that carry a confidence: the plain text ends in " · <number>",
    // stays short (the explanation is in the tooltip) and the tooltip says
    // what the number is.
    // T041 follow-up: every row with a stored confidence, in one
    // "rule · number" form (row 54 joins its parts with commas).
    for (const row of [1, 2, 3, 4, 5, 6, 7, 8, 10, 22, 35, 43, 54, 55, 59]) {
      const s = byRow.get(row);
      expect(s?.plain.text, `row ${String(row)}`).toMatch(/ · \d\.\d+(\/\d\.\d+)?$/);
      // Rule labels stay short; rows 43, 54 and 55 put the number after the
      // server's wording or the access verbs, which make them longer.
      if (![43, 54, 55].includes(row)) expect(s?.plain.text.length ?? 99, `row ${String(row)} length`).toBeLessThanOrEqual(24);
      // The Explorer chip (row 59) says its own (fixed per kind); every
      // other row, the legend chip (row 8) included, ends in the rank note.
      if (row === 59) expect(s?.plain.title, `row ${String(row)} title`).toContain(SHAPE_NOTE);
      else expect(s?.plain.title, `row ${String(row)} title`).toContain(RANK_NOTE);
    }
    // The legend chip's second rank (0.7) is in its tooltip.
    expect(byRow.get(8)?.plain.title).toContain("0.7");
    // A fact chip with no number in the API (an endpoint header) reads the
    // same way: every fact is 1.0.
    expect(factLabel("plain").text).toBe("Fact · 1.0");
    expect(factLabel("plain").title).toContain(RANK_NOTE);
    expect(factLabel("technical").text).toBe("fact 1.0");
    // The stored value in its shortest exact form, never two fixed decimals.
    const e = (confidence: number) => ({ kind: "calls_route", provenance: "inferred", confidence, match: "exact" });
    expect(label("plain", "edge", e(0.85)).text).toBe("Exact path match · 0.85");
    expect(label("plain", "edge", e(0.875)).text).toBe("Exact path match · 0.875");
    expect(label("technical", "edge", e(0.875)).text).toBe("inferred 0.875 exact");
    expect(label("technical", "provenance", { provenance: "inferred", confidence: 0.875 }).text).toBe("inferred 0.875");
    expect(label("plain", "provenance", { provenance: "inferred", confidence: 0.875 }).text).toBe("Inferred · 0.875");
    // No number where the API gives none (a move hint, a path inferred only
    // by its rule); both are inferred, a fact always has its 1.0.
    expect(label("plain", "provenance", { provenance: "inferred", confidence: null })).toEqual({ text: "Inferred", title: "provenance inferred" });
    // Ids and raw kinds stay out of the plain text.
    expect(label("plain", "impactRule", "I4").text).toBe("");
    expect(label("plain", "nodeKind", "client_call").text).toBe("client call");
    // The project link keeps "across repositories" in its tooltip.
    expect(label("plain", "projectLink", { kind: "calls_route", provenance: "inferred", confidence: 0.7 }).title).toContain("Inferred across repositories");
  });

  it("keeps unknown forms raw in both styles", () => {
    const odd = { kind: "documents_route", provenance: "inferred", confidence: 0.5, match: "fuzzy" };
    expect(label("plain", "edge", odd).text).toBe(storedEdge(odd));
    expect(label("technical", "edge", odd).text).toBe("inferred 0.5 fuzzy");
    expect(label("plain", "provenance", { provenance: "guessed", confidence: null }).text).toBe("guessed");
    expect(label("plain", "assumptionChip", { id: "Z9" }).text).toBe("Z9");
    expect(label("technical", "assumptionChip", { id: "Z9" }).text).toBe("Z9");
    expect(label("technical", "assumptionLead", { id: "Z9" }).text).toBe("");
  });

  it("names a T025 row in only one entry", () => {
    const seen = new Map<number, string>();
    for (const [key, entry] of Object.entries(LABELS)) {
      for (const row of Object.keys(entry.rows).map(Number)) {
        expect(seen.get(row), `row ${String(row)} in ${key}`).toBeUndefined();
        seen.set(row, key);
      }
    }
  });
});
