import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import type { IndexRun, IndexRunClient } from "@/api/indexRun";
import { MAX_RECHECKS, RUN_POLL_INTERVAL_MS, useIndexRun } from "@/hooks/useIndexRun";
import { POLL_INTERVAL_MS, type StatusView } from "@/hooks/useStatus";
import { goldenStatus } from "@/test/fixtures";
import { runDoc } from "@/test/indexRunFixtures";

interface Pending<T> {
  resolve: (v: T) => void;
  reject: (e: Error) => void;
}

/** controlledClient lets the test answer every GET and POST by hand. */
function controlledClient() {
  const gets: Pending<IndexRun>[] = [];
  const posts: Pending<IndexRun>[] = [];
  const client: IndexRunClient = {
    fetch: () =>
      new Promise<IndexRun>((resolve, reject) => {
        gets.push({ resolve, reject });
      }),
    start: () =>
      new Promise<IndexRun>((resolve, reject) => {
        posts.push({ resolve, reject });
      }),
  };
  return { gets, posts, client };
}

let visibility: DocumentVisibilityState = "visible";

async function settle<T>(p: Pending<T> | undefined, v: T | Error) {
  if (p === undefined) throw new Error("no such request");
  await act(async () => {
    if (v instanceof Error) p.reject(v);
    else p.resolve(v);
    await Promise.resolve();
  });
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  visibility = "visible";
  vi.spyOn(document, "visibilityState", "get").mockImplementation(() => visibility);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const fresh: StatusView = { kind: "ok", status: goldenStatus("fresh") };
const T2 = "2026-11-03T15:00:20.000Z";

function setup(status: StatusView = fresh) {
  const c = controlledClient();
  const check = vi.fn();
  const hook = renderHook(({ s }: { s: StatusView }) => useIndexRun(s, check, c.client), { initialProps: { s: status } });
  return { ...c, check, hook };
}

describe("useIndexRun polling", () => {
  it("requests on mount and every 30 s while the lock is free", async () => {
    const { gets, hook } = setup();
    expect(gets).toHaveLength(1);
    expect(hook.result.current.run.kind).toBe("loading");
    await settle(gets[0], runDoc("free"));
    expect(hook.result.current.run.kind).toBe("ok");
    await advance(POLL_INTERVAL_MS - 1);
    expect(gets).toHaveLength(1);
    await advance(1);
    expect(gets).toHaveLength(2);
  });

  it("polls every second while the lock is held, then back to 30 s", async () => {
    const { gets } = setup();
    await settle(gets[0], runDoc("held_elsewhere"));
    await advance(RUN_POLL_INTERVAL_MS);
    expect(gets).toHaveLength(2);
    await settle(gets[1], runDoc("held_here"));
    await advance(RUN_POLL_INTERVAL_MS);
    expect(gets).toHaveLength(3);
    await settle(gets[2], runDoc("free"));
    await advance(RUN_POLL_INTERVAL_MS * 5);
    expect(gets).toHaveLength(3);
    await advance(POLL_INTERVAL_MS);
    expect(gets).toHaveLength(4);
  });

  it("polls every second while a free lock sits next to a job that has not ended", async () => {
    const { gets } = setup();
    await settle(gets[0], runDoc("free", { phase: "finishing" }));
    await advance(RUN_POLL_INTERVAL_MS);
    expect(gets).toHaveLength(2);
    await settle(gets[1], runDoc("free", { state: "failed", phase: null, ended_at: "2026-11-03T15:00:05Z", error: { code: "index_failed", message: "x" } }));
    await advance(RUN_POLL_INTERVAL_MS * 5);
    expect(gets).toHaveLength(2);
  });

  it("skips ticks while hidden and requests at once on return", async () => {
    const { gets } = setup();
    await settle(gets[0], runDoc("held_here"));
    visibility = "hidden";
    await advance(RUN_POLL_INTERVAL_MS * 3);
    expect(gets).toHaveLength(1);
    visibility = "visible";
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(gets).toHaveLength(2);
  });

  it("never sends two requests at once", async () => {
    const { gets } = setup();
    await advance(POLL_INTERVAL_MS * 3);
    expect(gets).toHaveLength(1);
  });

  it("reports a failed request as failed, not as a free lock", async () => {
    const { gets, hook } = setup();
    await settle(gets[0], new ApiError("network", "down"));
    expect(hook.result.current.run).toMatchObject({ kind: "failed" });
  });
});

describe("useIndexRun run switch", () => {
  it("checks the status once when a job reaches committed", async () => {
    // A status that already names the new run, so only the transitions
    // check (the recheck rule has its own test).
    const named = goldenStatus("fresh");
    if (named.index.last_run) named.index.last_run.finished_at = T2;
    const { gets, check } = setup({ kind: "ok", status: named });
    await settle(gets[0], runDoc("held_here", { phase: "writing" }));
    expect(check).not.toHaveBeenCalled();
    await advance(RUN_POLL_INTERVAL_MS);
    await settle(gets[1], runDoc("held_here", { state: "committed", phase: "finishing", run: { id: "R2", finished_at: T2 } }));
    expect(check).toHaveBeenCalledTimes(1);
    await advance(RUN_POLL_INTERVAL_MS);
    // Still committed: no second check for the same job, but the lock
    // going free is a transition of its own.
    await settle(gets[2], runDoc("held_here", { state: "committed", phase: "finishing", run: { id: "R2", finished_at: T2 } }));
    expect(check).toHaveBeenCalledTimes(1);
    await advance(RUN_POLL_INTERVAL_MS);
    await settle(gets[3], runDoc("free", { state: "committed", phase: null, run: { id: "R2", finished_at: T2 } }));
    expect(check).toHaveBeenCalledTimes(2);
  });

  it("checks the status when another process releases the lock", async () => {
    const { gets, check } = setup();
    await settle(gets[0], runDoc("held_elsewhere"));
    await advance(RUN_POLL_INTERVAL_MS);
    await settle(gets[1], runDoc("free"));
    expect(check).toHaveBeenCalledTimes(1);
  });

  it("does not check on the first answer", async () => {
    const { gets, check } = setup();
    await settle(gets[0], runDoc("free", { state: "committed", phase: null, run: { id: "R2", finished_at: T2 } }));
    // The first answer is not a transition; the status request at mount
    // answers for it. A status answer older than the run does recheck.
    expect(check).toHaveBeenCalledTimes(1);
  });

  it("rechecks while the status answer is older than the committed run, at most MAX_RECHECKS times", async () => {
    const { gets, check, hook } = setup();
    await settle(gets[0], runDoc("free", { state: "committed", phase: null, run: { id: "R2", finished_at: T2 } }));
    for (let i = 0; i < MAX_RECHECKS + 3; i++) {
      hook.rerender({ s: { kind: "ok", status: goldenStatus("fresh") } });
    }
    expect(check).toHaveBeenCalledTimes(MAX_RECHECKS);
    const named = goldenStatus("fresh");
    if (named.index.last_run) named.index.last_run.finished_at = T2;
    hook.rerender({ s: { kind: "ok", status: named } });
    expect(check).toHaveBeenCalledTimes(MAX_RECHECKS);
  });
});

describe("useIndexRun start", () => {
  it("is pending until the POST answers, records the job and polls at once", async () => {
    const { gets, posts, hook } = setup();
    await settle(gets[0], runDoc("free"));
    act(() => {
      hook.result.current.start();
      hook.result.current.start();
    });
    expect(posts).toHaveLength(1);
    expect(hook.result.current.pending).toBe(true);
    await settle(posts[0], runDoc("held_here", { id: "J9", phase: "starting" }));
    expect(hook.result.current.pending).toBe(false);
    expect(hook.result.current.startedJobId).toBe("J9");
    expect(gets).toHaveLength(2);
  });

  it("does not show index_locked as an error: the next poll shows the holder", async () => {
    const { gets, posts, hook } = setup();
    await settle(gets[0], runDoc("free"));
    act(() => {
      hook.result.current.start();
    });
    await settle(posts[0], new ApiError("http", "index_locked: held by another process", 409, "index_locked"));
    expect(hook.result.current.postError).toBeNull();
    expect(gets).toHaveLength(2);
    await settle(gets[1], runDoc("held_elsewhere"));
    expect(hook.result.current.run).toMatchObject({ kind: "ok", doc: { lock: { state: "held_elsewhere" } } });
  });

  it("keeps another refusal until dismissed or the next POST", async () => {
    const { gets, posts, hook } = setup();
    await settle(gets[0], runDoc("free"));
    act(() => {
      hook.result.current.start();
    });
    await settle(posts[0], new ApiError("http", "reindex_unsupported: external snapshot", 409, "reindex_unsupported"));
    expect(hook.result.current.postError).toEqual({ code: "reindex_unsupported", message: "external snapshot" });
    act(() => {
      hook.result.current.dismiss();
    });
    expect(hook.result.current.postError).toBeNull();
  });

  it("clears a refusal once a run answer shows a held lock or another job", async () => {
    const { gets, posts, hook } = setup();
    await settle(gets[0], runDoc("free"));
    act(() => {
      hook.result.current.start();
    });
    await settle(posts[0], new ApiError("http", "index_lock_unavailable: probe failed", 503, "index_lock_unavailable"));
    expect(hook.result.current.postError).not.toBeNull();
    // The poll right after the refusal: same state, the refusal stays.
    await settle(gets[1], runDoc("free"));
    expect(hook.result.current.postError).not.toBeNull();
    await advance(POLL_INTERVAL_MS);
    await settle(gets[2], runDoc("held_elsewhere"));
    expect(hook.result.current.postError).toBeNull();

    act(() => {
      hook.result.current.start();
    });
    await settle(posts[1], new ApiError("http", "index_lock_unavailable: probe failed", 503, "index_lock_unavailable"));
    await settle(gets[3], runDoc("free"));
    expect(hook.result.current.postError).not.toBeNull();
    await advance(POLL_INTERVAL_MS);
    await settle(gets[4], runDoc("free", { id: "J7", state: "committed", phase: null, ended_at: "2026-11-03T15:00:05Z", run: { id: "R7", finished_at: T2 } }));
    expect(hook.result.current.postError).toBeNull();
  });

  it("dismisses a failed job, and a new start dismisses it too", async () => {
    const { gets, hook } = setup();
    await settle(gets[0], runDoc("free", { id: "J3", state: "failed", phase: null }));
    act(() => {
      hook.result.current.dismiss();
    });
    expect(hook.result.current.dismissedJobId).toBe("J3");
  });
});
