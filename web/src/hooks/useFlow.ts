// useFlow loads the Flow tab's neighbourhood of one endpoint (ADR 0045).

import type { Scope } from "@/api/endpoints";
import type { FlowClient, FlowResponse } from "@/api/flow";
import { useApiRequest, type RequestResult } from "@/hooks/useApiRequest";
import { useEditor, withEditor } from "@/hooks/useEditor";

/** useFlow requests the flow of endpoint id, or nothing when id is null
 * (no selection, or another tab is open). reload is the section's reload
 * token, so an index change requests it again with everything else. */
export function useFlow(client: FlowClient, id: string | null, scope: Scope, root: string | null, reload: number): RequestResult<FlowResponse> {
  const { requestEditor } = useEditor();
  const key = id === null ? null : JSON.stringify(["flow", id, scope, root, reload, requestEditor ?? null]);
  return useApiRequest(
    key,
    (signal) => {
      if (id === null) return Promise.reject(new Error("no endpoint selected"));
      return client.flow({ id, scope, root, ...withEditor(requestEditor) }, { signal });
    },
    requestEditor === undefined,
  );
}
