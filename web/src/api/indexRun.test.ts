import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/client";
import { httpIndexRunClient, INDEX_RUN_PATH, IndexRunFormatError, parseIndexRun } from "@/api/indexRun";

const doc = {
  api_version: 0,
  repository: "kftray",
  lock: { state: "held_here", error: null },
  job: {
    id: "Q7K2",
    state: "running",
    started_at: "2026-11-07T07:21:03.112Z",
    ended_at: null,
    phase: "parsing",
    progress: { done: 212, total: 457, unit: "files" },
    replaces: { finished_at: "2026-11-07T07:06:05.414Z" },
    run: null,
    error: null,
  },
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("parseIndexRun", () => {
  it("reads the documented example", () => {
    const r = parseIndexRun(structuredClone(doc));
    expect(r.lock).toEqual({ state: "held_here", reported_state: "held_here", error: null });
    expect(r.job?.progress).toEqual({ done: 212, total: 457, unit: "files" });
    expect(r.job?.replaces).toEqual({ finished_at: "2026-11-07T07:06:05.414Z" });
  });

  it("maps an unknown lock state to unknown, never free", () => {
    const r = parseIndexRun({ ...structuredClone(doc), lock: { state: "paused", error: null } });
    expect(r.lock.state).toBe("unknown");
    expect(r.lock.reported_state).toBe("paused");
  });

  it("maps an unknown job state to unknown and keeps an unknown phase", () => {
    const d = structuredClone(doc) as Record<string, unknown> & { job: Record<string, unknown> };
    d.job.state = "paused";
    d.job.phase = "warming";
    d.job.progress = null;
    const r = parseIndexRun(d);
    expect(r.job?.state).toBe("unknown");
    expect(r.job?.phase).toBe("warming");
  });

  it("reads a committed job and a failed job", () => {
    const c = parseIndexRun({ ...structuredClone(doc), job: { ...doc.job, state: "committed", phase: "finishing", progress: null, run: { id: "R2", finished_at: "2026-11-07T07:21:30Z" } } });
    expect(c.job?.run).toEqual({ id: "R2", finished_at: "2026-11-07T07:21:30Z" });
    const f = parseIndexRun({ ...structuredClone(doc), job: { ...doc.job, state: "failed", phase: null, progress: null, error: { code: "index_failed", message: "boom" } } });
    expect(f.job?.error).toEqual({ code: "index_failed", message: "boom" });
  });

  it("rejects malformed documents", () => {
    expect(() => parseIndexRun({ ...doc, api_version: 1 })).toThrow(IndexRunFormatError);
    expect(() => parseIndexRun({ ...doc, lock: {} })).toThrow(IndexRunFormatError);
    expect(() => parseIndexRun({ api_version: 0, repository: "x", lock: { state: "free", error: null } })).toThrow(/job is missing/);
    expect(() => parseIndexRun({ ...doc, job: { ...doc.job, progress: { done: 5, total: 4, unit: "files" } } })).toThrow(/exceeds/);
    expect(() => parseIndexRun({ ...doc, job: { ...doc.job, progress: { done: -1, total: 4, unit: "files" } } })).toThrow(/count/);
  });
});

describe("httpIndexRunClient", () => {
  it("POSTs {} as JSON and reads the 202 document", async () => {
    const fetchMock = vi.fn<(path: string, init?: RequestInit) => Promise<Response>>(() => Promise.resolve(new Response(JSON.stringify(doc), { status: 202 })));
    vi.stubGlobal("fetch", fetchMock);
    const r = await httpIndexRunClient.start();
    expect(r.job?.id).toBe("Q7K2");
    const [path, init] = fetchMock.mock.calls[0] ?? [];
    expect(path).toBe(INDEX_RUN_PATH);
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe("{}");
    expect(new Headers(init?.headers).get("Content-Type")).toBe("application/json");
    vi.unstubAllGlobals();
  });

  it("rejects a refusal with the server's code", async () => {
    vi.stubGlobal("fetch", () =>
      Promise.resolve(new Response(JSON.stringify({ error: { code: "index_locked", message: "held" } }), { status: 409 })),
    );
    await expect(httpIndexRunClient.start()).rejects.toMatchObject({ code: "index_locked", status: 409 });
    await expect(httpIndexRunClient.start()).rejects.toBeInstanceOf(ApiError);
    vi.unstubAllGlobals();
  });

  it("GETs without a body", async () => {
    const fetchMock = vi.fn<(path: string, init?: RequestInit) => Promise<Response>>(() => Promise.resolve(new Response(JSON.stringify(doc), { status: 200 })));
    vi.stubGlobal("fetch", fetchMock);
    await httpIndexRunClient.fetch({ signal: new AbortController().signal });
    const init = fetchMock.mock.calls[0]?.[1];
    expect(init?.method).toBe("GET");
    expect(init?.body).toBeUndefined();
    vi.unstubAllGlobals();
  });
});
