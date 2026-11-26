import { act, render, renderHook, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import type { Status } from "@/api/status";
import { POLL_INTERVAL_MS, useStatus, type StatusFetcher } from "@/hooks/useStatus";
import { AppShell } from "@/shell/AppShell";
import { fakeClient } from "@/test/apiFixtures";
import { goldenStatus } from "@/test/fixtures";
import { freeRunClient } from "@/test/indexRunFixtures";

interface Call {
  signal: AbortSignal;
  resolve: (s: Status) => void;
  reject: (e: Error) => void;
}

/** controlledFetcher records each request and lets the test answer it. */
function controlledFetcher() {
  const calls: Call[] = [];
  const fetcher: StatusFetcher = ({ signal }) =>
    new Promise<Status>((resolve, reject) => {
      calls.push({ signal, resolve, reject });
    });
  return { calls, fetcher };
}

let visibility: DocumentVisibilityState = "visible";

function setVisibility(value: DocumentVisibilityState) {
  visibility = value;
  document.dispatchEvent(new Event("visibilitychange"));
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

async function answer(call: Call | undefined, result: Status | Error) {
  if (call === undefined) throw new Error("no such request");
  await act(async () => {
    if (result instanceof Error) call.reject(result);
    else call.resolve(result);
    await Promise.resolve();
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

describe("useStatus", () => {
  it("requests on mount and every 30 s, never two at once", async () => {
    const { calls, fetcher } = controlledFetcher();
    const { result } = renderHook(() => useStatus(fetcher));
    expect(calls).toHaveLength(1);
    expect(result.current.view.kind).toBe("loading");
    expect(result.current.checking).toBe(true);

    // The first request is still open: the timer tick is skipped.
    await advance(POLL_INTERVAL_MS);
    expect(calls).toHaveLength(1);
    await answer(calls[0], goldenStatus("fresh"));
    expect(result.current.view.kind).toBe("ok");
    expect(result.current.checking).toBe(false);

    await advance(POLL_INTERVAL_MS);
    expect(calls).toHaveLength(2);
    // Focus and visibility events during the open request do not add one.
    act(() => {
      window.dispatchEvent(new Event("focus"));
      setVisibility("visible");
    });
    expect(calls).toHaveLength(2);
  });

  it("lets Check wait for an open request and then send its own", async () => {
    const { calls, fetcher } = controlledFetcher();
    const { result } = renderHook(() => useStatus(fetcher));
    act(() => {
      result.current.check();
      result.current.check();
    });
    expect(calls).toHaveLength(1);
    await answer(calls[0], goldenStatus("fresh"));
    expect(calls).toHaveLength(2);
    await answer(calls[1], goldenStatus("stale"));
    expect(calls).toHaveLength(2);
    expect(result.current.view.kind === "ok" && result.current.view.status.freshness.state).toBe("stale");
  });

  it("pauses while hidden and refetches when visible again", async () => {
    const { calls, fetcher } = controlledFetcher();
    renderHook(() => useStatus(fetcher));
    await answer(calls[0], goldenStatus("fresh"));
    act(() => {
      setVisibility("hidden");
    });
    await advance(POLL_INTERVAL_MS * 4);
    expect(calls).toHaveLength(1);
    act(() => {
      setVisibility("visible");
    });
    expect(calls).toHaveLength(2);
    await answer(calls[1], goldenStatus("fresh"));
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(calls).toHaveLength(3);
  });

  it("withdraws an answer older than the poll interval when the page comes back", async () => {
    const { calls, fetcher } = controlledFetcher();
    const { result } = renderHook(() => useStatus(fetcher));
    await answer(calls[0], goldenStatus("fresh"));
    expect(result.current.view.kind).toBe("ok");

    // A short absence keeps the answer while the new request runs.
    act(() => {
      setVisibility("hidden");
    });
    await advance(POLL_INTERVAL_MS / 2);
    act(() => {
      setVisibility("visible");
    });
    expect(calls).toHaveLength(2);
    expect(result.current.view.kind).toBe("ok");
    await answer(calls[1], goldenStatus("fresh"));

    // A long one withdraws it until the new answer arrives.
    act(() => {
      setVisibility("hidden");
    });
    await advance(POLL_INTERVAL_MS * 3);
    act(() => {
      setVisibility("visible");
    });
    expect(calls).toHaveLength(3);
    expect(result.current.view.kind).toBe("checking");
    expect(result.current.repository).toBe("fixture");
    // The focus event that follows does not bring the old answer back or
    // send a second request.
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(calls).toHaveLength(3);
    expect(result.current.view.kind).toBe("checking");
    await answer(calls[2], goldenStatus("stale"));
    expect(result.current.view.kind === "ok" && result.current.view.status.freshness.state).toBe("stale");
  });

  it("withdraws an old answer on focus even while a request is open", async () => {
    const { calls, fetcher } = controlledFetcher();
    const { result } = renderHook(() => useStatus(fetcher));
    await answer(calls[0], goldenStatus("fresh"));
    // The timer request is still open when the window gets focus later.
    await advance(POLL_INTERVAL_MS);
    expect(calls).toHaveLength(2);
    await advance(POLL_INTERVAL_MS / 2);
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(calls).toHaveLength(2);
    expect(result.current.view.kind).toBe("checking");
    // The open request started before the withdrawal: its answer may
    // describe the old state, so it is ignored and a new request follows.
    await answer(calls[1], goldenStatus("fresh"));
    expect(result.current.view.kind).toBe("checking");
    expect(result.current.checking).toBe(true);
    expect(calls).toHaveLength(3);
    await answer(calls[2], goldenStatus("stale"));
    expect(result.current.view.kind === "ok" && result.current.view.status.freshness.state).toBe("stale");
    expect(calls).toHaveLength(3);
  });

  it("ignores a failure answering a request started before the withdrawal", async () => {
    const { calls, fetcher } = controlledFetcher();
    const { result } = renderHook(() => useStatus(fetcher));
    await answer(calls[0], goldenStatus("fresh"));
    await advance(POLL_INTERVAL_MS * 1.5);
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await answer(calls[1], new ApiError("network", "Failed to fetch"));
    expect(result.current.view.kind).toBe("checking");
    expect(calls).toHaveLength(3);
    await answer(calls[2], goldenStatus("fresh"));
    expect(result.current.view.kind).toBe("ok");
  });

  it("replaces a fresh result with the failure and keeps the repository", async () => {
    const { calls, fetcher } = controlledFetcher();
    const { result } = renderHook(() => useStatus(fetcher));
    await answer(calls[0], goldenStatus("fresh"));
    await advance(POLL_INTERVAL_MS);
    await answer(calls[1], new ApiError("network", "Failed to fetch"));
    expect(result.current.view.kind).toBe("failed");
    expect(result.current.repository).toBe("fixture");
  });

  it("aborts the open request on unmount", () => {
    const { calls, fetcher } = controlledFetcher();
    const { unmount } = renderHook(() => useStatus(fetcher));
    unmount();
    expect(calls[0]?.signal.aborted).toBe(true);
  });
});

describe("AppShell with a failing refresh", () => {
  it("shows Status unavailable, never the old Fresh", async () => {
    window.location.hash = "#/api";
    const { calls, fetcher } = controlledFetcher();
    render(<AppShell fetcher={fetcher} client={fakeClient()} runClient={freeRunClient} />);
    await answer(calls[0], goldenStatus("fresh"));
    expect(screen.getByTestId("freshness-badge").textContent).toMatch(/^Fresh/);
    expect(screen.queryByRole("alert")).toBeNull();

    await advance(POLL_INTERVAL_MS);
    await answer(calls[1], new ApiError("network", "Failed to fetch"));
    const badge = screen.getByTestId("freshness-badge");
    expect(badge.textContent).toBe("Status unavailable");
    expect(badge.textContent).not.toMatch(/\bFresh\b/);
    expect(screen.getByRole("alert").textContent).toContain(
      `Cannot reach the Graphvein server at ${window.location.origin}. Is graphvein serve still running?`,
    );

    // Retry sends a request; a fresh answer clears the banner.
    act(() => {
      screen.getByRole("button", { name: "Retry" }).click();
    });
    expect(calls).toHaveLength(3);
    await answer(calls[2], goldenStatus("fresh"));
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByTestId("freshness-badge").textContent).toMatch(/^Fresh/);
  });
});
