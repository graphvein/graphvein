import { act, fireEvent, render, renderHook, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { IndexRun, IndexRunClient } from "@/api/indexRun";
import type { Status } from "@/api/status";
import type { StatusFetcher, StatusView } from "@/hooks/useStatus";
import { formatShortTime, formatTime } from "@/lib/time";
import { AppShell } from "@/shell/AppShell";
import { SwitchNotice, useRunSwitchNotice } from "@/shell/SwitchNotice";
import { fakeClient, type Request } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { runDoc } from "@/test/indexRunFixtures";

const T = "2026-11-03T14:32:05.123Z";
const T2 = "2026-11-03T15:00:20.000Z";

function statusAt(name: "fresh" | "stale", at: string): Status {
  const s = goldenStatus(name);
  if (s.index.last_run) s.index.last_run.finished_at = at;
  return s;
}

describe("useRunSwitchNotice", () => {
  it("reports a newer run than the previous answer, not the first answer or an older one", () => {
    const ok = (s: Status): StatusView => ({ kind: "ok", status: s });
    const { result, rerender } = renderHook(({ v }: { v: StatusView }) => useRunSwitchNotice(v), {
      initialProps: { v: { kind: "loading" } as StatusView },
    });
    rerender({ v: ok(statusAt("stale", T)) });
    expect(result.current.notice).toBeNull();
    rerender({ v: { kind: "checking" } });
    rerender({ v: ok(statusAt("fresh", T2)) });
    expect(result.current.notice).toEqual({ from: T, to: T2 });
    act(() => {
      result.current.dismiss();
    });
    expect(result.current.notice).toBeNull();
    rerender({ v: ok(statusAt("fresh", T)) });
    expect(result.current.notice).toBeNull();
  });

  it("adds seconds when both runs fall in one minute", () => {
    const later = "2026-11-03T14:32:45.000Z";
    render(<SwitchNotice notice={{ from: T, to: later }} onDismiss={vi.fn()} />);
    expect(screen.getByTestId("switch-notice").textContent).toContain(
      `Now showing the index of ${formatTime(later)} (was ${formatTime(T)})`,
    );
  });
});

describe("AppShell re-index flow", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.spyOn(document, "visibilityState", "get").mockImplementation(() => "visible");
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("never shows Fresh during the run, checks after the commit and switches with a notice", async () => {
    window.location.hash = "#/api";
    const statusCalls: ((s: Status) => void)[] = [];
    const fetcher: StatusFetcher = () =>
      new Promise<Status>((resolve) => {
        statusCalls.push(resolve);
      });
    let next: IndexRun = runDoc("free");
    const runClient: IndexRunClient = {
      fetch: () => Promise.resolve(next),
      start: () => {
        next = runDoc("held_here", { id: "J1", phase: "scanning" });
        return Promise.resolve(next);
      },
    };
    const requests: Request[] = [];
    render(<AppShell fetcher={fetcher} client={fakeClient(requests)} runClient={runClient} />);
    const badge = () => screen.getByTestId("freshness-badge").textContent;
    const slot = () => screen.getByTestId("reindex-slot");
    await act(async () => {
      statusCalls[0]?.(statusAt("fresh", T));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(badge()).toMatch(/^Fresh/);
    const before = requests.length;

    fireEvent.click(slot());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(slot().textContent).toBe("Indexing");
    expect(badge()).toBe(`Indexed${formatShortTime(T)}`);

    next = runDoc("held_here", { id: "J1", phase: "parsing", progress: { done: 10, total: 20, unit: "files" } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(slot().textContent).toBe("Indexing 10 / 20");
    expect(badge()).not.toMatch(/Fresh/);

    // Commit: the lock is still held while the store closes.
    next = runDoc("held_here", { id: "J1", state: "committed", phase: "finishing", run: { id: "R2", finished_at: T2 } });
    const callsBefore = statusCalls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(statusCalls.length).toBe(callsBefore + 1);
    expect(badge()).toBe(`Indexed${formatShortTime(T2)}, checking freshness...`);
    // A status answer naming the new run while the lock is held: no Fresh.
    await act(async () => {
      statusCalls[statusCalls.length - 1]?.(statusAt("fresh", T2));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(badge()).not.toMatch(/Fresh/);

    next = runDoc("free", { id: "J1", state: "committed", phase: null, run: { id: "R2", finished_at: T2 } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    await act(async () => {
      statusCalls[statusCalls.length - 1]?.(statusAt("fresh", T2));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(badge()).toBe(`Freshindexed ${formatShortTime(T2)}`);
    expect(slot().textContent).toBe("Re-index");
    expect(screen.getByTestId("switch-notice").textContent).toContain(`Now showing the index of ${formatShortTime(T2)} (was ${formatShortTime(T)})`);
    // The API section reloads for the new run.
    expect(requests.length).toBeGreaterThan(before);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByTestId("switch-notice")).toBeNull();
  });
});
