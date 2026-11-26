import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Status } from "@/api/status";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { StatusView } from "@/hooks/useStatus";
import { formatShortTime } from "@/lib/time";
import { COUNT_SIZER_SAMPLES } from "@/lib/count";
import { badgeSizerLabels, filesChanged, FreshnessBadge } from "@/shell/FreshnessBadge";
import {
  announcement,
  deriveReindex,
  formatCount,
  SLOT_SIZER_LABELS,
  unsettled,
  slotLabel,
  type ReindexInputs,
  type RunView,
} from "@/shell/reindexModel";
import { goldenStatus } from "@/test/fixtures";
import { runDoc } from "@/test/indexRunFixtures";

const T = "2026-11-03T14:32:05.123Z"; // the goldens' last run
const T2 = "2026-11-03T15:00:20.000Z"; // the new run

function ok(status: Status): StatusView {
  return { kind: "ok", status };
}

function withRun(name: "fresh" | "stale", finishedAt: string): StatusView {
  const s = goldenStatus(name);
  if (s.index.last_run) s.index.last_run.finished_at = finishedAt;
  return ok(s);
}

function run(lock: string, job: Parameters<typeof runDoc>[1] = null, error: string | null = null): RunView {
  return { kind: "ok", doc: runDoc(lock, job, error) };
}

function inputs(over: Partial<ReindexInputs>): ReindexInputs {
  return {
    status: ok(goldenStatus("fresh")),
    run: run("free"),
    pending: false,
    postError: null,
    startedJobId: null,
    dismissedJobId: null,
    ...over,
  };
}

function badgeText(input: ReindexInputs): string {
  const model = deriveReindex(input);
  const { getByTestId, unmount } = render(
    <TooltipProvider>
      <FreshnessBadge view={input.status} override={model.badge} />
    </TooltipProvider>,
  );
  const text = getByTestId("freshness-badge").textContent;
  unmount();
  return text;
}

const committedJob = { state: "committed", phase: null, run: { id: "R2", finished_at: T2 } } as const;

// One test per row of the state table (task section 5 as changed by the
// developer decisions D1-D8): inputs -> badge text and slot.
describe("deriveReindex: the state table", () => {
  const rows: { name: string; in: Partial<ReindexInputs>; badge: string; slot: string; kind: string; primary?: boolean }[] = [
    { name: "free, no job, fresh", in: {}, badge: `Freshindexed ${formatShortTime(T)}`, slot: "Re-index", kind: "idle", primary: false },
    { name: "free, no job, stale", in: { status: ok(goldenStatus("stale")) }, badge: `Stale: 3 files changedsince ${formatShortTime(T)}`, slot: "Re-index", kind: "idle", primary: true },
    { name: "free, no job, not indexed", in: { status: ok(goldenStatus("not_indexed")) }, badge: "Not indexed", slot: "Re-index", kind: "idle", primary: true },
    { name: "unknown lock, fresh", in: { run: run("unknown", null, "probe failed") }, badge: `Indexed${formatShortTime(T)}`, slot: "Re-index", kind: "unavailable" },
    { name: "unknown lock, stale", in: { status: ok(goldenStatus("stale")), run: run("unknown", null, "probe failed") }, badge: `Stale: 3 files changedsince ${formatShortTime(T)}`, slot: "Re-index", kind: "unavailable" },
    { name: "unrecognised lock state", in: { run: run("frozen") }, badge: `Indexed${formatShortTime(T)}`, slot: "Re-index", kind: "unavailable" },
    { name: "run state not read yet", in: { run: { kind: "loading" } }, badge: `Indexed${formatShortTime(T)}`, slot: "Re-index", kind: "unavailable" },
    { name: "run request failed", in: { run: { kind: "failed", error: new Error("boom") } }, badge: `Indexed${formatShortTime(T)}`, slot: "Re-index", kind: "unavailable" },
    { name: "held_here, parsing with counts, fresh index", in: { run: run("held_here", { progress: { done: 212, total: 457, unit: "files" } }) }, badge: `Indexed${formatShortTime(T)}`, slot: "Indexing 212 / 457", kind: "running" },
    { name: "held_here, scanning (no counts), stale index", in: { status: ok(goldenStatus("stale")), run: run("held_here", { phase: "scanning" }) }, badge: `Stale: 3 files changedsince ${formatShortTime(T)}`, slot: "Indexing", kind: "running" },
    { name: "held_elsewhere, fresh index", in: { run: run("held_elsewhere") }, badge: `Indexed${formatShortTime(T)}`, slot: "Indexing (CLI)", kind: "elsewhere" },
    { name: "held_elsewhere, stale index", in: { status: ok(goldenStatus("stale")), run: run("held_elsewhere") }, badge: `Stale: 3 files changedsince ${formatShortTime(T)}`, slot: "Indexing (CLI)", kind: "elsewhere" },
    { name: "committed, lock still held (finishing), status names the new run", in: { status: withRun("fresh", T2), run: run("held_here", { ...committedJob, phase: "finishing" }) }, badge: `Indexed${formatShortTime(T2)}, checking freshness...`, slot: "Indexing", kind: "running" },
    { name: "committed, lock free, status not yet naming the run", in: { status: ok(goldenStatus("stale")), run: run("free", committedJob) }, badge: `Indexed${formatShortTime(T2)}, checking freshness...`, slot: "Re-index", kind: "idle" },
    { name: "committed, lock free, status names the run (fresh)", in: { status: withRun("fresh", T2), run: run("free", committedJob) }, badge: `Freshindexed ${formatShortTime(T2)}`, slot: "Re-index", kind: "idle" },
    { name: "committed, lock free, status names a later run (stale)", in: { status: withRun("stale", "2026-11-03T16:00:00Z"), run: run("free", committedJob) }, badge: `Stale: 3 files changedsince ${formatShortTime("2026-11-03T16:00:00Z")}`, slot: "Re-index", kind: "idle" },
    { name: "held_elsewhere after this server's committed run was switched to", in: { status: withRun("fresh", T2), run: run("held_elsewhere", committedJob) }, badge: `Indexed${formatShortTime(T2)}`, slot: "Indexing (CLI)", kind: "elsewhere" },
    { name: "job failed", in: { status: ok(goldenStatus("stale")), run: run("free", { state: "failed", phase: null, error: { code: "index_failed", message: "disk full" } }) }, badge: `Stale: 3 files changedsince ${formatShortTime(T)}`, slot: "Re-index failed · details", kind: "failed" },
    { name: "job failed, lock still held by its cleanup", in: { run: run("held_here", { state: "failed", phase: "finishing", error: { code: "index_failed", message: "x" } }) }, badge: `Indexed${formatShortTime(T)}`, slot: "Re-index failed · details", kind: "failed" },
    { name: "job failed, dismissed", in: { run: run("free", { state: "failed", phase: null }), dismissedJobId: "J1" }, badge: `Freshindexed ${formatShortTime(T)}`, slot: "Re-index", kind: "idle" },
    { name: "job cancelled", in: { run: run("free", { state: "cancelled", phase: null }) }, badge: `Freshindexed ${formatShortTime(T)}`, slot: "Re-index cancelled · details", kind: "cancelled" },
    { name: "job null but this tab started one (server restarted)", in: { run: run("free"), startedJobId: "J0" }, badge: `Freshindexed ${formatShortTime(T)}`, slot: "Outcome unknown · details", kind: "outcome_unknown" },
    { name: "POST pending", in: { pending: true }, badge: `Freshindexed ${formatShortTime(T)}`, slot: "Starting...", kind: "pending" },
    { name: "POST refused (not index_locked)", in: { postError: { code: "reindex_unsupported", message: "external snapshot" } }, badge: `Freshindexed ${formatShortTime(T)}`, slot: "Re-index failed · details", kind: "not_started" },
    { name: "B1: free lock next to a running job (outcome not recorded yet)", in: { run: run("free", { phase: "finishing" }) }, badge: `Indexed${formatShortTime(T)}`, slot: "Checking...", kind: "checking" },
    { name: "B1: free lock next to a running job, stale index", in: { status: ok(goldenStatus("stale")), run: run("free", { phase: "writing" }) }, badge: `Stale: 3 files changedsince ${formatShortTime(T)}`, slot: "Checking...", kind: "checking" },
    { name: "B1: free lock next to a running job outranks a POST refusal", in: { run: run("free", { phase: "writing" }), postError: { code: "x", message: "y" } }, badge: `Indexed${formatShortTime(T)}`, slot: "Checking...", kind: "checking" },
    { name: "B3: held_here outranks a stale POST refusal", in: { run: run("held_here", { phase: "parsing" }), postError: { code: "index_lock_unavailable", message: "probe" } }, badge: `Indexed${formatShortTime(T)}`, slot: "Indexing", kind: "running" },
    { name: "B3: held_elsewhere outranks a stale POST refusal", in: { run: run("held_elsewhere"), postError: { code: "index_lock_unavailable", message: "probe" } }, badge: `Indexed${formatShortTime(T)}`, slot: "Indexing (CLI)", kind: "elsewhere" },
    { name: "held_here with an ended job (the release window)", in: { run: run("held_here", { ...committedJob, ended_at: "2026-11-03T15:00:21Z" }) }, badge: `Indexed${formatShortTime(T2)}, checking freshness...`, slot: "Indexing", kind: "running" },
    { name: "POST 409 index_locked, then the poll shows the holder", in: { run: run("held_elsewhere") }, badge: `Indexed${formatShortTime(T)}`, slot: "Indexing (CLI)", kind: "elsewhere" },
  ];
  for (const r of rows) {
    it(r.name, () => {
      const input = inputs(r.in);
      const model = deriveReindex(input);
      expect(model.slot.kind).toBe(r.kind);
      expect(slotLabel(model.slot)).toBe(r.slot);
      if (r.primary !== undefined && model.slot.kind === "idle") expect(model.slot.primary).toBe(r.primary);
      expect(badgeText(input)).toBe(r.badge);
    });
  }

  it("blocks Retry with the reason unless the lock is proven free", () => {
    expect(deriveReindex(inputs({})).startBlocked).toBeNull();
    expect(deriveReindex(inputs({ run: run("unknown", null, "read-only directory") })).startBlocked).toBe(
      "The server could not check the index lock: read-only directory",
    );
    expect(deriveReindex(inputs({ run: run("held_here", { state: "failed", phase: "finishing" }) })).startBlocked).toBe(
      "This server's re-index is running.",
    );
  });

  it("names the reason of an unavailable slot", () => {
    const m = deriveReindex(inputs({ run: run("unknown", null, "read-only directory") }));
    expect(m.slot).toEqual({ kind: "unavailable", reason: "The server could not check the index lock: read-only directory" });
  });

  it("keeps a status failure visible during a run", () => {
    const view: StatusView = { kind: "failed", error: new Error("down"), at: new Date() };
    expect(badgeText(inputs({ status: view, run: run("held_here") }))).toBe("Status unavailable");
  });
});

describe("Fresh is never shown while the lock is held or before status names the committed run", () => {
  // Property-style: every combination of a run answer and a status answer
  // in an interleaving of a run that starts from a fresh index.
  const runs: [string, RunView][] = [
    ["loading", { kind: "loading" }],
    ["failed", { kind: "failed", error: new Error("x") }],
    ["free", run("free")],
    ["unknown", run("unknown")],
    ["held_here running", run("held_here", { phase: "parsing", progress: { done: 1, total: 2, unit: "files" } })],
    ["held_here committed", run("held_here", { ...committedJob, phase: "finishing" })],
    ["free committed", run("free", committedJob)],
    ["held_elsewhere", run("held_elsewhere")],
    ["held_elsewhere after commit", run("held_elsewhere", committedJob)],
    ["free failed", run("free", { state: "failed", phase: null })],
    ["free running (unsettled)", run("free", { phase: "finishing" })],
  ];
  const statuses: [string, StatusView][] = [
    ["fresh old run", withRun("fresh", T)],
    ["fresh new run", withRun("fresh", T2)],
    ["stale old run", withRun("stale", T)],
  ];
  for (const [rn, r] of runs) {
    for (const [sn, s] of statuses) {
      for (const pending of [false, true]) {
        it(`${rn} / ${sn}${pending ? " / POST pending" : ""}`, () => {
          const text = badgeText(inputs({ run: r, status: s, pending }));
          const lockFree = r.kind === "ok" && r.doc.lock.state === "free" && !unsettled(r.doc);
          const job = r.kind === "ok" ? r.doc.job : null;
          const namesCommitted = job?.state !== "committed" || (s.kind === "ok" && s.status.index.last_run?.finished_at === T2);
          if (!lockFree || !namesCommitted) expect(text).not.toMatch(/Fresh/);
          else if (s.kind === "ok" && s.status.freshness.state === "fresh") expect(text).toMatch(/^Fresh/);
        });
      }
    }
  }
});

describe("slot helpers", () => {
  it("formats counts in full up to 9,999 and with three significant digits above", () => {
    const compact = new Intl.NumberFormat(undefined, { notation: "compact", maximumSignificantDigits: 3 });
    expect(formatCount(9999)).toBe(new Intl.NumberFormat().format(9999));
    for (const n of [10_000, 12_345, 99_999, 100_000, 123_456, 999_999, 1_234_567, 99_999_999]) {
      expect(formatCount(n)).toBe(compact.format(n));
      // At most three digits, one separator and one suffix.
      expect(formatCount(n).replace(/\D/g, "").length).toBeLessThanOrEqual(3);
    }
  });

  it("reserves every label the slot can show", () => {
    const labels = [
      deriveReindex(inputs({})).slot,
      deriveReindex(inputs({ pending: true })).slot,
      deriveReindex(inputs({ run: run("free", { phase: "writing" }) })).slot,
      deriveReindex(inputs({ run: run("held_here", { progress: { done: 9999, total: 9999, unit: "files" } }) })).slot,
      deriveReindex(inputs({ run: run("held_elsewhere") })).slot,
      deriveReindex(inputs({ run: run("free", { state: "failed", phase: null }) })).slot,
      deriveReindex(inputs({ run: run("free", { state: "cancelled", phase: null }) })).slot,
      deriveReindex(inputs({ startedJobId: "J0" })).slot,
    ].map(slotLabel);
    for (const l of labels) expect(SLOT_SIZER_LABELS).toContain(l);
  });

  it("announces phases, not counts", () => {
    const a = (lock: string, job: Parameters<typeof runDoc>[1]) => announcement(deriveReindex(inputs({ run: run(lock, job) })).slot);
    expect(a("held_here", { phase: "parsing", progress: { done: 3, total: 9, unit: "files" } })).toBe("Indexing: Parsing");
    expect(a("held_here", { phase: "warming", progress: null })).toBe("Indexing: warming");
    expect(a("held_elsewhere", null)).toBe("Another process is indexing");
    expect(a("free", null)).toBe("");
  });
});

describe("badge counts and times fit the reserved width", () => {
  it("formats large changed-file counts like the slot", () => {
    expect(filesChanged(1)).toBe("1 file changed");
    expect(filesChanged(9999)).toBe(`${formatCount(9999)} files changed`);
    expect(filesChanged(123_456)).toBe(`${formatCount(123_456)} files changed`);
    expect(filesChanged(123_456)).not.toContain("123456");
  });

  it("reserves a Stale row per count magnitude and every given time", () => {
    const long = "2026-09-30T23:59:59Z";
    const rows = badgeSizerLabels([T, long]).map(([main, small]) => main + " " + small);
    for (const c of COUNT_SIZER_SAMPLES) {
      for (const t of [T, long]) expect(rows).toContain(`Stale: ${c} files changed since ${formatShortTime(t)}`);
    }
    for (const t of [T, long]) {
      expect(rows).toContain(`Indexed ${formatShortTime(t)}, checking freshness...`);
      expect(rows).toContain(`Fresh indexed ${formatShortTime(t)}`);
    }
  });

  it("shows a 100,000+ count abbreviated in the badge", () => {
    const s = goldenStatus("stale");
    s.freshness.changed_count = 123_456;
    expect(badgeText(inputs({ status: ok(s) }))).toBe(`Stale: ${formatCount(123_456)} files changedsince ${formatShortTime(T)}`);
  });
});
