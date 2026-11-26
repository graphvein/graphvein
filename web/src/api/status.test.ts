import { describe, expect, it } from "vitest";

import { parseStatus, StatusFormatError } from "@/api/status";
import { GOLDENS, rawGolden } from "@/test/fixtures";

describe("parseStatus", () => {
  it.each(Object.keys(GOLDENS) as (keyof typeof GOLDENS)[])("accepts the %s golden", (name) => {
    const raw = GOLDENS[name];
    const status = parseStatus(raw);
    expect(status.freshness.state).toBe(name);
    expect(status.freshness.reported_state).toBe(raw.freshness.state);
    expect(status.repository).toBe(raw.repository);
    expect(status.extractor).toEqual(raw.extractor);
    expect(status.freshness.changed_files).toEqual(raw.freshness.changed_files);
  });

  it("keeps the unknown state's error and null count", () => {
    const status = parseStatus(GOLDENS.unknown);
    expect(status.freshness.error).toBe("injected failure");
    expect(status.freshness.changed_count).toBeNull();
  });

  it("parses a missing last run as null", () => {
    expect(parseStatus(GOLDENS.not_indexed).index.last_run).toBeNull();
  });

  it("maps an unrecognised state to unknown", () => {
    const raw = rawGolden("fresh");
    (raw.freshness as Record<string, unknown>).state = "weird";
    const status = parseStatus(raw);
    expect(status.freshness.state).toBe("unknown");
    expect(status.freshness.reported_state).toBe("weird");
  });

  it("does not trust a fresh state that lists changes", () => {
    const raw = rawGolden("stale");
    (raw.freshness as Record<string, unknown>).state = "fresh";
    expect(parseStatus(raw).freshness.state).toBe("unknown");
    const nullCount = rawGolden("unknown");
    (nullCount.freshness as Record<string, unknown>).state = "fresh";
    expect(parseStatus(nullCount).freshness.state).toBe("unknown");
  });

  const freshBreakers: [string, (raw: Record<string, unknown>) => void][] = [
    ["index.indexed false", (r) => { (r.index as Record<string, unknown>).indexed = false; }],
    ["last_run null", (r) => { (r.index as Record<string, unknown>).last_run = null; }],
    ["an error", (r) => { (r.freshness as Record<string, unknown>).error = "scan failed"; }],
    ["a truncated list", (r) => { (r.freshness as Record<string, unknown>).changed_files_truncated = true; }],
  ];
  it.each(freshBreakers)("does not trust a fresh state with %s", (_name, breakIt) => {
    const raw = rawGolden("fresh");
    expect(parseStatus(raw).freshness.state).toBe("fresh");
    breakIt(raw);
    const status = parseStatus(raw);
    expect(status.freshness.state).toBe("unknown");
    expect(status.freshness.reported_state).toBe("fresh");
  });

  const breakers: [string, (raw: Record<string, unknown>) => void][] = [
    ["a non-object body", () => undefined],
    ["missing repository", (r) => { delete r.repository; }],
    ["missing extractor", (r) => { delete r.extractor; }],
    ["missing extractor.current", (r) => { delete (r.extractor as Record<string, unknown>).current; }],
    ["missing index", (r) => { delete r.index; }],
    ["missing index.last_run", (r) => { delete (r.index as Record<string, unknown>).last_run; }],
    ["missing freshness", (r) => { delete r.freshness; }],
    ["missing freshness.state", (r) => { delete (r.freshness as Record<string, unknown>).state; }],
    ["a numeric state", (r) => { (r.freshness as Record<string, unknown>).state = 1; }],
    ["missing checked_at", (r) => { delete (r.freshness as Record<string, unknown>).checked_at; }],
    ["missing changed_files", (r) => { delete (r.freshness as Record<string, unknown>).changed_files; }],
    ["a non-string path", (r) => { (r.freshness as Record<string, unknown>).changed_files = [1]; }],
    ["a negative count", (r) => { (r.freshness as Record<string, unknown>).changed_count = -1; }],
    ["missing truncated flag", (r) => { delete (r.freshness as Record<string, unknown>).changed_files_truncated; }],
  ];

  it.each(breakers)("rejects %s", (label, breakIt) => {
    const raw = rawGolden("fresh");
    breakIt(raw);
    const input = label === "a non-object body" ? "fresh" : raw;
    expect(() => parseStatus(input)).toThrow(StatusFormatError);
  });

  it("rejects null and arrays", () => {
    expect(() => parseStatus(null)).toThrow(StatusFormatError);
    expect(() => parseStatus([])).toThrow(StatusFormatError);
  });
});
