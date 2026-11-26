import { describe, expect, it } from "vitest";

import { schemaEntryTag } from "@/sections/schema/entry";
import { craftedSummary, summaryGolden } from "@/test/schemaFixtures";

describe("schemaEntryTag (round 3 Y, Revision 1)", () => {
  it("no tag while loading or failed", () => {
    expect(schemaEntryTag(null)).toBeNull();
  });

  it("none found only for found exactly none", () => {
    expect(schemaEntryTag(craftedSummary({ found: "none" }))?.text).toBe("none found");
    for (const found of ["None", "unknown", "sets", "", "none "]) {
      expect(schemaEntryTag(craftedSummary({ found, sets: 1 }))?.text).not.toBe("none found");
    }
  });

  it.each([
    ["version_ambiguous", "re-index"],
    ["predates_schema", "re-index"],
    ["external_indexer", "unknown"],
    ["version_unreadable", "unknown"],
    ["scan_incomplete", "unknown"],
    ["manifest_unreadable", "unknown"],
    ["future_reason", "unknown"],
  ])("found unknown, %s → %s", (reason, text) => {
    const tag = schemaEntryTag(craftedSummary({ found: "unknown", unknown_reason: reason }));
    expect(tag?.text).toBe(text);
    expect(tag?.title).toMatch(/^Schema not known/);
  });

  it("not indexed: no tag", () => {
    expect(schemaEntryTag(summaryGolden("not_indexed"))).toBeNull();
  });

  it("an unrecognised found value is unknown", () => {
    expect(schemaEntryTag(craftedSummary({ found: "maybe" }))?.text).toBe("unknown");
  });

  it("N unreadable counts failed sets, sets without records and unreadable manifests", () => {
    expect(schemaEntryTag(summaryGolden("fixture"))).toMatchObject({ text: "2 unreadable", tone: "warn" });
    expect(schemaEntryTag(summaryGolden("states"))).toMatchObject({ text: "3 unreadable", tone: "warn" });
    expect(schemaEntryTag(craftedSummary({ found: "sets", sets: 1, unreadable_manifests: 1 }))?.text).toBe("1 unreadable");
  });

  it("missing counts read unknown, never 0 unreadable", () => {
    expect(schemaEntryTag(craftedSummary({ found: "sets", sets: 1, sets_without_records: null }))?.text).toBe("unknown");
  });

  it("scan_gaps: missing reads unknown; a count is never added to N unreadable (B8)", () => {
    expect(schemaEntryTag(craftedSummary({ found: "sets", sets: 1, scan_gaps: null }))?.text).toBe("unknown");
    expect(schemaEntryTag(craftedSummary({ found: "sets", sets: 1, failed_sets: 1, scan_gaps: null }))?.text).toBe("unknown");
    expect(schemaEntryTag(craftedSummary({ found: "sets", sets: 1, failed_sets: 1, scan_gaps: 4 }))?.text).toBe("1 unreadable");
    expect(schemaEntryTag(craftedSummary({ found: "sets", sets: 1, scan_gaps: 4 }))).toBeNull();
    // The server answers any gap with found unknown, scan_incomplete.
    expect(schemaEntryTag(craftedSummary({ found: "unknown", unknown_reason: "scan_incomplete", scan_gaps: 4 }))?.text).toBe("unknown");
  });

  it("tests only when every set is test-only, and unreadable takes precedence", () => {
    expect(schemaEntryTag(craftedSummary({ found: "sets", sets: 18, test_only_sets: 18 }))?.text).toBe("tests only");
    expect(schemaEntryTag(craftedSummary({ found: "sets", sets: 18, test_only_sets: 18, failed_sets: 1 }))?.text).toBe("1 unreadable");
    expect(schemaEntryTag(craftedSummary({ found: "sets", sets: 2, test_only_sets: 1 }))).toBeNull();
  });
});
