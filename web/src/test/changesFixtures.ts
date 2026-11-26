// The change-impact goldens (testdata/fixtures/change-impact.<case>.golden.json,
// T019a2): each is the `graphvein changes --json --tests` document with the
// impact of named seeds, built by the same internal/changes code the
// /api/v0/changes/* endpoints call. The fake client wraps their summary,
// items and impacts in the endpoints' envelopes (header, base, paging), so
// the guards and views are tested against what the server computes; a
// changed golden changes what these tests parse. Crafted variants are
// marked "crafted" with the golden they extend.
import extractorUpgrade from "../../../testdata/fixtures/change-impact.extractor-upgrade.golden.json";
import pinRev1Rev3 from "../../../testdata/fixtures/change-impact.pin-rev1-rev3.golden.json";
import previousRev1Rev2 from "../../../testdata/fixtures/change-impact.previous-rev1-rev2.golden.json";
import previousRev2Rev3 from "../../../testdata/fixtures/change-impact.previous-rev2-rev3.golden.json";
import models from "../../../testdata/fixtures/change-impact.models.golden.json";
import refRev1Rev3 from "../../../testdata/fixtures/change-impact.ref-rev1-rev3.golden.json";

import { ApiError } from "@/api/client";
import {
  parseBases,
  parseImpact,
  parseItems,
  parseSummary,
  type ChangesBases,
  type ChangesClient,
  type ChangesImpact,
  type ChangesItems,
  type ChangesSummary,
  type ItemsParams,
  type RefJob,
} from "@/api/changes";

export const CHANGE_GOLDENS = {
  "previous-rev1-rev2": previousRev1Rev2,
  "previous-rev2-rev3": previousRev2Rev3,
  "pin-rev1-rev3": pinRev1Rev3,
  "ref-rev1-rev3": refRev1Rev3,
  "extractor-upgrade": extractorUpgrade,
  // T029 W2: models changed in FastAPI, Go and Express (relation I16).
  models,
} as const;

export type ChangeCase = keyof typeof CHANGE_GOLDENS;

interface GoldenDoc {
  repository: string;
  base: string;
  summary: unknown;
  items: unknown[];
  impacts?: { seed: string; impact: Record<string, unknown> }[];
}

function doc(c: ChangeCase): GoldenDoc {
  // The JSON modules are typed by their content; the guards each wrapper
  // calls check the shape.
  return CHANGE_GOLDENS[c];
}

const HEADER = {
  api_version: 0,
  index: { finished_at: "2026-11-14T10:33:34.349957777Z", indexer: { name: "graphvein", version: "extractors-34" }, contract_version: 1 },
  freshness: { state: "fresh", checked_at: "2026-11-14T10:33:43.6Z", changed_count: 0, changed_files: [], changed_files_truncated: false },
};

/** summaryOf is the summary endpoint's answer for a golden. */
export function summaryOf(c: ChangeCase): ChangesSummary {
  const d = doc(c);
  return parseSummary({ ...HEADER, repository: d.repository, base: d.base, summary: d.summary });
}

/** rawItems are a golden's items as JSON (for the items endpoint). */
export function rawItems(c: ChangeCase): Record<string, unknown>[] {
  return doc(c).items as Record<string, unknown>[];
}

/** itemsOf filters and pages a golden's items as the server does (group,
 * change, q on identity or name, tests; cursor is the offset). */
export function itemsOf(c: ChangeCase, p: ItemsParams): ChangesItems {
  const all = rawItems(c);
  const match = all.filter((it) => {
    if (p.group != null && it.group !== p.group) return false;
    if (p.change != null && it.change !== p.change) return false;
    if (p.q != null && p.q !== "" && !(String(it.identity).includes(p.q) || String(it.name).includes(p.q))) return false;
    return true;
  });
  const visible = match.filter((it) => p.tests === true || it.test !== true);
  const offset = p.cursor == null ? 0 : Number(p.cursor);
  const limit = p.limit ?? 100;
  const page = visible.slice(offset, offset + limit);
  const next = offset + limit < visible.length ? String(offset + limit) : null;
  return parseItems({ ...HEADER, repository: doc(c).repository, base: doc(c).base, items: page, total: visible.length, hidden_tests: match.length - visible.length, next_cursor: next });
}

/** impactOf is the impact endpoint's answer for a golden's named seed, or
 * null when the golden has none for it. */
export function impactOf(c: ChangeCase, id: string): ChangesImpact | null {
  const named = doc(c).impacts?.find((i) => i.seed === id);
  if (named === undefined) return null;
  const paths = named.impact.paths as unknown[];
  return parseImpact({ ...HEADER, repository: doc(c).repository, base: doc(c).base, ...named.impact, total: paths.length, next_cursor: null });
}

/** basesFor is a crafted bases answer naming the golden's two runs
 * (run 2 the head, run 1 the base) and one pin. */
export function basesFor(c: ChangeCase, extra: Partial<{ runs: number; pins: string[] }> = {}): ChangesBases {
  const s = summaryOf(c).summary;
  const run = (seq: number, id: string, commit: string | null) => ({
    seq,
    run_id: id,
    indexer: { name: "graphvein", version: "extractors-34" },
    ...(commit !== null ? { commit, dirty: false } : {}),
    finished_at: "2026-11-14T10:33:34Z",
    counts: {},
    entry_bytes: 0,
    gaps: [],
    gaps_truncated: false,
    gaps_fileless: false,
  });
  const n = extra.runs ?? 2;
  const runs = [run(2, s.head.run, "429e3b4e9ea534bb53117d829c37a49eed129e6c"), run(1, s.base.run, "c4b7a952d5caa9f6893e9fdb0cb030b7c9a25c21")].slice(0, n);
  return parseBases({
    ...HEADER,
    repository: doc(c).repository,
    runs,
    pins: (extra.pins ?? ["release-1"]).map((name, i) => ({ id: `PIN${String(i)}`, name, seq: 1, created_at: "2026-11-14T10:33:34Z", run: runs[runs.length - 1] })),
    refs: [],
    retention: { journal_runs: 30, journal_budget: 1, max_pins: 5, max_ref_snapshots: 4 },
  });
}

export interface FakeChanges {
  /** The golden every read answers from. */
  case?: ChangeCase;
  /** Summary answers by base; an Error rejects. Default: the golden. */
  summary?: (base: string) => ChangesSummary | Error;
  bases?: ChangesBases | Error;
  /** The ref job answers: POST, then each GET in turn (the last repeats). */
  refs?: (RefJob | null)[];
  startRefs?: RefJob | Error;
  calls?: string[];
}

/** fakeChangesClient answers from a golden and records each call. */
export function fakeChangesClient(o: FakeChanges = {}): ChangesClient {
  const c = o.case ?? "previous-rev1-rev2";
  const calls = o.calls ?? [];
  let refIndex = 0;
  const settle = <T>(v: T | Error): Promise<T> => (v instanceof Error ? Promise.reject(v) : Promise.resolve(v));
  return {
    bases: () => {
      calls.push("bases");
      return settle(o.bases ?? basesFor(c));
    },
    summary: (base) => {
      calls.push(`summary:${base}`);
      return settle(o.summary !== undefined ? o.summary(base) : summaryOf(c));
    },
    items: (p) => {
      calls.push(`items:${JSON.stringify(p)}`);
      return Promise.resolve(itemsOf(c, p));
    },
    impact: (p) => {
      calls.push(`impact:${p.id}:${String(p.depth ?? 3)}`);
      const imp = impactOf(c, p.id);
      return imp === null ? Promise.reject(new ApiError("http", "not_found: no impact golden", 404, "not_found")) : Promise.resolve({ ...imp, depth: p.depth ?? imp.depth });
    },
    refs: () => {
      calls.push("refs");
      const list = o.refs ?? [null];
      const v = list[Math.min(refIndex, list.length - 1)] ?? null;
      refIndex += 1;
      return Promise.resolve(v);
    },
    startRefs: (base, head) => {
      calls.push(`startRefs:${base}:${head ?? ""}`);
      return settle(o.startRefs ?? null);
    },
    pin: (name, run) => {
      calls.push(`pin:${name}:${run ?? ""}`);
      const pin = basesFor(c, { pins: [name] }).pins[0];
      return pin === undefined ? Promise.reject(new Error("no pin")) : Promise.resolve(pin);
    },
    unpin: (name) => {
      calls.push(`unpin:${name}`);
      return Promise.resolve();
    },
  };
}

/** apiError builds a server refusal as the client reports it. */
export function apiError(status: number, code: string, message = code): ApiError {
  return new ApiError("http", `${code}: ${message}`, status, code);
}
