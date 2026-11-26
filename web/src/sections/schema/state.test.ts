import { describe, expect, it } from "vitest";

import type { SchemaTable } from "@/api/schema";
import { commonState, hiddenTestSets, needsSets, objectsState, pageState, summaryPage, visibleGroups } from "@/sections/schema/state";
import { craftedAllTestOnly, craftedFailedOnly, craftedSummary, setsGolden, summaryGolden } from "@/test/schemaFixtures";

const t = (completeness: string) => ({ completeness }) as SchemaTable;

describe("pageState", () => {
  it("not indexed: from the status or the summary", () => {
    expect(summaryPage(null, summaryGolden("not_indexed"))).toEqual({ kind: "not_indexed" });
    expect(summaryPage(false, summaryGolden("fixture"))).toEqual({ kind: "not_indexed" });
  });

  it.each(["external_indexer", "predates_schema", "version_ambiguous", "version_unreadable", "scan_incomplete", "manifest_unreadable", "future_reason"])(
    "found unknown (%s) is the unknown page, never none found",
    (reason) => {
      const s = craftedSummary({ found: "unknown", unknown_reason: reason });
      expect(summaryPage(true, s)).toEqual({ kind: "unknown", found: "unknown", reason });
      // Whatever /sets says (even nothing), unknown stays unknown.
      expect(pageState(true, s, { ...setsGolden("states"), sets: [], failed_sets: [], sets_without_records: [] }, false).kind).toBe("unknown");
    },
  );

  it("requests /sets only for found sets and the listed unknown reasons", () => {
    expect(needsSets(true, summaryGolden("fixture"))).toBe(true);
    expect(needsSets(true, craftedSummary({ found: "none" }))).toBe(false);
    expect(needsSets(true, craftedSummary({ found: "unknown", unknown_reason: "version_ambiguous" }))).toBe(false);
    expect(needsSets(true, craftedSummary({ found: "unknown", unknown_reason: "scan_incomplete" }))).toBe(true);
    expect(needsSets(true, craftedSummary({ found: "unknown", unknown_reason: "manifest_unreadable" }))).toBe(true);
    expect(needsSets(true, summaryGolden("not_indexed"))).toBe(false);
  });

  it("an unrecognised found value is unknown", () => {
    expect(summaryPage(true, craftedSummary({ found: "some" }))).toEqual({ kind: "unknown", found: "some", reason: null });
  });

  it("found none is the none-found page", () => {
    expect(summaryPage(true, craftedSummary({ found: "none" }))).toEqual({ kind: "none" });
  });

  it("failed-only: no group and failed or record-less sets", () => {
    expect(pageState(true, summaryGolden("states"), craftedFailedOnly(), false)).toEqual({ kind: "failed_only" });
    const onlyRecordless = { ...craftedFailedOnly(), failed_sets: [] };
    expect(pageState(true, summaryGolden("states"), onlyRecordless, false)).toEqual({ kind: "failed_only" });
  });

  it("all test-only with tests off is the test-only page; tests on is the home", () => {
    const s = craftedAllTestOnly();
    const p = pageState(true, summaryGolden("states"), s, false);
    expect(p.kind).toBe("tests_only");
    expect(pageState(true, summaryGolden("states"), s, true).kind).toBe("home");
  });

  it("summary sets with an empty /sets is 'changed', never none found", () => {
    expect(pageState(true, summaryGolden("fixture"), { ...craftedFailedOnly(), failed_sets: [], sets_without_records: [] }, false)).toEqual({ kind: "changed" });
  });

  it("otherwise the home with the visible groups", () => {
    const p = pageState(true, summaryGolden("states"), setsGolden("states"), false);
    expect(p.kind).toBe("home");
    if (p.kind !== "home") return;
    expect(p.groups.map((g) => g.set)).not.toContain("e/tests/migrations");
    expect(p.groups.map((g) => g.set)).toContain(null);
  });
});

describe("visible groups", () => {
  it("hides test_only true only, never the set: null group", () => {
    const s = setsGolden("states");
    expect(visibleGroups(s, false).length).toBe(s.sets.length - 1);
    expect(visibleGroups(s, true).length).toBe(s.sets.length);
    expect(visibleGroups(s, false).some((g) => g.set === null)).toBe(true);
    expect(hiddenTestSets(s, false)).toBe(1);
    expect(hiddenTestSets(s, true)).toBe(0);
  });
});

describe("commonState", () => {
  it("is the most frequent of complete and complete_in_source", () => {
    expect(commonState([t("complete"), t("complete"), t("complete_in_source")])).toBe("complete");
    expect(commonState([t("complete_in_source"), t("partial"), t("partial")])).toBe("complete_in_source");
  });
  it("ties go to complete_in_source", () => {
    expect(commonState([t("complete"), t("complete_in_source")])).toBe("complete_in_source");
  });
  it("partial and unknown are never common", () => {
    expect(commonState([t("partial"), t("unknown"), t("weird")])).toBeNull();
    expect(commonState([])).toBeNull();
  });
});

describe("objectsState (Review fixes R1)", () => {
  it.each([
    [{ objects: "modeled", set: "a/migrations" }, "extracted"],
    [{ objects: null, set: null }, "extracted"],
    [{ objects: "not_modeled", set: "a/migrations" }, "not_extracted"],
    [{ objects: null, set: "a/migrations" }, "unknown"],
    [{ objects: "modelled", set: "a/migrations" }, "unknown"],
  ] as const)("%o → %s", (g, want) => {
    expect(objectsState(g)).toBe(want);
  });
});
