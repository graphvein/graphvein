// Run documents and clients for the re-index tests (T016j).

import { parseIndexRun, type IndexRun, type IndexRunClient, type Job } from "@/api/indexRun";

/** runDoc builds a GET /api/v0/index/run document. */
export function runDoc(lock: string, job: Partial<Job> | null = null, error: string | null = null): IndexRun {
  return parseIndexRun({
    api_version: 0,
    repository: "graphvein",
    lock: { state: lock, error },
    job:
      job === null
        ? null
        : {
            id: "J1",
            state: "running",
            started_at: "2026-11-03T15:00:00Z",
            ended_at: null,
            phase: "parsing",
            progress: null,
            replaces: { finished_at: "2026-11-03T14:32:05.123Z" },
            run: null,
            error: null,
            ...job,
            ...(job.state !== undefined ? { state: job.state } : {}),
          },
  });
}

/** freeRunClient always reports a free lock and no job. */
export const freeRunClient: IndexRunClient = {
  fetch: () => Promise.resolve(runDoc("free")),
  start: () => Promise.reject(new Error("not expected in this test")),
};
