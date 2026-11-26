// useSchemaSets loads GET /api/v0/schema/sets: once per index run (the key
// is the summary's run plus a reload counter), never per view change. The
// response is about 1 s and 18 MB on a 2,000-table store (ADR 0048 known
// gaps), so tabs, selection and filters only read the loaded answer.
//
// The shell owns the request (useShellSchemaSets), as it owns the summary:
// leaving the Schema section and coming back (#/schema, #/api, #/schema)
// reads the answer already loaded for this run instead of asking again.

import { useCallback, useState } from "react";

import type { SchemaClient, SchemaSets, SchemaSummary } from "@/api/schema";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";
import { runOf } from "@/hooks/useSchemaSummary";
import type { StatusView } from "@/hooks/useStatus";
import { needsSets } from "@/sections/schema/state";

/** useSchemaSets requests /sets for run (null: nothing to request). */
export function useSchemaSets(client: SchemaClient, run: string | null, reload: number): RequestResult<SchemaSets> {
  const key = run === null ? null : JSON.stringify(["schema-sets", run, reload]);
  return useApiRequest(key, (signal) => client.sets({ signal }));
}

/** The shell's /sets request, and reload to ask again for the same run. */
export interface ShellSchemaSets {
  request: RequestResult<SchemaSets>;
  reload: () => void;
}

/** useShellSchemaSets requests /sets once per index run, from the first
 * time the Schema section is shown (active) and only when the summary
 * needs it (state.ts needsSets); later section changes keep the answer. */
export function useShellSchemaSets(client: SchemaClient, status: StatusView, summary: RequestResult<SchemaSummary>, active: boolean): ShellSchemaSets {
  const [seen, setSeen] = useState(false);
  const [reload, setReload] = useState(0);
  // State adjusted during render: the first visit turns the request on.
  if (active && !seen) setSeen(true);
  const statusIndexed = status.kind === "ok" ? status.status.index.indexed : null;
  const sum = summary.state.kind === "ok" ? summary.state.data : null;
  const run = sum !== null && (seen || active) && needsSets(statusIndexed, sum) ? runOf(sum.index) : null;
  const request = useSchemaSets(client, run, reload);
  const bump = useCallback(() => {
    setReload((r) => r + 1);
  }, []);
  return { request, reload: bump };
}
