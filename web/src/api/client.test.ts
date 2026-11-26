import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, fetchStatus, STATUS_PATH } from "@/api/client";
import { GOLDENS } from "@/test/fixtures";

function respond(body: string, status = 200) {
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(body, { status }))));
}

async function failure(promise: Promise<unknown>): Promise<ApiError> {
  try {
    await promise;
  } catch (err) {
    if (err instanceof ApiError) return err;
    throw err;
  }
  throw new Error("request did not fail");
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("fetchStatus", () => {
  it("requests the status path and parses the body", async () => {
    respond(JSON.stringify(GOLDENS.stale));
    const status = await fetchStatus();
    expect(status.freshness.state).toBe("stale");
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe(STATUS_PATH);
    expect(vi.mocked(fetch).mock.calls[0]?.[1]?.cache).toBe("no-store");
  });

  it("refuses another API version with a clear message", async () => {
    respond(JSON.stringify({ ...GOLDENS.fresh, api_version: 1 }));
    const err = await failure(fetchStatus());
    expect(err.kind).toBe("format");
    expect(err.message).toBe("the server speaks API version 1; this UI reads version 0");
  });

  it("decodes the error envelope", async () => {
    respond(JSON.stringify({ error: { code: "store_error", message: "reading runs: boom" } }), 500);
    const err = await failure(fetchStatus());
    expect(err.kind).toBe("http");
    expect(err.status).toBe(500);
    expect(err.code).toBe("store_error");
    expect(err.message).toBe("store_error: reading runs: boom");
  });

  it("reports a non-JSON error answer by status", async () => {
    respond("bad gateway", 502);
    const err = await failure(fetchStatus());
    expect(err.kind).toBe("http");
    expect(err.status).toBe(502);
    expect(err.code).toBeUndefined();
  });

  it("reports a body that is not JSON or not a status as a format error", async () => {
    respond("<html>");
    expect((await failure(fetchStatus())).kind).toBe("format");
    respond(JSON.stringify({ api_version: 0 }));
    expect((await failure(fetchStatus())).kind).toBe("format");
  });

  it("reports a network failure", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.reject(new TypeError("Failed to fetch"))));
    const err = await failure(fetchStatus());
    expect(err.kind).toBe("network");
    expect(err.message).toBe("Failed to fetch");
  });

  it("times out after 15 s", async () => {
    vi.useFakeTimers();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        (_url: string, init: RequestInit) =>
          new Promise((_resolve, reject) => {
            init.signal?.addEventListener("abort", () => {
              reject(new DOMException("aborted", "AbortError"));
            });
          }),
      ),
    );
    const pending = failure(fetchStatus());
    await vi.advanceTimersByTimeAsync(15_000);
    expect((await pending).kind).toBe("timeout");
  });

  it("passes the caller's abort through unchanged", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        (_url: string, init: RequestInit) =>
          new Promise((_resolve, reject) => {
            init.signal?.addEventListener("abort", () => {
              reject(new DOMException("aborted", "AbortError"));
            });
          }),
      ),
    );
    const controller = new AbortController();
    const pending = fetchStatus({ signal: controller.signal });
    const reason = new Error("unmounted");
    controller.abort(reason);
    await expect(pending).rejects.toBe(reason);
  });
});
