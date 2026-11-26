// The change endpoint guards on the T019a goldens (wrapped in the
// endpoints' envelopes by changesFixtures.ts) and on the documented
// shapes of docs/cli/serve.md.
import { describe, expect, it } from "vitest";

import { impactPath, itemsPath, parseBases, parseItems, parsePin, parseRefs, parseSummary, summaryPath } from "@/api/changes";
import { ApiFormatError } from "@/api/endpoints";
import { CHANGE_GOLDENS, impactOf, itemsOf, rawItems, summaryOf, type ChangeCase } from "@/test/changesFixtures";

const CASES = Object.keys(CHANGE_GOLDENS) as ChangeCase[];

describe("change guards on the goldens", () => {
  it.each(CASES)("%s: summary, every item and every named impact parse", (c) => {
    const s = summaryOf(c);
    expect(s.summary.groups.map((g) => g.group)).toEqual(["routes", "client_calls", "api_operations", "ipc", "server_actions", "migration_tables", "model_tables", "migrations", "code", "angular", "react", "architecture", "files", "gaps", "other", "unclassified"]);
    const items = itemsOf(c, { base: "previous", tests: true, limit: 500 });
    expect(items.items.length).toBe(rawItems(c).length);
    // The summary's group counts equal the items listed per group.
    for (const g of s.summary.groups) {
      expect(items.items.filter((i) => i.group === g.group).length, g.group).toBe(g.items + g.hidden_tests);
    }
    for (const it of items.items) expect(impactOf(c, it.id) === null || impactOf(c, it.id)?.seeds.includes(it.id)).toBe(true);
  });

  it("reads absent members as not known, never as false", () => {
    const s = summaryOf("previous-rev1-rev2");
    // The golden's sides carry dirty: null and no commit (made outside git).
    expect(s.summary.base.dirty).toBeNull();
    expect(s.summary.base.commit).toBeNull();
    expect(s.summary.not_extracted).toEqual(["architecture"]);
  });

  it("keeps the head side's server-built editor link and the I8 access entry", () => {
    const page = parseItems({
      api_version: 0,
      repository: "fx",
      index: null,
      freshness: { state: "fresh", changed_count: 0, changed_files: [] },
      base: "previous",
      items: [
        {
          ...rawItems("previous-rev1-rev2")[0],
          after: { file: "server/router.ts", span: { start_line: 5, start_column: 0, end_line: 5, end_column: 33 }, fields: {}, ide: { link: "vscode://file/x/server/router.ts:5:1", line: 5, column: 1, exact: true } },
        },
      ],
      total: 1,
      hidden_tests: 0,
      next_cursor: null,
    });
    expect(page.items[0]?.after?.ide?.link).toBe("vscode://file/x/server/router.ts:5:1");
    const age = impactOf("previous-rev1-rev2", "column:models/app/models.py#Hero.age");
    const step = age?.paths.find((p) => p.node.endsWith("#hero_by_id"))?.steps[0];
    expect(step?.access).toMatchObject({ verification: "mapped", verification_wording: "mapped, not verified", assumes: ["TA-V3c"], column_use: "not_extracted" });
    expect(age?.table_uses[0]?.may_also_be_used_by[0]?.owner).toBe("function:models/app/queries.py#largest_team");
  });

  it("rejects a summary without its groups", () => {
    const s = summaryOf("previous-rev1-rev2");
    expect(() => parseSummary({ ...s, summary: { ...s.summary, groups: null } })).toThrow(ApiFormatError);
  });
});

describe("documented shapes (docs/cli/serve.md)", () => {
  const header = { api_version: 0, repository: "app", index: null, freshness: { state: "fresh", checked_at: "x", changed_count: 0, changed_files: [], changed_files_truncated: false } };
  const run = {
    seq: 2,
    run_id: "6Z2H",
    previous_run: "T4HW",
    indexer: { name: "graphvein", version: "extractors-32" },
    finished_at: "2026-11-13T23:30:38.567819705Z",
    counts: {},
    entry_bytes: 37782,
    gaps: ["web/broken.ts"],
    gaps_truncated: false,
    gaps_fileless: false,
  };

  it("bases: runs without commit read commit and dirty as null", () => {
    const b = parseBases({ ...header, runs: [run], pins: [{ id: "P", name: "release-1", seq: 1, created_at: "t", run }], refs: [], retention: { journal_runs: 30, journal_budget: 1, max_pins: 5, max_ref_snapshots: 4 } });
    expect(b.runs[0]?.commit).toBeNull();
    expect(b.runs[0]?.dirty).toBeNull();
    expect(b.pins[0]?.name).toBe("release-1");
  });

  it("refs: no job is null; a failed job keeps its code", () => {
    expect(parseRefs({ api_version: 0, repository: "app", job: null })).toBeNull();
    const job = parseRefs({
      api_version: 0,
      repository: "app",
      job: { id: "Q", state: "failed", started_at: "t", ended_at: "t", base: "ref:main", commits: ["a"], indexed: [], error: { code: "ref_objects_missing", message: "objects missing" } },
    });
    expect(job?.error?.code).toBe("ref_objects_missing");
  });

  it("pins: the POST answer's pin", () => {
    expect(parsePin({ id: "A", name: "before-refactor", seq: 2, created_at: "t", run }, "pin").run.seq).toBe(2);
  });

  it("paths encode base, ids and filters", () => {
    expect(summaryPath("pin:release 1")).toBe("/api/v0/changes/summary?base=pin%3Arelease%201");
    expect(itemsPath({ base: "previous", group: "code", change: null, q: "", tests: true, cursor: "abc", limit: 50 })).toBe("/api/v0/changes/items?base=previous&group=code&tests=true&cursor=abc&limit=50");
    expect(impactPath({ base: "previous", id: 'edge:["calls","a","b"]', depth: 2 })).toBe("/api/v0/changes/impact?base=previous&id=edge%3A%5B%22calls%22%2C%22a%22%2C%22b%22%5D&depth=2");
  });
});
