// useSchemaSummary loads GET /api/v0/schema/summary for the shell's Schema
// entry and the schema section (one request, shared): at load, and again
// only when the polled status names another last run than the loaded
// summary (T018b1 section 2a), never on every status poll.

import { useState } from "react";

import type { SchemaClient, SchemaSummary } from "@/api/schema";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";
import type { StatusView } from "@/hooks/useStatus";

/** runOf names an index run ("none" without one). */
export function runOf(index: { finished_at: string } | null): string {
  return index?.finished_at ?? "none";
}

/** useSchemaSummary requests the summary and re-requests it once per
 * run the status reports that differs from the loaded one. */
export function useSchemaSummary(client: SchemaClient, status: StatusView): RequestResult<SchemaSummary> {
  const [gen, setGen] = useState(0);
  const [reloadedFor, setReloadedFor] = useState<string | null>(null);
  const result = useApiRequest(`schema-summary#${String(gen)}`, (signal) => client.summary({ signal }));
  const statusRun = status.kind === "ok" ? runOf(status.status.index.last_run) : null;
  const dataRun = result.state.kind === "ok" ? runOf(result.state.data.index) : null;
  // State adjusted during render (as ApiSection does): the mismatched
  // answer is replaced by a loading state before it is committed.
  if (statusRun !== null && dataRun !== null && statusRun !== dataRun && reloadedFor !== statusRun) {
    setReloadedFor(statusRun);
    setGen((g) => g + 1);
  }
  return result;
}
