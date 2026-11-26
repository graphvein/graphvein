// useSource loads the code peek source of one node (ADR 0044).

import type { SourceClient, SourceResponse } from "@/api/source";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";
import { useEditor, withEditor } from "@/hooks/useEditor";

/** useSource requests node id with context lines, or nothing when id is
 * null. reload is a token that forces a new request (index changed). */
export function useSource(client: SourceClient, id: string | null, context: number, reload: number): RequestResult<SourceResponse> {
  const { requestEditor } = useEditor();
  const key = id === null ? null : JSON.stringify(["source", id, context, reload, requestEditor ?? null]);
  return useApiRequest(
    key,
    (signal) => {
      if (id === null) return Promise.reject(new Error("no node selected"));
      return client.source({ id, context, ...withEditor(requestEditor) }, { signal });
    },
    requestEditor === undefined,
  );
}
